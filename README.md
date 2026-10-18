# tracklens

A small Go CLI that summarises [Toggl Track](https://toggl.com/track/) time entries by project and flags data-quality problems: entries with no project, timers left running, and overlapping entries.

Built as a portfolio piece for a Toggl application. Toggl's engineering stack is Go and PostgreSQL, with public APIs, so this uses Go with only the standard library.

```
$ go run ./cmd/tracklens -file testdata/sample.json
PROJECT           HOURS  BILLABLE  BILLABLE %
Client portal     4.25   4.25      100%
Internal tooling  1.00   0.00      0%
(no project)      0.50   0.00      0%
TOTAL             5.75   4.25      74%

Checks:
 - entry 104 "emails" has no project
 - entry 105 is still running (1h0m0s so far)
 - entries 101 and 102 overlap by 15m0s
```

## Usage

```
export TOGGL_API_TOKEN=...                   # from your Toggl profile page
tracklens -days 14                           # live data
tracklens -file testdata/sample.json         # offline sample
tracklens -file testdata/sample.json -by day -tz Europe/Tallinn   # daily totals in a time zone
tracklens -file testdata/sample.json -format json                 # or csv
tracklens -days 7 -compare                   # this week vs the week before, biggest movers first
tracklens -overlap-tolerance 2m              # ignore overlaps of up to 2 minutes
tracklens -days 7 -compare -format csv       # every view supports table, json and csv
tracklens -days 14 -by day -format json -v   # -v logs progress to stderr
tracklens -days 14 -strict                   # exit 2 if any check fires (for CI/cron)
```

### History in PostgreSQL

The API only returns a window of recent entries. `-sync` stores what it fetched; `-history` reports from the database with no API call.

```
export TRACKLENS_DATABASE_URL='postgres://user:pass@localhost/tracklens?sslmode=disable'
tracklens -days 30 -sync       # upsert entries and projects (safe to repeat)
tracklens -days 90 -history    # report from stored data
```

Schema migrations are embedded in the binary and applied on connect, under an advisory lock so concurrent runs are safe. Entries are upserted by Toggl id, so a timer that was running at the last sync is corrected once it stops.

### Scheduled sync

`.github/workflows/sync.yml` runs a daily `-sync` of the last 14 days. It is off until you set the repository variable `ENABLE_SYNC=true` and add the secrets `TOGGL_API_TOKEN` and `TRACKLENS_DATABASE_URL`; it can also be run by hand from the Actions tab.

### Releases

Pushing a tag such as `v0.1.0` runs `release.yml`: it tests, builds binaries for Linux, macOS and Windows (amd64 and arm64 where applicable) with a `SHA256SUMS` file, creates a GitHub release, and pushes an image to `ghcr.io/c-dobkins/p1`.

### Docker

```
make docker
docker run --rm -e TOGGL_API_TOKEN tracklens:dev -days 7
```

## Development

```
make test     # go test -race ./...  (set TEST_DATABASE_URL to include the Postgres tests)
make lint     # golangci-lint
make cover
make vuln     # govulncheck
```

CI runs gofmt, vet, tests with a Postgres service container, golangci-lint, govulncheck and a Docker build. Dependabot keeps Go modules, Actions and the base image current. See [SECURITY.md](SECURITY.md) for how secrets are handled.

## Layout

- `internal/toggl`: API v9 client with basic auth, context support and retry/backoff on 429 and 5xx (honours `Retry-After`).
- `internal/store`: PostgreSQL persistence, embedded migrations.
- `internal/report`: pure aggregation and checks, easy to unit test.
- `cmd/tracklens`: the CLI; `$TOGGL_API_URL` overrides the API base for tests.

## Status and limits

- Verified against an `httptest` fake and a real local PostgreSQL 16, not the live Toggl API. The endpoint paths are from memory of the v9 docs, so check them first.
- There is no pagination because the time-entries endpoint is queried by date range.
- Entries that cross midnight are split across days in the `-tz` zone, including 23- and 25-hour DST days.
- Overlap detection tracks the furthest-ending earlier entry, so one long entry covering several short ones is caught.
- `-compare` works on entry start time. Every view supports `-format table|json|csv`; JSON and CSV round hours to two decimals, and `change_percent` is null (JSON) or empty (CSV) for a project with no earlier time.
- The release and sync workflows have not run yet. The release build script was run locally for all five targets, and all workflows pass actionlint with shellcheck.
- Next: a `-project` filter, a weekly Slack/email digest, and a small HTTP mode that serves the same reports.
