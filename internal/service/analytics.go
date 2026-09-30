package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"botpanel/internal/auth"
	"botpanel/internal/domain"
	"botpanel/internal/store/sqlite"
)

// AnalyticsStore is the persistence surface for bot-pushed telemetry.
type AnalyticsStore interface {
	GetBotIDByTelemetryKey(ctx context.Context, hash []byte) (string, error)
	InsertBotTelemetry(ctx context.Context, rows []domain.BotTelemetryLog) error
	ListBotTelemetry(ctx context.Context, botID, kind string, sinceMS int64, limit int) ([]domain.BotTelemetryLog, error)
	AggregateBotCommands(ctx context.Context, botID string, sinceMS int64, limit int) ([]sqlite.CommandUsage, error)
	ListBotEvents(ctx context.Context, botID string, limit int) ([]domain.BotTelemetryLog, error)
	HasTelemetryKey(ctx context.Context, botID string) (bool, error)
	SetBotTelemetryKey(ctx context.Context, botID string, hash []byte, nowMS int64) error
	PruneBotTelemetry(ctx context.Context, beforeMS int64, batch int) (int64, error)
	BotsOverTelemetryCap(ctx context.Context, keep int) ([]string, error)
	TrimBotTelemetry(ctx context.Context, botID string, keep, batch int) (int64, error)
	UpsertBotWidgets(ctx context.Context, widgets []domain.BotWidget) error
	ListBotWidgets(ctx context.Context, botID string) ([]domain.BotWidget, error)
}

// Limits on what a bot may push.
const (
	MaxTelemetryBody    = 64 << 10
	maxStatsPerPush     = 16
	maxCommandsPerPush  = 20
	maxEventsPerPush    = 10
	maxWidgetsPerPush   = 24
	maxEventData        = 1024
	statMinInterval     = 10 * time.Second // per (bot, stat): faster samples are dropped
	MaxTelemetryRowsBot = 20000
	telemetryKeyPrefix  = "bpt_"
	maxSeriesRows       = 6000
)

var telemetryName = regexp.MustCompile(`^[A-Za-z0-9_.:\- ]{1,64}$`)

// TelemetryPayload is what a bot pushes.
type TelemetryPayload struct {
	// Ready is the bot's own view of its Discord connection (optional).
	Ready    *bool              `json:"ready"`
	Stats    map[string]float64 `json:"stats"`
	Commands []struct {
		Name  string `json:"name"`
		Count *int   `json:"count"`
	} `json:"commands"`
	Events []struct {
		Name string          `json:"name"`
		Data json.RawMessage `json:"data"`
	} `json:"events"`
	Widgets []struct {
		Key      string          `json:"key"`
		Kind     string          `json:"kind"`
		Title    string          `json:"title"`
		Position int             `json:"position"`
		Data     json.RawMessage `json:"data"`
	} `json:"widgets"`
}

// Analytics ingests and reports bot-pushed metrics.
type Analytics struct {
	Store AnalyticsStore
	Now   func() time.Time
	// Heartbeat, when set, is told about every accepted push (application
	// health is "the bot is still reporting").
	Heartbeat func(ctx context.Context, botID string, ready *bool)

	mu       sync.Mutex
	lastStat map[string]time.Time
	rate     map[string]*window
}

type window struct {
	n     int
	start time.Time
}

// MaxPushesPerMinute bounds pushes per bot (HTTP requests and WebSocket frames).
const MaxPushesPerMinute = 60

// Allow reports whether the bot may push now (fixed one-minute window).
func (a *Analytics) Allow(botID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.rate == nil {
		a.rate = map[string]*window{}
	}
	now := a.now()
	w := a.rate[botID]
	if w == nil || now.Sub(w.start) >= time.Minute {
		w = &window{start: now}
		a.rate[botID] = w
	}
	w.n++
	return w.n <= MaxPushesPerMinute
}

func (a *Analytics) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

// Authenticate resolves a bearer telemetry key to a bot.
func (a *Analytics) Authenticate(ctx context.Context, key string) (string, error) {
	if !strings.HasPrefix(key, telemetryKeyPrefix) || len(key) > 128 {
		return "", domain.ErrUnauthorized
	}
	id, err := a.Store.GetBotIDByTelemetryKey(ctx, auth.HashToken(key))
	if errors.Is(err, domain.ErrNotFound) {
		return "", domain.ErrUnauthorized
	}
	return id, err
}

// Ingest validates and stores a push. Stat samples that arrive faster than
// statMinInterval per name are dropped (still acknowledged) to bound storage.
func (a *Analytics) Ingest(ctx context.Context, botID string, p TelemetryPayload) (stored int, err error) {
	if len(p.Stats) > maxStatsPerPush || len(p.Commands) > maxCommandsPerPush || len(p.Events) > maxEventsPerPush || len(p.Widgets) > maxWidgetsPerPush {
		return 0, domain.Invalid("too many stats, commands, events or widgets in one push")
	}
	now := a.now()
	ms := now.UnixMilli()
	var rows []domain.BotTelemetryLog

	names := make([]string, 0, len(p.Stats))
	for n := range p.Stats {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		v := p.Stats[n]
		if !telemetryName.MatchString(n) || math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > 1e15 {
			return 0, domain.Invalid("invalid stat name or value")
		}
		if !a.stat(botID, n, now) {
			continue
		}
		v2 := v
		rows = append(rows, domain.BotTelemetryLog{BotID: botID, RecordedAtMS: ms, Kind: "stat", Name: n, Value: &v2})
	}
	for _, c := range p.Commands {
		if !telemetryName.MatchString(c.Name) {
			return 0, domain.Invalid("invalid command name")
		}
		n := 1
		if c.Count != nil {
			n = *c.Count
		}
		if n < 1 || n > 1_000_000 {
			return 0, domain.Invalid("command count must be between 1 and 1000000")
		}
		v := float64(n)
		rows = append(rows, domain.BotTelemetryLog{BotID: botID, RecordedAtMS: ms, Kind: "command", Name: c.Name, Value: &v})
	}
	for _, e := range p.Events {
		if !telemetryName.MatchString(e.Name) {
			return 0, domain.Invalid("invalid event name")
		}
		r := domain.BotTelemetryLog{BotID: botID, RecordedAtMS: ms, Kind: "event", Name: e.Name}
		if len(e.Data) > 0 && !bytes.Equal(bytes.TrimSpace(e.Data), []byte("null")) {
			if len(e.Data) > maxEventData || !json.Valid(e.Data) {
				return 0, domain.Invalid("event data must be valid JSON of at most 1024 bytes")
			}
			s := string(e.Data)
			r.PayloadJSON = &s
		}
		rows = append(rows, r)
	}
	widgets := make([]domain.BotWidget, 0, len(p.Widgets))
	seenWidgets := map[string]bool{}
	for _, w := range p.Widgets {
		if !telemetryName.MatchString(w.Key) || seenWidgets[w.Key] || len(strings.TrimSpace(w.Title)) < 1 || len(w.Title) > 80 || w.Position < 0 || w.Position > 1000 {
			return 0, domain.Invalid("invalid widget key, title or position")
		}
		seenWidgets[w.Key] = true
		switch w.Kind {
		case "metric", "status", "progress", "text", "chart", "table", "link":
		default:
			return 0, domain.Invalid("unknown widget kind")
		}
		if len(w.Data) == 0 || len(w.Data) > 4096 || !json.Valid(w.Data) || bytes.Equal(bytes.TrimSpace(w.Data), []byte("null")) {
			return 0, domain.Invalid("widget data must be valid JSON of at most 4096 bytes")
		}
		widgets = append(widgets, domain.BotWidget{BotID: botID, Key: w.Key, Kind: w.Kind, Title: strings.TrimSpace(w.Title), Position: w.Position, PayloadJSON: string(w.Data), UpdatedAtMS: ms})
	}
	// A valid push, even an empty one, is a heartbeat.
	if a.Heartbeat != nil {
		a.Heartbeat(ctx, botID, p.Ready)
	}
	if len(rows) == 0 {
		if len(widgets) > 0 {
			if err := a.Store.UpsertBotWidgets(ctx, widgets); err != nil {
				return 0, err
			}
			return len(widgets), nil
		}
		return 0, nil
	}
	if err := a.Store.InsertBotTelemetry(ctx, rows); err != nil {
		return 0, err
	}
	if len(widgets) > 0 {
		if err := a.Store.UpsertBotWidgets(ctx, widgets); err != nil {
			return 0, err
		}
	}
	return len(rows) + len(widgets), nil
}

// stat reports whether a sample for (bot, name) may be stored now.
func (a *Analytics) stat(botID, name string, now time.Time) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.lastStat == nil {
		a.lastStat = map[string]time.Time{}
	}
	k := botID + "\x00" + name
	if t, ok := a.lastStat[k]; ok && now.Sub(t) < statMinInterval {
		return false
	}
	if len(a.lastStat) > 20000 {
		for k, t := range a.lastStat {
			if now.Sub(t) >= statMinInterval {
				delete(a.lastStat, k)
			}
		}
	}
	a.lastStat[k] = now
	return true
}

// Point is one time-series sample.
type Point struct {
	AtMS  int64   `json:"t"`
	Value float64 `json:"v"`
}

// Series is a named stat with its latest value.
type Series struct {
	Name   string  `json:"name"`
	Latest float64 `json:"latest"`
	Points []Point `json:"points"`
}

// Event is a recent bot event.
type Event struct {
	AtMS int64           `json:"t"`
	Name string          `json:"name"`
	Data json.RawMessage `json:"data,omitempty"`
}

// CommandOut is command usage over the window.
type CommandOut struct {
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

// Report is the dashboard data for one bot.
type Report struct {
	KeySet   bool         `json:"key_set"`
	WindowMS int64        `json:"window_ms"`
	LastAtMS int64        `json:"last_at_ms"`
	Stats    []Series     `json:"stats"`
	Commands []CommandOut `json:"commands"`
	Events   []Event      `json:"events"`
	Widgets  []WidgetOut  `json:"widgets"`
}

type WidgetOut struct {
	Key         string          `json:"key"`
	Kind        string          `json:"kind"`
	Title       string          `json:"title"`
	Position    int             `json:"position"`
	Data        json.RawMessage `json:"data"`
	UpdatedAtMS int64           `json:"updated_at_ms"`
}

// Report builds the dashboard data for a window ending now.
func (a *Analytics) Report(ctx context.Context, botID string, window time.Duration) (Report, error) {
	r := Report{WindowMS: window.Milliseconds(), Stats: []Series{}, Events: []Event{}}
	var err error
	if r.KeySet, err = a.Store.HasTelemetryKey(ctx, botID); err != nil {
		return r, err
	}
	since := a.now().Add(-window).UnixMilli()
	rows, err := a.Store.ListBotTelemetry(ctx, botID, "stat", since, maxSeriesRows)
	if err != nil {
		return r, err
	}
	idx := map[string]int{}
	for _, row := range rows {
		if row.Value == nil {
			continue
		}
		i, ok := idx[row.Name]
		if !ok {
			i = len(r.Stats)
			idx[row.Name] = i
			r.Stats = append(r.Stats, Series{Name: row.Name})
		}
		r.Stats[i].Points = append(r.Stats[i].Points, Point{row.RecordedAtMS, *row.Value})
		r.Stats[i].Latest = *row.Value
		if row.RecordedAtMS > r.LastAtMS {
			r.LastAtMS = row.RecordedAtMS
		}
	}
	sort.Slice(r.Stats, func(i, j int) bool { return r.Stats[i].Name < r.Stats[j].Name })
	cmds, err := a.Store.AggregateBotCommands(ctx, botID, since, 25)
	if err != nil {
		return r, err
	}
	r.Commands = make([]CommandOut, len(cmds))
	for i, c := range cmds {
		r.Commands[i] = CommandOut{c.Name, c.Count}
	}
	evs, err := a.Store.ListBotEvents(ctx, botID, 50)
	if err != nil {
		return r, err
	}
	for _, e := range evs {
		ev := Event{AtMS: e.RecordedAtMS, Name: e.Name}
		if e.PayloadJSON != nil {
			ev.Data = json.RawMessage(*e.PayloadJSON)
		}
		r.Events = append(r.Events, ev)
		if e.RecordedAtMS > r.LastAtMS {
			r.LastAtMS = e.RecordedAtMS
		}
	}
	ws, err := a.Store.ListBotWidgets(ctx, botID)
	if err != nil {
		return r, err
	}
	r.Widgets = make([]WidgetOut, len(ws))
	for i, w := range ws {
		r.Widgets[i] = WidgetOut{w.Key, w.Kind, w.Title, w.Position, json.RawMessage(w.PayloadJSON), w.UpdatedAtMS}
		if w.UpdatedAtMS > r.LastAtMS {
			r.LastAtMS = w.UpdatedAtMS
		}
	}
	return r, nil
}

// Prune enforces time retention and the per-bot row cap in bounded batches.
func (a *Analytics) Prune(ctx context.Context, retention time.Duration) error {
	before := a.now().Add(-retention).UnixMilli()
	for {
		n, err := a.Store.PruneBotTelemetry(ctx, before, 1000)
		if err != nil {
			return err
		}
		if n < 1000 {
			break
		}
	}
	bots, err := a.Store.BotsOverTelemetryCap(ctx, MaxTelemetryRowsBot)
	if err != nil {
		return err
	}
	for _, id := range bots {
		for {
			n, err := a.Store.TrimBotTelemetry(ctx, id, MaxTelemetryRowsBot, 1000)
			if err != nil {
				return err
			}
			if n < 1000 {
				break
			}
		}
	}
	return nil
}
