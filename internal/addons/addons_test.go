package addons

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBotEnv(t *testing.T) {
	env := BotEnv([]string{"mariadb", "redis"}, map[string]string{"mariadb": "m"})
	if env["DATABASE_URL"] != "mysql://bot:m@mariadb:3306/bot" || env["REDIS_URL"] != "redis://redis:6379" {
		t.Fatalf("%v", env)
	}
	env = BotEnv([]string{"mariadb", "postgres"}, map[string]string{"mariadb": "m", "postgres": "p"})
	if env["DATABASE_URL"] != "postgres://bot:p@postgres:5432/bot?sslmode=disable" {
		t.Fatalf("postgres should own DATABASE_URL: %v", env)
	}
	for _, k := range List() {
		if len(k.VarNames) == 0 || k.MinMemory <= 0 || k.DefaultMemory < k.MinMemory || k.ValidateMemory(k.DefaultMemory) != nil {
			t.Errorf("%s catalog entry invalid", k.ID)
		}
	}
}

func TestDataRootPaths(t *testing.T) {
	d := DataRoot{Dir: t.TempDir()}
	id := "11111111-2222-4333-8444-555555555555"
	for _, bad := range [][2]string{{"../x", "redis"}, {id, "../../etc"}, {id, "oracle"}} {
		if _, err := d.Path(bad[0], bad[1]); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
	p, err := d.Ensure(id, "redis", os.Getuid(), os.Getgid())
	if err != nil || p != filepath.Join(d.Dir, id, "redis") {
		t.Fatalf("%s %v", p, err)
	}
	os.WriteFile(filepath.Join(p, "dump.rdb"), []byte("12345"), 0o600)
	os.Chmod(p, 0o500) // databases leave directories without owner write access
	if d.Usage(id, "redis") != 5 {
		t.Fatal("usage")
	}
	if err := d.RemoveBot(id); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(d.Dir, id)); !os.IsNotExist(err) {
		t.Fatal("not removed")
	}
	if err := (DataRoot{}).RemoveBot(id); err != nil {
		t.Fatal("unconfigured root must be a no-op")
	}
}
