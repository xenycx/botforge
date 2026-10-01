// Package runner reconciles the desired state stored in SQLite with Docker.
// Desired state is the durable work record; in-memory notifications only reduce
// latency, and periodic resynchronization repairs anything that was missed.
package runner

import (
	"context"
	"errors"
	"io"
	"strconv"
	"time"
)

// Labels applied to every managed container.
const (
	LabelManaged = "botpanel.managed"
	LabelBot     = "botpanel.bot_id"
	LabelNode    = "botpanel.node_id"
	// LabelInstall scopes containers to one installation (database), so two
	// panels sharing a Docker daemon never touch each other's containers.
	LabelInstall    = "botpanel.install"
	LabelGeneration = "botpanel.generation"
	LabelRole       = "botpanel.role"
	LabelSpec       = "botpanel.spec"
)

// Role distinguishes build containers from long-running bot containers.
type Role string

const (
	RoleRuntime    Role = "runtime"
	RoleBuilder    Role = "builder"
	RoleDiagnostic Role = "diagnostic"
)

var (
	// ErrNameConflict means a container with that name already exists; the
	// caller must re-list managed containers instead of blindly retrying.
	ErrNameConflict = errors.New("container name already in use")
	// ErrNoContainer means the container does not exist.
	ErrNoContainer = errors.New("no such container")
)

// Capabilities are the host enforcement features the runner depends on.
type Capabilities struct {
	ServerVersion string
	CgroupV2      bool
	MemoryLimit   bool
	CPUQuota      bool
	PidsLimit     bool
	SwapLimit     bool
	Rootless      bool
	// Problem, when set, is a host configuration issue that prevents limits
	// from being enforced even though the feature flags look available.
	Problem string
}

// Validate fails when a hard limit cannot be enforced by the host.
func (c Capabilities) Validate() error {
	switch {
	case c.Problem != "":
		return errors.New(c.Problem)
	case !c.MemoryLimit:
		return errors.New("docker host cannot enforce memory limits (cgroup memory controller unavailable)")
	case !c.CPUQuota:
		return errors.New("docker host cannot enforce CPU limits (cgroup cpu controller unavailable)")
	case !c.PidsLimit:
		return errors.New("docker host cannot enforce PID limits (cgroup pids controller unavailable)")
	}
	return nil
}

// ContainerSpec is everything needed to create one managed container. Values
// come from the approved runtime recipe and the stored bot; never from a request.
type ContainerSpec struct {
	Name       string
	Role       Role
	BotID      string
	NodeID     string
	InstallID  string // empty: no installation label (tests, legacy)
	Generation int64
	SpecHash   string

	Image             string // immutable reference
	Argv              []string
	Entrypoint        []string // optional; when set, Argv are its arguments
	Ports             []PortBinding
	Env               []string // KEY=VALUE
	WorkspaceHostPath string
	User              string
	Network           string

	MemoryBytes int64
	NanoCPUs    int64
	PidsLimit   int64
	TmpfsBytes  int64
	OpenStdin   bool
}

// PortBinding publishes one container port on the host.
type PortBinding struct {
	HostIP        string
	HostPort      int
	ContainerPort int
	Proto         string // tcp | udp
}

// ContainerInfo is the observed state of a managed container.
type ContainerInfo struct {
	ID         string
	Name       string
	Labels     map[string]string
	State      string // created, running, paused, restarting, exited, dead, removing
	ExitCode   int
	OOMKilled  bool
	StartedAt  time.Time
	FinishedAt time.Time
}

// Role returns the container's role label.
func (c ContainerInfo) Role() Role { return Role(c.Labels[LabelRole]) }

// Generation returns the generation label, or -1 if missing/invalid.
func (c ContainerInfo) Generation() int64 {
	g, err := strconv.ParseInt(c.Labels[LabelGeneration], 10, 64)
	if err != nil {
		return -1
	}
	return g
}

// Live reports whether the container may still be executing.
func (c ContainerInfo) Live() bool {
	return c.State == "running" || c.State == "paused" || c.State == "restarting"
}

// Event is a container lifecycle notification for a managed container.
type Event struct {
	Role        Role
	NodeID      string
	InstallID   string
	BotID       string
	ContainerID string
	Action      string
}

// Docker is the narrow surface the runner needs; internal/docker adapts the
// official SDK to it, and tests substitute a fake.
type Docker interface {
	Capabilities(ctx context.Context) (Capabilities, error)
	// ResolveImage returns an immutable reference for ref, pulling it if absent.
	ResolveImage(ctx context.Context, ref string) (string, error)
	// ListManaged lists managed containers for a bot, or all when botID is "".
	ListManaged(ctx context.Context, botID string) ([]ContainerInfo, error)
	Create(ctx context.Context, spec ContainerSpec) (string, error)
	Start(ctx context.Context, id string) error
	Stop(ctx context.Context, id string, timeout time.Duration) error
	// Kill sends SIGKILL immediately, without the graceful stop period.
	Kill(ctx context.Context, id string) error
	Remove(ctx context.Context, id string) error
	Inspect(ctx context.Context, id string) (ContainerInfo, error)
	// Tail returns the last n lines of a stopped container's output (stdout and
	// stderr merged), for diagnosing failed builds.
	Tail(ctx context.Context, id string, n int) (string, error)
	// Wait blocks until the container is no longer running and returns its exit code.
	Wait(ctx context.Context, id string) (int64, error)
	// Follow copies a container's output (stdout and stderr merged) to w until
	// the container stops or ctx ends.
	Follow(ctx context.Context, id string, w io.Writer) error
	Events(ctx context.Context) (<-chan Event, <-chan error)
}

// BuildRecorder records build stages as durable operations with retained
// output. It is optional; every method must tolerate being called with the
// empty ID returned when recording failed.
type BuildRecorder interface {
	BuildStarted(ctx context.Context, botID string, generation int64) string
	BuildStage(ctx context.Context, id, stage string)
	BuildOutput(id string) io.WriteCloser // nil when output is not retained
	BuildFinished(ctx context.Context, id, status, code, msg string)
}
