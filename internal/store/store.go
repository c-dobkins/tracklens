// Package store persists time entries in PostgreSQL so history outlives the
// window the Toggl API is queried for.
package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" driver

	"github.com/c-dobkins/p1/internal/toggl"
)

//go:embed migrations/*.sql
var migrations embed.FS

// migrationLock is an arbitrary key for pg_advisory_xact_lock, so concurrent
// processes apply migrations one at a time.
const migrationLock = 727274

// Store is a PostgreSQL-backed history of time entries.
type Store struct{ db *sql.DB }

// Open connects and applies any migrations not yet recorded. Use it where
// the process writes (sync).
func Open(ctx context.Context, dsn string) (*Store, error) {
	return open(ctx, dsn, (*Store).migrate)
}

// OpenReader connects without changing the schema, so it works with a
// read-only database role. It fails if migrations are pending.
func OpenReader(ctx context.Context, dsn string) (*Store, error) {
	return open(ctx, dsn, (*Store).checkMigrated)
}

func open(ctx context.Context, dsn string, prepare func(*Store, context.Context) error) (*Store, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: connect: %w", err)
	}
	s := &Store{db: db}
	if err := prepare(s, ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: %w", err)
	}
	return s, nil
}

// migrationNames lists the embedded migrations in order, by base name.
func migrationNames() ([]string, error) {
	paths, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return nil, err
	}
	slices.Sort(paths)
	names := make([]string, len(paths))
	for i, p := range paths {
		names[i] = path.Base(p) // recorded without the directory, e.g. "001_init.sql"
	}
	return names, nil
}

// checkMigrated fails unless every embedded migration has been applied.
func (s *Store) checkMigrated(ctx context.Context) error {
	want, err := migrationNames()
	if err != nil {
		return err
	}
	var exists bool
	if err := s.db.QueryRowContext(ctx, `SELECT to_regclass('schema_migrations') IS NOT NULL`).Scan(&exists); err != nil {
		return err
	}
	applied := map[string]bool{}
	if exists {
		rows, err := s.db.QueryContext(ctx, `SELECT name FROM schema_migrations`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				return err
			}
			applied[name] = true
		}
		if err := rows.Err(); err != nil {
			return err
		}
	}
	var pending []string
	for _, name := range want {
		if !applied[name] {
			pending = append(pending, name)
		}
	}
	if len(pending) > 0 {
		return fmt.Errorf("database schema is not up to date (pending %v); run tracklens -sync once to migrate it", pending)
	}
	return nil
}

// Ping checks that the database is reachable.
func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

// Close releases the connection pool.
func (s *Store) Close() error { return s.db.Close() }

// migrate applies pending migrations in name order inside one transaction,
// holding an advisory lock so concurrent processes cannot race.
func (s *Store) migrate(ctx context.Context) error {
	names, err := migrationNames()
	if err != nil {
		return err
	}

	return s.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, migrationLock); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
			name TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
			return err
		}
		for _, name := range names {
			var done bool
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE name = $1)`, name).Scan(&done); err != nil {
				return err
			}
			if done {
				continue
			}
			body, err := migrations.ReadFile("migrations/" + name)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, string(body)); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (name) VALUES ($1)`, name); err != nil {
				return err
			}
		}
		return nil
	})
}

// SyncOptions controls Sync.
type SyncOptions struct {
	// Prune deletes stored entries that started in the window but are not in
	// entries, so entries deleted in Toggl leave the history too. Only use it
	// when entries is the complete set the API returned for exactly that
	// window; otherwise it deletes real history.
	Prune bool
}

// SyncResult counts what Sync changed.
type SyncResult struct {
	Upserted int64
	Deleted  int64
}

// Sync stores entries fetched for the window [from, to). Entries are inserted
// or updated by Toggl id, so a timer still running at the last sync is
// corrected once it stops, and project names carried by the entries are
// saved. With opts.Prune, stored entries in the window that are missing from
// entries are deleted. Everything happens in one transaction, so a failed
// sync leaves the previous state intact.
func (s *Store) Sync(ctx context.Context, from, to time.Time, entries []toggl.Entry, opts SyncOptions) (SyncResult, error) {
	var res SyncResult
	entries = toggl.Dedupe(entries) // a batch upsert cannot touch one row twice
	n := len(entries)
	ids, workspaces, descriptions := make([]int64, n), make([]int64, n), make([]string, n)
	projects, stops := make([]*int64, n), make([]*time.Time, n)
	billable, starts, durations := make([]bool, n), make([]time.Time, n), make([]int64, n)
	for i, e := range entries {
		ids[i], workspaces[i], projects[i], descriptions[i] = e.ID, e.WorkspaceID, e.ProjectID, e.Description
		billable[i], starts[i], stops[i], durations[i] = e.Billable, e.Start, e.Stop, e.Duration
	}

	err := s.inTx(ctx, func(tx *sql.Tx) error {
		// One statement for the whole batch rather than a round trip per row.
		r, err := tx.ExecContext(ctx, `
			INSERT INTO time_entries (id, workspace_id, project_id, description, billable, start_at, stop_at, duration_seconds)
			SELECT * FROM unnest($1::bigint[], $2::bigint[], $3::bigint[], $4::text[], $5::boolean[], $6::timestamptz[], $7::timestamptz[], $8::bigint[])
			ON CONFLICT (id) DO UPDATE SET
				workspace_id = EXCLUDED.workspace_id, project_id = EXCLUDED.project_id,
				description = EXCLUDED.description, billable = EXCLUDED.billable,
				start_at = EXCLUDED.start_at, stop_at = EXCLUDED.stop_at,
				duration_seconds = EXCLUDED.duration_seconds, synced_at = now()`,
			ids, workspaces, projects, descriptions, billable, starts, stops, durations)
		if err != nil {
			return fmt.Errorf("upsert entries: %w", err)
		}
		if res.Upserted, err = r.RowsAffected(); err != nil {
			return err
		}

		if opts.Prune {
			r, err = tx.ExecContext(ctx, `
				DELETE FROM time_entries
				WHERE start_at >= $1 AND start_at < $2 AND NOT (id = ANY($3::bigint[]))`, from, to, ids)
			if err != nil {
				return fmt.Errorf("prune removed entries: %w", err)
			}
			if res.Deleted, err = r.RowsAffected(); err != nil {
				return err
			}
		}

		names := toggl.ProjectNames(entries)
		pids, pnames := make([]int64, 0, len(names)), make([]string, 0, len(names))
		for id, name := range names {
			pids, pnames = append(pids, id), append(pnames, name)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO projects (id, name) SELECT * FROM unnest($1::bigint[], $2::text[])
			ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name`, pids, pnames); err != nil {
			return fmt.Errorf("upsert projects: %w", err)
		}
		return nil
	})
	return res, err
}

// Projects returns every stored project id -> name.
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
		SELECT id, COALESCE(workspace_id, 0), project_id, description, billable, start_at, stop_at, duration_seconds
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
		if err := rows.Scan(&e.ID, &e.WorkspaceID, &pid, &e.Description, &e.Billable, &e.Start, &stop, &e.Duration); err != nil {
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

func (s *Store) inTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
