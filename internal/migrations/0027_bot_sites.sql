-- A site may be the public page for one Discord bot. Existing standalone
-- sites stay in files mode; bot pages use the safe generated page shell and
-- only expose widgets after a developer explicitly enables them.
ALTER TABLE sites ADD COLUMN bot_id TEXT
    REFERENCES bots(id) ON DELETE CASCADE;
ALTER TABLE sites ADD COLUMN mode TEXT NOT NULL DEFAULT 'files'
    CHECK (mode IN ('page', 'files'));
ALTER TABLE sites ADD COLUMN page_title TEXT NOT NULL DEFAULT ''
    CHECK (length(page_title) <= 80);
ALTER TABLE sites ADD COLUMN page_description TEXT NOT NULL DEFAULT ''
    CHECK (length(page_description) <= 500);
ALTER TABLE sites ADD COLUMN page_theme TEXT NOT NULL DEFAULT 'midnight'
    CHECK (page_theme IN ('midnight', 'daylight', 'system'));
ALTER TABLE sites ADD COLUMN page_accent TEXT NOT NULL DEFAULT '#5865f2'
    CHECK (length(page_accent) = 7);
ALTER TABLE sites ADD COLUMN page_html TEXT NOT NULL DEFAULT ''
    CHECK (length(page_html) <= 65536);
ALTER TABLE sites ADD COLUMN page_css TEXT NOT NULL DEFAULT ''
    CHECK (length(page_css) <= 32768);
ALTER TABLE sites ADD COLUMN widgets_public INTEGER NOT NULL DEFAULT 0
    CHECK (widgets_public IN (0, 1));

CREATE UNIQUE INDEX sites_bot_idx ON sites(bot_id) WHERE bot_id IS NOT NULL;
