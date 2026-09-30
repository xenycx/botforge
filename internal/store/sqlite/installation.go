package sqlite

import (
	"context"

	"github.com/google/uuid"
)

// InstallationID returns this installation's identity, creating it on first
// use. It lives in the database, so a restored installation keeps it.
func (db *DB) InstallationID(ctx context.Context, nowMS int64) (string, error) {
	if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO installation (id, install_id, created_at_ms) VALUES (1, ?, ?)`,
		uuid.NewString(), nowMS); err != nil {
		return "", err
	}
	var id string
	err := db.QueryRowContext(ctx, `SELECT install_id FROM installation WHERE id = 1`).Scan(&id)
	return id, mapErr(err)
}
