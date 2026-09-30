-- Application health reported by the bot itself (SDK pushes), kept apart
-- from Docker's process state. A bot that never pushed has no row: unknown,
-- not unhealthy.
CREATE TABLE bot_health (
    bot_id            TEXT PRIMARY KEY NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
    last_seen_at_ms   INTEGER NOT NULL,
    ready             INTEGER CHECK (ready IS NULL OR ready IN (0, 1)),
    stale_alerted     INTEGER NOT NULL DEFAULT 0 CHECK (stale_alerted IN (0, 1))
) STRICT;

-- Per-bot notification preferences. No row means the defaults: crash,
-- deployment and backup alerts on, heartbeat alert off.
CREATE TABLE bot_alert_prefs (
    bot_id             TEXT PRIMARY KEY NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
    crash              INTEGER NOT NULL DEFAULT 1 CHECK (crash IN (0, 1)),
    deploy             INTEGER NOT NULL DEFAULT 1 CHECK (deploy IN (0, 1)),
    backup             INTEGER NOT NULL DEFAULT 1 CHECK (backup IN (0, 1)),
    recovery           INTEGER NOT NULL DEFAULT 1 CHECK (recovery IN (0, 1)),
    heartbeat_after_s  INTEGER NOT NULL DEFAULT 0 CHECK (heartbeat_after_s = 0 OR heartbeat_after_s BETWEEN 60 AND 86400),
    updated_at_ms      INTEGER NOT NULL
) STRICT;
