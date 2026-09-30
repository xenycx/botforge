package sqlite

import (
	"botpanel/internal/domain"
	"context"
)

func (db *DB) UpsertBotWidgets(ctx context.Context, widgets []domain.BotWidget) error {
	if len(widgets) == 0 {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, w := range widgets {
		_, err = tx.ExecContext(ctx, `INSERT INTO bot_widgets (bot_id,widget_key,kind,title,position,payload_json,updated_at_ms) VALUES (?,?,?,?,?,?,?) ON CONFLICT(bot_id,widget_key) DO UPDATE SET kind=excluded.kind,title=excluded.title,position=excluded.position,payload_json=excluded.payload_json,updated_at_ms=excluded.updated_at_ms`, w.BotID, w.Key, w.Kind, w.Title, w.Position, w.PayloadJSON, w.UpdatedAtMS)
		if err != nil {
			return mapErr(err)
		}
	}
	return tx.Commit()
}

func (db *DB) ListBotWidgets(ctx context.Context, botID string) ([]domain.BotWidget, error) {
	rows, err := db.QueryContext(ctx, `SELECT bot_id,widget_key,kind,title,position,payload_json,updated_at_ms FROM bot_widgets WHERE bot_id=? ORDER BY position,widget_key LIMIT 24`, botID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.BotWidget
	for rows.Next() {
		var w domain.BotWidget
		if err := rows.Scan(&w.BotID, &w.Key, &w.Kind, &w.Title, &w.Position, &w.PayloadJSON, &w.UpdatedAtMS); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}
