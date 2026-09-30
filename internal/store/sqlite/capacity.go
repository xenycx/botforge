package sqlite

import "context"

// OwnerUsage counts a user's bots (not deleted) and the sum of their memory limits.
func (db *DB) OwnerUsage(ctx context.Context, ownerID string) (bots int, memory int64, err error) {
	err = db.QueryRowContext(ctx, `SELECT count(*), COALESCE(SUM(memory_bytes), 0) FROM bots
		WHERE owner_id = ? AND desired_state != 'deleted'`, ownerID).Scan(&bots, &memory)
	return
}

// NodeReserved sums the memory limits of bots wanted running on a node.
func (db *DB) NodeReserved(ctx context.Context, nodeID string) (running int, memory int64, err error) {
	err = db.QueryRowContext(ctx, `SELECT count(*), COALESCE(SUM(memory_bytes), 0) FROM bots
		WHERE node_id = ? AND desired_state = 'running'`, nodeID).Scan(&running, &memory)
	return
}
