# tracklens

[![ci](https://github.com/c-dobkins/p1/actions/workflows/ci.yml/badge.svg)](https://github.com/c-dobkins/p1/actions/workflows/ci.yml)

A Go command-line tool that reports on [Toggl Track](https://toggl.com/track/) time entries and flags data-quality problems: entries with no project, timers left running, and overlapping entries. It can keep a history in PostgreSQL.

I built it as a portfolio piece for a Toggl application, using the same stack Toggl lists for its backend: Go and PostgreSQL behind a public API.

```
$ tracklens -file testdata/sample.json
PROJECT           HOURS  BILLABLE  BILLABLE %
Client portal     3.25   3.25      100%
Internal tooling  1.00   0.00      0%
Side workspace    1.00   1.00      100%
(no project)      0.50   0.00      0%
TOTAL             5.75   4.25      74%

Checks:
 - entry 104 "emails" has no project
 - entry 105 is still running (1h0m0s so far)
 - entries 101 and 102 overlap by 15m0s
```

## Install

```
go install github.com/c-dobkins/p1/cmd/tracklens@latest
```

Once a version is tagged, binaries are also on the [releases page](https://github.com/c-dobkins/p1/releases) and the image is at `ghcr.io/c-dobkins/p1`. The repository has to be public for `go install` to work for other people.

## Usage

```
export TOGGL_API_TOKEN=...                       # from your Toggl profile page
tracklens -days 14                               # per project, last 14 days
tracklens -by day -tz Europe/Tallinn             # per day in your time zone
tracklens -compare                               # last 7 days vs the 7 before, biggest change first
tracklens -project portal                        # only projects whose name contains "portal"
tracklens -format json                           # or csv; every view supports table, json and csv
tracklens -overlap-tolerance 2m                  # ignore overlaps of up to 2 minutes
tracklens -strict                                # exit 3 if any check fires (for cron/CI)
tracklens -v                                     # log progress to stderr
tracklens -h                                     # all flags
```

`-file` reads a saved `/me/time_entries?meta=true` response instead of calling the API. An optional `now` field pins the clock so output is reproducible; see [testdata/sample.json](testdata/sample.json).

Exit status: 0 success, 1 error, 2 bad usage, 3 findings with `-strict`.

### History in PostgreSQL

The API only covers recent entries. `-sync` stores the fetched window, and `-history` reports from the database without calling the API.

```
export TRACKLENS_DATABASE_URL='postgres://user:pass@localhost/tracklens?sslmode=verify-full'
tracklens -days 30 -sync          # upsert the last 30 days
tracklens -days 30 -sync -prune   # ...and drop stored entries Toggl no longer returns
tracklens -days 90 -history       # report from stored data
```

A sync is one transaction. It upserts entries by Toggl id, so a timer that was running at the last sync is corrected once it stops. Deletion is opt-in: with `-prune`, stored entries in the window that Toggl no longer returns are removed, so entries deleted in Toggl leave the history too. It is opt-in because it trusts the API response to be the complete window. Migrations are embedded in the binary and applied on connect under an advisory lock, so several processes can start at once safely.

`.github/workflows/sync.yml` runs a daily sync (without `-prune`). It stays off until you set the repository variable `ENABLE_SYNC=true` and add the secrets `TOGGL_API_TOKEN` and `TRACKLENS_DATABASE_URL`.

## How it talks to the API

- It uses `GET /me/time_entries` with RFC3339 `start_date`/`end_date` and `meta=true`. Project names come back on each entry, so projects in every workspace are named without extra calls.
- The endpoint returns at most 1000 entries per call. A full response is treated as possibly truncated: the window is halved and fetched again until every part is complete, and entries on a split point are de-duplicated.
- 429, 5xx, timeouts and dropped connections are retried up to four times with exponential backoff and jitter. A `Retry-After` header is used as-is when present, but a wait longer than a minute, or past the deadline, returns the error at once. Client errors (401, 402, 403, ...), TLS failures, bad URLs and undecodable bodies are not retried, and the message says what to check.
- The window ends 5 minutes past the local clock, so entries from a device whose clock runs slightly fast are not missed.
- Requests send a `User-Agent` with the version. Response bodies are size-limited, and error bodies are included in messages.

## Design

| Package | Role |
|---|---|
| `internal/toggl` | API client: auth, retries, window splitting, `Entry` helpers (`Elapsed`, `End`, `Running`) |
| `internal/report` | Pure functions from entries to `Summary`, `Days` and `Comparison`. Each implements `Renderer` (table, JSON, CSV), so the CLI picks a view and a format independently. |
| `internal/store` | PostgreSQL persistence with embedded SQL migrations and set-based upserts (`unnest`) |
| `cmd/tracklens` | Flag parsing and wiring. `run(ctx, args, env, stdout, stderr) int` keeps `main` trivial and the CLI testable. |

Report details:
- Days are split at local midnight in `-tz`, so an entry crossing midnight counts on both days, and 23- and 25-hour DST days are handled.
- The overlap check sorts entries by start and tracks the furthest end seen so far. One long entry covering several short ones reports each overlap, by the overlapping length.
- Output is deterministic: ties are ordered by name or id. Hours are rounded to two decimals in every format. `change_percent` is null (JSON) or empty (CSV) for a project with no earlier time.

## Development

```
make test     # go test -race -shuffle=on ./...  (set TEST_DATABASE_URL for the PostgreSQL tests)
make lint     # golangci-lint v2 with modernize, revive, gocritic, gosec and more
make fuzz     # fuzz the day splitter
make golden   # rewrite the CLI golden files after an intended output change
make vuln     # govulncheck
```

Tests:
- The CLI is covered by golden files for every view and format, plus tests for usage errors, exit codes and an end-to-end sync against a fake API and a real database.
- PostgreSQL tests run in a throwaway schema each, so packages can run in parallel.

CI on every pull request:
- Tests against PostgreSQL 16 and 18, with a short fuzz run.
- golangci-lint, govulncheck, CodeQL (once the repository is public; private repositories need GitHub Advanced Security) and actionlint.
- A Docker build with a smoke test.

All actions are pinned by commit SHA and kept current by Dependabot. Tagging `vX.Y.Z` builds binaries for Linux, macOS and Windows with checksums and signed build provenance, and pushes a multi-tag image with SBOM and provenance to GHCR.

## Limits

- I haven't run it against the live Toggl API with a real token. The client follows the v9 docs and is tested against a fake server, so the first real run is the remaining check.
- Window boundaries go to the API as RFC3339 instants. Whether the API treats `end_date` as inclusive doesn't matter, because results are de-duplicated and filtered by start time.
- The comparison assigns each entry to a period by its start time and doesn't split entries that cross the boundary.

## License

[MIT](LICENSE). See [CHANGELOG.md](CHANGELOG.md) for changes.
