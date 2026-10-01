-- Email: one-use password-reset links (only the SHA-256 of the link token is
-- stored, at most one outstanding per account) and a per-account switch for
-- alert emails. Existing accounts default to receiving alert emails, which only
-- happens once an administrator configures Mailgun.
CREATE TABLE password_resets (
    id             TEXT PRIMARY KEY NOT NULL,
    user_id        TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash     BLOB NOT NULL UNIQUE CHECK (length(token_hash) = 32),
    created_at_ms  INTEGER NOT NULL,
    expires_at_ms  INTEGER NOT NULL
) STRICT;
CREATE INDEX password_resets_user_idx ON password_resets(user_id);
ALTER TABLE users ADD COLUMN email_alerts INTEGER NOT NULL DEFAULT 1 CHECK (email_alerts IN (0, 1));
