package oauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestPKCEChallengeMatchesVerifier(t *testing.T) {
	v, c := NewPKCE()
	v2, _ := NewPKCE()
	if v == v2 || len(v) < 43 || c == "" || c == v {
		t.Fatal(v, c)
	}
}

func TestStateStoreSingleUseExpiryBinderAndBound(t *testing.T) {
	now := time.Unix(1000, 0)
	s := NewStateStore()
	s.now = func() time.Time { return now }
	st, b := s.Put(Pending{Provider: "github", Verifier: "v"})
	if _, ok := s.Take(st, "wrong", "github"); ok {
		t.Fatal("wrong binder accepted")
	}
	if _, ok := s.Take(st, b, "discord"); ok {
		t.Fatal("wrong provider accepted")
	}
	if p, ok := s.Take(st, b, "github"); !ok || p.Verifier != "v" {
		t.Fatal("valid take failed")
	}
	if _, ok := s.Take(st, b, "github"); ok {
		t.Fatal("replay accepted")
	}
	st, b = s.Put(Pending{Provider: "github"})
	now = now.Add(11 * time.Minute)
	if _, ok := s.Take(st, b, "github"); ok {
		t.Fatal("expired state accepted")
	}
	s.max = 5
	for i := 0; i < 50; i++ {
		s.Put(Pending{Provider: "github"})
	}
	if len(s.m) > 5 {
		t.Fatalf("store grew to %d", len(s.m))
	}
}

func TestDiscordScopesAndWebhook(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("code_verifier") != "ver" {
			http.Error(w, "bad", 400)
			return
		}
		w.Write([]byte(`{"access_token":"t","scope":"identify email webhook.incoming","webhook":{"url":"https://discord.com/api/webhooks/1/x"}}`))
	})
	mux.HandleFunc("/users/@me", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":"42","username":"neo","global_name":"Neo","avatar":"abc","email":"n@x.io","verified":false}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	d := &Discord{ClientID: "c", Secret: "s", TokenURL: srv.URL + "/token", APIBase: srv.URL}

	u, _ := url.Parse(d.AuthURL(AuthParams{RedirectURI: "https://p/cb", State: "s", Challenge: "ch"}))
	if strings.Contains(u.Query().Get("scope"), "webhook") {
		t.Fatal("webhook scope requested without opt-in")
	}
	u, _ = url.Parse(d.AuthURL(AuthParams{RedirectURI: "https://p/cb", State: "s", Challenge: "ch", Notify: true}))
	if u.Query().Get("scope") != "identify email webhook.incoming" {
		t.Fatal(u.Query().Get("scope"))
	}
	tok, err := d.Exchange(context.Background(), "https://p/cb", "code", "ver")
	if err != nil || tok.WebhookURL != "https://discord.com/api/webhooks/1/x" {
		t.Fatal(tok, err)
	}
	id, err := d.Identity(context.Background(), tok)
	if err != nil || id.ID != "42" || id.Username != "Neo" || id.Email != "" || !strings.HasSuffix(id.AvatarURL, "/42/abc.png") {
		t.Fatalf("%+v %v", id, err) // unverified email must be dropped
	}
	if _, err := d.Exchange(context.Background(), "x", "code", "wrong"); err == nil {
		t.Fatal("expected exchange failure")
	}
}
