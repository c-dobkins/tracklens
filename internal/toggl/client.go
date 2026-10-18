// Package toggl is a minimal client for the Toggl Track API v9.
package toggl

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"syscall"
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

// Client calls the Toggl Track API. New returns one with defaults; a zero
// value for any field below also falls back to the default.
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
	return &Client{Token: token, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) baseURL() string   { return cmp.Or(c.BaseURL, DefaultBaseURL) }
func (c *Client) userAgent() string { return cmp.Or(c.UserAgent, "tracklens") }
func (c *Client) limit() int        { return cmp.Or(c.pageLimit, MaxEntriesPerRequest) }
func (c *Client) base() time.Duration {
	return cmp.Or(c.backoff, time.Second)
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP == nil {
		return http.DefaultClient
	}
	return c.HTTP
}

// Entries returns the entries that started in [start, end), with project
// names filled in. A response at the API's size cap is split in half and
// fetched again until every part is complete, so large windows are not
// silently truncated. Each entry appears once.
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
	if len(got) < c.limit() {
		return Dedupe(got), nil
	}

	mid := start.Add(end.Sub(start) / 2).Truncate(time.Second)
	if !mid.After(start) {
		return nil, fmt.Errorf("toggl: more than %d entries start at %s; cannot split further", c.limit(), start.Format(time.RFC3339))
	}
	first, err := c.Entries(ctx, start, mid)
	if err != nil {
		return nil, err
	}
	second, err := c.Entries(ctx, mid, end)
	if err != nil {
		return nil, err
	}
	// Both halves can contain an entry that starts exactly on mid if the API
	// treats both bounds as inclusive.
	return Dedupe(append(first, second...)), nil
}

// Dedupe drops entries whose id was already seen, keeping the first.
func Dedupe(es []Entry) []Entry {
	seen := make(map[int64]bool, len(es))
	out := make([]Entry, 0, len(es))
	for _, e := range es {
		if !seen[e.ID] {
			seen[e.ID] = true
			out = append(out, e)
		}
	}
	return out
}

// maxRetryWait caps how long a single retry may wait. A server asking for
// longer gets its error returned straight away rather than a silent stall.
const maxRetryWait = time.Minute

// get decodes a GET response into out, retrying 429s, 5xx and transient
// network errors with exponential backoff and jitter. A Retry-After header,
// when present, is used as the wait instead.
func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	u := c.baseURL() + path
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
			b := c.base()
			wait = b<<attempt + time.Duration(rand.Int64N(int64(b/2)+1)) //nolint:gosec // jitter, not security
		}
		if deadline, ok := ctx.Deadline(); wait > maxRetryWait || ok && time.Until(deadline) < wait {
			return fmt.Errorf("%w (server asked to wait %s; not retrying)", err, wait.Round(time.Second))
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

// decodeError marks a response that arrived but could not be decoded;
// retrying will not help.
type decodeError struct{ err error }

func (e decodeError) Error() string { return "toggl: decode response: " + e.err.Error() }
func (e decodeError) Unwrap() error { return e.err }

// retryable reports whether err is worth another attempt: rate limiting,
// server errors, timeouts and dropped connections. Bad URLs, TLS failures,
// client errors and undecodable bodies are final.
func retryable(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return false
	}
	if apiErr, ok := errors.AsType[*APIError](err); ok {
		return apiErr.retryable()
	}
	if _, ok := errors.AsType[decodeError](err); ok {
		return false
	}
	if netErr, ok := errors.AsType[net.Error](err); ok && netErr.Timeout() {
		return true
	}
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNREFUSED)
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
	req.Header.Set("User-Agent", c.userAgent())

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return retryAfter, err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096)) // let the connection be reused
		_ = resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(io.LimitReader(resp.Body, maxBodyBytes)).Decode(out); err != nil {
			return retryAfter, decodeError{err}
		}
		return retryAfter, nil
	}
	if s, perr := strconv.Atoi(resp.Header.Get("Retry-After")); perr == nil && s >= 0 {
		retryAfter = time.Duration(s) * time.Second
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return retryAfter, &APIError{Path: path, StatusCode: resp.StatusCode, Message: strings.TrimSpace(string(body))}
}
