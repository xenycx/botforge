package api

import (
	"net/url"
	"strings"

	"github.com/gofiber/fiber/v3"

	"botpanel/internal/auth"
	"botpanel/internal/domain"
)

const (
	sessionCookie = "botpanel_session"
	csrfHeader    = "X-CSRF-Token"
)

func isSafeMethod(m string) bool {
	return m == fiber.MethodGet || m == fiber.MethodHead || m == fiber.MethodOptions
}

// originOK rejects cross-origin state-changing requests when the browser
// supplies an Origin header. Non-browser clients omit it and rely on the CSRF token.
func originOK(c fiber.Ctx) bool {
	o := c.Get(fiber.HeaderOrigin)
	if o == "" {
		return true
	}
	u, err := url.Parse(o)
	return err == nil && strings.EqualFold(u.Host, c.Hostname()+portSuffix(c))
}

func portSuffix(c fiber.Ctx) string {
	h := c.Host()
	if i := strings.LastIndex(h, ":"); i >= 0 && !strings.Contains(h[i:], "]") {
		return h[i:]
	}
	return ""
}

func (s *server) checkOrigin(c fiber.Ctx) error {
	if !isSafeMethod(c.Method()) && !originOK(c) {
		return fiber.NewError(fiber.StatusForbidden, "cross-origin request rejected")
	}
	return c.Next()
}

// requireAuth resolves the session cookie and enforces CSRF on unsafe methods.
func (s *server) requireAuth(c fiber.Ctx) error {
	token := strings.Clone(c.Cookies(sessionCookie))
	u, err := s.auth.Authenticate(c.Context(), token)
	if err != nil {
		return err
	}
	if !isSafeMethod(c.Method()) && !auth.CSRFValid(token, c.Get(csrfHeader)) {
		return fiber.NewError(fiber.StatusForbidden, "invalid or missing CSRF token")
	}
	c.Locals(keyUser, u)
	c.Locals(keyToken, token)
	return c.Next()
}

func (s *server) requireAdmin(c fiber.Ctx) error {
	if !currentUser(c).IsAdmin() {
		return domain.ErrForbidden
	}
	return c.Next()
}
