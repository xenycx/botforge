ALTER TABLE users ADD COLUMN display_name TEXT NOT NULL DEFAULT ''
    CHECK (length(display_name) <= 64);
ALTER TABLE users ADD COLUMN avatar_jpeg BLOB
    CHECK (avatar_jpeg IS NULL OR length(avatar_jpeg) <= 65536);

CREATE TABLE account_invites (
    id            TEXT PRIMARY KEY NOT NULL,
    token_hash    BLOB NOT NULL UNIQUE CHECK (length(token_hash) = 32),
    email         TEXT COLLATE NOCASE,
    role          TEXT NOT NULL DEFAULT 'user' CHECK (role IN ('admin', 'user')),
    created_by    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at_ms INTEGER NOT NULL,
    expires_at_ms INTEGER NOT NULL,
    used_by       TEXT REFERENCES users(id) ON DELETE SET NULL,
    used_at_ms    INTEGER,
    CHECK (expires_at_ms > created_at_ms),
    CHECK ((used_by IS NULL) = (used_at_ms IS NULL))
) STRICT;
CREATE INDEX account_invites_active_idx ON account_invites(expires_at_ms, used_at_ms);

-- A language-neutral, safe alternative to bot-supplied HTML. Bots replace the
-- current value of a widget by key; the browser renders only known kinds.
CREATE TABLE bot_widgets (
    bot_id       TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
    widget_key   TEXT NOT NULL CHECK (length(widget_key) BETWEEN 1 AND 64),
    kind         TEXT NOT NULL CHECK (kind IN ('metric','status','progress','text','chart','table','link')),
    title        TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 80),
    position     INTEGER NOT NULL DEFAULT 0 CHECK (position BETWEEN 0 AND 1000),
    payload_json TEXT NOT NULL CHECK (json_valid(payload_json) AND length(payload_json) <= 4096),
    updated_at_ms INTEGER NOT NULL,
    PRIMARY KEY (bot_id, widget_key)
) STRICT, WITHOUT ROWID;
