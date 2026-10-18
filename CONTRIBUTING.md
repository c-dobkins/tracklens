# Contributing

1. `make test lint` must pass. Set `TEST_DATABASE_URL` to a scratch PostgreSQL database to include the integration tests. Each test creates and drops its own schema.
2. Output changes show up as golden-file diffs in `cmd/tracklens/testdata`. Run `make golden` and review the diff in your pull request.
3. Schema changes go in a new numbered file in `internal/store/migrations/`. Never edit a migration that has been released.
4. Keep `internal/report` free of I/O other than the `io.Writer` it is given.
5. Dashboard changes: `make web-test` runs lint, typecheck, Vitest and Playwright. Chart colours are validated palette slots in `web/src/theme.ts`. Don't add a hue without re-validating the set for colour-vision deficiency and contrast in both modes.
