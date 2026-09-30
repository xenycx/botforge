package domain

type BotWidget struct {
	BotID, Key, Kind, Title, PayloadJSON string
	Group                                string
	Span, MinHeight                      int
	Position                             int
	UpdatedAtMS                          int64
	ExpiresAtMS                          *int64
}
