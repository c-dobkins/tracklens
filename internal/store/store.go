// Package store persists time entries in PostgreSQL so history outlives the
// window the Toggl API is queried for.
package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" driver

	"github.com/c-dobkins/p1/internal/toggl"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type Store struct{ db *sql.DB }

// Open connects, then applies any migrations not yet recorded.
func Open(ctx context.Context, dsn string) (*Store, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: connect: %w", err)
	}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	files, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(files))
	for _, f := range files {
		names = append(names, f.Name())
	}
	sort.Strings(names)

	for _, name := range names {
		if err := s.apply(ctx, name); err != nil {
			return fmt.Errorf("store: migration %s: %w", name, err)
		}
	}
	return nil
}

// apply runs one migration and records it in the same transaction. The
// advisory lock stops two concurrent processes applying it twice.
func (s *Store) apply(ctx context.Context, name string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(727274)`); err != nil {
		return err
	}
	var done bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE name = $1)`, name).Scan(&done); err != nil {
		return err
	}
	if done {
		return nil
	}
	body, err := migrationFS.ReadFile("migrations/" + name)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, string(body)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (name) VALUES ($1)`, name); err != nil {
		return err
	}
	return tx.Commit()
}

// SaveEntries upserts by Toggl entry id, so re-syncing is idempotent and a
// timer that was running last time is updated once it is stopped.
func (s *Store) SaveEntries(ctx context.Context, entries []toggl.Entry) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO time_entries (id, project_id, description, billable, start_at, stop_at, duration_seconds)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (id) DO UPDATE SET
			project_id = EXCLUDED.project_id, description = EXCLUDED.description,
			billable = EXCLUDED.billable, start_at = EXCLUDED.start_at,
			stop_at = EXCLUDED.stop_at, duration_seconds = EXCLUDED.duration_seconds,
			synced_at = now()`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, e := range entries {
		if _, err := stmt.ExecContext(ctx, e.ID, e.ProjectID, e.Description, e.Billable, e.Start, e.Stop, e.Duration); err != nil {
			return fmt.Errorf("store: save entry %d: %w", e.ID, err)
		}
	}
	return tx.Commit()
}

// SaveProjects upserts project names.
func (s *Store) SaveProjects(ctx context.Context, names map[int64]string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for id, name := range names {
		if _, err := tx.ExecContext(ctx, `INSERT INTO projects (id, name) VALUES ($1, $2) ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name`, id, name); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Projects returns every stored project name.
func (s *Store) Projects(ctx context.Context) (map[int64]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name FROM projects`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = name
	}
	return out, rows.Err()
}

// Entries returns stored entries that started in [from, to), oldest first.
func (s *Store) Entries(ctx context.Context, from, to time.Time) ([]toggl.Entry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, project_id, description, billable, start_at, stop_at, duration_seconds
		FROM time_entries WHERE start_at >= $1 AND start_at < $2 ORDER BY start_at, id`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []toggl.Entry
	for rows.Next() {
		var e toggl.Entry
		var pid sql.NullInt64
		var stop sql.NullTime
		if err := rows.Scan(&e.ID, &pid, &e.Description, &e.Billable, &e.Start, &stop, &e.Duration); err != nil {
			return nil, err
		}
		if pid.Valid {
			e.ProjectID = &pid.Int64
		}
		if stop.Valid {
			e.Stop = &stop.Time
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
