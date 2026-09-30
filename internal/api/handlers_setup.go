package api

import (
	"time"

	"github.com/gofiber/fiber/v3"

	"botpanel/internal/service"
)

type settingsBody struct {
	PublicURL     *string `json:"public_url"`
	GitHubID      *string `json:"github_client_id"`
	GitHubSecret  *string `json:"github_client_secret"`
	DiscordID     *string `json:"discord_client_id"`
	DiscordSecret *string `json:"discord_client_secret"`
	AllowSignup   *bool   `json:"oauth_allow_signup"`
}

func (b settingsBody) input() service.SettingsInput {
	return service.SettingsInput{PublicURL: b.PublicURL, GitHubID: b.GitHubID, GitHubSecret: b.GitHubSecret,
		DiscordID: b.DiscordID, DiscordSecret: b.DiscordSecret, AllowSignup: b.AllowSignup}
}

func settingsJSON(v service.SettingsView) fiber.Map {
	return fiber.Map{"public_url": v.PublicURL, "github_client_id": v.GitHubID, "github_secret_set": v.GitHubSecretSet,
		"discord_client_id": v.DiscordID, "discord_secret_set": v.DiscordSecSet, "oauth_allow_signup": v.AllowSignup,
		"locked": v.Locked, "github_enabled": v.GitHubEnabled, "discord_enabled": v.DiscordEnabled}
}

// setupStatus is public: the interface uses it to send a fresh installation
// to the setup wizard.
func (s *server) setupStatus(c fiber.Ctx) error {
	need, err := s.settings.SetupNeeded(c.Context())
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"needed": need, "code_file": s.setupCodeFile})
}

func (s *server) setupCheck(c fiber.Ctx) error {
	var in struct {
		Code string `json:"code"`
	}
	if err := decode(c, &in); err != nil {
		return err
	}
	if err := s.settings.CheckSetupCode(c.Context(), in.Code); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (s *server) setupComplete(c fiber.Ctx) error {
	var in struct {
		Code     string       `json:"code"`
		Email    string       `json:"email"`
		Password string       `json:"password"`
		Settings settingsBody `json:"settings"`
	}
	if err := decode(c, &in); err != nil {
		return err
	}
	sess, err := s.settings.CompleteSetup(c.Context(), service.SetupInput{Code: in.Code, Email: in.Email, Password: in.Password,
		Settings: in.Settings.input()}, deviceLabel(c.Get(fiber.HeaderUserAgent)))
	if sess.Token != "" {
		s.setSessionCookie(c, sess.Token, time.UnixMilli(sess.ExpiresAtMS))
		if s.onSetupDone != nil {
			s.onSetupDone()
		}
	}
	if err != nil && sess.Token == "" {
		return err
	}
	out := fiber.Map{"user": toUser(sess.User), "csrf_token": sess.CSRF}
	if err != nil {
		out["settings_error"] = err.Error()
	}
	return c.JSON(out)
}

func (s *server) getSettings(c fiber.Ctx) error {
	v, err := s.settings.View(c.Context(), currentUser(c))
	if err != nil {
		return err
	}
	return c.JSON(settingsJSON(v))
}

func (s *server) putSettings(c fiber.Ctx) error {
	var in settingsBody
	if err := decode(c, &in); err != nil {
		return err
	}
	if err := s.settings.Update(c.Context(), currentUser(c), in.input()); err != nil {
		return err
	}
	return s.getSettings(c)
}
