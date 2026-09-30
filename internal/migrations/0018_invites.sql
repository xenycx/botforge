-- One-use invitation links to share a bot. Only the SHA-256 of the link token
-- is stored. Accepting one grants exactly the stored permissions.
CREATE TABLE bot_invites (
    id             TEXT PRIMARY KEY NOT NULL,
    bot_id         TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
    token_hash     BLOB NOT NULL UNIQUE CHECK (length(token_hash) = 32),
    permissions    INTEGER NOT NULL CHECK (permissions BETWEEN 1 AND 31),
    created_by     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at_ms  INTEGER NOT NULL,
    expires_at_ms  INTEGER NOT NULL,
    used_by        TEXT REFERENCES users(id) ON DELETE SET NULL,
    used_at_ms     INTEGER
) STRICT;
CREATE INDEX bot_invites_bot_idx ON bot_invites(bot_id);
