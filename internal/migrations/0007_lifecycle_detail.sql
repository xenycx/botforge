-- Explicit lifecycle detail written by the runner, so clients never infer the
-- state from error text. restart_count is the consecutive-crash counter of the
-- current generation (it survives panel restarts); next_retry_at_ms is set
-- while waiting to retry; state_reason is a short machine code (see
-- domain.Reason*), NULL when the observed state needs no explanation.
ALTER TABLE bots ADD COLUMN restart_count INTEGER NOT NULL DEFAULT 0
    CHECK (restart_count >= 0);
ALTER TABLE bots ADD COLUMN next_retry_at_ms INTEGER;
ALTER TABLE bots ADD COLUMN state_reason TEXT
    CHECK (state_reason IS NULL OR length(state_reason) BETWEEN 1 AND 32);
-- When the bot last became running (set on the transition, not on every
-- observation), so the panel can tell "has run successfully" and show uptime.
ALTER TABLE bots ADD COLUMN last_started_at_ms INTEGER;
