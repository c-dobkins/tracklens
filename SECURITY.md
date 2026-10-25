# Security

## Reporting

Please report vulnerabilities privately through GitHub security advisories on this repository, not in a public issue.

## How the project handles secrets and supply chain

- The Toggl API token is read only from `$TOGGL_API_TOKEN`, never from a flag, so it doesn't appear in `ps` output or shell history. `.env` is git-ignored.
- Database credentials come from `$TRACKLENS_DATABASE_URL` or `-db`. Prefer the environment variable for the same reason, and use `sslmode=verify-full` outside local development.
- All SQL uses bound parameters. HTTP response bodies are size-limited.
- The container image runs as a non-root user on a distroless base.
- CI runs govulncheck and CodeQL, actions are pinned by commit SHA, and Dependabot keeps Go modules, actions and base images current.
- Release binaries and images carry signed build provenance. Verify a download with:

  ```
  gh attestation verify tracklens_v1.0.0_linux_amd64.tar.gz --repo c-dobkins/tracklens
  ```
