package domain

// Invite is a one-use link that shares a bot with whoever accepts it.
type Invite struct {
	ID          string
	BotID       string
	BotName     string // filled by queries
	Permissions int
	CreatedBy   string
	CreatorName string // email, filled by queries
	CreatedAtMS int64
	ExpiresAtMS int64
	UsedBy      *string
	UsedAtMS    *int64
}
