package api

import (
	"encoding/base64"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"botpanel/internal/auth"
	"botpanel/internal/domain"
	"botpanel/internal/service"
)

func (s *server) setSessionCookie(c fiber.Ctx, token string, exp time.Time) {
	c.Cookie(&fiber.Cookie{
		Name: sessionCookie, Value: token, Path: "/", Expires: exp,
		HTTPOnly: true, Secure: s.secureCookies, SameSite: fiber.CookieSameSiteStrictMode,
	})
}

func (s *server) login(c fiber.Ctx) error {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decode(c, &in); err != nil {
		return err
	}
	// The password is hashed once; the session is issued only after the second
	// step when the account has two-step sign-in.
	u, err := s.auth.CheckPassword(c.Context(), in.Email, in.Password)
	if err != nil {
		s.recordSignIn(c, u, strings.Clone(in.Email), err, "password")
		return err
	}
	if pending, err := s.beginMFA(c, u, "password"); err != nil {
		return err
	} else if pending {
		return c.JSON(fiber.Map{"mfa_required": true})
	}
	sess, err := s.auth.IssueSessionFor(c.Context(), u, deviceLabel(c.Get(fiber.HeaderUserAgent)))
	s.recordSignIn(c, sess.User, strings.Clone(in.Email), err, "password")
	if err != nil {
		return err
	}
	s.setSessionCookie(c, sess.Token, time.UnixMilli(sess.ExpiresAtMS))
	return c.JSON(fiber.Map{"user": toUser(sess.User), "csrf_token": sess.CSRF})
}

func (s *server) logout(c fiber.Ctx) error {
	if err := s.auth.Logout(c.Context(), currentToken(c)); err != nil {
		return err
	}
	s.setSessionCookie(c, "", time.Unix(0, 0))
	return c.SendStatus(fiber.StatusNoContent)
}

func (s *server) me(c fiber.Ctx) error {
	u := currentUser(c)
	return c.JSON(fiber.Map{"user": toUser(u), "csrf_token": auth.CSRFToken(currentToken(c)), "has_password": u.PasswordHash != "",
		// What this installation offers, so the interface can explain missing
		// features instead of calling routes that do not exist.
		"features": fiber.Map{
			"runner": s.bots.Notifier != nil, "console": s.console != nil, "stats": s.stats != nil, "files": s.files != nil,
			"deploy": s.deploy != nil && s.oauth.Enabled("github"), "backups": s.backups != nil, "analytics": s.analytics != nil, "sftp": s.sftp != nil,
			"operations": s.ops != nil, "oauth": s.oauth.AnyEnabled(), "schedules": s.schedules != nil, "mfa": s.mfa != nil, "automation": s.tokens != nil, "health": s.health != nil,
			"sites": s.sites.Enabled(), "workspaces": true,
		}})
}

func (s *server) updateProfile(c fiber.Ctx) error {
	var in struct {
		DisplayName string  `json:"display_name"`
		Avatar      *string `json:"avatar_jpeg"`
	}
	if err := decode(c, &in); err != nil {
		return err
	}
	var avatar []byte
	if in.Avatar != nil && *in.Avatar != "" {
		var err error
		avatar, err = base64.StdEncoding.DecodeString(*in.Avatar)
		if err != nil {
			return domain.Invalid("profile picture is not valid base64")
		}
	}
	u, err := s.auth.UpdateProfile(c.Context(), currentUser(c), in.DisplayName, avatar, in.Avatar != nil)
	if err != nil {
		return err
	}
	return c.JSON(toUser(u))
}

func (s *server) userAvatar(c fiber.Ctx) error {
	u, err := s.auth.Store.GetUserByID(c.Context(), strings.Clone(c.Params("id")))
	if err != nil {
		return err
	}
	if len(u.AvatarJPEG) == 0 {
		return fiber.ErrNotFound
	}
	c.Set(fiber.HeaderContentType, "image/jpeg")
	c.Set(fiber.HeaderCacheControl, "private, max-age=86400")
	return c.Send(u.AvatarJPEG)
}

// deviceLabel reduces a User-Agent to "Browser on OS" so the sessions page is
// recognizable without storing the raw header.
func deviceLabel(ua string) string {
	l := strings.ToLower(ua)
	browser := "Unknown browser"
	switch {
	case strings.Contains(l, "edg/"):
		browser = "Edge"
	case strings.Contains(l, "firefox/"):
		browser = "Firefox"
	case strings.Contains(l, "chrome/") || strings.Contains(l, "chromium/"):
		browser = "Chrome"
	case strings.Contains(l, "safari/"):
		browser = "Safari"
	case strings.HasPrefix(l, "curl/"):
		browser = "curl"
	}
	os := ""
	switch {
	case strings.Contains(l, "android"):
		os = "Android"
	case strings.Contains(l, "iphone") || strings.Contains(l, "ipad"):
		os = "iOS"
	case strings.Contains(l, "windows"):
		os = "Windows"
	case strings.Contains(l, "mac os") || strings.Contains(l, "macintosh"):
		os = "macOS"
	case strings.Contains(l, "linux"):
		os = "Linux"
	}
	if os == "" {
		return browser
	}
	return browser + " on " + os
}

type sessionDTO struct {
	ID           string `json:"id"`
	Device       string `json:"device"`
	CreatedAtMS  int64  `json:"created_at_ms"`
	LastSeenAtMS int64  `json:"last_seen_at_ms"`
	ExpiresAtMS  int64  `json:"expires_at_ms"`
	Current      bool   `json:"current"`
}

func (s *server) listSessions(c fiber.Ctx) error {
	_, cur, err := s.auth.AuthenticateSession(c.Context(), currentToken(c))
	if err != nil {
		return err
	}
	ss, err := s.auth.ListSessions(c.Context(), currentUser(c))
	if err != nil {
		return err
	}
	out := make([]sessionDTO, len(ss))
	for i, x := range ss {
		out[i] = sessionDTO{x.ID, x.Device, x.CreatedAtMS, x.LastSeenAtMS, x.ExpiresAtMS, x.ID == cur.ID}
	}
	return c.JSON(fiber.Map{"sessions": out})
}

func (s *server) revokeSession(c fiber.Ctx) error {
	if err := s.auth.RevokeSession(c.Context(), currentUser(c), strings.Clone(c.Params("sid"))); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (s *server) revokeOtherSessions(c fiber.Ctx) error {
	n, err := s.auth.RevokeOtherSessions(c.Context(), currentUser(c), currentToken(c))
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"revoked": n})
}

func (s *server) changePassword(c fiber.Ctx) error {
	var in struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if err := decode(c, &in); err != nil {
		return err
	}
	if err := s.auth.ChangePassword(c.Context(), currentUser(c), currentToken(c), in.Current, in.New); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (s *server) createUser(c fiber.Ctx) error {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := decode(c, &in); err != nil {
		return err
	}
	if in.Role == "" {
		in.Role = domain.RoleUser
	}
	u, err := s.auth.CreateUser(c.Context(), in.Email, in.Password, in.Role)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(toUser(u))
}

func (s *server) listUsers(c fiber.Ctx) error {
	us, err := s.auth.ListUsers(c.Context(), currentUser(c))
	if err != nil {
		return err
	}
	counts, err := s.auth.BotCounts(c.Context(), currentUser(c))
	if err != nil {
		return err
	}
	type adminUserDTO struct {
		userDTO
		Bots        int  `json:"bots"`
		HasPassword bool `json:"has_password"`
	}
	out := make([]adminUserDTO, len(us))
	for i, u := range us {
		out[i] = adminUserDTO{toUser(u), counts[u.ID], u.PasswordHash != ""}
	}
	return c.JSON(fiber.Map{"users": out})
}

// patchUser changes an account's role or enabled state. stop_bots, with
// disabled=true, also stops every bot the account owns (offboarding).
func (s *server) patchUser(c fiber.Ctx) error {
	var in struct {
		Disabled *bool   `json:"disabled"`
		Role     *string `json:"role"`
		StopBots bool    `json:"stop_bots"`
	}
	if err := decode(c, &in); err != nil {
		return err
	}
	if in.Disabled == nil && in.Role == nil {
		return domain.Invalid("nothing to change")
	}
	id := strings.Clone(c.Params("id"))
	actor := currentUser(c)
	if in.Role != nil {
		if err := s.auth.SetRole(c.Context(), actor, id, *in.Role); err != nil {
			return err
		}
	}
	if in.Disabled != nil {
		if err := s.auth.SetDisabled(c.Context(), actor, id, *in.Disabled); err != nil {
			return err
		}
		if *in.Disabled && in.StopBots {
			if _, err := s.bots.StopOwnedBy(c.Context(), actor, id); err != nil {
				return err
			}
		}
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// recordSignIn keeps successful and failed password/provider sign-ins in the
// account's activity (failed attempts only for existing accounts).
func (s *server) recordSignIn(c fiber.Ctx, u domain.User, email string, err error, method string) {
	if s.audit == nil {
		return
	}
	ip := c.IP()
	ev := domain.AuditEvent{Action: "account.sign_in", Target: &method, IP: &ip}
	if err != nil {
		if norm, nerr := service.NormalizeEmail(email); nerr == nil {
			if acct, gerr := s.auth.Store.GetUserByEmail(c.Context(), norm); gerr == nil {
				ev.Outcome, ev.SubjectUserID, ev.ActorLabel = "denied", &acct.ID, &acct.Email
				s.audit.Record(c.Context(), ev)
			}
		}
		return
	}
	ev.ActorID, ev.ActorLabel, ev.SubjectUserID = &u.ID, &u.Email, &u.ID
	s.audit.Record(c.Context(), ev)
}
