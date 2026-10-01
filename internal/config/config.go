// Package config loads and validates BotPanel application configuration.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// RunnerMode selects how bot lifecycle work is executed.
type RunnerMode string

const (
	// RunnerLocal runs the reconciler in-process (not yet implemented).
	RunnerLocal RunnerMode = "local"
	// RunnerNone disables the local runner; only the control plane runs.
	RunnerNone RunnerMode = "none"
)

// Global lower bounds for per-bot resource limits.
const (
	MinBotMemory int64 = 32 << 20   // 32 MiB
	MinBotCPUs   int64 = 50_000_000 // 0.05 CPU
)

// Config is the validated application configuration.
type Config struct {
	Listen          string
	DBPath          string
	DataRoot        string // parent of per-bot workspaces
	RunnerMode      RunnerMode
	Production      bool
	DBMaxOpenConns  int
	ShutdownTimeout time.Duration

	KeyDir       string // directory of <id>.key files, outside SQLite
	ActiveKeyID  string
	RuntimesDir  string // optional override of the embedded runtime catalog
	SessionTTL   time.Duration
	HashWorkers  int // concurrent Argon2id operations
	MaxBotMemory int64
	MaxBotCPUs   int64 // nano CPUs

	DockerHost       string
	ContainerUser    string // uid:gid of bot processes
	WorkspaceOwner   string // host uid:gid owning workspaces; defaults to ContainerUser
	AllowRootUser    bool   // permit uid/gid 0 (only sensible with a rootless daemon)
	ContainerNetwork string // Docker network mode for bot and build containers
	RunnerWorkers    int
	MaxBuilds        int // concurrent build containers

	// Capacity budgets (0 = unlimited): see service.Limits.
	MaxBotsPerUser  int
	UserMemoryBytes int64
	NodeMemoryBytes int64
	// MinFreeDisk is the free space backups, restores and deployments need
	// before they start (0 disables the check).
	MinFreeDisk  int64
	BuildTimeout time.Duration

	MaxUploadBytes int64 // per file upload / archive

	TelemetryInterval  time.Duration // 30s-60s
	TelemetryRetention time.Duration

	// PublicURL is the externally reachable origin of the panel (for example
	// https://panel.example.com). OAuth redirect URIs are derived from it.
	PublicURL       string
	GitHubClientID  string
	GitHubSecret    string
	DiscordClientID string
	DiscordSecret   string
	// Mailgun (transactional email); the panel's Mail settings are used for
	// whatever these leave empty. The key is a credential.
	MailgunAPIKey, MailgunDomain, MailgunRegion, MailFrom string
	OAuthAllowSignup                                      bool // let unknown OAuth identities create accounts
	// OAuthAllowSignupSet is true when the environment decides it (then the
	// settings page cannot change it).
	OAuthAllowSignupSet bool

	// ProxyHeader names the header carrying the real client IP when behind a
	// reverse proxy on the same host (X-Forwarded-For, CF-Connecting-IP). It is
	// honored only for connections from loopback.
	ProxyHeader string
	// MetricsToken enables /metrics and authenticates Prometheus scrapes.
	// Empty disables the endpoint.
	MetricsToken string

	// SFTPListen is the address of the embedded SFTP server; empty disables it.
	SFTPListen  string
	SFTPMaxFile int64  // largest file an SFTP client may write
	SFTPHostKey string // persistent SSH host key; defaults next to the database (writable state, not the read-only key dir)

	BackupDir      string        // per-bot snapshot archives
	BackupInterval time.Duration // scheduled backups; 0 disables
	BackupKeep     int           // scheduled backups kept per bot
	BackupMaxBytes int64         // largest workspace a backup will include

	// Published-port policy for bots: host ports must be inside [PortMin, PortMax];
	// binding beyond 127.0.0.1 requires PortPublicBind.
	PortMin, PortMax int
	PortPublicBind   bool

	// Static site hosting. SitesListen is a separate listener that only
	// serves hosted sites (never the panel), so uploaded HTML and scripts
	// never share the panel's origin; empty disables site hosting.
	SitesListen string
	// SitesBaseURL defines the default site address: a site with slug "docs"
	// is served at <scheme>://docs.<host>[:port]. Point a wildcard DNS record
	// (*.host) at the reverse proxy in front of SitesListen.
	SitesBaseURL    string
	SitesDir        string // releases, one directory per site
	SiteMaxBytes    int64  // largest release (uncompressed)
	MaxSitesPerUser int    // sites an account may create (0 = unlimited)
	// SitesDNSTarget is the host name custom domains should CNAME to (shown in
	// the domain instructions); defaults to the host of SitesBaseURL.
	SitesDNSTarget string
}

// SitesDomain is the host part of SitesBaseURL ("" when unset).
func (c Config) SitesDomain() string {
	u, err := url.Parse(c.SitesBaseURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// OAuthConfigured reports whether a provider ("github" or "discord") has credentials.
func (c Config) OAuthConfigured(provider string) bool {
	switch provider {
	case "github":
		return c.GitHubClientID != "" && c.GitHubSecret != ""
	case "discord":
		return c.DiscordClientID != "" && c.DiscordSecret != ""
	}
	return false
}

// Load reads configuration from the environment. Production defaults use
// system directories; set BOTPANEL_ENV=development to default to ./.dev-data
// so nothing requires writes outside the working tree.
func Load(getenv func(string) string) (Config, error) {
	return LoadLookup(func(name string) (string, bool) {
		if v := strings.TrimSpace(getenv(name)); v != "" {
			return v, true
		}
		// An explicitly empty variable can disable a development default; the
		// getenv function cannot tell it from an unset one, so ask the process.
		if v, ok := os.LookupEnv(name); ok && strings.TrimSpace(v) == "" && getenv(name) == v {
			return "", true
		}
		return "", false
	})
}

// Lookup reads one variable and reports whether it is set. A variable that is
// set to the empty string is "set": it can switch a feature off even though a
// default or a lower layer would turn it on.
type Lookup func(name string) (value string, set bool)

// LoadLookup is Load over a Lookup, so the configuration can be built from
// layered sources (the process environment plus overrides stored by the panel).
func LoadLookup(look Lookup) (Config, error) {
	getenv := func(name string) string { v, _ := look(name); return v }
	env := strings.ToLower(strings.TrimSpace(getenv("BOTPANEL_ENV")))
	switch env {
	case "", "production", "development":
	default:
		return Config{}, fmt.Errorf("BOTPANEL_ENV must be production or development, got %q", env)
	}
	dev := env == "development"

	c := Config{
		Listen:          "127.0.0.1:8080",
		DBPath:          "/var/lib/botpanel/botpanel.db",
		DataRoot:        "/var/lib/botpanel/bots",
		RunnerMode:      RunnerLocal,
		Production:      !dev,
		DBMaxOpenConns:  4,
		ShutdownTimeout: 10 * time.Second,
		KeyDir:          "/etc/botpanel/keys",
		ActiveKeyID:     "k1",
		SessionTTL:      7 * 24 * time.Hour,
		HashWorkers:     1,
		MaxBotMemory:    4 << 30,
		MaxBotCPUs:      4_000_000_000,

		DockerHost:       "unix:///var/run/docker.sock",
		ContainerUser:    "65532:65532",
		ContainerNetwork: "bridge",
		RunnerWorkers:    2,
		MaxBuilds:        1,
		MinFreeDisk:      1 << 30,
		BuildTimeout:     10 * time.Minute,

		MaxUploadBytes: 32 << 20,

		TelemetryInterval:  30 * time.Second,
		TelemetryRetention: 7 * 24 * time.Hour,
	}
	if dev {
		c.KeyDir = filepath.Join(".dev-data", "keys")
		c.DBPath = filepath.Join(".dev-data", "botpanel.db")
		c.DataRoot = filepath.Join(".dev-data", "bots")
	}
	c.BackupDir = "/var/lib/botpanel/backups"
	if dev {
		c.BackupDir = filepath.Join(".dev-data", "backups")
	}
	c.BackupInterval, c.BackupKeep, c.BackupMaxBytes = 24*time.Hour, 7, 2<<30

	if v := getenv("BOTPANEL_LISTEN"); v != "" {
		c.Listen = v
	}
	if v := getenv("BOTPANEL_DB_PATH"); v != "" {
		c.DBPath = v
	}
	if v := getenv("BOTPANEL_DATA_ROOT"); v != "" {
		c.DataRoot = v
	}
	if v := getenv("BOTPANEL_RUNNER_MODE"); v != "" {
		c.RunnerMode = RunnerMode(strings.ToLower(v))
	}
	if v := getenv("BOTPANEL_DOCKER_HOST"); v != "" {
		c.DockerHost = v
	}
	if v := getenv("BOTPANEL_CONTAINER_USER"); v != "" {
		c.ContainerUser = v
	}
	if v := getenv("BOTPANEL_CONTAINER_NETWORK"); v != "" {
		c.ContainerNetwork = v
	}
	if v := getenv("BOTPANEL_WORKSPACE_OWNER"); v != "" {
		c.WorkspaceOwner = v
	}
	c.AllowRootUser = getenv("BOTPANEL_ALLOW_ROOT_CONTAINER_USER") == "1"
	if v := getenv("BOTPANEL_RUNNER_WORKERS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return Config{}, fmt.Errorf("BOTPANEL_RUNNER_WORKERS: %w", err)
		}
		c.RunnerWorkers = n
	}
	for name, dst := range map[string]*int{"BOTPANEL_MAX_BUILDS": &c.MaxBuilds, "BOTPANEL_MAX_BOTS_PER_USER": &c.MaxBotsPerUser} {
		if v := getenv(name); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				return Config{}, fmt.Errorf("%s: %w", name, err)
			}
			*dst = n
		}
	}
	for name, dst := range map[string]*int64{"BOTPANEL_USER_MEMORY_BYTES": &c.UserMemoryBytes, "BOTPANEL_NODE_MEMORY_BYTES": &c.NodeMemoryBytes,
		"BOTPANEL_MIN_FREE_DISK_BYTES": &c.MinFreeDisk} {
		if v := getenv(name); v != "" {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return Config{}, fmt.Errorf("%s: %w", name, err)
			}
			*dst = n
		}
	}
	if v := getenv("BOTPANEL_BUILD_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("BOTPANEL_BUILD_TIMEOUT: %w", err)
		}
		c.BuildTimeout = d
	}
	if v := getenv("BOTPANEL_MAX_UPLOAD_BYTES"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("BOTPANEL_MAX_UPLOAD_BYTES: %w", err)
		}
		c.MaxUploadBytes = n
	}
	for name, dst := range map[string]*time.Duration{"BOTPANEL_TELEMETRY_INTERVAL": &c.TelemetryInterval, "BOTPANEL_TELEMETRY_RETENTION": &c.TelemetryRetention} {
		if v := getenv(name); v != "" {
			d, err := time.ParseDuration(v)
			if err != nil {
				return Config{}, fmt.Errorf("%s: %w", name, err)
			}
			*dst = d
		}
	}
	c.PublicURL = strings.TrimRight(strings.TrimSpace(getenv("BOTPANEL_PUBLIC_URL")), "/")
	c.GitHubClientID = strings.TrimSpace(getenv("BOTPANEL_GITHUB_CLIENT_ID"))
	c.GitHubSecret = strings.TrimSpace(getenv("BOTPANEL_GITHUB_CLIENT_SECRET"))
	c.DiscordClientID = strings.TrimSpace(getenv("BOTPANEL_DISCORD_CLIENT_ID"))
	c.DiscordSecret = strings.TrimSpace(getenv("BOTPANEL_DISCORD_CLIENT_SECRET"))
	c.MailgunAPIKey = strings.TrimSpace(getenv("BOTPANEL_MAILGUN_API_KEY"))
	c.MailgunDomain = strings.TrimSpace(getenv("BOTPANEL_MAILGUN_DOMAIN"))
	c.MailgunRegion = strings.ToLower(strings.TrimSpace(getenv("BOTPANEL_MAILGUN_REGION")))
	c.MailFrom = strings.TrimSpace(getenv("BOTPANEL_MAIL_FROM"))
	c.ProxyHeader = strings.TrimSpace(getenv("BOTPANEL_PROXY_HEADER"))
	c.MetricsToken = strings.TrimSpace(getenv("BOTPANEL_METRICS_TOKEN"))
	if v := getenv("BOTPANEL_BACKUP_DIR"); v != "" {
		c.BackupDir = v
	}
	if v := getenv("BOTPANEL_BACKUP_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("BOTPANEL_BACKUP_INTERVAL: %w", err)
		}
		c.BackupInterval = d
	}
	if v := getenv("BOTPANEL_BACKUP_KEEP"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return Config{}, fmt.Errorf("BOTPANEL_BACKUP_KEEP: %w", err)
		}
		c.BackupKeep = n
	}
	if v := getenv("BOTPANEL_BACKUP_MAX_BYTES"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("BOTPANEL_BACKUP_MAX_BYTES: %w", err)
		}
		c.BackupMaxBytes = n
	}
	c.PortMin, c.PortMax = 20000, 29999
	if v := strings.TrimSpace(getenv("BOTPANEL_PORT_RANGE")); v != "" {
		lo, hi, ok := strings.Cut(v, "-")
		a, e1 := strconv.Atoi(lo)
		b, e2 := strconv.Atoi(hi)
		if !ok || e1 != nil || e2 != nil {
			return Config{}, fmt.Errorf("BOTPANEL_PORT_RANGE %q must look like 20000-29999", v)
		}
		c.PortMin, c.PortMax = a, b
	}
	c.PortPublicBind = getenv("BOTPANEL_PORT_PUBLIC_BIND") == "1"
	c.SitesDir, c.SiteMaxBytes, c.MaxSitesPerUser = "/var/lib/botpanel/sites", 100<<20, 10
	if dev {
		c.SitesDir = filepath.Join(".dev-data", "sites")
		c.SitesListen, c.SitesBaseURL = "127.0.0.1:8081", "http://localhost:8081"
	}
	if v, ok := look("BOTPANEL_SITES_LISTEN"); ok {
		c.SitesListen = v
	}
	if v := strings.TrimRight(strings.TrimSpace(getenv("BOTPANEL_SITES_BASE_URL")), "/"); v != "" {
		c.SitesBaseURL = v
	}
	if v := strings.TrimSpace(getenv("BOTPANEL_SITES_DIR")); v != "" {
		c.SitesDir = v
	}
	c.SitesDNSTarget = strings.ToLower(strings.TrimSpace(getenv("BOTPANEL_SITES_DNS_TARGET")))
	if v := getenv("BOTPANEL_SITE_MAX_BYTES"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("BOTPANEL_SITE_MAX_BYTES: %w", err)
		}
		c.SiteMaxBytes = n
	}
	if v := getenv("BOTPANEL_MAX_SITES_PER_USER"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return Config{}, fmt.Errorf("BOTPANEL_MAX_SITES_PER_USER: %w", err)
		}
		c.MaxSitesPerUser = n
	}
	c.SFTPListen = strings.TrimSpace(getenv("BOTPANEL_SFTP_LISTEN"))
	c.SFTPHostKey = strings.TrimSpace(getenv("BOTPANEL_SFTP_HOST_KEY"))
	if c.SFTPHostKey == "" {
		c.SFTPHostKey = filepath.Join(filepath.Dir(c.DBPath), "sftp_host_ed25519")
	}
	c.SFTPMaxFile = 256 << 20
	if v := getenv("BOTPANEL_SFTP_MAX_FILE_BYTES"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("BOTPANEL_SFTP_MAX_FILE_BYTES: %w", err)
		}
		c.SFTPMaxFile = n
	}
	c.OAuthAllowSignup = getenv("BOTPANEL_OAUTH_ALLOW_SIGNUP") == "1"
	c.OAuthAllowSignupSet = strings.TrimSpace(getenv("BOTPANEL_OAUTH_ALLOW_SIGNUP")) != ""
	if v := getenv("BOTPANEL_KEY_DIR"); v != "" {
		c.KeyDir = v
	}
	if v := getenv("BOTPANEL_ACTIVE_KEY_ID"); v != "" {
		c.ActiveKeyID = v
	}
	if v := getenv("BOTPANEL_RUNTIMES_DIR"); v != "" {
		c.RuntimesDir = v
	}
	for name, dst := range map[string]*int64{"BOTPANEL_MAX_BOT_MEMORY_BYTES": &c.MaxBotMemory, "BOTPANEL_MAX_BOT_NANO_CPUS": &c.MaxBotCPUs} {
		if v := getenv(name); v != "" {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return Config{}, fmt.Errorf("%s: %w", name, err)
			}
			*dst = n
		}
	}
	if v := getenv("BOTPANEL_SESSION_TTL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("BOTPANEL_SESSION_TTL: %w", err)
		}
		c.SessionTTL = d
	}
	if v := getenv("BOTPANEL_DB_MAX_CONNS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return Config{}, fmt.Errorf("BOTPANEL_DB_MAX_CONNS: %w", err)
		}
		c.DBMaxOpenConns = n
	}
	if v := getenv("BOTPANEL_SHUTDOWN_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("BOTPANEL_SHUTDOWN_TIMEOUT: %w", err)
		}
		c.ShutdownTimeout = d
	}
	return c, c.Validate()
}

// LoadEnv loads configuration from the process environment.
func LoadEnv() (Config, error) { return Load(os.Getenv) }

func (c Config) validateSites() []error {
	if c.SitesListen == "" {
		return nil
	}
	var errs []error
	if _, _, err := net.SplitHostPort(c.SitesListen); err != nil {
		errs = append(errs, fmt.Errorf("sites listen address %q: %w", c.SitesListen, err))
	} else if c.SitesListen == c.Listen {
		errs = append(errs, errors.New("BOTPANEL_SITES_LISTEN must differ from BOTPANEL_LISTEN: sites never share the panel's listener"))
	}
	u, err := url.Parse(c.SitesBaseURL)
	switch {
	case c.SitesBaseURL == "":
		errs = append(errs, errors.New("BOTPANEL_SITES_BASE_URL is required when BOTPANEL_SITES_LISTEN is set (for example https://sites.example.com)"))
	case err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.User != nil:
		errs = append(errs, fmt.Errorf("BOTPANEL_SITES_BASE_URL %q must be an origin such as https://sites.example.com", c.SitesBaseURL))
	case c.PublicURL != "":
		if p, err := url.Parse(c.PublicURL); err == nil {
			ph, sh := strings.ToLower(p.Hostname()), strings.ToLower(u.Hostname())
			// Sites are served on subdomains of sh: none of them may be the
			// panel, and in production the panel must not be their parent
			// (a site could then set cookies for the panel's host).
			if strings.HasSuffix(ph, "."+sh) || (ph == sh && c.Production) {
				errs = append(errs, errors.New("the panel's host must not be the sites domain or one of its subdomains; use a separate domain such as sites.example.com or example-sites.net"))
			}
		}
	}
	if c.SitesDir == "" || strings.ContainsRune(c.SitesDir, 0) {
		errs = append(errs, errors.New("sites directory is empty or invalid"))
	}
	if c.SiteMaxBytes < 1<<20 || c.SiteMaxBytes > 4<<30 {
		errs = append(errs, errors.New("BOTPANEL_SITE_MAX_BYTES must be between 1 MiB and 4 GiB"))
	}
	if c.MaxSitesPerUser < 0 {
		errs = append(errs, errors.New("BOTPANEL_MAX_SITES_PER_USER cannot be negative (0 means unlimited)"))
	}
	return errs
}

// Validate checks the configuration for internal consistency.
func (c Config) Validate() error {
	var errs []error
	if _, port, err := net.SplitHostPort(c.Listen); err != nil {
		errs = append(errs, fmt.Errorf("listen address %q: %w", c.Listen, err))
	} else if p, err := strconv.Atoi(port); err != nil || p < 0 || p > 65535 {
		errs = append(errs, fmt.Errorf("listen address %q: invalid port", c.Listen))
	}
	if c.DBPath == "" || strings.ContainsAny(c.DBPath, "?#\x00") {
		errs = append(errs, errors.New("database path is empty or contains reserved characters (? # NUL)"))
	}
	if c.DataRoot == "" || strings.ContainsRune(c.DataRoot, 0) {
		errs = append(errs, errors.New("data root is empty or invalid"))
	}
	switch c.RunnerMode {
	case RunnerLocal, RunnerNone:
	default:
		errs = append(errs, fmt.Errorf("runner mode must be %q or %q, got %q", RunnerLocal, RunnerNone, c.RunnerMode))
	}
	if c.DBMaxOpenConns < 1 || c.DBMaxOpenConns > 16 {
		errs = append(errs, errors.New("database max connections must be between 1 and 16"))
	}
	if c.KeyDir == "" || c.ActiveKeyID == "" {
		errs = append(errs, errors.New("key directory and active key id are required"))
	}
	if c.SessionTTL < time.Minute || c.SessionTTL > 90*24*time.Hour {
		errs = append(errs, errors.New("session TTL must be between 1m and 90d"))
	}
	if c.HashWorkers < 1 {
		errs = append(errs, errors.New("hash workers must be >= 1"))
	}
	if c.MaxBotMemory < MinBotMemory || c.MaxBotCPUs < MinBotCPUs {
		errs = append(errs, errors.New("bot resource maximums are below the minimums"))
	}
	if uid, gid, ok := parseUser(c.ContainerUser); !ok {
		errs = append(errs, fmt.Errorf("container user %q must be numeric uid:gid", c.ContainerUser))
	} else if (uid == 0 || gid == 0) && !c.AllowRootUser {
		errs = append(errs, errors.New("container user must be nonroot (set BOTPANEL_ALLOW_ROOT_CONTAINER_USER=1 only with a rootless daemon)"))
	}
	if c.WorkspaceOwner != "" {
		if _, _, ok := parseUser(c.WorkspaceOwner); !ok {
			errs = append(errs, fmt.Errorf("workspace owner %q must be numeric uid:gid", c.WorkspaceOwner))
		}
	}
	if strings.TrimSpace(c.ContainerNetwork) == "" || strings.ContainsAny(c.ContainerNetwork, " \t\n") ||
		c.ContainerNetwork == "host" || strings.HasPrefix(c.ContainerNetwork, "container:") {
		errs = append(errs, errors.New("container network must be a named Docker network mode (host and container: modes are not allowed)"))
	}
	if c.RunnerWorkers < 1 || c.RunnerWorkers > 16 {
		errs = append(errs, errors.New("runner workers must be between 1 and 16"))
	}
	if c.MaxBuilds < 1 || c.MaxBuilds > 16 {
		errs = append(errs, errors.New("BOTPANEL_MAX_BUILDS must be between 1 and 16"))
	}
	if c.MaxBotsPerUser < 0 || c.UserMemoryBytes < 0 || c.NodeMemoryBytes < 0 || c.MinFreeDisk < 0 {
		errs = append(errs, errors.New("capacity budgets cannot be negative (0 means unlimited)"))
	}
	if c.NodeMemoryBytes > 0 && c.NodeMemoryBytes < c.MaxBotMemory {
		errs = append(errs, errors.New("BOTPANEL_NODE_MEMORY_BYTES is smaller than BOTPANEL_MAX_BOT_MEMORY_BYTES; the largest bot could never start"))
	}
	if c.BuildTimeout < time.Minute || c.BuildTimeout > 2*time.Hour {
		errs = append(errs, errors.New("build timeout must be between 1m and 2h"))
	}
	if c.MaxUploadBytes < 1<<10 || c.MaxUploadBytes > 1<<30 {
		errs = append(errs, errors.New("max upload must be between 1 KiB and 1 GiB"))
	}
	if c.TelemetryInterval < 30*time.Second || c.TelemetryInterval > 60*time.Second {
		errs = append(errs, errors.New("telemetry interval must be between 30s and 60s"))
	}
	if c.TelemetryRetention < time.Hour || c.TelemetryRetention > 365*24*time.Hour {
		errs = append(errs, errors.New("telemetry retention must be between 1h and 365d"))
	}
	errs = append(errs, c.validateOAuth()...)
	errs = append(errs, c.validateSites()...)
	if c.BackupDir == "" || strings.ContainsRune(c.BackupDir, 0) {
		errs = append(errs, errors.New("backup directory is empty or invalid"))
	}
	if c.BackupInterval != 0 && (c.BackupInterval < time.Hour || c.BackupInterval > 90*24*time.Hour) {
		errs = append(errs, errors.New("BOTPANEL_BACKUP_INTERVAL must be 0 (off) or between 1h and 90d"))
	}
	if c.BackupKeep < 1 || c.BackupKeep > 45 {
		errs = append(errs, errors.New("BOTPANEL_BACKUP_KEEP must be between 1 and 45"))
	}
	if c.BackupMaxBytes < 1<<20 {
		errs = append(errs, errors.New("BOTPANEL_BACKUP_MAX_BYTES must be at least 1 MiB"))
	}
	if c.PortMin < 1024 || c.PortMax > 65535 || c.PortMin > c.PortMax {
		errs = append(errs, errors.New("BOTPANEL_PORT_RANGE must lie within 1024-65535 and be ordered"))
	}
	if c.SFTPListen != "" {
		if _, port, err := net.SplitHostPort(c.SFTPListen); err != nil || port == "" || port == "0" {
			errs = append(errs, fmt.Errorf("BOTPANEL_SFTP_LISTEN %q must be host:port", c.SFTPListen))
		}
		if c.SFTPMaxFile < 1<<20 || c.SFTPMaxFile > 64<<30 {
			errs = append(errs, errors.New("BOTPANEL_SFTP_MAX_FILE_BYTES must be between 1 MiB and 64 GiB"))
		}
	}
	if c.ProxyHeader != "" && strings.Trim(c.ProxyHeader, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-") != "" {
		errs = append(errs, errors.New("BOTPANEL_PROXY_HEADER must be a header name such as X-Forwarded-For"))
	}
	if c.MetricsToken != "" && len(c.MetricsToken) < 24 {
		errs = append(errs, errors.New("BOTPANEL_METRICS_TOKEN must be at least 24 characters"))
	}
	if c.ShutdownTimeout <= 0 {
		errs = append(errs, errors.New("shutdown timeout must be positive"))
	}
	return errors.Join(errs...)
}

func parseUser(u string) (uid, gid int, ok bool) {
	a, b, found := strings.Cut(u, ":")
	if !found {
		return 0, 0, false
	}
	var err error
	if uid, err = strconv.Atoi(a); err != nil || uid < 0 {
		return 0, 0, false
	}
	if gid, err = strconv.Atoi(b); err != nil || gid < 0 {
		return 0, 0, false
	}
	return uid, gid, true
}

func (c Config) validateOAuth() []error {
	var errs []error
	for _, p := range []struct{ name, id, secret string }{
		{"GitHub", c.GitHubClientID, c.GitHubSecret},
		{"Discord", c.DiscordClientID, c.DiscordSecret},
	} {
		if (p.id == "") != (p.secret == "") {
			errs = append(errs, fmt.Errorf("%s OAuth needs both a client id and a client secret", p.name))
		}
	}
	if !c.OAuthConfigured("github") && !c.OAuthConfigured("discord") {
		return errs
	}
	u, err := url.Parse(c.PublicURL)
	switch {
	case c.PublicURL == "":
		errs = append(errs, errors.New("BOTPANEL_PUBLIC_URL is required when OAuth is configured (e.g. https://panel.example.com)"))
	case err != nil || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil:
		errs = append(errs, fmt.Errorf("BOTPANEL_PUBLIC_URL %q must be a bare origin like https://panel.example.com", c.PublicURL))
	case u.Scheme != "https" && !(u.Scheme == "http" && isLoopbackHost(u.Hostname())):
		errs = append(errs, errors.New("BOTPANEL_PUBLIC_URL must use https (http is only allowed for localhost)"))
	case c.Production && u.Scheme != "https":
		errs = append(errs, errors.New("BOTPANEL_PUBLIC_URL must use https in production"))
	}
	return errs
}

func isLoopbackHost(h string) bool {
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}
