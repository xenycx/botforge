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
}
