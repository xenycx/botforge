package sqlite

import (
	"context"

	"botpanel/internal/domain"
)

const schedCols = `s.id, s.bot_id, s.owner_id, COALESCE(u.email, ''), s.action, s.spec, s.timezone, s.enabled, s.next_run_at_ms,
	s.last_run_at_ms, s.last_status, s.last_message, s.created_at_ms, s.updated_at_ms`

func scanSchedule(row interface{ Scan(...any) error }) (domain.Schedule, error) {
	var s domain.Schedule
	var en int
	err := row.Scan(&s.ID, &s.BotID, &s.OwnerID, &s.OwnerEmail, &s.Action, &s.Spec, &s.Timezone, &en, &s.NextRunMS,
		&s.LastRunMS, &s.LastStatus, &s.LastMessage, &s.CreatedAtMS, &s.UpdatedAtMS)
	s.Enabled = en == 1
	return s, mapErr(err)
}

func (db *DB) InsertSchedule(ctx context.Context, s domain.Schedule) error {
	_, err := db.ExecContext(ctx, `INSERT INTO schedules (id, bot_id, owner_id, action, spec, timezone, enabled, next_run_at_ms,
		created_at_ms, updated_at_ms) VALUES (?,?,?,?,?,?,?,?,?,?)`, s.ID, s.BotID, s.OwnerID, s.Action, s.Spec, s.Timezone,
		boolInt(s.Enabled), s.NextRunMS, s.CreatedAtMS, s.UpdatedAtMS)
	return mapErr(err)
}

func (db *DB) UpdateSchedule(ctx context.Context, s domain.Schedule) error {
	res, err := db.ExecContext(ctx, `UPDATE schedules SET action = ?, spec = ?, timezone = ?, enabled = ?, next_run_at_ms = ?,
		updated_at_ms = ? WHERE id = ? AND bot_id = ?`, s.Action, s.Spec, s.Timezone, boolInt(s.Enabled), s.NextRunMS, s.UpdatedAtMS, s.ID, s.BotID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (db *DB) GetSchedule(ctx context.Context, botID, id string) (domain.Schedule, error) {
	return scanSchedule(db.QueryRowContext(ctx, `SELECT `+schedCols+` FROM schedules s LEFT JOIN users u ON u.id = s.owner_id
		WHERE s.id = ? AND s.bot_id = ?`, id, botID))
}

func (db *DB) ListSchedules(ctx context.Context, botID string) ([]domain.Schedule, error) {
	return db.querySchedules(ctx, `SELECT `+schedCols+` FROM schedules s LEFT JOIN users u ON u.id = s.owner_id
		WHERE s.bot_id = ? ORDER BY s.created_at_ms LIMIT 50`, botID)
}

// DueSchedules returns enabled schedules due at nowMS, oldest first, bounded.
func (db *DB) DueSchedules(ctx context.Context, nowMS int64, limit int) ([]domain.Schedule, error) {
	return db.querySchedules(ctx, `SELECT `+schedCols+` FROM schedules s LEFT JOIN users u ON u.id = s.owner_id
		WHERE s.enabled = 1 AND s.next_run_at_ms IS NOT NULL AND s.next_run_at_ms <= ? ORDER BY s.next_run_at_ms LIMIT ?`, nowMS, limit)
}

func (db *DB) querySchedules(ctx context.Context, q string, args ...any) ([]domain.Schedule, error) {
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Schedule
	for rows.Next() {
		s, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// RecordScheduleRun stores the outcome of a run and the next due time; a nil
// next disables the schedule.
func (db *DB) RecordScheduleRun(ctx context.Context, id string, runMS int64, status, msg string, next *int64, disable bool) error {
	_, err := db.ExecContext(ctx, `UPDATE schedules SET last_run_at_ms = ?, last_status = ?, last_message = ?, next_run_at_ms = ?,
		enabled = CASE WHEN ? THEN 0 ELSE enabled END, updated_at_ms = ? WHERE id = ?`,
		runMS, status, nullStr(msg), next, boolInt(disable), runMS, id)
	return err
}

func (db *DB) DeleteSchedule(ctx context.Context, botID, id string) error {
	res, err := db.ExecContext(ctx, `DELETE FROM schedules WHERE id = ? AND bot_id = ?`, id, botID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// BotsWithBackupSchedule lists bots that have their own enabled backup
// schedule, so the panel-wide backup interval skips them (no double backups).
func (db *DB) BotsWithBackupSchedule(ctx context.Context) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT DISTINCT bot_id FROM schedules WHERE action = 'backup' AND enabled = 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}
