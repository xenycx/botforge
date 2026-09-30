package domain

type BotWidget struct {
	BotID, Key, Kind, Title, PayloadJSON string
	Position                             int
	UpdatedAtMS                          int64
}
