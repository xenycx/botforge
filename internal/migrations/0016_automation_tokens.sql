-- Scoped HTTP bearer tokens for scripts and CI ("bpa_" prefix). Separate from
-- the SFTP keys in api_keys, which never gain HTTP access. Effective access is
-- the token's actions and bots intersected with the owner's CURRENT
-- permissions. Only the SHA-256 of the token is stored.
CREATE TABLE automation_tokens (
    id               TEXT PRIMARY KEY NOT NULL,
    user_id          TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name             TEXT NOT NULL CHECK (length(trim(name)) BETWEEN 1 AND 64),
    prefix           TEXT NOT NULL,
    token_hash       BLOB NOT NULL UNIQUE CHECK (length(token_hash) = 32),
    actions          TEXT NOT NULL CHECK (length(actions) BETWEEN 1 AND 100),
    bot_ids          TEXT CHECK (bot_ids IS NULL OR length(bot_ids) <= 4000),
    created_at_ms    INTEGER NOT NULL,
    last_used_at_ms  INTEGER,
    expires_at_ms    INTEGER NOT NULL
) STRICT;
CREATE INDEX automation_tokens_user_idx ON automation_tokens(user_id);
