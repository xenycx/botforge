package domain

// AutomationToken is a scoped HTTP bearer credential. BotIDs nil means every
// bot the owner can access.
type AutomationToken struct {
	ID           string
	UserID       string
	Name         string
	Prefix       string
	TokenHash    []byte
	Actions      []string
	BotIDs       []string
	CreatedAtMS  int64
	LastUsedAtMS *int64
	ExpiresAtMS  int64
}
