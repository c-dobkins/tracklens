# Security

- The Toggl API token is read from `$TOGGL_API_TOKEN` only, never from a flag, so it does not show up in `ps` output or shell history. Don't commit it; `.env` is git-ignored.
- Database credentials come from `-db` or `$TRACKLENS_DATABASE_URL`. Prefer the environment variable for the same reason, and use `sslmode=verify-full` outside local development.
- All SQL uses bound parameters.
- The container image runs as a non-root user on a distroless base.
- Dependencies are scanned with `govulncheck` in CI and kept current by Dependabot.

Report vulnerabilities privately through GitHub security advisories on this repository.
