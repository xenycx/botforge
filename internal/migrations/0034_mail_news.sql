-- Announcements: a per-account switch for optional news emails. Notices about
-- policy and security are not covered by it. Existing accounts default to on,
-- which only matters once an administrator sends an announcement.
ALTER TABLE users ADD COLUMN email_news INTEGER NOT NULL DEFAULT 1 CHECK (email_news IN (0, 1));
