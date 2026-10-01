-- The AI assistant becomes one panel-wide chat instead of one workspace per
-- bot or site. A conversation now belongs to its creator only; the bot or site
-- a message is about is chosen per run from what the person was viewing, and
-- each run records that target. Existing conversations keep their target.
--
-- ai_conversations has to be rebuilt to drop its "exactly one target" CHECK.
-- Dropping it cascades into everything below it, so the whole conversation
-- subtree is stashed first and restored in dependency order.
CREATE TABLE ai_conversations_stash AS SELECT * FROM ai_conversations;
CREATE TABLE ai_messages_stash AS SELECT * FROM ai_messages;
CREATE TABLE ai_runs_stash AS SELECT * FROM ai_runs;
CREATE TABLE ai_tool_calls_stash AS SELECT * FROM ai_tool_calls;
CREATE TABLE ai_change_sets_stash AS SELECT * FROM ai_change_sets;
CREATE TABLE ai_change_files_stash AS SELECT * FROM ai_change_files;

DROP TABLE ai_conversations;

CREATE TABLE ai_conversations (
    id              TEXT PRIMARY KEY,
    creator_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    bot_id          TEXT REFERENCES bots(id) ON DELETE CASCADE,
    site_id         TEXT REFERENCES sites(id) ON DELETE CASCADE,
    title           TEXT NOT NULL DEFAULT 'New chat' CHECK (length(title) BETWEEN 1 AND 120),
    provider_id     TEXT REFERENCES ai_provider_profiles(id) ON DELETE SET NULL,
    model           TEXT CHECK (model IS NULL OR length(model) BETWEEN 1 AND 160),
    created_at_ms   INTEGER NOT NULL,
    updated_at_ms   INTEGER NOT NULL,
    CHECK (bot_id IS NULL OR site_id IS NULL)
) STRICT;
CREATE INDEX ai_conversation_creator_idx ON ai_conversations(creator_id, updated_at_ms DESC);
CREATE INDEX ai_conversation_bot_idx ON ai_conversations(bot_id, updated_at_ms DESC);
CREATE INDEX ai_conversation_site_idx ON ai_conversations(site_id, updated_at_ms DESC);

-- What the person was looking at when they sent a message, shown on the
-- message and given to the model. Never used for authorization.
ALTER TABLE ai_messages ADD COLUMN context_json TEXT NOT NULL DEFAULT '{}'
    CHECK (json_valid(context_json) AND length(context_json) <= 4096);
-- The bot or site a run works on. A run without either has no target tools
-- until the model focuses one.
ALTER TABLE ai_runs ADD COLUMN bot_id TEXT REFERENCES bots(id) ON DELETE SET NULL;
ALTER TABLE ai_runs ADD COLUMN site_id TEXT REFERENCES sites(id) ON DELETE SET NULL;

INSERT INTO ai_conversations (id, creator_id, bot_id, site_id, title, provider_id, model, created_at_ms, updated_at_ms)
SELECT id, creator_id, bot_id, site_id, title, provider_id, model, created_at_ms, updated_at_ms FROM ai_conversations_stash;

INSERT INTO ai_messages (id, conversation_id, role, content, citations_json, created_at_ms)
SELECT id, conversation_id, role, content, citations_json, created_at_ms FROM ai_messages_stash;

INSERT INTO ai_runs (id, conversation_id, user_id, provider_id, model, mode, status, limits_json, plan_json, auto_approved_at_ms,
    input_tokens, output_tokens, error_code, error_message, created_at_ms, started_at_ms, finished_at_ms, bot_id, site_id)
SELECT r.id, r.conversation_id, r.user_id, r.provider_id, r.model, r.mode, r.status, r.limits_json, r.plan_json, r.auto_approved_at_ms,
    r.input_tokens, r.output_tokens, r.error_code, r.error_message, r.created_at_ms, r.started_at_ms, r.finished_at_ms, c.bot_id, c.site_id
FROM ai_runs_stash r JOIN ai_conversations_stash c ON c.id = r.conversation_id;

INSERT INTO ai_tool_calls (id, run_id, call_index, name, arguments_json, output, approval_state, status, exit_code, duration_ms,
    error_message, created_at_ms, finished_at_ms, provider_call_id)
SELECT id, run_id, call_index, name, arguments_json, output, approval_state, status, exit_code, duration_ms,
    error_message, created_at_ms, finished_at_ms, provider_call_id FROM ai_tool_calls_stash;

INSERT INTO ai_change_sets (id, run_id, target_kind, target_id, status, summary, created_at_ms, applied_at_ms, reverted_at_ms)
SELECT id, run_id, target_kind, target_id, status, summary, created_at_ms, applied_at_ms, reverted_at_ms FROM ai_change_sets_stash;

INSERT INTO ai_change_files (change_set_id, path, operation, before_gzip, after_gzip, before_revision, after_revision, mode, diff)
SELECT change_set_id, path, operation, before_gzip, after_gzip, before_revision, after_revision, mode, diff FROM ai_change_files_stash;

DROP TABLE ai_change_files_stash;
DROP TABLE ai_change_sets_stash;
DROP TABLE ai_tool_calls_stash;
DROP TABLE ai_runs_stash;
DROP TABLE ai_messages_stash;
DROP TABLE ai_conversations_stash;

CREATE INDEX ai_run_target_idx ON ai_runs(bot_id, status);
