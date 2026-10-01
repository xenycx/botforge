package sqlite

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Migrate applies unapplied migrations from fsys in version order. Each
// migration and its version record commit in one transaction. It is
// idempotent and safe to call on every start.
func (db *DB) Migrate(ctx context.Context, fsys fs.FS) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version       INTEGER PRIMARY KEY NOT NULL,
		name          TEXT NOT NULL,
		applied_at_ms INTEGER NOT NULL
	) STRICT`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	applied := map[int]bool{}
	rows, err := db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return err
		}
		applied[v] = true
	}
	if err := rows.Close(); err != nil {
		return err
	}

	type mig struct {
		version int
		name    string
		file    string
	}
	entries, err := fs.Glob(fsys, "*.sql")
	if err != nil {
		return err
	}
	var migs []mig
	for _, f := range entries {
		vs, _, ok := strings.Cut(strings.TrimSuffix(f, ".sql"), "_")
		v, err := strconv.Atoi(vs)
		if !ok || err != nil {
			return fmt.Errorf("migration %q: name must be NNNN_description.sql", f)
		}
		migs = append(migs, mig{v, strings.TrimSuffix(f, ".sql"), f})
	}
	sort.Slice(migs, func(i, j int) bool { return migs[i].version < migs[j].version })
	for i := 1; i < len(migs); i++ {
		if migs[i].version == migs[i-1].version {
			return fmt.Errorf("duplicate migration version %d", migs[i].version)
		}
	}

	// A database written by a newer BotPanel may rely on columns and rules
	// this binary does not know; refuse instead of corrupting it.
	known := 0
	if len(migs) > 0 {
		known = migs[len(migs)-1].version
	}
	for v := range applied {
		if v > known {
			return fmt.Errorf("the database schema (version %d) is newer than this BotPanel build supports (version %d); "+
				"install the newer release again, or restore a backup taken before the upgrade", v, known)
		}
	}
	for _, m := range migs {
		if applied[m.version] {
			continue
		}
		body, err := fs.ReadFile(fsys, m.file)
		if err != nil {
			return err
		}
		if err := db.apply(ctx, m.version, m.name, string(body)); err != nil {
			return err
		}
	}
	return nil
}

// noForeignKeys marks a migration that rebuilds a table other tables
// reference. With foreign keys on, dropping the old table would cascade into
// every referencing row, so the migration runs on one connection with
// foreign keys off (SQLite's documented table-rebuild procedure) and must
// leave no dangling reference behind.
const noForeignKeys = "-- botpanel:foreign-keys-off"

func (db *DB) apply(ctx context.Context, version int, name, body string) (err error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	rebuild := strings.HasPrefix(body, noForeignKeys)
	if rebuild {
		// PRAGMA foreign_keys is a no-op inside a transaction: set it first,
		// and restore it before the connection returns to the pool.
		if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
			return err
		}
		defer func() {
			var on int
			if _, e := conn.ExecContext(context.Background(), `PRAGMA foreign_keys = ON`); e != nil && err == nil {
				err = e
			} else if e := conn.QueryRowContext(context.Background(), `PRAGMA foreign_keys`).Scan(&on); (e != nil || on != 1) && err == nil {
				err = fmt.Errorf("re-enable foreign keys after %s", name)
			}
		}()
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, body); err != nil {
		return fmt.Errorf("apply %s: %w", name, err)
	}
	if rebuild {
		rows, err := tx.QueryContext(ctx, `PRAGMA foreign_key_check`)
		if err != nil {
			return err
		}
		broken := rows.Next()
		rows.Close()
		if broken {
			return fmt.Errorf("apply %s: the rebuilt tables leave a broken foreign key", name)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, name, applied_at_ms) VALUES (?, ?, ?)`,
		version, name, time.Now().UnixMilli()); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit %s: %w", name, err)
	}
	return nil
}
