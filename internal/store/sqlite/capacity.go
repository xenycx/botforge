package sqlite

import "context"

// botMemory is a bot's memory reservation: its own limit plus its add-ons'.
const botMemory = `(memory_bytes + (SELECT COALESCE(SUM(a.memory_bytes), 0) FROM bot_addons a WHERE a.bot_id = bots.id))`

// OwnerUsage counts a user's bots (not deleted) and the sum of their memory
// limits, add-ons included.
func (db *DB) OwnerUsage(ctx context.Context, ownerID string) (bots int, memory int64, err error) {
	err = db.QueryRowContext(ctx, `SELECT count(*), COALESCE(SUM(`+botMemory+`), 0) FROM bots
		WHERE owner_id = ? AND desired_state != 'deleted'`, ownerID).Scan(&bots, &memory)
	return
}

// NodeReserved sums the memory limits (add-ons included) of bots wanted
// running on a node.
func (db *DB) NodeReserved(ctx context.Context, nodeID string) (running int, memory int64, err error) {
	err = db.QueryRowContext(ctx, `SELECT count(*), COALESCE(SUM(`+botMemory+`), 0) FROM bots
		WHERE node_id = ? AND desired_state = 'running'`, nodeID).Scan(&running, &memory)
	return
}
