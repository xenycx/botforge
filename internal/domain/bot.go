package domain

const (
	DesiredStopped = "stopped"
	DesiredRunning = "running"
	DesiredDeleted = "deleted"
)

// Bot is a managed Discord bot and its desired/observed lifecycle state.
type Bot struct {
	ID                 string
	OwnerID            string
	WorkspaceID        string
	NodeID             string
	Name               string
	Runtime            string
	ImageRef           string
	Argv               []string
	MemoryBytes        int64
	NanoCPUs           int64
	PidsLimit          int64
	DesiredState       string
	ObservedState      string
	Generation         int64
	ObservedGeneration int64
	ContainerID        *string
	LastExitCode       *int64
	LastError          *string
	ObservedAtMS       *int64
	CreatedAtMS        int64
	UpdatedAtMS        int64
	DiscordUserID      string
	DiscordUsername    string
	DiscordAvatarURL   string

	// Startup. When Entrypoint is set it is the container entrypoint (its first
	// element must be a permitted command) and Argv are its arguments; otherwise
	// Argv itself is the exec-form entrypoint.
	Entrypoint []string
	SourceType string // manual | template | github ("" = manual)
	TemplateID *string

	// Network. Zero values keep the historical behaviour (outbound allowed, no
	// published ports). BandwidthKbps is recorded intent: Docker has no native
	// bandwidth cap, so it is not enforced without host traffic shaping.
	NetworkDisabled bool
	BandwidthKbps   *int64
	Ports           []BotPort // loaded by GetBot only

	AutoBackupOff bool // scheduled backups disabled for this bot

	// Restart policy. "" behaves as RestartOnFailure with runner defaults.
	RestartPolicy           string
	RestartMaxAttempts      int64 // consecutive crashes before giving up; 0 = unlimited
	RestartBackoffInitialMS int64
	RestartBackoffMaxMS     int64

	// Lifecycle detail written by the runner. RestartCount is the consecutive
	// crash count of the current generation; NextRetryAtMS is set while waiting
	// to retry; StateReason is one of the Reason* codes or nil.
	RestartCount  int64
	NextRetryAtMS *int64
	StateReason   *string
	// LastStartedAtMS is when the bot last became running (nil = never).
	LastStartedAtMS *int64
}

// Machine-readable explanations for an observed state (bots.state_reason).
const (
	ReasonCleanExit      = "clean_exit"      // exited with code 0; never restarted
	ReasonExitedNoRetry  = "exited"          // crashed under the "never" restart policy
	ReasonGaveUp         = "gave_up"         // crashed more often than the restart budget allows
	ReasonCrashBackoff   = "crash_backoff"   // crashed; waiting before the next restart
	ReasonSetupFailed    = "setup_failed"    // image/build/create/start failed; retrying
	ReasonBuildFailed    = "build_failed"    // the build step failed; retrying
	ReasonRuntimeMissing = "runtime_missing" // the runtime was removed from the catalog
	ReasonCleanupFailed  = "cleanup_failed"  // deletion could not finish; retrying
	ReasonKilled         = "killed"          // stopped with SIGKILL by a user
)

const (
	RestartNever     = "never"
	RestartOnFailure = "on_failure"
)

// EnvVar is an encrypted environment variable row.
type EnvVar struct {
	BotID       string
	Name        string
	Ciphertext  []byte
	Nonce       []byte
	KeyID       string
	CreatedAtMS int64
	UpdatedAtMS int64
}
