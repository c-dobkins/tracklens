package store

import (
	"context"
	"testing"
	"time"

	"github.com/c-dobkins/p1/internal/testdb"
	"github.com/c-dobkins/p1/internal/toggl"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), testdb.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestSaveAndReadBack(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	start := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	pid := int64(5)

	running := toggl.Entry{ID: 1, ProjectID: &pid, Description: "work", Billable: true, Start: start, Duration: -1}
	if err := s.SaveEntries(ctx, []toggl.Entry{running}); err != nil {
		t.Fatal(err)
	}

	// Re-sync after the timer stopped: same id must update, not duplicate.
	stop := start.Add(time.Hour)
	stopped := running
	stopped.Stop, stopped.Duration = &stop, 3600
	if err := s.SaveEntries(ctx, []toggl.Entry{stopped, {ID: 2, Start: start.Add(2 * time.Hour), Duration: 60}}); err != nil {
		t.Fatal(err)
	}

	got, err := s.Entries(ctx, start, start.AddDate(0, 0, 1))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2", len(got))
	}
	if got[0].Duration != 3600 || got[0].Stop == nil || !got[0].Stop.Equal(stop) || *got[0].ProjectID != 5 || !got[0].Billable {
		t.Fatalf("entry 1 not updated: %+v", got[0])
	}
	if got[1].ProjectID != nil || got[1].Stop != nil {
		t.Fatalf("entry 2 nulls lost: %+v", got[1])
	}

	// The window is half-open on start time.
	none, err := s.Entries(ctx, start.AddDate(0, 0, 1), start.AddDate(0, 0, 2))
	if err != nil || len(none) != 0 {
		t.Fatalf("outside window: %v, %v", none, err)
	}
}

func TestProjectsRoundTripAndMigrateTwice(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	if err := s.SaveProjects(ctx, map[int64]string{1: "A"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveProjects(ctx, map[int64]string{1: "A renamed", 2: "B"}); err != nil {
		t.Fatal(err)
	}
	if err := s.migrate(ctx); err != nil { // idempotent
		t.Fatal(err)
	}
	got, err := s.Projects(ctx)
	if err != nil || len(got) != 2 || got[1] != "A renamed" {
		t.Fatalf("projects = %v, err = %v", got, err)
	}
}
