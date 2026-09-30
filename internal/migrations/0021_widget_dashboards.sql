-- Dashboard widgets are still declarative and inert, but may now be grouped,
-- sized and expired. Rebuilding the table also expands the safe kind enum.
CREATE TABLE bot_widgets_v2 (
    bot_id        TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
    widget_key    TEXT NOT NULL CHECK (length(widget_key) BETWEEN 1 AND 64),
    kind          TEXT NOT NULL CHECK (kind IN (
        'metric','status','progress','text','chart','table','link',
        'line','area','donut','gauge','heatmap','sparkline','kv','markdown','image','log','code'
    )),
    title         TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 80),
    group_name    TEXT NOT NULL DEFAULT 'Overview' CHECK (length(group_name) BETWEEN 1 AND 48),
    span          INTEGER NOT NULL DEFAULT 1 CHECK (span BETWEEN 1 AND 3),
    min_height    INTEGER NOT NULL DEFAULT 0 CHECK (min_height BETWEEN 0 AND 800),
    position      INTEGER NOT NULL DEFAULT 0 CHECK (position BETWEEN 0 AND 1000),
    payload_json  TEXT NOT NULL CHECK (json_valid(payload_json) AND length(payload_json) <= 4096),
    updated_at_ms INTEGER NOT NULL,
    expires_at_ms INTEGER,
    PRIMARY KEY (bot_id, widget_key)
) STRICT, WITHOUT ROWID;

INSERT INTO bot_widgets_v2
    (bot_id,widget_key,kind,title,position,payload_json,updated_at_ms)
SELECT bot_id,widget_key,kind,title,position,payload_json,updated_at_ms FROM bot_widgets;

DROP TABLE bot_widgets;
ALTER TABLE bot_widgets_v2 RENAME TO bot_widgets;
CREATE INDEX bot_widgets_order_idx ON bot_widgets(bot_id,group_name,position,widget_key);
CREATE INDEX bot_widgets_expiry_idx ON bot_widgets(expires_at_ms) WHERE expires_at_ms IS NOT NULL;
