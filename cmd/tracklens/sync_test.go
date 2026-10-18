package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c-dobkins/p1/internal/testdb"
	"github.com/c-dobkins/p1/internal/toggl"
)

// End to end: fake Toggl API -> -sync into PostgreSQL -> -history with the
// API gone. After an entry is deleted in Toggl, only -sync -prune removes it.
func TestSyncThenHistory(t *testing.T) {
	dsn := testdb.DSN(t)
	start := time.Now().UTC().Add(-3 * time.Hour).Truncate(time.Second)
	pid := int64(1)
	entries := []toggl.Entry{
		{ID: 1, ProjectID: &pid, ProjectName: "Synced project", Billable: true, Start: start, Stop: new(start.Add(2 * time.Hour)), Duration: 7200},
		{ID: 2, ProjectID: &pid, ProjectName: "Synced project", Start: start.Add(2 * time.Hour), Stop: new(start.Add(150 * time.Minute)), Duration: 1800},
	}
	var served atomic.Pointer[[]toggl.Entry]
	served.Store(&entries)

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/me/time_entries" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(*served.Load())
	}))
	vars := map[string]string{"TOGGL_API_URL": api.URL, "TOGGL_API_TOKEN": "tok", "TRACKLENS_DATABASE_URL": dsn}

	code, _, errOut := runCLI(t, vars, "-sync")
	if code != exitOK || !strings.Contains(errOut, "entries=2 removed=0") {
		t.Fatalf("first sync: exit %d: %s", code, errOut)
	}

	remaining := entries[:1] // entry 2 deleted in Toggl
	served.Store(&remaining)
	if code, _, errOut := runCLI(t, vars, "-sync"); code != exitOK || !strings.Contains(errOut, "entries=1 removed=0") {
		t.Fatalf("sync without -prune must keep history: exit %d: %s", code, errOut)
	}
	if code, _, errOut := runCLI(t, vars, "-sync", "-prune"); code != exitOK || !strings.Contains(errOut, "entries=1 removed=1") {
		t.Fatalf("second sync: exit %d: %s", code, errOut)
	}

	api.Close() // history must not need the API
	code, out, errOut := runCLI(t, vars, "-history", "-format", "csv")
	if code != exitOK || out != "project,hours,billable_hours\nSynced project,2.00,2.00\n" {
		t.Fatalf("history: exit %d: %s\n%s", code, errOut, out)
	}
}
