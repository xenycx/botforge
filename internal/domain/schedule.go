package domain

// Schedule is a recurring action on a bot.
type Schedule struct {
	ID          string
	BotID       string
	OwnerID     string
	OwnerEmail  string // filled by listing queries
	Action      string // backup | start | stop | restart | deploy
	Spec        string // five-field cron
	Timezone    string
	Enabled     bool
	NextRunMS   *int64
	LastRunMS   *int64
	LastStatus  *string
	LastMessage *string
	CreatedAtMS int64
	UpdatedAtMS int64
}
