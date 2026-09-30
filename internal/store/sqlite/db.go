// Package sqlite provides the SQLite database opener, migrations and queries.
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver, registers "sqlite"
)

// connPragmas are applied by the driver to EVERY new pooled connection via the
// DSN. A one-off db.Exec would only configure a single connection.
var connPragmas = []string{
	"journal_mode(WAL)",
	"foreign_keys(1)",
	"busy_timeout(5000)",
	"synchronous(FULL)",
	"cache_size(-2048)",
}

// DB wraps *sql.DB.
type DB struct {
	*sql.DB
}

// Open opens (creating if needed) the database at path with a bounded pool.
func Open(ctx context.Context, path string, maxConns int) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	q := url.Values{}
	for _, p := range connPragmas {
		q.Add("_pragma", p)
	}
	q.Set("_txlock", "immediate") // write transactions take the lock up front
	dsn := (&url.URL{Scheme: "file", Opaque: escapePath(path), RawQuery: q.Encode()}).String()

	sdb, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	sdb.SetMaxOpenConns(maxConns)
	sdb.SetMaxIdleConns(maxConns)
	sdb.SetConnMaxLifetime(0)

	pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := sdb.PingContext(pctx); err != nil {
		sdb.Close()
		return nil, fmt.Errorf("open database: %w", err)
	}
	return &DB{sdb}, nil
}

// escapePath percent-encodes characters that would be misparsed in a file: URI.
func escapePath(p string) string {
	u := url.URL{Path: p}
	return u.EscapedPath()
}
