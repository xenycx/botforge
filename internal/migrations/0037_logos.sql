-- Custom logos for bots and sites. A bot shows its custom logo, else the
-- Discord avatar (from telemetry or fetched with the bot's token); a site
-- shows its custom logo, else the favicon of its active release, else its
-- bot's logo. Images are small (resized in the browser) PNG or JPEG files.
ALTER TABLE bots ADD COLUMN logo BLOB CHECK (logo IS NULL OR length(logo) <= 262144);
ALTER TABLE bots ADD COLUMN logo_type TEXT CHECK (logo_type IS NULL OR logo_type IN ('image/png', 'image/jpeg'));
ALTER TABLE bots ADD COLUMN logo_updated_at_ms INTEGER;
ALTER TABLE sites ADD COLUMN logo BLOB CHECK (logo IS NULL OR length(logo) <= 262144);
ALTER TABLE sites ADD COLUMN logo_type TEXT CHECK (logo_type IS NULL OR logo_type IN ('image/png', 'image/jpeg'));
ALTER TABLE sites ADD COLUMN logo_updated_at_ms INTEGER;
