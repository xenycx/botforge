package api

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"botpanel/internal/domain"
)

type accountInviteDTO struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	Role        string `json:"role"`
	CreatedAtMS int64  `json:"created_at_ms"`
	ExpiresAtMS int64  `json:"expires_at_ms"`
}

func accountInviteOut(v domain.AccountInvite) accountInviteDTO {
	return accountInviteDTO{v.ID, v.Email, v.Role, v.CreatedAtMS, v.ExpiresAtMS}
}

func (s *server) registrationStatus(c fiber.Ctx) error {
	enabled := false
	if s.settings != nil {
		if e, err := s.settings.Effective(c.Context()); err == nil {
			enabled = e.AllowSignup
		}
	}
	providers := []string{}
	if enabled && s.oauth != nil {
		for _, p := range []string{domain.ProviderGitHub, domain.ProviderDiscord} {
			if s.oauth.Enabled(p) {
				providers = append(providers, p)
			}
		}
	}
	return c.JSON(fiber.Map{"enabled": enabled, "providers": providers})
}

func (s *server) previewAccountInvite(c fiber.Ctx) error {
	var in struct {
		Token string `json:"token"`
	}
	if err := decode(c, &in); err != nil {
		return err
	}
	v, err := s.auth.PreviewAccountInvite(c.Context(), in.Token)
	if err != nil {
		return err
	}
	return c.JSON(accountInviteOut(v))
}

func (s *server) registerAccount(c fiber.Ctx) error {
	if s.settings == nil {
		return domain.Invalid("registration is disabled")
	}
	e, err := s.settings.Effective(c.Context())
	if err != nil {
		return err
	}
	if !e.AllowSignup {
		return domain.Invalid("registration is disabled")
	}
	var in struct{ Token, Email, Password string }
	if err := decode(c, &in); err != nil {
		return err
	}
	u, err := s.auth.RegisterWithInvite(c.Context(), in.Token, in.Email, in.Password)
	if err != nil {
		return err
	}
	sess, err := s.auth.IssueSessionFor(c.Context(), u, deviceLabel(c.Get(fiber.HeaderUserAgent)))
	if err != nil {
		return err
	}
	s.setSessionCookie(c, sess.Token, time.UnixMilli(sess.ExpiresAtMS))
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"user": toUser(u), "csrf_token": sess.CSRF})
}

func (s *server) createAccountInvite(c fiber.Ctx) error {
	var in struct {
		Email, Role   string
		ExpiresInDays int `json:"expires_in_days"`
	}
	if err := decode(c, &in); err != nil {
		return err
	}
	v, tok, err := s.auth.CreateAccountInvite(c.Context(), currentUser(c), in.Email, in.Role, in.ExpiresInDays)
	if err != nil {
		return err
	}
	c.Set(fiber.HeaderCacheControl, "no-store")
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"invite": accountInviteOut(v), "token": tok, "path": "/register#" + tok})
}

func (s *server) listAccountInvites(c fiber.Ctx) error {
	vs, err := s.auth.ListAccountInvites(c.Context(), currentUser(c))
	if err != nil {
		return err
	}
	out := make([]accountInviteDTO, len(vs))
	for i, v := range vs {
		out[i] = accountInviteOut(v)
	}
	return c.JSON(fiber.Map{"invites": out})
}

func (s *server) deleteAccountInvite(c fiber.Ctx) error {
	if err := s.auth.DeleteAccountInvite(c.Context(), currentUser(c), strings.Clone(c.Params("id"))); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}
