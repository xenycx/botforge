-- Environment variables an administrator changed from the panel. They are
-- layered over the process environment when the panel starts, so a change needs
-- a restart. Secret values (the metrics token) are sealed like other secrets:
-- namespace "env", name = variable name. Boot variables and the ones the panel
-- stores elsewhere (sign-in providers) are never read from this table.
CREATE TABLE env_overrides (
    name           TEXT PRIMARY KEY NOT NULL CHECK (length(name) BETWEEN 1 AND 64),
    value          TEXT CHECK (value IS NULL OR length(value) <= 1024),
    secret_cipher  BLOB,
    secret_nonce   BLOB,
    secret_key_id  TEXT,
    updated_at_ms  INTEGER NOT NULL,
    updated_by     TEXT NOT NULL DEFAULT '',
    CHECK ((secret_cipher IS NULL) = (secret_nonce IS NULL) AND (secret_cipher IS NULL) = (secret_key_id IS NULL))
) STRICT;
