// Package sdk embeds the bot-side telemetry snippets served by the panel.
package sdk

import "embed"

// FS holds the snippets: botpanel.js (discord.js), botpanel.py (discord.py),
// botpanel.rb (discordrb), go/botpanel.go (any Go library), botpanel.rs
// (serenity/poise) and BotPanel.java (JDA and others).
//
//go:embed botpanel.js botpanel.py botpanel.rb go/botpanel.go botpanel.rs BotPanel.java
var FS embed.FS

// Files maps the language names the API serves to snippet files.
var Files = map[string]string{
	"discordjs": "botpanel.js", "discordpy": "botpanel.py", "ruby": "botpanel.rb",
	"go": "go/botpanel.go", "rust": "botpanel.rs", "java": "BotPanel.java",
}
