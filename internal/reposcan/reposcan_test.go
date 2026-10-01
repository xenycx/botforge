package reposcan

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"slices"
	"strings"
	"testing"
)

func tarball(t *testing.T, files map[string]string) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: "o-r-abc/", Typeflag: tar.TypeDir, Mode: 0o755})
	for n, c := range files {
		tw.WriteHeader(&tar.Header{Name: "o-r-abc/" + n, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(c))})
		tw.Write([]byte(c))
	}
	tw.Close()
	gz.Close()
	return &buf
}

func scan(t *testing.T, files map[string]string, root string) *Snapshot {
	t.Helper()
	s, err := Scan(tarball(t, files), root, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func env(p Plan, name string) (EnvVar, bool) {
	for _, e := range p.Env {
		if e.Name == name {
			return e, true
		}
	}
	return EnvVar{}, false
}

func TestDetectNodeWithPostgresAndEnvExample(t *testing.T) {
	s := scan(t, map[string]string{
		"package.json":        `{"main":"src/index.js","scripts":{"start":"node src/index.js"},"dependencies":{"discord.js":"14","pg":"8"}}`,
		"package-lock.json":   "{}",
		"src/index.js":        "client.login(process.env.DISCORD_TOKEN); const x = process.env['GUILD_ID']; process.env.NODE_ENV",
		".env.example":        "# Bot token from the portal\nDISCORD_TOKEN=your-token-here\n# Prefix\nPREFIX=!\n#OLD=1\nDATABASE_URL=postgres://user:pass@localhost/db\n",
		"node_modules/x/a.js": "process.env.SHOULD_NOT_APPEAR",
		"test/a.test.js":      "process.env.TEST_ONLY",
	}, "")
	p := Detect(s, "r")
	if p.Runtime != "nodejs" || strings.Join(p.Argv, " ") != "node src/index.js" || p.BuildCommand != "" {
		t.Fatalf("plan %+v", p)
	}
	if !slices.Contains(p.Addons, "postgres") {
		t.Fatalf("addons %v", p.Addons)
	}
	tok, ok := env(p, "DISCORD_TOKEN")
	if !ok || !tok.Secret || !tok.Required || tok.Value != "" || tok.Description != "Bot token from the portal" {
		t.Fatalf("DISCORD_TOKEN %+v", tok)
	}
	if pre, _ := env(p, "PREFIX"); pre.Value != "!" || pre.Secret {
		t.Fatalf("PREFIX %+v", pre)
	}
	if db, _ := env(p, "DATABASE_URL"); db.Value != "${DATABASE_URL}" && db.Name != "" {
		// DATABASE_URL equals the add-on's own variable: it needs no entry.
		t.Fatalf("DATABASE_URL %+v", db)
	}
	for _, n := range []string{"GUILD_ID"} {
		if _, ok := env(p, n); !ok {
			t.Errorf("missing %s", n)
		}
	}
	for _, n := range []string{"NODE_ENV", "SHOULD_NOT_APPEAR", "TEST_ONLY", "OLD"} {
		if _, ok := env(p, n); ok {
			t.Errorf("unexpected %s", n)
		}
	}
}

func TestDetectTypeScriptBuild(t *testing.T) {
	s := scan(t, map[string]string{
		"package.json":  `{"scripts":{"build":"tsc","start":"node dist/index.js"},"devDependencies":{"typescript":"5"}}`,
		"tsconfig.json": "{}",
	}, "")
	p := Detect(s, "r")
	if !strings.Contains(p.BuildCommand, "npm run build") || !strings.Contains(p.BuildCommand, "npm prune --omit=dev") ||
		strings.Join(p.Argv, " ") != "node dist/index.js" {
		t.Fatalf("plan %+v", p)
	}
}

func TestDetectGoSubcommand(t *testing.T) {
	s := scan(t, map[string]string{
		"go.mod":              "module x\n\nrequire github.com/redis/go-redis/v9 v9.0.0\n",
		"lib/lib.go":          "package lib\n",
		"cmd/tool/main.go":    "package main\nfunc main() {}\n",
		"cmd/mybot/main.go":   "package main\nimport \"os\"\nfunc main() { os.Getenv(\"BOT_TOKEN\") }\n",
		"cmd/mybot/x_test.go": "package main\nfunc main() {}\n",
	}, "")
	p := Detect(s, "mybot")
	if p.Runtime != "go" || !strings.HasSuffix(p.BuildCommand, "./cmd/mybot") || p.Argv[0] != "./app" || !slices.Contains(p.Addons, "redis") {
		t.Fatalf("plan %+v", p)
	}
	if _, ok := env(p, "BOT_TOKEN"); !ok {
		t.Fatal("BOT_TOKEN not found")
	}
}

func TestDetectPythonPackageAndFolder(t *testing.T) {
	s := scan(t, map[string]string{
		"README.md":             "top",
		"bot/pyproject.toml":    "[project]\nname='x'\ndependencies=['discord.py','asyncpg']\n",
		"bot/mybot/__main__.py": "import os\nos.environ['TOKEN']\nos.getenv('REDIS_URL')\n",
		"other/main.py":         "os.getenv('NOPE')",
	}, "bot")
	p := Detect(s, "r")
	if p.Runtime != "python" || strings.Join(p.Argv, " ") != "/workspace/.venv/bin/python -m mybot" || !slices.Contains(p.Addons, "postgres") {
		t.Fatalf("plan %+v", p)
	}
	if _, ok := env(p, "NOPE"); ok {
		t.Fatal("scanned outside the folder")
	}
	if _, err := Scan(tarball(t, map[string]string{"a.txt": "x"}), "missing", DefaultLimits); err == nil {
		t.Fatal("missing folder accepted")
	}
}

func TestComposeAddonsAndRecipes(t *testing.T) {
	s := scan(t, map[string]string{
		"requirements.txt":   "discord.py\n",
		"bot.py":             "",
		"docker-compose.yml": "services:\n  db:\n    image: postgres:16\n  cache:\n    image: redis:7\n  music:\n    image: ghcr.io/lavalink-devs/lavalink:4\n",
	}, "")
	p := Detect(s, "r")
	if !slices.Contains(p.Addons, "postgres") || !slices.Contains(p.Addons, "redis") || strings.Join(p.Argv, " ") != "/workspace/.venv/bin/python bot.py" {
		t.Fatalf("plan %+v", p)
	}
	if !strings.Contains(strings.Join(p.Notes, " "), "Lavalink") {
		t.Fatal("lavalink not mentioned")
	}
	r, ok := RecipeFor("cog-creators/red-discordbot")
	if !ok || r.Plan.Runtime != "python" || r.Plan.Env[0].Name != "RED_TOKEN" {
		t.Fatalf("recipe %+v", r)
	}
	if y, ok := RecipeFor("botlabs-gg/YAGPDB"); !ok || len(y.Plan.Addons) != 2 {
		t.Fatal("yagpdb recipe")
	}
}
