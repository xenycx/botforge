CREATE TABLE users (
    id                  TEXT PRIMARY KEY NOT NULL,
    email               TEXT NOT NULL COLLATE NOCASE UNIQUE,
    password_hash       TEXT NOT NULL,
    role                TEXT NOT NULL DEFAULT 'user'
                        CHECK (role IN ('admin', 'user')),
    disabled            INTEGER NOT NULL DEFAULT 0
                        CHECK (disabled IN (0, 1)),
    created_at_ms       INTEGER NOT NULL,
    updated_at_ms       INTEGER NOT NULL
) STRICT;

CREATE TABLE sessions (
    token_hash          BLOB PRIMARY KEY NOT NULL
                        CHECK (length(token_hash) = 32),
    user_id             TEXT NOT NULL
                        REFERENCES users(id) ON DELETE CASCADE,
    created_at_ms       INTEGER NOT NULL,
    expires_at_ms       INTEGER NOT NULL,
    CHECK (expires_at_ms > created_at_ms)
) STRICT;

CREATE INDEX sessions_user_idx ON sessions(user_id);
CREATE INDEX sessions_expiry_idx ON sessions(expires_at_ms);

CREATE TABLE nodes (
    id                  TEXT PRIMARY KEY NOT NULL,
    name                TEXT NOT NULL UNIQUE,
    transport           TEXT NOT NULL
                        CHECK (transport IN ('local', 'https')),
    endpoint            TEXT,
    enabled             INTEGER NOT NULL DEFAULT 1
                        CHECK (enabled IN (0, 1)),
    last_seen_at_ms      INTEGER,
    created_at_ms       INTEGER NOT NULL,
    updated_at_ms       INTEGER NOT NULL,
    CHECK (
        (transport = 'local' AND endpoint IS NULL)
        OR
        (transport = 'https'
         AND endpoint IS NOT NULL
         AND length(endpoint) > 0)
    )
) STRICT;

CREATE TABLE bots (
    id                  TEXT PRIMARY KEY NOT NULL,
    owner_id            TEXT NOT NULL
                        REFERENCES users(id) ON DELETE RESTRICT,
    node_id             TEXT NOT NULL
                        REFERENCES nodes(id) ON DELETE RESTRICT,
    name                TEXT NOT NULL
                        CHECK (length(trim(name)) BETWEEN 1 AND 64),
    runtime             TEXT NOT NULL
                        CHECK (runtime IN (
                            'nodejs', 'python', 'rust',
                            'go', 'java', 'ruby'
                        )),
    image_ref           TEXT NOT NULL,
    argv_json           TEXT NOT NULL
                        CHECK (
                            json_valid(argv_json)
                            AND json_type(argv_json) = 'array'
                            AND json_array_length(argv_json) > 0
                        ),
    memory_bytes        INTEGER NOT NULL
                        CHECK (memory_bytes > 0),
    nano_cpus           INTEGER NOT NULL
                        CHECK (nano_cpus > 0),
    pids_limit          INTEGER NOT NULL DEFAULT 128
                        CHECK (pids_limit BETWEEN 1 AND 4096),
    desired_state       TEXT NOT NULL DEFAULT 'stopped'
                        CHECK (desired_state IN (
                            'stopped', 'running', 'deleted'
                        )),
    observed_state      TEXT NOT NULL DEFAULT 'stopped'
                        CHECK (observed_state IN (
                            'unknown', 'stopped', 'building',
                            'starting', 'running', 'stopping', 'failed'
                        )),
    generation          INTEGER NOT NULL DEFAULT 0
                        CHECK (generation >= 0),
    observed_generation INTEGER NOT NULL DEFAULT 0
                        CHECK (
                            observed_generation >= 0
                            AND observed_generation <= generation
                        ),
    container_id        TEXT UNIQUE,
    last_exit_code      INTEGER,
    last_error          TEXT,
    observed_at_ms      INTEGER,
    created_at_ms       INTEGER NOT NULL,
    updated_at_ms       INTEGER NOT NULL
) STRICT;

CREATE INDEX bots_owner_idx ON bots(owner_id);
CREATE INDEX bots_node_state_idx ON bots(node_id, desired_state);

CREATE TABLE bot_env_vars (
    bot_id              TEXT NOT NULL
                        REFERENCES bots(id) ON DELETE CASCADE,
    name                TEXT NOT NULL
                        CHECK (
                            length(name) > 0
                            AND name GLOB '[A-Za-z_]*'
                            AND name NOT GLOB '*[^A-Za-z0-9_]*'
                        ),
    ciphertext          BLOB NOT NULL
                        CHECK (length(ciphertext) >= 16),
    nonce               BLOB NOT NULL
                        CHECK (length(nonce) = 12),
    key_id              TEXT NOT NULL,
    created_at_ms       INTEGER NOT NULL,
    updated_at_ms       INTEGER NOT NULL,
    PRIMARY KEY (bot_id, name)
) STRICT, WITHOUT ROWID;

CREATE TABLE node_telemetry (
    node_id             TEXT NOT NULL
                        REFERENCES nodes(id) ON DELETE CASCADE,
    sampled_at_ms       INTEGER NOT NULL,
    cpu_percent         REAL NOT NULL
                        CHECK (cpu_percent BETWEEN 0 AND 100),
    logical_cpus        INTEGER NOT NULL
                        CHECK (logical_cpus > 0),
    memory_used_bytes   INTEGER NOT NULL
                        CHECK (memory_used_bytes >= 0),
    memory_total_bytes  INTEGER NOT NULL
                        CHECK (memory_total_bytes > 0),
    disk_used_bytes     INTEGER NOT NULL
                        CHECK (disk_used_bytes >= 0),
    disk_total_bytes    INTEGER NOT NULL
                        CHECK (disk_total_bytes > 0),
    running_bots        INTEGER NOT NULL
                        CHECK (running_bots >= 0),
    PRIMARY KEY (node_id, sampled_at_ms),
    CHECK (memory_used_bytes <= memory_total_bytes),
    CHECK (disk_used_bytes <= disk_total_bytes)
) STRICT, WITHOUT ROWID;

CREATE INDEX node_telemetry_retention_idx
    ON node_telemetry(sampled_at_ms);
