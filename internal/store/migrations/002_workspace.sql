-- Entries can come from any workspace the user belongs to.
ALTER TABLE time_entries ADD COLUMN workspace_id BIGINT;
