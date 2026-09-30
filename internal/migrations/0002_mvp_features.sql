-- MVP feature schema: OAuth identities, GitHub deployments, sub-user sharing,
-- backups, bot-to-panel telemetry, API keys, ports, startup/network/restart
-- settings. Secrets (OAuth tokens, webhook secrets) are AES-256-GCM sealed with
-- the same keyring as bot_env_vars; only SHA-256 hashes of API keys are stored.
--
-- Users who sign in only through OAuth have password_hash = '' (no local
-- password). Password verification always fails for the empty hash.

CREATE TABLE oauth_accounts (
    provider            TEXT NOT NULL
                        CHECK (provider IN ('github', 'discord')),
    provider_user_id    TEXT NOT NULL CHECK (length(provider_user_id) > 0),
    user_id             TEXT NOT NULL
                        REFERENCES users(id) ON DELETE CASCADE,
    username            TEXT NOT NULL,
    email               TEXT,
    avatar_url          TEXT,
    scopes              TEXT NOT NULL DEFAULT '',
    token_ciphertext    BLOB,
    token_nonce         BLOB,
    token_key_id        TEXT,
    notify_enabled      INTEGER NOT NULL DEFAULT 1
                        CHECK (notify_enabled IN (0, 1)),
    created_at_ms       INTEGER NOT NULL,
    updated_at_ms       INTEGER NOT NULL,
    PRIMARY KEY (provider, provider_user_id),
    -- one linked account per provider per user
    UNIQUE (user_id, provider),
    CHECK (
        (token_ciphertext IS NULL AND token_nonce IS NULL AND token_key_id IS NULL)
        OR
        (token_ciphertext IS NOT NULL AND length(token_ciphertext) >= 16
         AND token_nonce IS NOT NULL AND length(token_nonce) = 12
         AND token_key_id IS NOT NULL)
    )
) STRICT, WITHOUT ROWID;

CREATE TABLE api_keys (
    id                  TEXT PRIMARY KEY NOT NULL,
    user_id             TEXT NOT NULL
                        REFERENCES users(id) ON DELETE CASCADE,
    name                TEXT NOT NULL
                        CHECK (length(trim(name)) BETWEEN 1 AND 64),
    prefix              TEXT NOT NULL,
    token_hash          BLOB NOT NULL UNIQUE
                        CHECK (length(token_hash) = 32),
    scope               TEXT NOT NULL DEFAULT 'sftp'
                        CHECK (scope IN ('sftp')),
    created_at_ms       INTEGER NOT NULL,
    last_used_at_ms     INTEGER,
    expires_at_ms       INTEGER
) STRICT;

CREATE INDEX api_keys_user_idx ON api_keys(user_id);

-- Bot-level sharing. permissions is a bitmask:
--   1 view_console, 2 power, 4 edit_files, 8 manage_env, 16 full_admin
-- full_admin implies every other permission (enforced in Go).
CREATE TABLE bot_subusers (
    bot_id              TEXT NOT NULL
                        REFERENCES bots(id) ON DELETE CASCADE,
    user_id             TEXT NOT NULL
                        REFERENCES users(id) ON DELETE CASCADE,
    permissions         INTEGER NOT NULL
                        CHECK (permissions BETWEEN 1 AND 31),
    invited_by          TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at_ms       INTEGER NOT NULL,
    updated_at_ms       INTEGER NOT NULL,
    PRIMARY KEY (bot_id, user_id)
) STRICT, WITHOUT ROWID;

CREATE INDEX bot_subusers_user_idx ON bot_subusers(user_id);

-- One GitHub source per bot. token_user_id is the panel user whose linked
-- GitHub token is used to clone/pull. webhook_secret_* seals the per-repo HMAC
-- secret (needed in plaintext to verify X-Hub-Signature-256).
CREATE TABLE github_repos (
    bot_id              TEXT PRIMARY KEY NOT NULL
                        REFERENCES bots(id) ON DELETE CASCADE,
    token_user_id       TEXT NOT NULL
                        REFERENCES users(id) ON DELETE RESTRICT,
    full_name           TEXT NOT NULL COLLATE NOCASE
                        CHECK (full_name GLOB '?*/?*'),
    branch              TEXT NOT NULL CHECK (length(branch) BETWEEN 1 AND 255),
    root_dir            TEXT NOT NULL DEFAULT '',
    private             INTEGER NOT NULL DEFAULT 0 CHECK (private IN (0, 1)),
    auto_deploy         INTEGER NOT NULL DEFAULT 0 CHECK (auto_deploy IN (0, 1)),
    webhook_secret_ciphertext BLOB NOT NULL
                        CHECK (length(webhook_secret_ciphertext) >= 16),
    webhook_secret_nonce BLOB NOT NULL
                        CHECK (length(webhook_secret_nonce) = 12),
    webhook_secret_key_id TEXT NOT NULL,
    last_deployed_sha   TEXT,
    last_deployed_at_ms INTEGER,
    last_error          TEXT,
    created_at_ms       INTEGER NOT NULL,
    updated_at_ms       INTEGER NOT NULL
) STRICT;

CREATE INDEX github_repos_lookup_idx ON github_repos(full_name, auto_deploy);

-- Archive metadata. file_name is server-generated and relative to
-- <data>/backups/<bot_id>/; it is never taken from a client.
CREATE TABLE bot_backups (
    id                  TEXT PRIMARY KEY NOT NULL,
    bot_id              TEXT NOT NULL
                        REFERENCES bots(id) ON DELETE CASCADE,
    kind                TEXT NOT NULL
                        CHECK (kind IN ('manual', 'auto', 'pre_restore')),
    status              TEXT NOT NULL DEFAULT 'creating'
                        CHECK (status IN ('creating', 'ready', 'failed')),
    file_name           TEXT NOT NULL
                        CHECK (file_name NOT GLOB '*[/\]*' AND file_name NOT LIKE '.%'),
    size_bytes          INTEGER NOT NULL DEFAULT 0 CHECK (size_bytes >= 0),
    sha256_hex          TEXT,
    includes_env        INTEGER NOT NULL DEFAULT 1 CHECK (includes_env IN (0, 1)),
    error               TEXT,
    created_by          TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at_ms       INTEGER NOT NULL,
    UNIQUE (bot_id, file_name)
) STRICT;

CREATE INDEX bot_backups_bot_idx ON bot_backups(bot_id, created_at_ms);

-- Bot-pushed metrics/events. Bounded by count-per-bot and time retention,
-- pruned in batches. payload_json is size-capped in Go before insert.
CREATE TABLE bot_telemetry_logs (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    bot_id              TEXT NOT NULL
                        REFERENCES bots(id) ON DELETE CASCADE,
    recorded_at_ms      INTEGER NOT NULL,
    kind                TEXT NOT NULL
                        CHECK (kind IN ('stat', 'command', 'event')),
    name                TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 64),
    value               REAL,
    payload_json        TEXT CHECK (
                            payload_json IS NULL
                            OR (json_valid(payload_json) AND length(payload_json) <= 2048)
                        )
) STRICT;

CREATE INDEX bot_telemetry_logs_bot_idx
    ON bot_telemetry_logs(bot_id, kind, name, recorded_at_ms);
CREATE INDEX bot_telemetry_logs_retention_idx
    ON bot_telemetry_logs(recorded_at_ms);

-- Published ports. Empty by default (no ports published). Host ports are
-- unique per protocol because Phase 1 is a single host.
CREATE TABLE bot_ports (
    bot_id              TEXT NOT NULL
                        REFERENCES bots(id) ON DELETE CASCADE,
    container_port      INTEGER NOT NULL CHECK (container_port BETWEEN 1 AND 65535),
    host_port           INTEGER NOT NULL CHECK (host_port BETWEEN 1024 AND 65535),
    protocol            TEXT NOT NULL DEFAULT 'tcp'
                        CHECK (protocol IN ('tcp', 'udp')),
    host_ip             TEXT NOT NULL DEFAULT '127.0.0.1',
    created_at_ms       INTEGER NOT NULL,
    PRIMARY KEY (bot_id, container_port, protocol),
    UNIQUE (host_ip, host_port, protocol)
) STRICT, WITHOUT ROWID;

-- Startup, network, restart-policy, source and telemetry-key settings.
-- Changing any of these advances bots.generation (in Go) like other config.
ALTER TABLE bots ADD COLUMN entrypoint_json TEXT
    CHECK (entrypoint_json IS NULL OR (
        json_valid(entrypoint_json) AND json_type(entrypoint_json) = 'array'));
ALTER TABLE bots ADD COLUMN source_type TEXT NOT NULL DEFAULT 'manual'
    CHECK (source_type IN ('manual', 'template', 'github'));
ALTER TABLE bots ADD COLUMN template_id TEXT;
ALTER TABLE bots ADD COLUMN network_enabled INTEGER NOT NULL DEFAULT 1
    CHECK (network_enabled IN (0, 1));
-- Intent only: Docker has no native bandwidth cap; enforcement needs tc/plugin.
ALTER TABLE bots ADD COLUMN bandwidth_kbps INTEGER
    CHECK (bandwidth_kbps IS NULL OR bandwidth_kbps > 0);
ALTER TABLE bots ADD COLUMN restart_policy TEXT NOT NULL DEFAULT 'never'
    CHECK (restart_policy IN ('never', 'on_failure'));
ALTER TABLE bots ADD COLUMN restart_max_attempts INTEGER NOT NULL DEFAULT 5
    CHECK (restart_max_attempts BETWEEN 0 AND 100);
ALTER TABLE bots ADD COLUMN restart_backoff_initial_ms INTEGER NOT NULL DEFAULT 2000
    CHECK (restart_backoff_initial_ms BETWEEN 100 AND 3600000);
ALTER TABLE bots ADD COLUMN restart_backoff_max_ms INTEGER NOT NULL DEFAULT 300000
    CHECK (restart_backoff_max_ms BETWEEN 100 AND 3600000);
ALTER TABLE bots ADD COLUMN telemetry_key_hash BLOB
    CHECK (telemetry_key_hash IS NULL OR length(telemetry_key_hash) = 32);
CREATE UNIQUE INDEX bots_telemetry_key_idx ON bots(telemetry_key_hash)
    WHERE telemetry_key_hash IS NOT NULL;
