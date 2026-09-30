package domain

// MFA is a user's TOTP enrollment. EnabledAtMS is nil while the enrollment
// waits for the first code.
type MFA struct {
	UserID       string
	Cipher       []byte
	Nonce        []byte
	KeyID        string
	EnabledAtMS  *int64
	LastStep     int64
	CreatedAtMS  int64
	RecoveryLeft int
}
