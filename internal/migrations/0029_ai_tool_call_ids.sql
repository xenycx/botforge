-- Tool-call rows are keyed by a panel-generated UUID. Providers reuse short
-- call ids (for example "call_0") across runs, so the provider's id is kept
-- separately and only echoed back in the tool-result message.
ALTER TABLE ai_tool_calls ADD COLUMN provider_call_id TEXT
    CHECK (provider_call_id IS NULL OR length(provider_call_id) BETWEEN 1 AND 200);
UPDATE ai_tool_calls SET provider_call_id = id WHERE call_index >= 0 AND length(id) <= 200;
