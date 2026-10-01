-- Target-scoped AI operator. Provider/search credentials are encrypted with
-- the panel keyring; no plaintext credential, prompt, source, or tool output
-- is written to the audit log.
CREATE TABLE ai_provider_profiles (
    id                  TEXT PRIMARY KEY,
    name                TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
    enabled             INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
    is_default          INTEGER NOT NULL DEFAULT 0 CHECK (is_default IN (0,1)),
    base_url            TEXT NOT NULL CHECK (length(base_url) BETWEEN 8 AND 500),
    chat_path           TEXT NOT NULL DEFAULT '/chat/completions' CHECK (length(chat_path) BETWEEN 1 AND 200),
    models_path         TEXT NOT NULL DEFAULT '/models' CHECK (length(models_path) BETWEEN 1 AND 200),
    default_model       TEXT NOT NULL CHECK (length(default_model) BETWEEN 1 AND 160),
    context_size        INTEGER CHECK (context_size IS NULL OR context_size BETWEEN 1024 AND 2000000),
    max_output_tokens   INTEGER NOT NULL DEFAULT 4096 CHECK (max_output_tokens BETWEEN 64 AND 131072),
    temperature         REAL NOT NULL DEFAULT 0.2 CHECK (temperature BETWEEN 0 AND 2),
    timeout_ms          INTEGER NOT NULL DEFAULT 120000 CHECK (timeout_ms BETWEEN 5000 AND 600000),
    input_price_micros  INTEGER CHECK (input_price_micros IS NULL OR input_price_micros >= 0),
    output_price_micros INTEGER CHECK (output_price_micros IS NULL OR output_price_micros >= 0),
    key_cipher          BLOB,
    key_nonce           BLOB,
    key_id              TEXT,
    created_at_ms       INTEGER NOT NULL,
    updated_at_ms       INTEGER NOT NULL,
    CHECK ((key_cipher IS NULL AND key_nonce IS NULL AND key_id IS NULL) OR
           (key_cipher IS NOT NULL AND key_nonce IS NOT NULL AND key_id IS NOT NULL))
) STRICT;
CREATE UNIQUE INDEX ai_provider_default_idx ON ai_provider_profiles(is_default) WHERE is_default = 1;

CREATE TABLE ai_conversations (
    id              TEXT PRIMARY KEY,
    creator_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    bot_id          TEXT REFERENCES bots(id) ON DELETE CASCADE,
    site_id         TEXT REFERENCES sites(id) ON DELETE CASCADE,
    title           TEXT NOT NULL DEFAULT 'New incident' CHECK (length(title) BETWEEN 1 AND 120),
    provider_id     TEXT REFERENCES ai_provider_profiles(id) ON DELETE SET NULL,
    model           TEXT CHECK (model IS NULL OR length(model) BETWEEN 1 AND 160),
    created_at_ms   INTEGER NOT NULL,
    updated_at_ms   INTEGER NOT NULL,
    CHECK ((bot_id IS NOT NULL) != (site_id IS NOT NULL))
) STRICT;
CREATE INDEX ai_conversation_creator_idx ON ai_conversations(creator_id, updated_at_ms DESC);
CREATE INDEX ai_conversation_bot_idx ON ai_conversations(bot_id, updated_at_ms DESC);
CREATE INDEX ai_conversation_site_idx ON ai_conversations(site_id, updated_at_ms DESC);

CREATE TABLE ai_messages (
    id              TEXT PRIMARY KEY,
    conversation_id TEXT NOT NULL REFERENCES ai_conversations(id) ON DELETE CASCADE,
    role            TEXT NOT NULL CHECK (role IN ('user','assistant','tool')),
    content         TEXT NOT NULL CHECK (length(content) <= 1048576),
    citations_json  TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(citations_json)),
    created_at_ms   INTEGER NOT NULL
) STRICT;
CREATE INDEX ai_message_conversation_idx ON ai_messages(conversation_id, created_at_ms);

CREATE TABLE ai_runs (
    id                  TEXT PRIMARY KEY,
    conversation_id     TEXT NOT NULL REFERENCES ai_conversations(id) ON DELETE CASCADE,
    user_id             TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider_id         TEXT REFERENCES ai_provider_profiles(id) ON DELETE SET NULL,
    model               TEXT NOT NULL CHECK (length(model) BETWEEN 1 AND 160),
    mode                TEXT NOT NULL CHECK (mode IN ('approval','auto')),
    status              TEXT NOT NULL CHECK (status IN ('queued','running','waiting_approval','completed','failed','cancelled','interrupted')),
    limits_json         TEXT NOT NULL CHECK (json_valid(limits_json)),
    plan_json           TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(plan_json)),
    auto_approved_at_ms INTEGER,
    input_tokens        INTEGER NOT NULL DEFAULT 0,
    output_tokens       INTEGER NOT NULL DEFAULT 0,
    error_code          TEXT,
    error_message       TEXT CHECK (error_message IS NULL OR length(error_message) <= 1000),
    created_at_ms       INTEGER NOT NULL,
    started_at_ms       INTEGER,
    finished_at_ms      INTEGER
) STRICT;
CREATE INDEX ai_run_conversation_idx ON ai_runs(conversation_id, created_at_ms DESC);
CREATE INDEX ai_run_status_idx ON ai_runs(status, created_at_ms);

CREATE TABLE ai_tool_calls (
    id                  TEXT PRIMARY KEY,
    run_id              TEXT NOT NULL REFERENCES ai_runs(id) ON DELETE CASCADE,
    call_index          INTEGER NOT NULL,
    name                TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
    arguments_json      TEXT NOT NULL CHECK (json_valid(arguments_json) AND length(arguments_json) <= 65536),
    output              TEXT NOT NULL DEFAULT '' CHECK (length(output) <= 1048576),
    approval_state      TEXT NOT NULL DEFAULT 'not_required' CHECK (approval_state IN ('not_required','pending','approved','rejected')),
    status              TEXT NOT NULL DEFAULT 'proposed' CHECK (status IN ('proposed','running','completed','failed','cancelled')),
    exit_code           INTEGER,
    duration_ms         INTEGER,
    error_message       TEXT CHECK (error_message IS NULL OR length(error_message) <= 1000),
    created_at_ms       INTEGER NOT NULL,
    finished_at_ms      INTEGER,
    UNIQUE(run_id, call_index, id)
) STRICT;
CREATE INDEX ai_tool_call_run_idx ON ai_tool_calls(run_id, created_at_ms);

CREATE TABLE ai_change_sets (
    id                  TEXT PRIMARY KEY,
    run_id              TEXT NOT NULL REFERENCES ai_runs(id) ON DELETE CASCADE,
    target_kind         TEXT NOT NULL CHECK (target_kind IN ('bot','site')),
    target_id           TEXT NOT NULL,
    status              TEXT NOT NULL CHECK (status IN ('draft','approved','applied','conflicted','reverted','failed')),
    summary             TEXT NOT NULL DEFAULT '' CHECK (length(summary) <= 2000),
    created_at_ms       INTEGER NOT NULL,
    applied_at_ms       INTEGER,
    reverted_at_ms      INTEGER
) STRICT;
CREATE INDEX ai_change_set_run_idx ON ai_change_sets(run_id, created_at_ms);

CREATE TABLE ai_change_files (
    change_set_id       TEXT NOT NULL REFERENCES ai_change_sets(id) ON DELETE CASCADE,
    path                TEXT NOT NULL CHECK (length(path) BETWEEN 1 AND 500),
    operation           TEXT NOT NULL CHECK (operation IN ('add','modify','delete')),
    before_gzip         BLOB,
    after_gzip          BLOB,
    before_revision     TEXT,
    after_revision      TEXT,
    mode                INTEGER NOT NULL DEFAULT 420,
    diff                TEXT NOT NULL CHECK (length(diff) <= 2097152),
    PRIMARY KEY(change_set_id, path)
) STRICT;

-- Site actions remain identifiable after a site is removed, just like bot
-- audit records, so this deliberately has no foreign key.
ALTER TABLE audit_events ADD COLUMN site_id TEXT;
ALTER TABLE audit_events ADD COLUMN site_name TEXT CHECK (site_name IS NULL OR length(site_name) <= 80);
CREATE INDEX audit_site_idx ON audit_events(site_id, at_ms);
