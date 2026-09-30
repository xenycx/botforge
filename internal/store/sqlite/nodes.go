package sqlite

import (
	"context"
	"time"

	"botpanel/internal/domain"
)

// EnsureLocalNode idempotently registers the local node under a stable ID.
// It never overwrites an existing row (e.g. an administrator-disabled node).
func (db *DB) EnsureLocalNode(ctx context.Context) error {
	now := time.Now().UnixMilli()
	_, err := db.ExecContext(ctx, `INSERT INTO nodes
		(id, name, transport, endpoint, enabled, created_at_ms, updated_at_ms)
		VALUES (?, ?, 'local', NULL, 1, ?, ?)
		ON CONFLICT(id) DO NOTHING`,
		domain.LocalNodeID, domain.LocalNodeName, now, now)
	return err
}

// GetNode returns a node by ID; sql.ErrNoRows if absent.
func (db *DB) GetNode(ctx context.Context, id string) (domain.Node, error) {
	var n domain.Node
	var enabled int
	err := db.QueryRowContext(ctx,
		`SELECT id, name, transport, endpoint, enabled, last_seen_at_ms FROM nodes WHERE id = ?`, id).
		Scan(&n.ID, &n.Name, &n.Transport, &n.Endpoint, &enabled, &n.LastSeenMS)
	n.Enabled = enabled == 1
	return n, mapErr(err)
}
