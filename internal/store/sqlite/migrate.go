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
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply %s: %w", m.name, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations (version, name, applied_at_ms) VALUES (?, ?, ?)`,
			m.version, m.name, time.Now().UnixMilli()); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit %s: %w", m.name, err)
		}
	}
	return nil
}
