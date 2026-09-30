package domain

const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

// User is a panel account. PasswordHash is never serialized to clients.
type User struct {
	ID           string
	Email        string
	DisplayName  string
	AvatarJPEG   []byte
	PasswordHash string
	Role         string
	Disabled     bool
	CreatedAtMS  int64
	UpdatedAtMS  int64
}

func (u User) IsAdmin() bool { return u.Role == RoleAdmin }

// Session is a signed-in browser. The token itself is never stored; ID is a
// separate public handle for listing and revoking.
type Session struct {
	ID           string
	UserID       string
	CreatedAtMS  int64
	ExpiresAtMS  int64
	LastSeenAtMS int64
	AuthAtMS     int64 // when the user last proved their identity in this session
	Device       string
}
