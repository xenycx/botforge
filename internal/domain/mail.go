package domain

// MailRecipient is an enabled account an announcement can go to.
type MailRecipient struct {
	Email string
	Admin bool
	News  bool // wants optional news emails
}
