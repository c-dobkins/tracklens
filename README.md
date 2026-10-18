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
export TOGGL_API_TOKEN=...        # from your Toggl profile page
go run ./cmd/tracklens -days 14   # live data
go run ./cmd/tracklens -file testdata/sample.json
go test ./...
```

## Layout

- `internal/toggl` is a minimal API v9 client (basic auth, rate-limit handling, context support).
- `internal/report` holds the pure aggregation and checks, which are easy to unit test.
- `cmd/tracklens` is the CLI.

## Status and limits

- Tests use an `httptest` server and a fixture. I haven't run it against the live API, and the endpoint paths are from memory of the v9 docs, so check them first.
- No pagination or retry/backoff yet; a 429 is reported, not retried.
- Possible next steps: weekly/daily breakdown, CSV/JSON output, a Postgres store for history.
