package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"botpanel/internal/domain"
	"botpanel/internal/migrations"
)

func open(t *testing.T) *DB {
	t.Helper()
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "sub", "t.db"), 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(context.Background(), migrations.FS); err != nil {
		t.Fatal(err)
	}
	return db
}

// Pragmas must hold on every pooled connection, not just the first.
func TestPragmasOnEveryConnection(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	const n = 4
	conns := make([]interface{ Close() error }, 0, n)
	for i := 0; i < n; i++ {
		c, err := db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, c)
		var jm string
		var fk, bt, sync, cs int
		must := func(q string, dst any) {
			if err := c.QueryRowContext(ctx, q).Scan(dst); err != nil {
				t.Fatal(err)
			}
		}
		must("PRAGMA journal_mode", &jm)
		must("PRAGMA foreign_keys", &fk)
		must("PRAGMA busy_timeout", &bt)
		must("PRAGMA synchronous", &sync)
		must("PRAGMA cache_size", &cs)
		if jm != "wal" || fk != 1 || bt != 5000 || sync != 2 || cs != -2048 {
			t.Fatalf("conn %d pragmas: jm=%s fk=%d bt=%d sync=%d cs=%d", i, jm, fk, bt, sync, cs)
		}
	}
	for _, c := range conns {
		c.Close()
	}
}

func TestMigrateIdempotentAndSchema(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, migrations.FS); err != nil {
		t.Fatal(err)
	}
	var versions int
	db.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&versions)
	if versions != 30 {
		t.Fatalf("versions = %d", versions)
	}
	var tables int
	db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table'
		AND name IN ('users','sessions','nodes','bots','bot_env_vars','node_telemetry',
		'oauth_accounts','api_keys','bot_subusers','github_repos','bot_backups','bot_telemetry_logs','bot_ports',
		'workspaces','workspace_members','sites','site_releases','site_domains','operations',
		'ai_provider_profiles','ai_conversations','ai_messages','ai_runs','ai_tool_calls','ai_change_sets','ai_change_files')`).Scan(&tables)
	if tables != 26 {
		t.Fatalf("tables = %d", tables)
	}
}

func TestEnsureLocalNodeIdempotentAndPreservesEdits(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if err := db.EnsureLocalNode(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	db.QueryRowContext(ctx, `SELECT count(*) FROM nodes`).Scan(&n)
	if n != 1 {
		t.Fatalf("nodes = %d", n)
	}
	db.ExecContext(ctx, `UPDATE nodes SET enabled = 0 WHERE id = ?`, domain.LocalNodeID)
	db.EnsureLocalNode(ctx)
	node, err := db.GetNode(ctx, domain.LocalNodeID)
	if err != nil || node.Enabled || node.Transport != "local" || node.Endpoint != nil {
		t.Fatalf("node = %+v err=%v", node, err)
	}
}

func TestConstraintsEnforced(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	// Foreign key: bot with unknown owner/node must fail.
	_, err := db.ExecContext(ctx, `INSERT INTO bots (id, owner_id, node_id, name, runtime, image_ref,
		argv_json, memory_bytes, nano_cpus, created_at_ms, updated_at_ms)
		VALUES ('b','nouser','nonode','x','nodejs','img','["node"]',1,1,1,1)`)
	if err == nil {
		t.Fatal("expected foreign key violation")
	}
	// local node must not have an endpoint
	_, err = db.ExecContext(ctx, `INSERT INTO nodes (id,name,transport,endpoint,created_at_ms,updated_at_ms)
		VALUES ('n','n','local','http://x',1,1)`)
	if err == nil {
		t.Fatal("expected check violation")
	}
}

func TestSetDesiredAndObserveGenerationRules(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	db.EnsureLocalNode(ctx)
	db.ExecContext(ctx, `INSERT INTO users (id,email,password_hash,created_at_ms,updated_at_ms) VALUES ('u','u@x','h',1,1)`)
	b := domain.Bot{ID: "b1", OwnerID: "u", NodeID: domain.LocalNodeID, Name: "n", Runtime: "nodejs", ImageRef: "i",
		Argv: []string{"node"}, MemoryBytes: 1, NanoCPUs: 1, PidsLimit: 1, CreatedAtMS: 1, UpdatedAtMS: 1}
	if err := db.CreateBot(ctx, b); err != nil {
		t.Fatal(err)
	}
	got, changed, err := db.SetDesired(ctx, "b1", "running", false, 2)
	if err != nil || !changed || got.Generation != 1 {
		t.Fatal(got, changed, err)
	}
	if got, changed, _ = db.SetDesired(ctx, "b1", "running", false, 3); changed || got.Generation != 1 {
		t.Fatal("repeat start must be idempotent", got.Generation)
	}
	if got, changed, _ = db.SetDesired(ctx, "b1", "running", true, 3); !changed || got.Generation != 2 {
		t.Fatal("forced restart must advance generation")
	}
	// A stale observation (older generation) is dropped.
	ok, err := db.Observe(ctx, Observation{BotID: "b1", Generation: 1, State: "running", SettleGeneration: true, NowMS: 4})
	if err != nil || ok {
		t.Fatal("stale observation applied", ok, err)
	}
	ok, _ = db.Observe(ctx, Observation{BotID: "b1", Generation: 2, State: "running", SettleGeneration: true, ContainerID: "c1", NowMS: 4})
	cur, _ := db.GetBot(ctx, "b1")
	if !ok || cur.ObservedState != "running" || cur.ObservedGeneration != 2 || *cur.ContainerID != "c1" {
		t.Fatalf("%+v", cur)
	}
	// unsettled observation keeps observed_generation and container
	db.Observe(ctx, Observation{BotID: "b1", Generation: 2, State: "stopping", NowMS: 5})
	cur, _ = db.GetBot(ctx, "b1")
	if cur.ObservedGeneration != 2 || cur.ContainerID == nil {
		t.Fatalf("%+v", cur)
	}
	n, _ := db.MarkNodeObservedUnknown(ctx, domain.LocalNodeID, 6)
	cur, _ = db.GetBot(ctx, "b1")
	if n != 1 || cur.ObservedState != "unknown" {
		t.Fatalf("n=%d %+v", n, cur)
	}
	db.MarkBotDeleted(ctx, "b1", 7)
	if _, _, err := db.SetDesired(ctx, "b1", "running", false, 8); err == nil {
		t.Fatal("deleted bot accepted lifecycle request")
	}
}

func TestMigrateRefusesNewerSchema(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO schema_migrations (version, name, applied_at_ms) VALUES (9999, '9999_future', 1)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, migrations.FS); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("migrate over a newer schema: %v", err)
	}
}

func TestInstallationIDIsStable(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	a, err := db.InstallationID(ctx, 1)
	if err != nil || len(a) != 36 {
		t.Fatalf("first: %q %v", a, err)
	}
	b, err := db.InstallationID(ctx, 2)
	if err != nil || a != b {
		t.Fatalf("second call changed the identity: %q -> %q (%v)", a, b, err)
	}
}
