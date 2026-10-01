package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"botpanel/internal/domain"
	"botpanel/internal/filesystem"
	"botpanel/internal/service"
)

// fakeProvider is a scripted OpenAI-compatible streaming endpoint. Each
// request receives the next step; requests are recorded for assertions.
type fakeProvider struct {
	mu     sync.Mutex
	steps  []func(w http.ResponseWriter)
	bodies []map[string]any
}

func (f *fakeProvider) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	f.mu.Lock()
	f.bodies = append(f.bodies, body)
	n := len(f.bodies)
	f.mu.Unlock()
	if n > len(f.steps) {
		http.Error(w, "unexpected request", 500)
		return
	}
	f.steps[n-1](w)
}

func (f *fakeProvider) body(i int) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i >= len(f.bodies) {
		return nil
	}
	return f.bodies[i]
}

func sse(w http.ResponseWriter, chunks ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, c := range chunks {
		fmt.Fprintf(w, "data: %s\n\n", c)
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
}

func toolStep(id, name string, args map[string]any) func(http.ResponseWriter) {
	raw, _ := json.Marshal(args)
	a, _ := json.Marshal(string(raw))
	return func(w http.ResponseWriter) {
		sse(w,
			fmt.Sprintf(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":%q,"type":"function","function":{"name":%q,"arguments":""}}]}}]}`, id, name),
			fmt.Sprintf(`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":%s}}]},"finish_reason":"tool_calls"}]}`, a),
			`{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`)
	}
}

func textStep(text string) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		sse(w, fmt.Sprintf(`{"choices":[{"delta":{"content":%q},"finish_reason":"stop"}]}`, text),
			`{"choices":[],"usage":{"prompt_tokens":20,"completion_tokens":3,"total_tokens":23}}`)
	}
}

func aiEnv(t *testing.T, fp http.Handler) (*env, *service.AIService, *client, string, string) {
	t.Helper()
	e := newEnv(t)
	ai := &service.AIService{Store: e.db, Keys: e.bots.Keys, Bots: e.bots, Files: e.bots.Workspaces.(*filesystem.Manager), Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ai.Start(ctx)
	e.app = New(Deps{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), DB: e.db, Auth: e.auth, Bots: e.bots, Catalog: e.bots.Catalog, SecureCookies: true,
		Nodes: e.db, Files: e.bots.Workspaces.(*filesystem.Manager), MaxUpload: 2 << 20, AI: ai})
	srv := httptest.NewServer(fp)
	t.Cleanup(srv.Close)

	admin := e.user("admin@example.com", domain.RoleAdmin)
	var p struct{ ID string }
	json.Unmarshal(admin.mustStatus(201, "POST", "/api/v1/admin/ai/providers", map[string]any{
		"name": "Fake", "enabled": true, "default": true, "base_url": srv.URL, "chat_path": "/chat/completions",
		"models_path": "/models", "default_model": "fake-model", "max_output_tokens": 512, "timeout_ms": 10000, "key": "sk-provider-secret",
	}), &p)
	providers := string(admin.mustStatus(200, "GET", "/api/v1/admin/ai/providers", nil))
	if strings.Contains(providers, "sk-provider-secret") || !strings.Contains(providers, `"key_set":true`) {
		t.Fatalf("provider key exposed or not stored: %s", providers)
	}

	bot := admin.createBot("aibot")
	admin.mustStatus(200, "PUT", "/api/v1/bots/"+bot+"/env", map[string]any{"vars": map[string]string{"DISCORD_TOKEN": "tok-very-secret-value"}})
	w, err := e.bots.Workspaces.(*filesystem.Manager).Open(bot)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Write("index.js", strings.NewReader("login('tok-very-secret-value')\n"), 1<<20); err != nil {
		t.Fatal(err)
	}
	w.Close()

	var conv struct{ ID string }
	json.Unmarshal(admin.mustStatus(201, "POST", "/api/v1/bots/"+bot+"/ai/conversations", map[string]any{"title": "incident"}), &conv)
	return e, ai, admin, bot, conv.ID
}

func waitRun(t *testing.T, c *client, id string, want ...string) map[string]any {
	t.Helper()
	var r map[string]any
	for i := 0; i < 300; i++ {
		json.Unmarshal(c.mustStatus(200, "GET", "/api/v1/ai/runs/"+id, nil), &r)
		for _, s := range want {
			if r["status"] == s {
				return r
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("run %s never reached %v: %v", id, want, r)
	return nil
}

func readBotFile(t *testing.T, e *env, bot, p string) string {
	t.Helper()
	w, err := e.bots.Workspaces.(*filesystem.Manager).Open(bot)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	b, err := w.Read(p, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestAIOperatorApprovalApplyAndUndo(t *testing.T) {
	fp := &fakeProvider{}
	fp.steps = []func(http.ResponseWriter){
		toolStep("call-read", "read_file", map[string]any{"path": "index.js"}),
		toolStep("call-env", "read_file", map[string]any{"path": ".env"}),
		toolStep("call-fix", "propose_file_change", map[string]any{"path": "index.js", "content": "login(process.env.DISCORD_TOKEN)\n", "summary": "Read the token from the environment"}),
		textStep("Moved the token into the environment."),
	}
	e, _, admin, bot, conv := aiEnv(t, fp)

	var run struct{ ID string }
	json.Unmarshal(admin.mustStatus(202, "POST", "/api/v1/ai/conversations/"+conv+"/messages", map[string]any{"content": "Why does login fail?", "mode": "approval"}), &run)
	waitRun(t, admin, run.ID, "waiting_approval")

	// Secrets never reach the provider: file contents are redacted and
	// protected paths are refused.
	raw, _ := json.Marshal(fp.body(2))
	if strings.Contains(string(raw), "tok-very-secret-value") || !strings.Contains(string(raw), "[REDACTED]") {
		t.Fatalf("secret value reached the provider or was not redacted: %s", raw)
	}
	if !strings.Contains(string(raw), "Permission is required") {
		t.Fatalf("protected .env read was not refused: %s", raw)
	}
	if got := readBotFile(t, e, bot, "index.js"); !strings.Contains(got, "tok-very-secret-value") {
		t.Fatalf("file changed before approval: %q", got)
	}

	var call string
	e.db.QueryRow(`SELECT id FROM ai_tool_calls WHERE run_id=? AND approval_state='pending'`, run.ID).Scan(&call)
	if call == "" {
		t.Fatal("no pending approval")
	}
	admin.mustStatus(204, "POST", "/api/v1/ai/tool-calls/"+call+"/decision", map[string]any{"approve": true})
	r := waitRun(t, admin, run.ID, "completed", "failed")
	if r["status"] != "completed" {
		t.Fatalf("run did not complete: %v", r)
	}
	if got := readBotFile(t, e, bot, "index.js"); got != "login(process.env.DISCORD_TOKEN)\n" {
		t.Fatalf("approved change not applied: %q", got)
	}
	var cv struct {
		Messages []struct{ Role, Content string }
	}
	json.Unmarshal(admin.mustStatus(200, "GET", "/api/v1/ai/conversations/"+conv, nil), &cv)
	if n := len(cv.Messages); n != 2 || cv.Messages[1].Content != "Moved the token into the environment." {
		t.Fatalf("conversation messages: %+v", cv.Messages)
	}

	var change string
	e.db.QueryRow(`SELECT id FROM ai_change_sets WHERE run_id=? AND status='applied'`, run.ID).Scan(&change)
	admin.mustStatus(204, "POST", "/api/v1/ai/change-sets/"+change+"/revert", nil)
	if got := readBotFile(t, e, bot, "index.js"); got != "login('tok-very-secret-value')\n" {
		t.Fatalf("undo did not restore the file: %q", got)
	}

	// Another account cannot see the conversation or its run.
	other := e.user("other@example.com", domain.RoleUser)
	if resp, _ := other.do("GET", "/api/v1/ai/runs/"+run.ID, nil); resp.StatusCode != 404 {
		t.Fatalf("foreign run visible: %d", resp.StatusCode)
	}
}

func TestAIOperatorProviderFailureEndsRun(t *testing.T) {
	fp := &fakeProvider{steps: []func(http.ResponseWriter){func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(401)
		fmt.Fprint(w, `{"error":{"message":"bad key","code":"invalid_api_key"}}`)
	}}}
	_, ai, admin, _, conv := aiEnv(t, fp)
	var run struct{ ID string }
	json.Unmarshal(admin.mustStatus(202, "POST", "/api/v1/ai/conversations/"+conv+"/messages", map[string]any{"content": "hello"}), &run)
	r := waitRun(t, admin, run.ID, "failed", "completed", "cancelled")
	if r["status"] != "failed" || r["error_code"] != "authentication" {
		t.Fatalf("expected an authentication failure: %v", r)
	}
	events, _, cancel := ai.Subscribe(run.ID, 0)
	cancel()
	if last := events[len(events)-1]; last.Type != "error" {
		t.Fatalf("terminal stream event = %q, want error", last.Type)
	}
}
