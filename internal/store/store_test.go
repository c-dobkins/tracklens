package store

import (
	"context"
	"database/sql"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/c-dobkins/p1/internal/testdb"
	"github.com/c-dobkins/p1/internal/toggl"
)

var day = time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.Context(), testdb.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func entriesOn(t *testing.T, s *Store, from, to time.Time) map[int64]toggl.Entry {
	t.Helper()
	es, err := s.Entries(t.Context(), from, to)
	if err != nil {
		t.Fatal(err)
	}
	out := map[int64]toggl.Entry{}
	for _, e := range es {
		out[e.ID] = e
	}
	return out
}

func TestSyncUpsertsAndCorrectsRunningTimers(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	start := day.Add(9 * time.Hour)
	running := toggl.Entry{ID: 1, WorkspaceID: 3, ProjectID: new(int64(5)), ProjectName: "Portal",
		Description: "work", Billable: true, Start: start, Duration: -1}

	if _, err := s.Sync(ctx, day, day.AddDate(0, 0, 1), []toggl.Entry{running}, SyncOptions{}); err != nil {
		t.Fatal(err)
	}

	stopped := running
	stopped.Stop, stopped.Duration = new(start.Add(time.Hour)), 3600
	noProject := toggl.Entry{ID: 2, Start: start.Add(2 * time.Hour), Duration: 60}
	res, err := s.Sync(ctx, day, day.AddDate(0, 0, 1), []toggl.Entry{stopped, noProject}, SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Upserted != 2 || res.Deleted != 0 {
		t.Fatalf("result = %+v", res)
	}

	got := entriesOn(t, s, day, day.AddDate(0, 0, 1))
	e1 := got[1]
	if len(got) != 2 || e1.Duration != 3600 || e1.Stop == nil || !e1.Stop.Equal(*stopped.Stop) ||
		*e1.ProjectID != 5 || e1.WorkspaceID != 3 || !e1.Billable || e1.Description != "work" {
		t.Fatalf("entry 1 not updated: %+v", e1)
	}
	if got[2].ProjectID != nil || got[2].Stop != nil {
		t.Fatalf("entry 2 nulls lost: %+v", got[2])
	}

	names, err := s.Projects(ctx)
	if err != nil || names[5] != "Portal" {
		t.Fatalf("projects = %v, %v", names, err)
	}
}

func TestSyncPruneRemovesEntriesDeletedInTogglOnlyInsideTheWindow(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	yesterday := toggl.Entry{ID: 10, Start: day.Add(-12 * time.Hour), Duration: 60}
	keep := toggl.Entry{ID: 11, Start: day.Add(9 * time.Hour), Duration: 60}
	deleted := toggl.Entry{ID: 12, Start: day.Add(10 * time.Hour), Duration: 60}

	if _, err := s.Sync(ctx, day.AddDate(0, 0, -1), day.AddDate(0, 0, 1), []toggl.Entry{yesterday, keep, deleted}, SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	// Without Prune, a missing entry is left alone.
	if res, err := s.Sync(ctx, day, day.AddDate(0, 0, 1), []toggl.Entry{keep}, SyncOptions{}); err != nil || res.Deleted != 0 {
		t.Fatalf("unpruned sync: %+v, %v", res, err)
	}
	// Re-sync only today with Prune; the API no longer returns entry 12.
	res, err := s.Sync(ctx, day, day.AddDate(0, 0, 1), []toggl.Entry{keep}, SyncOptions{Prune: true})
	if err != nil {
		t.Fatal(err)
	}
	got := entriesOn(t, s, day.AddDate(0, 0, -1), day.AddDate(0, 0, 1))
	if res.Deleted != 1 || len(got) != 2 || got[10].ID != 10 || got[11].ID != 11 {
		t.Fatalf("result %+v, entries %v", res, got)
	}

	// An empty window from the API clears that window.
	if res, err := s.Sync(ctx, day, day.AddDate(0, 0, 1), nil, SyncOptions{Prune: true}); err != nil || res.Deleted != 1 {
		t.Fatalf("empty sync: %+v, %v", res, err)
	}
}

func TestEntriesWindowIsHalfOpen(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	atEnd := toggl.Entry{ID: 1, Start: day.AddDate(0, 0, 1), Duration: 60}
	if _, err := s.Sync(ctx, day, day.AddDate(0, 0, 2), []toggl.Entry{atEnd}, SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := entriesOn(t, s, day, day.AddDate(0, 0, 1)); len(got) != 0 {
		t.Fatalf("entry at the end bound was included: %v", got)
	}
	if got := entriesOn(t, s, day.AddDate(0, 0, 1), day.AddDate(0, 0, 2)); len(got) != 1 {
		t.Fatalf("entry at the start bound was excluded: %v", got)
	}
}

// Several processes starting at once on an empty database must not race on
// creating tables, and later runs must find nothing left to do.
func TestMigrationsAreConcurrentSafeAndIdempotent(t *testing.T) {
	dsn := testdb.DSN(t)
	for round := range 2 {
		var wg sync.WaitGroup
		errs := make(chan error, 4)
		for range 4 {
			wg.Go(func() {
				s, err := Open(context.Background(), dsn)
				if err == nil {
					err = s.Close()
				}
				errs <- err
			})
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatalf("round %d: %v", round, err)
			}
		}
	}
}

func TestSyncToleratesDuplicateIDs(t *testing.T) {
	s := openTest(t)
	e := toggl.Entry{ID: 1, Start: day.Add(time.Hour), Duration: 60}
	if res, err := s.Sync(t.Context(), day, day.AddDate(0, 0, 1), []toggl.Entry{e, e}, SyncOptions{}); err != nil || res.Upserted != 1 {
		t.Fatalf("%+v, %v", res, err)
	}
}

// A database migrated by an earlier release must only get the new migrations.
func TestUpgradeAppliesOnlyNewMigrations(t *testing.T) {
	dsn := testdb.DSN(t)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	initSQL, err := migrations.ReadFile("migrations/001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	for _, stmt := range []string{
		`CREATE TABLE schema_migrations (name TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`,
		string(initSQL),
		`INSERT INTO schema_migrations (name) VALUES ('001_init.sql')`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}

	s, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("upgrade failed: %v", err)
	}
	defer s.Close()
	var applied []string
	rows, err := db.QueryContext(ctx, `SELECT name FROM schema_migrations ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		applied = append(applied, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(applied, []string{"001_init.sql", "002_workspace.sql"}) {
		t.Fatalf("applied = %v", applied)
	}
}

func TestOpenReaderNeverMigrates(t *testing.T) {
	dsn := testdb.DSN(t)
	if _, err := OpenReader(t.Context(), dsn); err == nil || !strings.Contains(err.Error(), "001_init.sql") {
		t.Fatalf("empty database: err = %v", err)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var exists bool
	if err := db.QueryRowContext(t.Context(), `SELECT to_regclass('time_entries') IS NOT NULL`).Scan(&exists); err != nil || exists {
		t.Fatalf("reader created tables: %v %v", exists, err)
	}

	w, err := Open(t.Context(), dsn) // a writer migrates
	if err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	r, err := OpenReader(t.Context(), dsn)
	if err != nil {
		t.Fatalf("migrated database: %v", err)
	}
	if err := r.Ping(t.Context()); err != nil {
		t.Fatal(err)
	}
	_ = r.Close()
}
