package api

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"botpanel/internal/domain"
	"botpanel/internal/service"
	"botpanel/internal/sitehost"
)

type fakeTXT struct {
	mu   sync.Mutex
	recs map[string][]string
}

func (f *fakeTXT) LookupTXT(_ context.Context, name string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r, ok := f.recs[name]; ok {
		return r, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
}

func (f *fakeTXT) set(name string, vals ...string) {
	f.mu.Lock()
	f.recs[name] = vals
	f.mu.Unlock()
}

type siteRig struct {
	e     *env
	svc   *service.SiteService
	host  http.Handler
	dns   *fakeTXT
	owner *client
}

func newSiteRig(t *testing.T) *siteRig {
	e := newEnv(t)
	dns := &fakeTXT{recs: map[string][]string{}}
	svc := &service.SiteService{Store: e.db, Bots: e.bots, Dir: filepath.Join(t.TempDir(), "sites"), BaseURL: "http://sites.test:8081",
		PanelHost: "panel.example.com", MaxBytes: 8 << 20, MaxPerUser: 3, Resolver: dns}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); svc.Wait() })
	if err := svc.Start(ctx); err != nil {
		t.Fatal(err)
	}
	e.app = New(Deps{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), DB: e.db, Auth: e.auth, Bots: e.bots, Catalog: e.bots.Catalog,
		SecureCookies: true, Nodes: e.db, MaxUpload: 2 << 20, Sites: svc})
	return &siteRig{e: e, svc: svc, host: &sitehost.Handler{Sites: svc}, dns: dns, owner: e.user("owner@x.io", domain.RoleUser)}
}

func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for n, c := range files {
		w, err := zw.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(c))
	}
	zw.Close()
	return buf.Bytes()
}

// get requests a hosted page by host name.
func (r *siteRig) get(host, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path, nil)
	req.Host = host
	rec := httptest.NewRecorder()
	r.host.ServeHTTP(rec, req)
	return rec
}

func TestStaticSiteLifecycle(t *testing.T) {
	r := newSiteRig(t)
	c := r.owner
	var info struct {
		Enabled    bool
		ExampleURL string `json:"example_url"`
	}
	json.Unmarshal(c.mustStatus(200, "GET", "/api/v1/sites-info", nil), &info)
	if !info.Enabled || info.ExampleURL != "http://example.sites.test:8081" {
		t.Fatalf("info = %+v", info)
	}
	var site struct{ ID, Slug, URL string }
	json.Unmarshal(c.mustStatus(201, "POST", "/api/v1/sites", map[string]string{"name": "My Docs!"}), &site)
	if site.Slug != "my-docs" || site.URL != "http://my-docs.sites.test:8081" {
		t.Fatalf("site = %+v", site)
	}
	c.mustStatus(400, "POST", "/api/v1/sites", map[string]string{"name": "x", "slug": "my-docs"})
	c.mustStatus(400, "POST", "/api/v1/sites", map[string]string{"name": "x", "slug": "admin"})
	c.mustStatus(400, "POST", "/api/v1/sites", map[string]string{"name": "x", "slug": "Bad_Slug"})
	host := "my-docs.sites.test:8081"
	if rec := r.get(host, "/"); rec.Code != 503 {
		t.Fatalf("empty site: %d", rec.Code)
	}

	// A zipped dist/ folder is unwrapped; dotfiles are never served.
	up := zipOf(t, map[string]string{"dist/index.html": "home", "dist/about.html": "about", "dist/docs/index.html": "docs",
		"dist/.env": "SECRET=1", "dist/404.html": "custom 404", "dist/app.js": "js"})
	resp, body := c.raw("POST", "/api/v1/sites/"+site.ID+"/upload", up, "application/zip")
	if resp.StatusCode != 201 {
		t.Fatalf("upload: %d %s", resp.StatusCode, body)
	}
	for _, tc := range []struct {
		path, want string
		code       int
	}{
		{"/", "home", 200}, {"/about", "about", 200}, {"/about.html", "about", 200}, {"/docs/", "docs", 200},
		{"/app.js", "js", 200}, {"/.env", "custom 404", 404}, {"/missing", "custom 404", 404}, {"/../../etc/passwd", "custom 404", 404},
	} {
		rec := r.get(host, tc.path)
		if rec.Code != tc.code || rec.Body.String() != tc.want {
			t.Fatalf("GET %s: %d %q", tc.path, rec.Code, rec.Body.String())
		}
	}
	if rec := r.get(host, "/docs"); rec.Code != 301 || rec.Header().Get("Location") != "/docs/" {
		t.Fatalf("directory redirect: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	if rec := r.get(host, "/"); rec.Header().Get("Cache-Control") != "no-cache" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("headers = %v", rec.Header())
	}
	if rec := r.get("nothing.sites.test", "/"); rec.Code != 404 || !strings.Contains(rec.Body.String(), "No site here") {
		t.Fatalf("unknown host: %d", rec.Code)
	}
	if rec := r.get("panel.example.com", "/"); rec.Code != 404 {
		t.Fatalf("panel host served a site: %d", rec.Code)
	}

	// Single-page applications fall back to index.html.
	c.mustStatus(200, "PATCH", "/api/v1/sites/"+site.ID, map[string]any{"spa": true})
	if rec := r.get(host, "/deep/link"); rec.Code != 200 || rec.Body.String() != "home" {
		t.Fatalf("spa fallback: %d %q", rec.Code, rec.Body.String())
	}

	// A new release replaces the old one; rolling back restores it.
	c.raw("POST", "/api/v1/sites/"+site.ID+"/upload", zipOf(t, map[string]string{"index.html": "v2"}), "application/zip")
	if rec := r.get(host, "/"); rec.Body.String() != "v2" {
		t.Fatalf("after second upload: %q", rec.Body.String())
	}
	var detail struct {
		Releases []struct {
			ID      string
			Current bool
		}
	}
	json.Unmarshal(c.mustStatus(200, "GET", "/api/v1/sites/"+site.ID, nil), &detail)
	if len(detail.Releases) != 2 || !detail.Releases[0].Current {
		t.Fatalf("releases = %+v", detail.Releases)
	}
	c.mustStatus(204, "POST", "/api/v1/sites/"+site.ID+"/releases/"+detail.Releases[1].ID+"/activate", nil)
	if rec := r.get(host, "/"); rec.Body.String() != "home" {
		t.Fatalf("after rollback: %q", rec.Body.String())
	}

	// Bad archives change nothing.
	if resp, _ := c.raw("POST", "/api/v1/sites/"+site.ID+"/upload", []byte("not a zip"), "application/zip"); resp.StatusCode != 400 {
		t.Fatalf("bad zip: %d", resp.StatusCode)
	}
	if rec := r.get(host, "/"); rec.Body.String() != "home" {
		t.Fatal("a rejected upload changed the site")
	}

	// Access follows the workspace: outsiders see nothing, viewers only read.
	other := r.e.user("other@x.io", domain.RoleUser)
	other.mustStatus(404, "GET", "/api/v1/sites/"+site.ID, nil)
	var ws wsList
	json.Unmarshal(c.mustStatus(200, "GET", "/api/v1/workspaces", nil), &ws)
	c.mustStatus(200, "PUT", "/api/v1/workspaces/"+ws.Workspaces[0].ID+"/members", map[string]string{"email": "other@x.io", "role": "viewer"})
	other.mustStatus(200, "GET", "/api/v1/sites/"+site.ID, nil)
	if resp, _ := other.raw("POST", "/api/v1/sites/"+site.ID+"/upload", up, "application/zip"); resp.StatusCode != 403 {
		t.Fatalf("viewer upload: %d", resp.StatusCode)
	}

	// Suspension by an administrator stops serving.
	admin := r.e.user("root@x.io", domain.RoleAdmin)
	admin.mustStatus(204, "PATCH", "/api/v1/admin/sites/"+site.ID, map[string]bool{"disabled": true})
	if rec := r.get(host, "/"); rec.Code != 503 {
		t.Fatalf("suspended: %d", rec.Code)
	}
	c.mustStatus(403, "PATCH", "/api/v1/admin/sites/"+site.ID, map[string]bool{"disabled": false})
	admin.mustStatus(204, "PATCH", "/api/v1/admin/sites/"+site.ID, map[string]bool{"disabled": false})

	c.mustStatus(204, "DELETE", "/api/v1/sites/"+site.ID, nil)
	if rec := r.get(host, "/"); rec.Code != 404 {
		t.Fatalf("deleted site still served: %d", rec.Code)
	}
}

func TestBotSitePageAndEditableDraft(t *testing.T) {
	r := newSiteRig(t)
	c := r.owner
	botID := c.createBot("Beacon")

	var detail struct {
		Site struct {
			ID        string  `json:"id"`
			URL       string  `json:"url"`
			Mode      string  `json:"mode"`
			PageTitle string  `json:"page_title"`
			BotID     *string `json:"bot_id"`
		} `json:"site"`
	}
	json.Unmarshal(c.mustStatus(201, "POST", "/api/v1/bots/"+botID+"/site", map[string]string{"slug": "beacon-page"}), &detail)
	if detail.Site.BotID == nil || *detail.Site.BotID != botID || detail.Site.Mode != "page" || detail.Site.PageTitle != "Beacon" {
		t.Fatalf("bot site = %+v", detail.Site)
	}
	c.mustStatus(400, "POST", "/api/v1/bots/"+botID+"/site", map[string]string{"slug": "another-page"})

	patch := map[string]any{
		"page_title": "Beacon community", "page_description": "Commands, status, and community links.",
		"page_theme": "daylight", "page_accent": "#3366cc", "page_html": "<h2>Join us</h2>",
		"page_css": ".author-content{max-width:40rem}", "widgets_public": true,
	}
	c.mustStatus(200, "PATCH", "/api/v1/sites/"+detail.Site.ID, patch)
	rec := r.get("beacon-page.sites.test", "/")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Beacon community") || !strings.Contains(rec.Body.String(), "Join us") {
		t.Fatalf("public page: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "owner@x.io") || strings.Contains(rec.Body.String(), "Commands, status, and community links.</p></header><section class=\"author-content\"></section>") {
		t.Fatal("public page leaked private identity or omitted configured content")
	}
	if rec := r.get("beacon-page.sites.test", "/private"); rec.Code != 404 {
		t.Fatalf("generated page unknown path = %d", rec.Code)
	}

	// Site files are a private draft until explicitly published and selected.
	var list struct{ Entries []entryDTO }
	json.Unmarshal(c.mustStatus(200, "GET", "/api/v1/sites/"+detail.Site.ID+"/files?path=.", nil), &list)
	if len(list.Entries) == 0 {
		t.Fatal("new site draft was not seeded")
	}
	resp, body := c.raw("PUT", "/api/v1/sites/"+detail.Site.ID+"/files/content?path=index.html", []byte("<!doctype html><title>Hand made</title><h1>Custom site</h1>"), "text/html")
	if resp.StatusCode != 204 {
		t.Fatalf("write draft: %d %s", resp.StatusCode, body)
	}
	c.mustStatus(201, "POST", "/api/v1/sites/"+detail.Site.ID+"/files/publish", nil)
	c.mustStatus(200, "PATCH", "/api/v1/sites/"+detail.Site.ID, map[string]string{"mode": "files"})
	rec = r.get("beacon-page.sites.test", "/")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Custom site") {
		t.Fatalf("custom file site: %d %s", rec.Code, rec.Body.String())
	}
}

func TestSiteCustomDomains(t *testing.T) {
	r := newSiteRig(t)
	c := r.owner
	var site struct{ ID string }
	json.Unmarshal(c.mustStatus(201, "POST", "/api/v1/sites", map[string]string{"name": "Blog", "slug": "blog"}), &site)
	c.raw("POST", "/api/v1/sites/"+site.ID+"/upload", zipOf(t, map[string]string{"index.html": "blog"}), "application/zip")
	base := "/api/v1/sites/" + site.ID + "/domains"

	for _, bad := range []string{"panel.example.com", "x.panel.example.com", "blog.sites.test", "localhost", "1.2.3.4", "https://a.com/", "nodot", "a..com", "-a.com"} {
		c.mustStatus(400, "POST", base, map[string]string{"domain": bad})
	}
	var d struct {
		Domain   string
		Verified bool
		TXTName  string  `json:"txt_name"`
		TXTValue string  `json:"txt_value"`
		Error    *string `json:"last_error"`
		RecType  string  `json:"record_type"`
		RecValue string  `json:"record_target"`
	}
	json.Unmarshal(c.mustStatus(201, "POST", base, map[string]string{"domain": " WWW.Example.COM. "}), &d)
	if d.Domain != "www.example.com" || d.Verified || d.TXTName != "_botforge-verify.www.example.com" || !strings.HasPrefix(d.TXTValue, "botforge-verify=") ||
		d.RecType != "CNAME" || d.RecValue != "blog.sites.test" {
		t.Fatalf("domain = %+v", d)
	}
	// Not served, and no certificate, before verification.
	if rec := r.get("www.example.com", "/"); rec.Code != 404 {
		t.Fatalf("unverified domain served: %d", rec.Code)
	}
	if r.svc.TLSAllowed("www.example.com") {
		t.Fatal("certificate allowed for an unverified domain")
	}
	json.Unmarshal(c.mustStatus(200, "POST", base+"/www.example.com/verify", nil), &d)
	if d.Verified || d.Error == nil || !strings.Contains(*d.Error, "No TXT record") {
		t.Fatalf("verify without record = %+v", d)
	}
	r.dns.set(d.TXTName, "unrelated", d.TXTValue)
	json.Unmarshal(c.mustStatus(200, "POST", base+"/www.example.com/verify", nil), &d)
	if !d.Verified {
		t.Fatalf("verify = %+v", d)
	}
	if rec := r.get("www.example.com:443", "/"); rec.Code != 200 || rec.Body.String() != "blog" {
		t.Fatalf("verified domain: %d %q", rec.Code, rec.Body.String())
	}
	if !r.svc.TLSAllowed("www.example.com") || !r.svc.TLSAllowed("blog.sites.test") || r.svc.TLSAllowed("evil.test") {
		t.Fatal("TLS permission wrong")
	}
	// The TLS permission endpoint answers only loopback peers.
	req := httptest.NewRequest("GET", sitehost.TLSAskPath+"?domain=www.example.com", nil)
	req.RemoteAddr = "127.0.0.1:5000"
	rec := httptest.NewRecorder()
	r.host.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("tls ask: %d", rec.Code)
	}
	req.RemoteAddr = "203.0.113.9:5000"
	rec = httptest.NewRecorder()
	r.host.ServeHTTP(rec, req)
	if rec.Code != 404 {
		t.Fatalf("tls ask from outside: %d", rec.Code)
	}

	// Another site cannot take a verified domain.
	var other struct{ ID string }
	json.Unmarshal(c.mustStatus(201, "POST", "/api/v1/sites", map[string]string{"name": "Other"}), &other)
	c.mustStatus(400, "POST", "/api/v1/sites/"+other.ID+"/domains", map[string]string{"domain": "www.example.com"})

	c.mustStatus(204, "DELETE", base+"/www.example.com", nil)
	if rec := r.get("www.example.com", "/"); rec.Code != 404 {
		t.Fatalf("removed domain still served: %d", rec.Code)
	}
	// The per-account site limit applies (3 in this rig).
	c.mustStatus(201, "POST", "/api/v1/sites", map[string]string{"name": "Third"})
	c.mustStatus(400, "POST", "/api/v1/sites", map[string]string{"name": "Fourth"})
}
