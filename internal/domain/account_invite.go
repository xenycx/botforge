package domain

type AccountInvite struct {
	ID, Email, Role, CreatedBy string
	CreatedAtMS, ExpiresAtMS   int64
}
