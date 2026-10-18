// Package toggl is a minimal client for the Toggl Track API v9.
package toggl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL is the production API root.
const DefaultBaseURL = "https://api.track.toggl.com/api/v9"

// MaxEntriesPerRequest is the most entries /me/time_entries returns in one
// response. A response of exactly this size may have been truncated.
const MaxEntriesPerRequest = 1000

const (
	maxAttempts  = 4
	maxBodyBytes = 64 << 20 // refuse to decode absurdly large responses
)

// Entry is one time entry as returned by /me/time_entries?meta=true.
type Entry struct {
	ID          int64      `json:"id"`
	WorkspaceID int64      `json:"workspace_id"`
	ProjectID   *int64     `json:"project_id"`
	ProjectName string     `json:"project_name,omitempty"` // only present with meta=true
	Description string     `json:"description"`
	Billable    bool       `json:"billable"`
	Start       time.Time  `json:"start"`
	Stop        *time.Time `json:"stop"`
	// Duration is in seconds and negative while the timer is running.
	Duration int64 `json:"duration"`
}

// Running reports whether the entry's timer has not been stopped.
func (e Entry) Running() bool { return e.Duration < 0 }

// Elapsed is the entry's length; a running timer is measured up to now and
// never reported as negative.
func (e Entry) Elapsed(now time.Time) time.Duration {
	if e.Running() {
		return max(now.Sub(e.Start), 0)
	}
	return time.Duration(e.Duration) * time.Second
}

// End is when the entry stopped, or now for a running timer.
func (e Entry) End(now time.Time) time.Time { return e.Start.Add(e.Elapsed(now)) }

// ProjectNames collects project id -> name from entries fetched with meta.
func ProjectNames(entries []Entry) map[int64]string {
	names := map[int64]string{}
	for _, e := range entries {
		if e.ProjectID != nil && e.ProjectName != "" {
			names[*e.ProjectID] = e.ProjectName
		}
	}
	return names
}

// APIError is a non-2xx response from the API.
type APIError struct {
	Path       string
	StatusCode int
	Message    string // response body, trimmed
}

func (e *APIError) Error() string {
	msg := fmt.Sprintf("toggl: GET %s: %d %s", e.Path, e.StatusCode, http.StatusText(e.StatusCode))
	if e.Message != "" {
		msg += ": " + e.Message
	}
	switch e.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		msg += " (check TOGGL_API_TOKEN)"
	case http.StatusPaymentRequired:
		msg += " (the API request quota for this period is used up; try again later)"
	}
	return msg
}

func (e *APIError) retryable() bool {
	return e.StatusCode == http.StatusTooManyRequests || e.StatusCode >= 500
}

// Client calls the Toggl Track API. Create one with New.
type Client struct {
	BaseURL   string
	Token     string
	UserAgent string
	HTTP      *http.Client

	backoff   time.Duration // base delay before the first retry
	pageLimit int           // see MaxEntriesPerRequest; lowered in tests
}

// New returns a client for the production API.
func New(token string) *Client {
	return &Client{
		BaseURL:   DefaultBaseURL,
		Token:     token,
		UserAgent: "tracklens",
		HTTP:      &http.Client{Timeout: 30 * time.Second},
		backoff:   time.Second,
		pageLimit: MaxEntriesPerRequest,
	}
}

// Entries returns the entries that started in [start, end), with project
// names filled in. A response at the API's size cap is split in half and
// fetched again until every part is complete, so large windows are not
// silently truncated.
func (c *Client) Entries(ctx context.Context, start, end time.Time) ([]Entry, error) {
	var got []Entry
	q := url.Values{
		"start_date": {start.UTC().Format(time.RFC3339)},
		"end_date":   {end.UTC().Format(time.RFC3339)},
		"meta":       {"true"},
	}
	if err := c.get(ctx, "/me/time_entries", q, &got); err != nil {
		return nil, err
	}
	if len(got) < c.pageLimit {
		return got, nil
	}

	mid := start.Add(end.Sub(start) / 2).Truncate(time.Second)
	if !mid.After(start) {
		return nil, fmt.Errorf("toggl: more than %d entries start at %s; cannot split further", c.pageLimit, start.Format(time.RFC3339))
	}
	first, err := c.Entries(ctx, start, mid)
	if err != nil {
		return nil, err
	}
	second, err := c.Entries(ctx, mid, end)
	if err != nil {
		return nil, err
	}
	return dedupe(append(first, second...)), nil
}

// dedupe drops repeated ids, which appear if the API treats both window
// bounds as inclusive and an entry starts exactly on a split point.
func dedupe(es []Entry) []Entry {
	seen := make(map[int64]bool, len(es))
	out := es[:0]
	for _, e := range es {
		if !seen[e.ID] {
			seen[e.ID] = true
			out = append(out, e)
		}
	}
	return out
}

// get decodes a GET response into out, retrying 429s, 5xx and network
// errors with exponential backoff and jitter. A Retry-After header, when
// present, is used as the wait instead.
func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	u := c.BaseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	for attempt := range maxAttempts {
		retryAfter, err := c.do(ctx, u, path, out)
		if err == nil {
			return nil
		}
		if !retryable(ctx, err) || attempt == maxAttempts-1 {
			return err
		}
		wait := retryAfter
		if wait < 0 {
			wait = c.backoff<<attempt + time.Duration(rand.Int64N(int64(c.backoff/2)+1)) //nolint:gosec // jitter, not security
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
	}
	panic("unreachable")
}

func retryable(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return false
	}
	if apiErr, ok := errors.AsType[*APIError](err); ok {
		return apiErr.retryable()
	}
	_, isSyntax := errors.AsType[*json.SyntaxError](err)
	_, isType := errors.AsType[*json.UnmarshalTypeError](err)
	if isSyntax || isType {
		return false // a malformed body will not improve on retry
	}
	return true // transport errors
}

// do performs one request. retryAfter is the server's Retry-After, or -1.
func (c *Client) do(ctx context.Context, u, path string, out any) (retryAfter time.Duration, err error) {
	retryAfter = -1
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, http.NoBody)
	if err != nil {
		return retryAfter, err
	}
	req.SetBasicAuth(c.Token, "api_token")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.UserAgent)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return retryAfter, err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096)) // let the connection be reused
		_ = resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusOK {
		return retryAfter, json.NewDecoder(io.LimitReader(resp.Body, maxBodyBytes)).Decode(out)
	}
	if s, perr := strconv.Atoi(resp.Header.Get("Retry-After")); perr == nil && s >= 0 {
		retryAfter = time.Duration(s) * time.Second
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return retryAfter, &APIError{Path: path, StatusCode: resp.StatusCode, Message: strings.TrimSpace(string(body))}
}
