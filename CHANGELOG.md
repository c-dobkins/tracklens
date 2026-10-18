# Changelog

All notable changes are listed here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow [Semantic Versioning](https://semver.org/). Until 1.0.0, minor versions may break compatibility.

## [Unreleased]

Nothing has been tagged yet. These are the changes since the first version on `main`.

### Breaking
- `-strict` exits with status 3 when findings are present. Status 2 now means bad usage, matching Go's flag package.
- JSON `findings` are objects (`kind`, `entry_ids`, `message`) instead of strings.
- `-file` reads a saved `/me/time_entries?meta=true` response with an optional `now` field, and rejects unknown fields. The old top-level `projects` list is gone, because names come from each entry's `project_name`.
- `store.Sync` replaces `SaveEntries`/`SaveProjects`.

### Added
- `-serve` provides a read-only HTTP API (`/v1/summary`, `/v1/days`, `/v1/compare`, `/healthz`) over the PostgreSQL history or a fixture, in JSON or CSV, with graceful shutdown.
- `-prune`, used with `-sync`, deletes stored entries in the window that Toggl no longer returns.
- `-project` filters by project name, case-insensitively.
- `-timeout` sets the overall time limit (default 2m).
- `-h` shows usage with examples and exit codes.
- Migration `002_workspace.sql` stores `workspace_id`.
- CI tests against PostgreSQL 16 and 18, adds a fuzz run, CodeQL and a Docker smoke test.
- Releases carry signed build provenance, an SBOM and semver image tags.

### Fixed
- `-compare` now splits an entry that crosses from one period into the other, instead of counting all of it in the period where it started.
- Projects outside the default workspace were shown as "project 123". Names now come from `meta=true`.
- Responses at the API's 1000-entry cap were silently truncated. The window is now split until every part is complete.
- `Retry-After` was multiplied by the backoff. It is now used as-is and capped, and a wait longer than a minute, or past the deadline, returns the error immediately.
- Permanent errors (bad URL, TLS failure, undecodable body) are no longer retried.
- Window bounds are sent as RFC3339 instants instead of whole dates.
- Entries starting up to 5 minutes after the local clock are included, to allow for device clock skew.
- Reports with equal totals sort deterministically.
- A contained overlap reports its own length.
- The table's percentages are rounded once.
- `schema_migrations` is created under the migration lock, so concurrent first runs cannot race.
