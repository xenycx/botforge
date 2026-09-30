-- Backup labels, explicit verification results, and whether the bot was
-- stopped while the archive was written (an application-consistent copy).
ALTER TABLE bot_backups ADD COLUMN label TEXT
    CHECK (label IS NULL OR length(label) BETWEEN 1 AND 80);
ALTER TABLE bot_backups ADD COLUMN verified_at_ms INTEGER;
ALTER TABLE bot_backups ADD COLUMN verify_error TEXT
    CHECK (verify_error IS NULL OR length(verify_error) <= 300);
ALTER TABLE bot_backups ADD COLUMN consistent INTEGER NOT NULL DEFAULT 0
    CHECK (consistent IN (0, 1));
