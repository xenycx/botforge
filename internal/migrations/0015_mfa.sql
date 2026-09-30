-- Two-step sign-in (TOTP). The secret is sealed like other secrets
-- (namespace "mfa:<user>", name "totp"); enabled_at_ms is NULL while an
-- enrollment waits for its first code. last_step blocks code replay.
CREATE TABLE user_mfa (
    user_id        TEXT PRIMARY KEY NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    secret_cipher  BLOB NOT NULL,
    secret_nonce   BLOB NOT NULL,
    secret_key_id  TEXT NOT NULL,
    enabled_at_ms  INTEGER,
    last_step      INTEGER NOT NULL DEFAULT 0,
    created_at_ms  INTEGER NOT NULL
) STRICT;

-- One-use recovery codes; only SHA-256 hashes are stored.
CREATE TABLE mfa_recovery_codes (
    user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash   BLOB NOT NULL,
    used_at_ms  INTEGER,
    PRIMARY KEY (user_id, code_hash)
) STRICT;
