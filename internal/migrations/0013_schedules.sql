-- Per-bot scheduled actions. owner_id is the user whose CURRENT permissions
-- the action runs with; a revoked grant or disabled account stops it.
CREATE TABLE schedules (
    id                  TEXT PRIMARY KEY NOT NULL,
    bot_id              TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
    owner_id            TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    action              TEXT NOT NULL CHECK (action IN ('backup', 'start', 'stop', 'restart', 'deploy')),
    spec                TEXT NOT NULL CHECK (length(spec) BETWEEN 9 AND 100),
    timezone            TEXT NOT NULL CHECK (length(timezone) BETWEEN 1 AND 64),
    enabled             INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    next_run_at_ms      INTEGER,
    last_run_at_ms      INTEGER,
    last_status         TEXT CHECK (last_status IS NULL OR last_status IN ('ok', 'failed', 'skipped', 'missed', 'denied')),
    last_message        TEXT CHECK (last_message IS NULL OR length(last_message) <= 300),
    created_at_ms       INTEGER NOT NULL,
    updated_at_ms       INTEGER NOT NULL
) STRICT;
CREATE INDEX schedules_bot_idx ON schedules(bot_id);
CREATE INDEX schedules_due_idx ON schedules(next_run_at_ms) WHERE enabled = 1;
