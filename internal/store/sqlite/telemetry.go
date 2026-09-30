package sqlite

import (
	"context"

	"botpanel/internal/domain"
)

// InsertTelemetry stores one node sample. The table's CHECK constraints reject
// out-of-range values, so the sampler must clamp before inserting.
func (db *DB) InsertTelemetry(ctx context.Context, s domain.Telemetry) error {
	_, err := db.ExecContext(ctx, `INSERT INTO node_telemetry
		(node_id, sampled_at_ms, cpu_percent, logical_cpus, memory_used_bytes, memory_total_bytes,
		 disk_used_bytes, disk_total_bytes, running_bots) VALUES (?,?,?,?,?,?,?,?,?)
		ON CONFLICT(node_id, sampled_at_ms) DO NOTHING`,
		s.NodeID, s.SampledAtMS, s.CPUPercent, s.LogicalCPUs, s.MemoryUsedBytes, s.MemoryTotalBytes,
		s.DiskUsedBytes, s.DiskTotalBytes, s.RunningBots)
	return mapErr(err)
}

// ListTelemetry returns samples with sampled_at_ms > sinceMS in ascending order.
func (db *DB) ListTelemetry(ctx context.Context, nodeID string, sinceMS int64, limit int) ([]domain.Telemetry, error) {
	// Newest `limit` rows after sinceMS, returned oldest-first.
	rows, err := db.QueryContext(ctx, `SELECT node_id, sampled_at_ms, cpu_percent, logical_cpus, memory_used_bytes,
			memory_total_bytes, disk_used_bytes, disk_total_bytes, running_bots
		FROM (SELECT * FROM node_telemetry WHERE node_id = ? AND sampled_at_ms > ? ORDER BY sampled_at_ms DESC LIMIT ?)
		ORDER BY sampled_at_ms ASC`, nodeID, sinceMS, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Telemetry
	for rows.Next() {
		var s domain.Telemetry
		if err := rows.Scan(&s.NodeID, &s.SampledAtMS, &s.CPUPercent, &s.LogicalCPUs, &s.MemoryUsedBytes,
			&s.MemoryTotalBytes, &s.DiskUsedBytes, &s.DiskTotalBytes, &s.RunningBots); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// PruneTelemetry deletes at most batch samples older than beforeMS and returns
// how many it deleted; callers loop until it returns less than batch. Keeping
// each delete small keeps the write lock short.
func (db *DB) PruneTelemetry(ctx context.Context, beforeMS int64, batch int) (int64, error) {
	res, err := db.ExecContext(ctx, `DELETE FROM node_telemetry WHERE (node_id, sampled_at_ms) IN
		(SELECT node_id, sampled_at_ms FROM node_telemetry WHERE sampled_at_ms < ? LIMIT ?)`, beforeMS, batch)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// CountRunningBots counts bots on a node whose last observation is "running".
func (db *DB) CountRunningBots(ctx context.Context, nodeID string) (int, error) {
	var n int
	err := db.QueryRowContext(ctx, `SELECT count(*) FROM bots WHERE node_id = ? AND observed_state = 'running'`, nodeID).Scan(&n)
	return n, err
}

// ListNodes returns all nodes.
func (db *DB) ListNodes(ctx context.Context) ([]domain.Node, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, name, transport, endpoint, enabled, last_seen_at_ms FROM nodes ORDER BY name LIMIT 1000`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Node
	for rows.Next() {
		var n domain.Node
		var enabled int
		if err := rows.Scan(&n.ID, &n.Name, &n.Transport, &n.Endpoint, &enabled, &n.LastSeenMS); err != nil {
			return nil, err
		}
		n.Enabled = enabled == 1
		out = append(out, n)
	}
	return out, rows.Err()
}
