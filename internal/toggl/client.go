// Package toggl is a minimal client for the Toggl Track API v9.
package toggl

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const DefaultBaseURL = "https://api.track.toggl.com/api/v9"

type Entry struct {
	ID          int64      `json:"id"`
	ProjectID   *int64     `json:"project_id"`
	Description string     `json:"description"`
	Billable    bool       `json:"billable"`
	Start       time.Time  `json:"start"`
	Stop        *time.Time `json:"stop"`
	// Duration is seconds; negative while the timer is running.
	Duration int64 `json:"duration"`
}

type Project struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client

	backoff time.Duration // base delay between retries
}

func New(token string) *Client {
	return &Client{BaseURL: DefaultBaseURL, Token: token, HTTP: &http.Client{Timeout: 30 * time.Second}, backoff: time.Second}
}

const maxAttempts = 4

// get retries on 429 and 5xx, honouring Retry-After when present.
func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	u := c.BaseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		wait, err := c.try(ctx, u, path, out)
		if err == nil {
			return nil
		}
		if wait < 0 { // not retryable
			return err
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait << attempt):
		}
	}
	return lastErr
}

// try performs one request. A negative wait means the error is final.
func (c *Client) try(ctx context.Context, u, path string, out any) (time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return -1, err
	}
	req.SetBasicAuth(c.Token, "api_token")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return c.backoff, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusOK:
		return -1, json.NewDecoder(resp.Body).Decode(out)
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		wait := c.backoff
		if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil {
			wait = time.Duration(s) * time.Second
		}
		return wait, fmt.Errorf("toggl: GET %s: %s", path, resp.Status)
	}
	return -1, fmt.Errorf("toggl: GET %s: %s", path, resp.Status)
}

// Entries returns time entries in [start, end).
func (c *Client) Entries(ctx context.Context, start, end time.Time) ([]Entry, error) {
	var out []Entry
	q := url.Values{"start_date": {start.Format("2006-01-02")}, "end_date": {end.Format("2006-01-02")}}
	return out, c.get(ctx, "/me/time_entries", q, &out)
}

// Projects returns project id -> name for the user's default workspace.
func (c *Client) Projects(ctx context.Context) (map[int64]string, error) {
	var me struct {
		DefaultWorkspaceID int64 `json:"default_workspace_id"`
	}
	if err := c.get(ctx, "/me", nil, &me); err != nil {
		return nil, err
	}
	var ps []Project
	if err := c.get(ctx, fmt.Sprintf("/workspaces/%d/projects", me.DefaultWorkspaceID), nil, &ps); err != nil {
		return nil, err
	}
	names := make(map[int64]string, len(ps))
	for _, p := range ps {
		names[p.ID] = p.Name
	}
	return names, nil
}
