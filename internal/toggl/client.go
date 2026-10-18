// Package toggl is a minimal client for the Toggl Track API v9.
package toggl

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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
}

func New(token string) *Client {
	return &Client{BaseURL: DefaultBaseURL, Token: token, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	u := c.BaseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.Token, "api_token")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return fmt.Errorf("toggl: rate limited, retry after %ss", resp.Header.Get("Retry-After"))
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("toggl: GET %s: %s", path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
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
