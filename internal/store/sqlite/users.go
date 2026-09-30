package sqlite

import (
	"context"
	"database/sql"

	"botpanel/internal/domain"
)

const userCols = `id, email, display_name, avatar_jpeg, password_hash, role, disabled, created_at_ms, updated_at_ms`

func scanUser(row interface{ Scan(...any) error }) (domain.User, error) {
	var u domain.User
	var dis int
	err := row.Scan(&u.ID, &u.Email, &u.DisplayName, &u.AvatarJPEG, &u.PasswordHash, &u.Role, &dis, &u.CreatedAtMS, &u.UpdatedAtMS)
	u.Disabled = dis == 1
	return u, mapErr(err)
}

// CreateUser inserts a user; a duplicate email returns domain.ErrConflict.
func (db *DB) CreateUser(ctx context.Context, u domain.User) error {
	_, err := db.ExecContext(ctx, `INSERT INTO users (`+userCols+`) VALUES (?,?,?,?,?,?,?,?,?)`,
		u.ID, u.Email, u.DisplayName, u.AvatarJPEG, u.PasswordHash, u.Role, boolInt(u.Disabled), u.CreatedAtMS, u.UpdatedAtMS)
	return mapErr(err)
}

func (db *DB) UpdateProfile(ctx context.Context, id, name string, avatar []byte, replaceAvatar bool, nowMS int64) error {
	q := `UPDATE users SET display_name = ?, updated_at_ms = ? WHERE id = ?`
	args := []any{name, nowMS, id}
	if replaceAvatar {
		q = `UPDATE users SET display_name = ?, avatar_jpeg = ?, updated_at_ms = ? WHERE id = ?`
		args = []any{name, avatar, nowMS, id}
	}
	res, err := db.ExecContext(ctx, q, args...)
	if err != nil {
		return mapErr(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (db *DB) GetUserByEmail(ctx context.Context, email string) (domain.User, error) {
	return scanUser(db.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE email = ?`, email))
}

func (db *DB) GetUserByID(ctx context.Context, id string) (domain.User, error) {
	return scanUser(db.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE id = ?`, id))
}

func (db *DB) ListUsers(ctx context.Context) ([]domain.User, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+userCols+` FROM users ORDER BY created_at_ms LIMIT 1000`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// ErrLastAdmin refuses changes that would leave no active administrator.
var ErrLastAdmin = domain.Invalid("this is the only active administrator; make someone else an administrator first")

// activeAdminsExcept counts enabled administrators other than id, inside tx.
func activeAdminsExcept(ctx context.Context, tx interface {
	QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row
}, id string) (int, error) {
	var n int
	err := tx.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE role = 'admin' AND disabled = 0 AND id != ?`, id).Scan(&n)
	return n, err
}

// SetUserRole changes a role. Demoting the last active administrator is
// refused inside the same (immediate) transaction, so two concurrent demotions
// cannot both succeed.
func (db *DB) SetUserRole(ctx context.Context, id, role string, nowMS int64) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if role != domain.RoleAdmin {
		var cur string
		var dis int
		if err := tx.QueryRowContext(ctx, `SELECT role, disabled FROM users WHERE id = ?`, id).Scan(&cur, &dis); err != nil {
			return mapErr(err)
		}
		if cur == domain.RoleAdmin && dis == 0 {
			n, err := activeAdminsExcept(ctx, tx, id)
			if err != nil {
				return err
			}
			if n == 0 {
				return ErrLastAdmin
			}
		}
	}
	res, err := tx.ExecContext(ctx, `UPDATE users SET role = ?, updated_at_ms = ? WHERE id = ?`, role, nowMS, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	return tx.Commit()
}

// SetUserDisabled updates the flag and, when disabling, revokes all sessions in
// the same transaction. Disabling the last active administrator is refused.
func (db *DB) SetUserDisabled(ctx context.Context, id string, disabled bool, nowMS int64) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if disabled {
		var role string
		if err := tx.QueryRowContext(ctx, `SELECT role FROM users WHERE id = ?`, id).Scan(&role); err != nil {
			return mapErr(err)
		}
		if role == domain.RoleAdmin {
			n, err := activeAdminsExcept(ctx, tx, id)
			if err != nil {
				return err
			}
			if n == 0 {
				return ErrLastAdmin
			}
		}
	}
	res, err := tx.ExecContext(ctx, `UPDATE users SET disabled = ?, updated_at_ms = ? WHERE id = ?`, boolInt(disabled), nowMS, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	if disabled {
		if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// BotCountsByOwner returns how many (non-deleted) bots each user owns.
func (db *DB) BotCountsByOwner(ctx context.Context) (map[string]int, error) {
	rows, err := db.QueryContext(ctx, `SELECT owner_id, count(*) FROM bots WHERE desired_state != 'deleted' GROUP BY owner_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}
