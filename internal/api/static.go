package api

import (
	"io/fs"
	"mime"
	"path"
	"strings"

	"github.com/gofiber/fiber/v3"
)

const csp = "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: https:; connect-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'"

// staticHandler serves the built SPA from fsys. Existing files are served
// as-is; extensionless paths fall back to the SPA shell. Missing files that
// look like assets (have an extension) return 404 instead of HTML.
func staticHandler(fsys fs.FS) fiber.Handler {
	return func(c fiber.Ctx) error {
		if c.Method() != fiber.MethodGet && c.Method() != fiber.MethodHead {
			return fiber.ErrMethodNotAllowed
		}
		p := path.Clean("/" + strings.TrimPrefix(c.Path(), "/"))
		name := strings.TrimPrefix(p, "/")
		isRoute := path.Ext(p) == "" // extensionless paths are client-side routes
		if name == "" {
			name = "index.html"
		}
		if fsys == nil {
			return fiber.ErrNotFound
		}

		body, err := fs.ReadFile(fsys, name)
		if err != nil {
			// Prerendered route (e.g. /about -> about.html), then SPA fallback.
			if b, e := fs.ReadFile(fsys, name+".html"); e == nil {
				body, name = b, name+".html"
			} else if isRoute {
				for _, f := range []string{"200.html", "index.html"} {
					if b, e := fs.ReadFile(fsys, f); e == nil {
						body, name, err = b, f, nil
						break
					}
				}
			}
			if body == nil {
				return fiber.ErrNotFound
			}
		}

		ct := mime.TypeByExtension(path.Ext(name))
		switch path.Ext(name) { // not in every system MIME table
		case ".webmanifest":
			ct = "application/manifest+json"
		case ".ico":
			ct = "image/x-icon"
		}
		if ct == "" {
			ct = "application/octet-stream"
		}
		c.Set(fiber.HeaderContentType, ct)
		if strings.HasPrefix(name, "_app/immutable/") {
			c.Set(fiber.HeaderCacheControl, "public, max-age=31536000, immutable")
		} else {
			c.Set(fiber.HeaderCacheControl, "no-cache")
		}
		if strings.HasSuffix(name, ".html") {
			c.Set("Content-Security-Policy", csp)
		}
		if c.Method() == fiber.MethodHead {
			return nil
		}
		return c.Send(body)
	}
}
