// Package domain holds core BotPanel types shared across layers.
package domain

// LocalNodeID is the stable ID of the node seeded on initial setup.
const LocalNodeID = "6f1c0a52-3b7e-4d0e-9a41-0c5b7d2e8f10"

// LocalNodeName is the name of the seeded local node.
const LocalNodeName = "local"

// Node is a host that runs bot containers.
type Node struct {
	ID         string
	Name       string
	Transport  string // "local" or "https"
	Endpoint   *string
	Enabled    bool
	LastSeenMS *int64
}

// Telemetry is one node resource sample. CPUPercent is normalized to 0-100%
// of ALL logical CPUs.
type Telemetry struct {
	NodeID           string
	SampledAtMS      int64
	CPUPercent       float64
	LogicalCPUs      int
	MemoryUsedBytes  int64
	MemoryTotalBytes int64
	DiskUsedBytes    int64
	DiskTotalBytes   int64
	RunningBots      int

	// Added in migration 0032. Rates are bytes per second averaged over the
	// interval since the previous sample.
	Load1                         float64
	SwapUsedBytes, SwapTotalBytes int64
	NetRxBps, NetTxBps            int64
	DiskReadBps, DiskWriteBps     int64
}

// TelemetryBucket is one point of a downsampled series: the mean of the samples
// in the bucket, with the highest CPU and memory readings kept so a short spike
// is still visible on a week-long chart.
type TelemetryBucket struct {
	Telemetry
	CPUMax    float64
	MemoryMax int64
	Samples   int
}
