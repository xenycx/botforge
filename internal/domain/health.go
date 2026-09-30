package domain

// AlertPrefs are a bot's notification preferences.
type AlertPrefs struct {
	Crash          bool
	Deploy         bool
	Backup         bool
	Recovery       bool
	HeartbeatAfter int // seconds without a push before alerting; 0 = off
}

// DefaultAlertPrefs apply to bots without saved preferences.
var DefaultAlertPrefs = AlertPrefs{Crash: true, Deploy: true, Backup: true, Recovery: true}

// BotHealth is what the bot itself last reported through the SDK.
type BotHealth struct {
	BotID        string
	LastSeenAtMS int64
	Ready        *bool
	StaleAlerted bool
}

// HeartbeatWatch is a bot with a heartbeat alert rule and its last report
// (nil when it never pushed).
type HeartbeatWatch struct {
	Bot    Bot
	Prefs  AlertPrefs
	Health *BotHealth
}
