package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/c-dobkins/p1/internal/testdb"
)

// End to end: fake Toggl API -> -sync into Postgres -> -history without the API.
func TestSyncThenHistory(t *testing.T) {
	dsn := testdb.DSN(t)
	start := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/me":
			w.Write([]byte(`{"default_workspace_id": 9}`))
		case "/workspaces/9/projects":
			w.Write([]byte(`[{"id": 1, "name": "Synced project"}]`))
		case "/me/time_entries":
			w.Write([]byte(`[{"id": 4242, "project_id": 1, "billable": true, "start": "` + start + `", "stop": "` + start + `", "duration": 7200}]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer api.Close()
	t.Setenv("TOGGL_API_URL", api.URL)
	t.Setenv("TOGGL_API_TOKEN", "tok")

	c := base()
	c.file, c.dsn, c.sync = "", dsn, true
	if err := run(context.Background(), c, &strings.Builder{}); err != nil {
		t.Fatal(err)
	}

	api.Close() // history must not need the API
	c.sync, c.history = false, true
	var out strings.Builder
	if err := run(context.Background(), c, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Synced project") || !strings.Contains(out.String(), "2.00") {
		t.Fatalf("history output:\n%s", out.String())
	}
}
