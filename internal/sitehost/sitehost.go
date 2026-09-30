// Package sitehost serves hosted static sites on their own listener. It never
// serves the panel: site files are arbitrary user HTML and scripts, so they
// must not share the panel's origin (cookies, CSRF tokens, service workers).
// Hosts are mapped to an immutable release directory opened through os.Root,
// so paths cannot leave the release.
package sitehost

import (
	"errors"
	"html"
	"io/fs"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"botpanel/internal/domain"
)

// Router resolves hosts to releases (service.SiteService).
type Router interface {
	Resolve(host string) (domain.SiteRoute, bool)
	ReleaseRoot(siteID, releaseID string) (*os.Root, error)
	TLSAllowed(host string) bool
}

// Handler serves sites.
type Handler struct {
	Sites Router
	Log   *slog.Logger
}

// TLSAskPath answers on-demand TLS permission checks (Caddy's "ask").
const TLSAskPath = "/.well-known/botforge/tls-allowed"

// NewServer returns an HTTP server with conservative timeouts.
func NewServer(h *Handler) *http.Server {
	return &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      10 * time.Minute, // large files over slow links
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
	if r.URL.Path == TLSAskPath {
		h.tlsAsk(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		page(w, http.StatusMethodNotAllowed, "Method not allowed", "Hosted sites are static: only GET and HEAD requests are served.")
		return
	}
	route, ok := h.Sites.Resolve(r.Host)
	switch {
	case !ok:
		page(w, http.StatusNotFound, "No site here", "No site is published at this address.")
		return
	case route.Disabled:
		page(w, http.StatusServiceUnavailable, "Site unavailable", "This site has been suspended by the administrator of this server.")
		return
	case route.Release == "":
		page(w, http.StatusServiceUnavailable, "Nothing published yet", "This site exists but nothing has been published to it yet.")
		return
	}
	root, err := h.Sites.ReleaseRoot(route.SiteID, route.Release)
	if err != nil {
		h.warn("open release", err)
		page(w, http.StatusServiceUnavailable, "Site unavailable", "This site cannot be served right now.")
		return
	}
	defer root.Close()
	h.serve(w, r, root, route)
}

// tlsAsk lets a TLS-terminating proxy on this host ask whether to issue a
// certificate for ?domain=. Only loopback peers may ask.
func (h *Handler) tlsAsk(w http.ResponseWriter, r *http.Request) {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		http.NotFound(w, r)
		return
	}
	if h.Sites.TLSAllowed(r.URL.Query().Get("domain")) {
		w.WriteHeader(http.StatusOK)
		return
	}
	w.WriteHeader(http.StatusNotFound)
}

// hidden reports whether a path has a dot-segment other than .well-known:
// .git, .env and similar files are never served even when uploaded.
func hidden(p string) bool {
	for _, seg := range strings.Split(p, "/") {
		if strings.HasPrefix(seg, ".") && seg != ".well-known" {
			return true
		}
	}
	return false
}

func (h *Handler) serve(w http.ResponseWriter, r *http.Request, root *os.Root, route domain.SiteRoute) {
	clean := path.Clean("/" + r.URL.Path)
	rel := strings.TrimPrefix(clean, "/")
	if hidden(rel) {
		h.notFound(w, r, root, route)
		return
	}
	var candidates []string
	switch {
	case rel == "":
		candidates = []string{"index.html"}
	case strings.HasSuffix(r.URL.Path, "/"):
		candidates = []string{rel + "/index.html"}
	default:
		candidates = []string{rel}
		if route.CleanURLs && path.Ext(rel) == "" {
			candidates = append(candidates, rel+".html")
		}
	}
	for _, name := range candidates {
		st, err := root.Stat(name)
		if err != nil {
			continue
		}
		if st.IsDir() {
			// /docs → /docs/ so relative links inside the page resolve.
			if _, err := root.Stat(path.Join(name, "index.html")); err == nil {
				target := clean + "/"
				if r.URL.RawQuery != "" {
					target += "?" + r.URL.RawQuery
				}
				http.Redirect(w, r, target, http.StatusMovedPermanently)
				return
			}
			continue
		}
		if h.file(w, r, root, name, http.StatusOK, route.Release) {
			return
		}
	}
	if route.SPA && (path.Ext(rel) == "" || strings.HasSuffix(rel, ".html")) {
		if h.file(w, r, root, "index.html", http.StatusOK, route.Release) {
			return
		}
	}
	h.notFound(w, r, root, route)
}

func (h *Handler) notFound(w http.ResponseWriter, r *http.Request, root *os.Root, route domain.SiteRoute) {
	if h.file(w, r, root, "404.html", http.StatusNotFound, route.Release) {
		return
	}
	page(w, http.StatusNotFound, "Page not found", "There is no page at this address.")
}

// file serves one regular file; false means it does not exist (or is not a
// regular file). Conditional and range requests are handled for 200s.
func (h *Handler) file(w http.ResponseWriter, r *http.Request, root *os.Root, name string, status int, release string) bool {
	f, err := root.Open(name)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			h.warn("open file", err)
		}
		return false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return false
	}
	ext := strings.ToLower(filepath.Ext(name))
	if ct := mime.TypeByExtension(ext); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	// HTML revalidates on every visit so a new release shows immediately;
	// other assets are cached briefly (build tools fingerprint their names).
	if ext == ".html" || ext == ".htm" || ext == "" {
		w.Header().Set("Cache-Control", "no-cache")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=3600")
	}
	if status != http.StatusOK {
		w.WriteHeader(status)
		if r.Method != http.MethodHead {
			_, _ = copyN(w, f, st.Size())
		}
		return true
	}
	w.Header().Set("ETag", `"`+release[:8]+"-"+strconv.FormatInt(st.Size(), 36)+"-"+strconv.FormatInt(st.ModTime().UnixNano(), 36)+`"`)
	http.ServeContent(w, r, name, st.ModTime(), f)
	return true
}

func copyN(w http.ResponseWriter, f *os.File, n int64) (int64, error) {
	buf := make([]byte, 32<<10)
	var total int64
	for total < n {
		m, err := f.Read(buf)
		if m > 0 {
			if _, werr := w.Write(buf[:m]); werr != nil {
				return total, werr
			}
			total += int64(m)
		}
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

func (h *Handler) warn(msg string, err error) {
	if h.Log != nil {
		h.Log.Warn("sitehost: "+msg, "err", err)
	}
}

// page writes a small self-contained status page.
func page(w http.ResponseWriter, status int, title, text string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">` +
		`<title>` + html.EscapeString(title) + `</title><style>` +
		`:root{color-scheme:light dark}body{margin:0;min-height:100vh;display:grid;place-items:center;font:16px/1.5 system-ui,sans-serif;` +
		`background:#f4f1ee;color:#1d1a18}@media(prefers-color-scheme:dark){body{background:#0b0908;color:#f3ece7}}` +
		`main{max-width:32rem;padding:2rem}p.code{font:600 .8rem ui-monospace,monospace;letter-spacing:.14em;opacity:.6;margin:0}` +
		`h1{margin:.25rem 0 .5rem;font-size:1.6rem}p{margin:0;opacity:.8}</style></head><body><main>` +
		`<p class="code">` + strconv.Itoa(status) + `</p><h1>` + html.EscapeString(title) + `</h1><p>` + html.EscapeString(text) + `</p>` +
		`</main></body></html>`))
}
