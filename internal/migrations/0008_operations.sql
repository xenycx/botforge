-- Durable record of long-running work (builds, deployments, backups,
-- restores) so outcomes survive page reloads and panel restarts. Bounded per
-- bot by the application (oldest pruned). message/result_code are safe,
-- user-facing text; build output lives in capped files, not here.
CREATE TABLE operations (
    id                  TEXT PRIMARY KEY NOT NULL,
    bot_id              TEXT NOT NULL
                        REFERENCES bots(id) ON DELETE CASCADE,
    kind                TEXT NOT NULL
                        CHECK (kind IN ('build', 'deploy', 'backup', 'restore', 'rollback')),
    trigger             TEXT NOT NULL
                        CHECK (trigger IN ('manual', 'push', 'schedule', 'initial', 'start', 'api', 'system')),
    actor_id            TEXT REFERENCES users(id) ON DELETE SET NULL,
    status              TEXT NOT NULL
                        CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled', 'interrupted')),
    stage               TEXT NOT NULL DEFAULT '' CHECK (length(stage) <= 64),
    source_ref          TEXT CHECK (source_ref IS NULL OR length(source_ref) <= 128),
    source_label        TEXT CHECK (source_label IS NULL OR length(source_label) <= 300),
    generation          INTEGER,
    result_code         TEXT CHECK (result_code IS NULL OR length(result_code) <= 64),
    message             TEXT CHECK (message IS NULL OR length(message) <= 2000),
    detail_json         TEXT CHECK (detail_json IS NULL OR (json_valid(detail_json) AND length(detail_json) <= 4096)),
    log_bytes           INTEGER NOT NULL DEFAULT 0 CHECK (log_bytes >= 0),
    created_at_ms       INTEGER NOT NULL,
    started_at_ms       INTEGER,
    finished_at_ms      INTEGER
) STRICT;

CREATE INDEX operations_bot_idx ON operations(bot_id, created_at_ms);
CREATE INDEX operations_active_idx ON operations(status)
    WHERE status IN ('queued', 'running');
