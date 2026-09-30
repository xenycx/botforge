-- Per-bot switch for scheduled backups (on by default).
ALTER TABLE bots ADD COLUMN auto_backup INTEGER NOT NULL DEFAULT 1
    CHECK (auto_backup IN (0, 1));
