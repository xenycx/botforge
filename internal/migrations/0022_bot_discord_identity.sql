ALTER TABLE bots ADD COLUMN discord_user_id TEXT NOT NULL DEFAULT '' CHECK (length(discord_user_id) <= 32);
ALTER TABLE bots ADD COLUMN discord_username TEXT NOT NULL DEFAULT '' CHECK (length(discord_username) <= 80);
ALTER TABLE bots ADD COLUMN discord_avatar_url TEXT NOT NULL DEFAULT '' CHECK (length(discord_avatar_url) <= 512);
