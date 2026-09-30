-- Fleet organization. Tags are shared bot metadata (edited by people with
-- full access); favorites are a personal preference per user.
CREATE TABLE bot_tags (
    bot_id              TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
    tag                 TEXT NOT NULL CHECK (length(tag) BETWEEN 1 AND 24),
    PRIMARY KEY (bot_id, tag)
) STRICT, WITHOUT ROWID;
CREATE INDEX bot_tags_tag_idx ON bot_tags(tag);

CREATE TABLE user_bot_favorites (
    user_id             TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    bot_id              TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
    created_at_ms       INTEGER NOT NULL,
    PRIMARY KEY (user_id, bot_id)
) STRICT, WITHOUT ROWID;
