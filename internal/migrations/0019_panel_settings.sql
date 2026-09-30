-- Settings chosen in the setup wizard or the administration page. Environment
-- variables still take precedence. Secret values (OAuth client secrets) are
-- sealed like other secrets: namespace "settings", name = key.
CREATE TABLE panel_settings (
    key            TEXT PRIMARY KEY NOT NULL CHECK (length(key) BETWEEN 1 AND 64),
    value          TEXT CHECK (value IS NULL OR length(value) <= 2048),
    secret_cipher  BLOB,
    secret_nonce   BLOB,
    secret_key_id  TEXT,
    updated_at_ms  INTEGER NOT NULL,
    CHECK ((secret_cipher IS NULL) = (secret_nonce IS NULL) AND (secret_cipher IS NULL) = (secret_key_id IS NULL))
) STRICT;
