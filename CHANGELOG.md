# Changelog

All notable changes are listed here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow [Semantic Versioning](https://semver.org/). Until 1.0.0, minor versions may break compatibility.

## [Unreleased]

First version.

### Added
- **CLI reports** on Toggl Track time entries: per project, per day (in any time zone, with entries split at midnight) and a comparison with the previous period, as a table, JSON or CSV.
- **Data-quality checks:**
  - entries with no project, running timers and overlapping entries (with `-overlap-tolerance`);
  - `-strict` exits with status 3 when a check fires, for use in cron and CI.
- **Toggl API client:**
  - `meta=true` requests, so projects in every workspace are named;
  - large windows are split at the 1000-entry response cap;
  - retries with backoff and jitter that honour a capped `Retry-After`;
  - typed errors with hints for 401, 402 and 403.
- **PostgreSQL history:**
  - `-sync` upserts entries, and `-sync -prune` also removes entries deleted in Toggl;
  - `-history` reports without calling the API;
  - migrations are embedded and applied under an advisory lock, and readers never migrate.
- **HTTP API (`-serve`):**
  - `/v1/summary`, `/v1/days`, `/v1/compare` and `/healthz` over the history or a fixture;
  - validated parameters, per-request deadlines, security headers and graceful shutdown.
- **Dashboard:**
  - React + TypeScript with Nivo charts, embedded in the binary with `-tags webui`;
  - filters, stat tiles, three charts with table views, data checks, dark mode and a phone layout.
- **Tooling:**
  - golden-file, fuzz, PostgreSQL integration, Vitest and Playwright tests;
  - CI with lint, govulncheck, CodeQL and Docker smoke tests;
  - releases with checksums, signed build provenance and an SBOM.
