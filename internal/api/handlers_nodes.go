package api

import (
	"context"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"

	"botpanel/internal/domain"
)

// NodeStore is the persistence surface for node and telemetry reads.
type NodeStore interface {
	ListNodes(ctx context.Context) ([]domain.Node, error)
	GetNode(ctx context.Context, id string) (domain.Node, error)
	ListTelemetry(ctx context.Context, nodeID string, sinceMS int64, limit int) ([]domain.Telemetry, error)
	ListTelemetryBuckets(ctx context.Context, nodeID string, sinceMS, bucketMS int64) ([]domain.TelemetryBucket, error)
}

const (
	defaultTelemetryLimit = 120
	maxTelemetryLimit     = 2880 // a day at 30 s
)

type nodeDTO struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Transport  string `json:"transport"`
	Enabled    bool   `json:"enabled"`
	LastSeenMS *int64 `json:"last_seen_at_ms"`
}

type sampleDTO struct {
	SampledAtMS      int64   `json:"sampled_at_ms"`
	CPUPercent       float64 `json:"cpu_percent"`
	LogicalCPUs      int     `json:"logical_cpus"`
	MemoryUsedBytes  int64   `json:"memory_used_bytes"`
	MemoryTotalBytes int64   `json:"memory_total_bytes"`
	DiskUsedBytes    int64   `json:"disk_used_bytes"`
	DiskTotalBytes   int64   `json:"disk_total_bytes"`
	RunningBots      int     `json:"running_bots"`
	Load1            float64 `json:"load1"`
	SwapUsedBytes    int64   `json:"swap_used_bytes"`
	SwapTotalBytes   int64   `json:"swap_total_bytes"`
	NetRxBps         int64   `json:"net_rx_bps"`
	NetTxBps         int64   `json:"net_tx_bps"`
	DiskReadBps      int64   `json:"disk_read_bps"`
	DiskWriteBps     int64   `json:"disk_write_bps"`
}

// listNodes returns nodes with their latest sample. Host-level data is
// administrator-only; regular users see only their own bots' states.
func (s *server) listNodes(c fiber.Ctx) error {
	nodes, err := s.nodes.ListNodes(c.Context())
	if err != nil {
		return err
	}
	type item struct {
		nodeDTO
		Latest *sampleDTO `json:"latest,omitempty"`
	}
	out := make([]item, 0, len(nodes))
	for _, n := range nodes {
		it := item{nodeDTO: nodeDTO{n.ID, n.Name, n.Transport, n.Enabled, n.LastSeenMS}}
		if rows, err := s.nodes.ListTelemetry(c.Context(), n.ID, 0, 1); err == nil && len(rows) == 1 {
			d := toSample(rows[0])
			it.Latest = &d
		}
		out = append(out, it)
	}
	return c.JSON(fiber.Map{"nodes": out})
}

func toSample(t domain.Telemetry) sampleDTO {
	return sampleDTO{t.SampledAtMS, t.CPUPercent, t.LogicalCPUs, t.MemoryUsedBytes, t.MemoryTotalBytes,
		t.DiskUsedBytes, t.DiskTotalBytes, t.RunningBots, t.Load1, t.SwapUsedBytes, t.SwapTotalBytes,
		t.NetRxBps, t.NetTxBps, t.DiskReadBps, t.DiskWriteBps}
}

// nodeTelemetry returns up to `limit` of the newest samples after `since_ms`,
// oldest first.
func (s *server) nodeTelemetry(c fiber.Ctx) error {
	id := strings.Clone(c.Params("id"))
	if _, err := s.nodes.GetNode(c.Context(), id); err != nil {
		return err
	}
	limit, since := defaultTelemetryLimit, int64(0)
	if v := c.Query("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxTelemetryLimit {
			return domain.Invalid("limit must be between 1 and " + strconv.Itoa(maxTelemetryLimit))
		}
		limit = n
	}
	if v := c.Query("since_ms"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			return domain.Invalid("since_ms must be a non-negative integer")
		}
		since = n
	}
	rows, err := s.nodes.ListTelemetry(c.Context(), id, since, limit)
	if err != nil {
		return err
	}
	out := make([]sampleDTO, len(rows))
	for i, r := range rows {
		out[i] = toSample(r)
	}
	return c.JSON(fiber.Map{"node_id": id, "samples": out})
}
