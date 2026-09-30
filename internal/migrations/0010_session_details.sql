-- Session details for the "active sessions" page. id is a public handle (the
-- token hash never leaves the server); device is a coarse label derived from
-- the User-Agent ("Firefox on Linux"), never the raw header; auth_at_ms is
-- when the user last proved who they are (password or provider), for
-- recent-authentication checks.
ALTER TABLE sessions ADD COLUMN id TEXT;
ALTER TABLE sessions ADD COLUMN last_seen_at_ms INTEGER;
ALTER TABLE sessions ADD COLUMN device TEXT
    CHECK (device IS NULL OR length(device) <= 64);
ALTER TABLE sessions ADD COLUMN auth_at_ms INTEGER;
UPDATE sessions SET id = lower(hex(randomblob(16))), auth_at_ms = created_at_ms, last_seen_at_ms = created_at_ms;
CREATE UNIQUE INDEX sessions_id_idx ON sessions(id);
