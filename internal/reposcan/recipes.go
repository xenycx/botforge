package reposcan

import (
	"sort"
	"strings"
)

// Recipe is a hosting plan verified against a well-known open-source bot.
type Recipe struct {
	Repo        string `json:"repo"`   // owner/name
	Branch      string `json:"branch"` // "" = default branch
	Name        string `json:"name"`
	Description string `json:"description"`
	Language    string `json:"language"`
	Plan        Plan   `json:"plan"`
}

var recipes = []Recipe{
	{
		Repo: "Cog-Creators/Red-DiscordBot", Name: "Red-DiscordBot", Language: "Python",
		Description: "A modular, self-hosted general-purpose bot with music, moderation and hundreds of community cogs.",
		Plan: Plan{
			Source: "recipe", Confidence: "high", Runtime: "python",
			Summary: "Red is installed from the repository into a virtual environment; the first build creates a Red instance named red whose data lives in red-data/.",
			BuildCommand: "python -m venv .venv\n.venv/bin/pip install --no-cache-dir .\n" +
				"[ -f .config/Red-DiscordBot/config.json ] || .venv/bin/python -m redbot.setup --no-prompt --instance-name red --data-path /workspace/red-data --backend json",
			Argv: []string{"/workspace/.venv/bin/python", "-m", "redbot", "red", "--no-prompt", "--prefix", "!"},
			Env: []EnvVar{
				{Name: "RED_TOKEN", Description: "The bot token from the Discord Developer Portal (Bot → Reset Token). Red reads it from this variable.", Required: true, Secret: true},
			},
			MemoryBytes: 512 * mib,
			Setup: []string{
				"Create an application in the Discord Developer Portal, add a bot user and copy its token.",
				"Enable all three privileged gateway intents (Presence, Server Members and Message Content) on the Bot page; Red refuses to start without them.",
				"Red treats the owner of the Discord application as its owner. If a Discord team owns the application, Red refuses to start: add --owner <your Discord user ID> to the start command under Startup.",
				"Invite the bot to your server and run !help.",
				"Change the prefix under Startup (the --prefix argument) if you do not want !.",
			},
			Notes:    []string{"The built-in Audio cog needs Java and Lavalink, which this Python image does not include; other cogs work."},
			Evidence: []string{"pyproject.toml", "setup.py", "redbot/setup.py"},
		},
	},
	{
		Repo: "botlabs-gg/yagpdb", Name: "YAGPDB", Language: "Go",
		Description: "Yet Another General Purpose Discord Bot: moderation, custom commands, feeds and a web control panel.",
		Plan: Plan{
			Source: "recipe", Confidence: "high", Runtime: "go",
			Summary:      "YAGPDB is compiled from cmd/yagpdb with PostgreSQL and Redis add-ons; the web panel listens on port 5000 inside the container.",
			BuildCommand: "cd cmd/yagpdb\nGOEXPERIMENT=jsonv2 CGO_ENABLED=0 go build -buildvcs=false -p 2 -o /workspace/app .",
			Argv:         []string{"./app", "-all", "-https=false", "-exthttps=false"},
			Addons:       []string{"postgres", "redis"},
			Env: []EnvVar{
				{Name: "YAGPDB_BOTTOKEN", Description: "The bot token from the Discord Developer Portal.", Required: true, Secret: true},
				{Name: "YAGPDB_CLIENTID", Description: "The application's client ID (OAuth2 page).", Required: true},
				{Name: "YAGPDB_CLIENTSECRET", Description: "The application's client secret (OAuth2 page), used by the web panel's login.", Required: true, Secret: true},
				{Name: "YAGPDB_OWNER", Description: "Your Discord user ID (enable Developer Mode, then right-click your name → Copy User ID).", Required: true},
				{Name: "YAGPDB_HOST", Description: "Public host name of the web panel, without https://.", Required: true, Value: "localhost"},
				{Name: "YAGPDB_PQHOST", Description: "PostgreSQL host (the add-on).", Value: "postgres"},
				{Name: "YAGPDB_PQUSERNAME", Description: "PostgreSQL user (the add-on).", Value: "bot"},
				{Name: "YAGPDB_PQPASSWORD", Description: "PostgreSQL password, taken from the add-on.", Value: "${POSTGRES_PASSWORD}"},
				{Name: "YAGPDB_PQDB", Description: "PostgreSQL database (the add-on).", Value: "bot"},
				{Name: "YAGPDB_REDIS", Description: "Redis address (the add-on).", Value: "redis:6379"},
			},
			MemoryBytes: 1536 * mib, NanoCPUs: 2_000_000_000, PidsLimit: 512, Ports: []int{5000},
			Setup: []string{
				"Create an application in the Discord Developer Portal, add a bot user and copy its token, client ID and client secret.",
				"Enable the Server Members, Presence and Message Content privileged intents on the Bot page.",
				"To use the web panel, publish container port 5000 under Network and add http://<host>/manage as an OAuth2 redirect.",
			},
			Notes:    []string{"The first build downloads several hundred modules and needs about 1.5 GiB of memory; later builds reuse the module cache."},
			Evidence: []string{"cmd/yagpdb/main.go", "yagpdb_docker/Dockerfile", "yagpdb_docker/app.example.env"},
		},
	},
}

// Recipes lists the verified recipes.
func Recipes() []Recipe {
	out := append([]Recipe(nil), recipes...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// RecipeFor returns the verified recipe for a repository, if any.
func RecipeFor(fullName string) (Recipe, bool) {
	for _, r := range recipes {
		if strings.EqualFold(r.Repo, fullName) {
			return r, true
		}
	}
	return Recipe{}, false
}
