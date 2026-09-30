-- Who changed what. Values, file contents and credentials are never stored:
-- target holds a name (variable, file path, email), never a value. bot_id has
-- no foreign key so the record of a deleted bot remains for administrators;
-- bot_name is a snapshot for display. Retention is enforced by the panel.
CREATE TABLE audit_events (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    at_ms               INTEGER NOT NULL,
    actor_id            TEXT,
    actor_label         TEXT CHECK (actor_label IS NULL OR length(actor_label) <= 320),
    bot_id              TEXT,
    bot_name            TEXT CHECK (bot_name IS NULL OR length(bot_name) <= 64),
    subject_user_id     TEXT,
    action              TEXT NOT NULL CHECK (length(action) BETWEEN 1 AND 64),
    target              TEXT CHECK (target IS NULL OR length(target) <= 300),
    outcome             TEXT NOT NULL DEFAULT 'ok'
                        CHECK (outcome IN ('ok', 'denied', 'failed')),
    ip                  TEXT CHECK (ip IS NULL OR length(ip) <= 64)
) STRICT;

CREATE INDEX audit_bot_idx ON audit_events(bot_id, at_ms);
CREATE INDEX audit_actor_idx ON audit_events(actor_id, at_ms);
CREATE INDEX audit_subject_idx ON audit_events(subject_user_id, at_ms);
CREATE INDEX audit_at_idx ON audit_events(at_ms);
