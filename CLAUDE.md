# CLAUDE.md

Guidance for AI assistants and contributors working in this repository.

## What this is

`tracklens` reports on Toggl Track time entries and flags data-quality problems (no project, running timers, overlaps). It has three faces over one core:

- a CLI (`cmd/tracklens`)
- a read-only HTTP API (`-serve`)
- a React + TypeScript dashboard embedded in the binary

Go module: `github.com/c-dobkins/tracklens`. Go ≥ 1.26 (the toolchain is pinned in `go.mod`); Node 24 for `web/`.

## Commands

```
make test         # go test -race -shuffle=on ./...
make lint         # golangci-lint (config: .golangci.yml)
make golden       # rewrite cmd/tracklens/testdata/*.golden after an intended output change
make fuzz         # fuzz the day splitter
make build-ui     # build web/ and a binary with the dashboard embedded (-tags webui)
make web-test     # web: lint, typecheck, Vitest, then Playwright against the real binary
go run ./cmd/tracklens -file testdata/sample.json        # try it with no token or database
go run -tags webui ./cmd/tracklens -serve :8080 -file testdata/sample.json   # after `make web`
```

PostgreSQL tests are skipped unless `TEST_DATABASE_URL` points at a scratch database. Each test creates and drops its own schema (`internal/testdb`), so packages can run in parallel. CI runs them against PostgreSQL 16 and 18.

Before finishing a change, run the following. CI runs all of them:
- `gofmt -l .` (must print nothing)
- `go vet ./...` and `go vet -tags webui ./...`
- `golangci-lint run`
- `go test -race ./...` (with `TEST_DATABASE_URL` set if you touched the store)
- `go mod tidy -diff`
- for `web/` changes: `npm --prefix web run lint`, `typecheck` and `test`
- for workflow changes: `actionlint`

## Architecture

| Path | Responsibility |
|---|---|
| `internal/toggl` | API v9 client: `meta=true`, window splitting at the 1000-entry cap, retries (429/5xx/transient network only, capped `Retry-After`), typed `APIError`. `Entry.Elapsed/End/Running` are the only places that interpret `duration` (negative = running). `ClockSkew` widens query windows only. |
| `internal/report` | Pure functions, no I/O except the `io.Writer` passed in. `Summarize`, `ByDay` (+`Fill`), `Compare`, `FilterProject`. Each view implements `Renderer` (table/JSON/CSV). `Window`, `DayWindow` and `Periods` are the single definition of report windows, so don't compute windows elsewhere. |
| `internal/store` | PostgreSQL via pgx/stdlib. `Open` migrates (writers); `OpenReader` never changes the schema and fails if migrations are pending. `Sync` is one transaction; `SyncOptions.Prune` deletes rows Toggl no longer returns and is opt-in on purpose. |
| `internal/server` | Read-only API on the Go 1.22+ mux. Every endpoint accepts only its own query parameters (unknown ones get a 400). It serves the dashboard at `/` when given a UI `fs.FS`, with CSP and security headers. |
| `internal/webui` | `FS()` returns the embedded `dist/` with `-tags webui`, nil otherwise. `dist/` is build output and is git-ignored. |
| `web/` | Vite + React 19 + TypeScript + Nivo. `src/api.ts` mirrors the server's JSON. `src/theme.ts` holds the validated chart palette. Playwright (`e2e/`) starts the real Go binary. |
| `cmd/tracklens` | Flags and wiring. `main` only calls `run(ctx, args, env, stdout, stderr) int`; tests drive `run` with a fake env. |

## Conventions

- **Behaviour changes need tests.** CLI output changes show up as golden-file diffs; regenerate with `make golden` and review the diff. New API behaviour gets an `httptest` test against the real `server.New`.
- **Deterministic output.** Sort with a tiebreaker (`cmp.Or`), round hours once (two decimals) and percentages once (half away from zero, in Go and TypeScript alike).
- **Errors** wrap with context (`fmt.Errorf("...: %w", err)`). The API never leaks internal error text: log it and return a generic 503.
- **Migrations:** add a new numbered file in `internal/store/migrations/`. Never edit a released one. Names are recorded without the directory.
- **Secrets:** the Toggl token comes only from `$TOGGL_API_TOKEN`, never a flag. Don't log tokens or DSNs.
- **Exit codes:** 0 ok, 1 error, 2 bad usage, 3 findings with `-strict`.
- **Frontend:**
  - strict type-aware ESLint;
  - every chart has a legend and a table view;
  - status is never shown by colour alone;
  - hooks only set state from async callbacks (see `useDashboard`).
- **Chart colours** are validated palette slots in `web/src/theme.ts`, one meaning per hue across the dashboard. Don't add or change a hue without re-validating the set for colour-vision deficiency and contrast in light and dark mode.
- **Dependencies:** GitHub Actions are pinned by commit SHA with a version comment. Dependabot updates Go modules, actions, npm and Docker.

## Gotchas

- `web/go.mod` exists only so `go ... ./...` skips `web/node_modules`, which can contain stray `.go` files. Run Go commands from the repo root; `go run` from inside `web/` fails.
- `go build -tags webui` fails until `web` has been built (`make web`), because the embed needs `internal/webui/dist`.
- golangci-lint must be built with a Go at least as new as the `go.mod` toolchain. CI builds it with the module's toolchain (`install-mode: goinstall`).
- Per-day views use whole calendar days (`DayWindow`), so "7 days" is seven rows. Summary and compare windows are instant-based (`Window`).
- `-serve` rejects report flags such as `-days`, because each request sets its own.
- CodeQL is skipped while the repository is private. Code scanning needs a public repository or GitHub Advanced Security.
- The client follows the v9 docs but has not yet been run against the live Toggl API. Treat changes to request shape (`internal/toggl`) with care, and test them against `httptest` fakes.
