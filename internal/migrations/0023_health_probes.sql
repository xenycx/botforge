CREATE TABLE bot_health_probes (
    bot_id              TEXT PRIMARY KEY NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
    kind                TEXT NOT NULL CHECK (kind IN ('tcp','http')),
    host_port           INTEGER NOT NULL CHECK (host_port BETWEEN 1 AND 65535),
    path                TEXT NOT NULL DEFAULT '/' CHECK (length(path) BETWEEN 1 AND 256),
    interval_s          INTEGER NOT NULL DEFAULT 15 CHECK (interval_s BETWEEN 5 AND 300),
    timeout_ms          INTEGER NOT NULL DEFAULT 2000 CHECK (timeout_ms BETWEEN 250 AND 10000),
    failure_threshold   INTEGER NOT NULL DEFAULT 3 CHECK (failure_threshold BETWEEN 1 AND 10),
    success_threshold   INTEGER NOT NULL DEFAULT 1 CHECK (success_threshold BETWEEN 1 AND 10),
    startup_grace_s     INTEGER NOT NULL DEFAULT 30 CHECK (startup_grace_s BETWEEN 0 AND 600),
    restart_unhealthy   INTEGER NOT NULL DEFAULT 1 CHECK (restart_unhealthy IN (0,1)),
    status              TEXT NOT NULL DEFAULT 'unknown' CHECK (status IN ('unknown','starting','healthy','unhealthy')),
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    consecutive_successes INTEGER NOT NULL DEFAULT 0,
    last_checked_at_ms  INTEGER,
    last_error          TEXT CHECK (last_error IS NULL OR length(last_error) <= 240),
    updated_at_ms       INTEGER NOT NULL
) STRICT;
CREATE INDEX bot_health_probes_due_idx ON bot_health_probes(last_checked_at_ms, interval_s);
