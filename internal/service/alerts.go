package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"botpanel/internal/domain"
	"botpanel/internal/events"
	"botpanel/internal/secrets"
)

// AlertService posts deployment and crash notifications to the Discord webhook
// a user granted when connecting Discord ("webhook.incoming"). Sending is best
// effort: failures are logged and never affect the operation that triggered them.
type AlertService struct {
	Store OAuthStore
	Keys  *secrets.Keyring
	Bots  interface {
		GetBot(ctx context.Context, id string) (domain.Bot, error)
	}
	HTTP *http.Client
	Log  *slog.Logger
	Now  func() time.Time
	// Prefs returns a bot's notification preferences; nil sends everything.
	Prefs func(ctx context.Context, botID string) (domain.AlertPrefs, error)

	mu   sync.Mutex
	last map[string]time.Time // per-bot crash alert throttle
}

const alertCrashInterval = 10 * time.Minute

func (a *AlertService) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

// validWebhook accepts only Discord's own webhook endpoints so a stored URL can
// never be used to make the panel call an arbitrary host.
func validWebhook(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return false
	}
	switch u.Hostname() {
	case "discord.com", "discordapp.com", "ptb.discord.com", "canary.discord.com":
	default:
		return false
	}
	return strings.HasPrefix(u.Path, "/api/webhooks/")
}

func (a *AlertService) webhookFor(ctx context.Context, userID string) (string, bool) {
	accts, err := a.Store.ListOAuthAccounts(ctx, userID)
	if err != nil {
		return "", false
	}
	for _, ac := range accts {
		if ac.Provider != domain.ProviderDiscord || !ac.NotifyEnabled || ac.WebhookCipher == nil || ac.WebhookKeyID == nil {
			continue
		}
		pt, err := a.Keys.Open(ownerNS(userID), secrets.OAuthWebhookName(domain.ProviderDiscord),
			secrets.Sealed{Ciphertext: ac.WebhookCipher, Nonce: ac.WebhookNonce, KeyID: *ac.WebhookKeyID})
		if err != nil || !validWebhook(string(pt)) {
			return "", false
		}
		return string(pt), true
	}
	return "", false
}

// Wants reports whether a bot's owner wants alerts of a kind
// (crash, deploy, backup).
func (a *AlertService) Wants(ctx context.Context, botID, kind string) bool {
	if a == nil {
		return false
	}
	if a.Prefs == nil {
		return true
	}
	p, err := a.Prefs(ctx, botID)
	if err != nil {
		return true
	}
	switch kind {
	case "crash":
		return p.Crash
	case "deploy":
		return p.Deploy
	case "backup":
		return p.Backup
	}
	return true
}

// Send posts a message to the user's webhook if they have one.
func (a *AlertService) Send(ctx context.Context, userID, title, message string) {
	_ = a.SendErr(ctx, userID, title, message)
}

// SendErr is Send that reports whether Discord accepted the message.
func (a *AlertService) SendErr(ctx context.Context, userID, title, message string) error {
	hook, ok := a.webhookFor(ctx, userID)
	if !ok {
		return domain.Invalid("no Discord webhook")
	}
	body, _ := json.Marshal(map[string]any{
		"username": "BotForge", "allowed_mentions": map[string]any{"parse": []string{}},
		"embeds": []map[string]any{{"title": clip(title, 200), "description": clip(message, 1500)}},
	})
	hc := a.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 8 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hook, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := hc.Do(req)
	if err != nil {
		a.warn("discord webhook", errors.New("request failed"))
		return domain.Invalid("Discord could not be reached")
	}
	defer res.Body.Close()
	io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
	if res.StatusCode/100 != 2 {
		a.warn("discord webhook", fmt.Errorf("status %d", res.StatusCode))
		return domain.Invalid(fmt.Sprintf("Discord refused the message (HTTP %d); reconnect Discord notifications", res.StatusCode))
	}
	return nil
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func (a *AlertService) warn(msg string, err error) {
	if a.Log != nil {
		a.Log.Warn(msg, "err", err) // never logs the webhook URL
	}
}

// Watch alerts the owner when a bot settles in a failed state (crash with no
// restart left, or a failure the restart policy will not retry), at most once
// per bot per interval. It returns when ctx ends.
func (a *AlertService) Watch(ctx context.Context, bus *events.Bus) {
	sub := bus.SubscribeAll()
	defer sub.Close()
	for {
		select {
		case <-ctx.Done():
			return
		case st := <-sub.C:
			if st.ObservedState != "failed" || st.LastError == "" || st.DesiredState != domain.DesiredRunning {
				continue
			}
			a.mu.Lock()
			if a.last == nil {
				a.last = map[string]time.Time{}
			}
			if t, ok := a.last[st.BotID]; ok && a.now().Sub(t) < alertCrashInterval {
				a.mu.Unlock()
				continue
			}
			a.last[st.BotID] = a.now()
			a.mu.Unlock()
			bot, err := a.Bots.GetBot(ctx, st.BotID)
			if err != nil || !a.Wants(ctx, st.BotID, "crash") {
				continue
			}
			a.Send(ctx, bot.OwnerID, "⚠️ "+bot.Name+" needs attention", st.LastError)
		}
	}
}
