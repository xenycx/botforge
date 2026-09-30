package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"botpanel/internal/domain"
)

// HealthStore is the persistence application health and alert rules need.
type HealthStore interface {
	RecordHeartbeat(ctx context.Context, botID string, nowMS int64, ready *bool) error
	GetHealth(ctx context.Context, botID string) (domain.BotHealth, error)
	SetStaleAlerted(ctx context.Context, botID string, on bool) error
	GetAlertPrefs(ctx context.Context, botID string) (domain.AlertPrefs, error)
	SetAlertPrefs(ctx context.Context, botID string, p domain.AlertPrefs, nowMS int64) error
	HeartbeatWatches(ctx context.Context) ([]domain.HeartbeatWatch, error)
}

const (
	heartbeatWriteGap = 15 * time.Second
	healthTick        = time.Minute
)

// HealthService tracks whether bots are responsive by their own account (SDK
// pushes), separately from Docker's process state, and evaluates the
// heartbeat alert rule. Missing SDK data means "unknown", never "failed".
type HealthService struct {
	Store  HealthStore
	Bots   *BotService
	Alerts *AlertService // nil: rules are evaluated but nothing is sent
	Log    *slog.Logger
	Now    func() time.Time

	mu      sync.Mutex
	written map[string]time.Time
}

func (h *HealthService) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

// Heartbeat records an SDK push. Writes are throttled per bot; a change in
// readiness is written at once.
func (h *HealthService) Heartbeat(ctx context.Context, botID string, ready *bool) {
	now := h.now()
	h.mu.Lock()
	if h.written == nil {
		h.written = map[string]time.Time{}
	}
	last, seen := h.written[botID]
	due := !seen || now.Sub(last) >= heartbeatWriteGap || ready != nil
	if due {
		if len(h.written) > 10000 {
			clear(h.written)
		}
		h.written[botID] = now
	}
	h.mu.Unlock()
	if due {
		if err := h.Store.RecordHeartbeat(ctx, botID, now.UnixMilli(), ready); err != nil && h.Log != nil {
			h.Log.Warn("record heartbeat", "bot", botID, "err", err)
		}
	}
}

// HealthView is a bot's application health for the interface.
type HealthView struct {
	State        string // unknown | ok | stale | not_ready
	LastSeenAtMS *int64
	Ready        *bool
	Prefs        domain.AlertPrefs
	Webhook      bool // the owner has Discord notifications set up
}

// Get returns a bot's health and alert preferences (console permission).
func (h *HealthService) Get(ctx context.Context, actor domain.User, botID string) (HealthView, error) {
	b, err := h.Bots.Authorize(ctx, actor, botID, domain.PermViewConsole)
	if err != nil {
		return HealthView{}, err
	}
	prefs, err := h.Store.GetAlertPrefs(ctx, botID)
	if err != nil {
		return HealthView{}, err
	}
	v := HealthView{State: "unknown", Prefs: prefs}
	if h.Alerts != nil {
		_, v.Webhook = h.Alerts.webhookFor(ctx, b.OwnerID)
	}
	hl, err := h.Store.GetHealth(ctx, botID)
	if errors.Is(err, domain.ErrNotFound) {
		return v, nil
	}
	if err != nil {
		return HealthView{}, err
	}
	v.LastSeenAtMS, v.Ready = &hl.LastSeenAtMS, hl.Ready
	after := time.Duration(max(prefs.HeartbeatAfter, 120)) * time.Second
	switch {
	case h.stale(b, hl, after):
		v.State = "stale"
	case hl.Ready != nil && !*hl.Ready:
		v.State = "not_ready"
	default:
		v.State = "ok"
	}
	return v, nil
}

// stale reports whether a running bot has not pushed for `after`, measured
// from its last start so the previous run's pushes do not count against it.
func (h *HealthService) stale(b domain.Bot, hl domain.BotHealth, after time.Duration) bool {
	if b.DesiredState != domain.DesiredRunning || b.ObservedState != "running" {
		return false
	}
	ref := hl.LastSeenAtMS
	if b.LastStartedAtMS != nil && *b.LastStartedAtMS > ref {
		ref = *b.LastStartedAtMS
	}
	return h.now().Sub(time.UnixMilli(ref)) > after
}

// SetPrefs saves a bot's alert preferences (full control of the bot).
func (h *HealthService) SetPrefs(ctx context.Context, actor domain.User, botID string, p domain.AlertPrefs) (domain.AlertPrefs, error) {
	if _, err := h.Bots.Authorize(ctx, actor, botID, domain.PermFullAdmin); err != nil {
		return domain.AlertPrefs{}, err
	}
	if p.HeartbeatAfter != 0 && (p.HeartbeatAfter < 60 || p.HeartbeatAfter > 86400) {
		return domain.AlertPrefs{}, domain.Invalid("the heartbeat alert waits between 1 minute and 24 hours")
	}
	if err := h.Store.SetAlertPrefs(ctx, botID, p, h.now().UnixMilli()); err != nil {
		return domain.AlertPrefs{}, err
	}
	if p.HeartbeatAfter == 0 {
		_ = h.Store.SetStaleAlerted(ctx, botID, false)
	}
	return p, nil
}

// Test sends a test message to the bot owner's Discord webhook.
func (h *HealthService) Test(ctx context.Context, actor domain.User, botID string) error {
	b, err := h.Bots.Authorize(ctx, actor, botID, domain.PermFullAdmin)
	if err != nil {
		return err
	}
	if h.Alerts == nil {
		return domain.Invalid("notifications need Discord sign-in to be configured on this panel")
	}
	if _, ok := h.Alerts.webhookFor(ctx, b.OwnerID); !ok {
		return domain.Invalid("the bot's owner has not turned on Discord notifications (Settings → Connected accounts)")
	}
	return h.Alerts.SendErr(ctx, b.OwnerID, "🔔 Test notification for "+b.Name, "Alerts for this bot will arrive here.")
}

// Run evaluates the heartbeat rule every minute until ctx ends.
func (h *HealthService) Run(ctx context.Context) {
	t := time.NewTicker(healthTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			h.Evaluate(ctx)
		}
	}
}

// Evaluate sends one alert when a running bot stops pushing for longer than
// its threshold, and one recovery message when pushes resume. A bot that
// never pushed is unknown and never alerts.
func (h *HealthService) Evaluate(ctx context.Context) {
	ws, err := h.Store.HeartbeatWatches(ctx)
	if err != nil {
		if h.Log != nil && ctx.Err() == nil {
			h.Log.Warn("heartbeat rules", "err", err)
		}
		return
	}
	for _, w := range ws {
		if w.Health == nil || w.Bot.DesiredState == domain.DesiredDeleted {
			continue
		}
		after := time.Duration(w.Prefs.HeartbeatAfter) * time.Second
		stale := h.stale(w.Bot, *w.Health, after)
		switch {
		case stale && !w.Health.StaleAlerted:
			_ = h.Store.SetStaleAlerted(ctx, w.Bot.ID, true)
			if h.Alerts != nil {
				h.Alerts.Send(ctx, w.Bot.OwnerID, "💤 "+w.Bot.Name+" stopped reporting",
					fmt.Sprintf("No heartbeat for more than %s while the container is running. The bot may be stuck or disconnected from Discord.", after))
			}
		case !stale && w.Health.StaleAlerted:
			_ = h.Store.SetStaleAlerted(ctx, w.Bot.ID, false)
			if h.Alerts != nil && w.Prefs.Recovery && w.Bot.ObservedState == "running" {
				h.Alerts.Send(ctx, w.Bot.OwnerID, "✅ "+w.Bot.Name+" is reporting again", "Heartbeats resumed.")
			}
		}
	}
}
