CREATE TABLE time_entries (
    id               BIGINT PRIMARY KEY,
    project_id       BIGINT,
    description      TEXT        NOT NULL DEFAULT '',
    billable         BOOLEAN     NOT NULL DEFAULT FALSE,
    start_at         TIMESTAMPTZ NOT NULL,
    stop_at          TIMESTAMPTZ,
    duration_seconds BIGINT      NOT NULL,
    synced_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX time_entries_start_at_idx ON time_entries (start_at);

CREATE TABLE projects (
    id   BIGINT PRIMARY KEY,
    name TEXT NOT NULL
);
