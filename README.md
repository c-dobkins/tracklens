# tracklens

[![ci](https://github.com/c-dobkins/tracklens/actions/workflows/ci.yml/badge.svg)](https://github.com/c-dobkins/tracklens/actions/workflows/ci.yml)

A Go command-line tool that reports on [Toggl Track](https://toggl.com/track/) time entries and flags data-quality problems: entries with no project, timers left running, and overlapping entries. It can keep a history in PostgreSQL.

It is a portfolio project: a Go backend with PostgreSQL behind a small HTTP API, and a React + TypeScript dashboard, built with production-style tests, CI and releases.

tracklens is an independent project and is not affiliated with or endorsed by Toggl.

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
go install github.com/c-dobkins/tracklens/cmd/tracklens@latest
```

That build includes the CLI and the HTTP API. For the dashboard as well, build from a clone with `make build-ui` (needs Node 24), or use a release: tagged versions publish binaries with the dashboard on the [releases page](https://github.com/c-dobkins/tracklens/releases) and an image at `ghcr.io/c-dobkins/tracklens`.

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

## Dashboard

`-serve` also serves a React + TypeScript dashboard built with [Nivo](https://nivo.rocks) charts:

![tracklens dashboard showing total and billable hours, hours by project, change by project, hours per day and data checks](docs/dashboard-light.png)

```
make build-ui                                          # npm ci + vite build, then go build -tags webui
./bin/tracklens -serve :8080 -file testdata/sample.json
open http://localhost:8080
```

- **Filters:** one row for period (7/14/30/90 days), project and time zone scopes every tile and chart. Changing them keeps the previous render, dimmed, while new data loads, so the layout never jumps.
- **Charts:** hours by project and hours per day are stacked billable/non-billable bars. Change by project is a diverging bar of each project's change against the previous period. Every day of the period is shown, including empty ones.
- **Accessibility:**
  - Every chart has a legend and a "View as table" twin, so no value depends on hover or on telling colours apart.
  - Bars are keyboard-focusable and show the same tooltip as on hover.
  - Findings use a status icon and label, never colour alone.
- **Colour:** categorical and diverging colours were checked with a palette validator for colour-vision-deficiency separation and contrast, separately for light and dark mode. Dark mode is its own palette, not an inversion.
- **Bars:** thin (at most 24px) with a rounded value end, a 2px gap between stacked segments, and hit areas larger than the marks.
- **Build and security:** the built assets are embedded in the Go binary (`-tags webui`), served with a strict Content-Security-Policy, and cached forever under `/assets/` (fingerprinted) while `index.html` always revalidates. A plain `go build` without the tag still works and serves the API only.

For frontend development, run `npm --prefix web run dev` against `tracklens -serve :8080 -file testdata/sample.json`. Vite proxies `/v1` to it.

## HTTP API

`-serve` exposes the same reports as a small read-only JSON/CSV API, either over the PostgreSQL history or over a fixture:

```
tracklens -serve :8080 -history                        # needs -db / TRACKLENS_DATABASE_URL
tracklens -serve :8080 -file testdata/sample.json      # no database needed
curl 'localhost:8080/v1/summary?days=14&project=portal'
curl 'localhost:8080/v1/days?tz=Europe/Tallinn&format=csv'
curl 'localhost:8080/v1/compare?days=7'
curl 'localhost:8080/healthz'
```

| Parameter | Endpoints | Meaning |
|---|---|---|
| `days` | all | window length, 1 to 366 (default 7) |
| `project` | all | case-insensitive project-name filter |
| `format` | all | `json` (default) or `csv` |
| `tz` | `/v1/days` | IANA time zone for day boundaries |
| `overlap_tolerance` | `/v1/summary` | e.g. `2m` |

- Bad parameters, and parameters an endpoint doesn't use, get a `400` with `{"error": "..."}`, so a typo is never silently ignored. Report flags such as `-days` are rejected with `-serve`, because each request sets its own.
- An unavailable data source gets a `503` that doesn't reveal internal details. The cause is logged instead.
- `/healthz` pings the database. Each request has a 20-second deadline, so a slow query ends in a `503` rather than a dropped connection.
- `-history` and `-serve -history` never change the schema, so they work with a read-only database role. If migrations are pending they stop with a message to run `-sync` once.
- The server has read, write and idle timeouts, logs every request with its status and duration, and shuts down gracefully on SIGINT/SIGTERM, letting in-flight requests finish.
- It has no authentication, so bind it to localhost or put it behind a proxy that adds auth.

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
| `web` | React 19 + TypeScript dashboard (Vite, Nivo), strict ESLint, Vitest + Testing Library, Playwright end-to-end tests against the real binary |
| `internal/webui` | Embeds the built dashboard when compiled with `-tags webui` |
| `internal/server` | Read-only HTTP API over any `Source` (the store, or a fixture) using the Go 1.22+ method-and-path router |
| `cmd/tracklens` | Flag parsing and wiring. `run(ctx, args, env, stdout, stderr) int` keeps `main` trivial and the CLI testable. |

Report details:
- Days are split at local midnight in `-tz`, so an entry crossing midnight counts on both days, and 23- and 25-hour DST days are handled.
- The overlap check sorts entries by start and tracks the furthest end seen so far. One long entry covering several short ones reports each overlap, by the overlapping length.
- The comparison clips entries to each period, so an entry crossing a period edge counts only the time inside it. A day of extra history is loaded to catch entries running into the previous period.
- Output is deterministic: ties are ordered by name or id. Hours are rounded to two decimals in every format. `change_percent` is null (JSON) or empty (CSV) for a project with no earlier time.

## Development

```
make test     # go test -race -shuffle=on ./...  (set TEST_DATABASE_URL for the PostgreSQL tests)
make lint     # golangci-lint v2 with modernize, revive, gocritic, gosec and more
make fuzz     # fuzz the day splitter
make golden   # rewrite the CLI golden files after an intended output change
make vuln     # govulncheck
make web-test # dashboard: lint, typecheck, unit tests, end-to-end tests
```

Tests:
- The CLI is covered by golden files for every view and format, plus tests for usage errors, exit codes and an end-to-end sync against a fake API and a real database.
- PostgreSQL tests run in a throwaway schema each, so packages can run in parallel.

CI on every pull request:
- Tests against PostgreSQL 16 and 18, with a short fuzz run.
- golangci-lint, govulncheck, CodeQL for Go and TypeScript, and actionlint.
- The dashboard: npm audit of runtime dependencies, ESLint (strict, type-aware), TypeScript, Vitest, a production build, and Playwright end-to-end tests that start the real Go binary with the dashboard embedded.
- A Docker build with a smoke test of the CLI, the API and the embedded dashboard.

All actions are pinned by commit SHA and kept current by Dependabot. Tagging `vX.Y.Z` builds binaries for Linux, macOS and Windows with checksums and signed build provenance, and pushes a multi-tag image with SBOM and provenance to GHCR.

## Limits

- It has not yet been run against the live Toggl API with a real token. The client follows the v9 API docs and is tested against a fake server.
- Window boundaries go to the API as RFC3339 instants. Whether the API treats `end_date` as inclusive doesn't matter, because results are de-duplicated and filtered by start time.

## License

[MIT](LICENSE). See [CHANGELOG.md](CHANGELOG.md) for changes.
