package api

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v3"

	"botpanel/internal/domain"
)

// StatsSource follows a container's Docker stats stream.
type StatsSource interface {
	StreamStats(ctx context.Context, containerID string, fn func(domain.ResourceSample) bool) error
}

const (
	diskRefresh    = 30 * time.Second
	statsIdleWait  = 3 * time.Second
	statsReauth    = 20 * time.Second
	statsKeepAlive = 15 * time.Second
)

// diskCache memoizes workspace usage: walking a tree per client per second
// would be expensive.
type diskCache struct {
	mu sync.Mutex
	m  map[string]diskEntry
}

type diskEntry struct {
	at   time.Time
	used int64
	err  error
}

func (d *diskCache) get(botID string, f func() (int64, error)) (int64, error) {
	d.mu.Lock()
	if e, ok := d.m[botID]; ok && time.Since(e.at) < diskRefresh {
		d.mu.Unlock()
		return e.used, e.err
	}
	d.mu.Unlock()
	used, err := f()
	d.mu.Lock()
	if d.m == nil || len(d.m) > 1000 {
		d.m = map[string]diskEntry{}
	}
	d.m[botID] = diskEntry{time.Now(), used, err}
	d.mu.Unlock()
	return used, err
}

type gaugeEvent struct {
	Running       bool    `json:"running"`
	CPUCores      float64 `json:"cpu_cores"`
	CPULimitCores float64 `json:"cpu_limit_cores"`
	CPUPercent    float64 `json:"cpu_percent"` // of the bot's own limit
	MemUsedBytes  int64   `json:"mem_used_bytes"`
	MemLimitBytes int64   `json:"mem_limit_bytes"`
	PIDs          int64   `json:"pids"`
	NetRxBytes    int64   `json:"net_rx_bytes"`
	NetTxBytes    int64   `json:"net_tx_bytes"`
	DiskUsedBytes int64   `json:"disk_used_bytes"`
	DiskTotal     uint64  `json:"disk_total_bytes"` // node filesystem
	DiskFree      uint64  `json:"disk_free_bytes"`
}

// statsStream serves live resource gauges as Server-Sent Events. While the bot
// runs it relays Docker's stats stream (about one frame per second); otherwise
// it reports running=false and keeps polling for a start.
func (s *server) statsStream(c fiber.Ctx) error {
	if s.stats == nil {
		return fiber.ErrNotFound
	}
	user := currentUser(c)
	botID := strings.Clone(c.Params("id"))
	bot, err := s.bots.Authorize(c.Context(), user, botID, domain.PermViewConsole)
	if err != nil {
		return err
	}
	release, ok := s.statsLimit.Acquire(user.ID, bot.ID)
	if !ok {
		return fiber.NewError(fiber.StatusTooManyRequests, "too many live views open")
	}
	token := currentToken(c)
	c.Set(fiber.HeaderContentType, "text/event-stream")
	c.Set(fiber.HeaderCacheControl, "no-cache")
	c.Set("X-Accel-Buffering", "no") // nginx: do not buffer the stream

	return c.SendStreamWriter(func(w *bufio.Writer) {
		defer release()
		ctx, cancel := context.WithCancel(s.baseCtx)
		defer cancel()

		gone := false // the client disconnected: stop, do not reconnect to Docker
		send := func(ev gaugeEvent) bool {
			b, _ := json.Marshal(ev)
			if _, err := fmt.Fprintf(w, "event: stats\ndata: %s\n\n", b); err != nil {
				gone = true
				return false
			}
			if w.Flush() != nil {
				gone = true
			}
			return !gone
		}
		total, free, _ := s.files.Statfs()
		lastAuth := time.Now()
		for ctx.Err() == nil {
			// Session, permission and bot state are re-checked regularly so
			// logout, revocation and deletion end the stream.
			u, err := s.auth.Authenticate(ctx, token)
			if err != nil {
				return
			}
			cur, err := s.bots.Authorize(ctx, u, botID, domain.PermViewConsole)
			if err != nil {
				return
			}
			lastAuth = time.Now()
			disk, _ := s.disk.get(botID, func() (int64, error) { return s.files.Usage(botID) })
			base := gaugeEvent{CPULimitCores: float64(cur.NanoCPUs) / 1e9, MemLimitBytes: cur.MemoryBytes,
				DiskUsedBytes: disk, DiskTotal: total, DiskFree: free}
			if cur.ObservedState != "running" || cur.ContainerID == nil {
				if !send(base) {
					return
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(statsIdleWait):
				}
				continue
			}
			err = s.stats.StreamStats(ctx, *cur.ContainerID, func(m domain.ResourceSample) bool {
				ev := base
				ev.Running = true
				ev.CPUCores, ev.MemUsedBytes, ev.PIDs, ev.NetRxBytes, ev.NetTxBytes = m.CPUCores, m.MemUsedBytes, m.PIDs, m.NetRxBytes, m.NetTxBytes
				if m.MemLimitBytes > 0 {
					ev.MemLimitBytes = m.MemLimitBytes
				}
				if ev.CPULimitCores > 0 {
					ev.CPUPercent = min(100, m.CPUCores/ev.CPULimitCores*100)
				}
				if d, err := s.disk.get(botID, func() (int64, error) { return s.files.Usage(botID) }); err == nil {
					ev.DiskUsedBytes = d
				}
				if !send(ev) {
					return false
				}
				return time.Since(lastAuth) < statsReauth // fall out to re-authorize
			})
			if gone {
				return
			}
			if err != nil && !errors.Is(err, context.Canceled) && ctx.Err() == nil {
				s.log.Debug("stats stream ended", "bot", botID, "err", err)
				select {
				case <-ctx.Done():
					return
				case <-time.After(statsIdleWait):
				}
			}
		}
	})
}
