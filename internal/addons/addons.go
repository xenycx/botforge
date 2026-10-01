// Package addons is the fixed catalog of companion services (databases and
// caches) a bot can run next to it. Each add-on runs in its own capped
// container on a private, internal per-bot Docker network: it has no internet
// access and only the bot (and its other add-ons) can reach it, by the
// add-on's kind as host name.
package addons

import (
	"fmt"
	"sort"
)

// Kind is one approved add-on.
type Kind struct {
	ID          string `json:"id"` // also the host name on the private network
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	Image       string `json:"image"`
	Port        int    `json:"port"`
	// DataPath is where the persistent data directory is mounted.
	DataPath string `json:"-"`
	// Tmpfs are extra writable in-memory paths (the root filesystem is read-only).
	Tmpfs []string `json:"-"`
	// Argv overrides the image command (the image entrypoint is kept).
	Argv func(password string) []string `json:"-"`
	// Env is the add-on container's environment.
	Env func(password string) map[string]string `json:"-"`
	// Health is the health-check command, run directly (no shell).
	Health []string `json:"-"`
	// BotEnv are the connection variables given to the bot.
	BotEnv func(password string) map[string]string `json:"-"`
	// VarNames lists BotEnv's names, for display without a password.
	VarNames []string `json:"variables"`

	DefaultMemory int64 `json:"default_memory_bytes"`
	MinMemory     int64 `json:"min_memory_bytes"`
	NanoCPUs      int64 `json:"-"`
	PidsLimit     int64 `json:"-"`
	// Password reports whether a generated password protects the add-on.
	Password bool `json:"password"`
}

const mib = 1 << 20

var kinds = map[string]Kind{
	"postgres": {
		ID: "postgres", DisplayName: "PostgreSQL 17", Image: "postgres:17-alpine", Port: 5432,
		Description: "Relational database. Database and user are both named bot.",
		DataPath:    "/var/lib/postgresql/data", Tmpfs: []string{"/var/run/postgresql"},
		Env: func(pw string) map[string]string {
			return map[string]string{"POSTGRES_USER": "bot", "POSTGRES_PASSWORD": pw, "POSTGRES_DB": "bot",
				"PGDATA": "/var/lib/postgresql/data/pgdata"}
		},
		Health: []string{"pg_isready", "-h", "127.0.0.1", "-U", "bot", "-d", "bot"},
		BotEnv: func(pw string) map[string]string {
			return map[string]string{
				"DATABASE_URL":  "postgres://bot:" + pw + "@postgres:5432/bot?sslmode=disable",
				"POSTGRES_HOST": "postgres", "POSTGRES_PORT": "5432", "POSTGRES_USER": "bot", "POSTGRES_PASSWORD": pw, "POSTGRES_DB": "bot",
				"PGHOST": "postgres", "PGPORT": "5432", "PGUSER": "bot", "PGPASSWORD": pw, "PGDATABASE": "bot",
			}
		},
		DefaultMemory: 256 * mib, MinMemory: 128 * mib, NanoCPUs: 500_000_000, PidsLimit: 256, Password: true,
	},
	"redis": {
		ID: "redis", DisplayName: "Redis 7", Image: "redis:7-alpine", Port: 6379,
		Description: "In-memory key-value store, saved to disk (append-only file). Reachable only from this bot, without a password.",
		DataPath:    "/data",
		Argv: func(string) []string {
			// protected-mode refuses non-loopback clients without a password; the
			// private network is the access control here.
			return []string{"redis-server", "--appendonly", "yes", "--dir", "/data", "--protected-mode", "no"}
		},
		Health: []string{"redis-cli", "ping"},
		BotEnv: func(string) map[string]string {
			return map[string]string{"REDIS_URL": "redis://redis:6379", "REDIS_HOST": "redis", "REDIS_PORT": "6379"}
		},
		DefaultMemory: 128 * mib, MinMemory: 32 * mib, NanoCPUs: 250_000_000, PidsLimit: 64,
	},
	"mongodb": {
		ID: "mongodb", DisplayName: "MongoDB 7", Image: "mongo:7", Port: 27017,
		Description: "Document database. The user bot is an administrator; connect with authSource=admin.",
		DataPath:    "/data/db", Tmpfs: []string{"/data/configdb"},
		Argv: func(string) []string { return []string{"mongod", "--wiredTigerCacheSizeGB", "0.25", "--bind_ip_all"} },
		Env: func(pw string) map[string]string {
			return map[string]string{"MONGO_INITDB_ROOT_USERNAME": "bot", "MONGO_INITDB_ROOT_PASSWORD": pw}
		},
		Health: []string{"mongosh", "--quiet", "--eval", "db.adminCommand('ping').ok"},
		BotEnv: func(pw string) map[string]string {
			u := "mongodb://bot:" + pw + "@mongodb:27017/?authSource=admin"
			return map[string]string{"MONGODB_URI": u, "MONGO_URL": u, "MONGO_HOST": "mongodb", "MONGO_PORT": "27017",
				"MONGO_USER": "bot", "MONGO_PASSWORD": pw}
		},
		DefaultMemory: 512 * mib, MinMemory: 256 * mib, NanoCPUs: 500_000_000, PidsLimit: 256, Password: true,
	},
	"mariadb": {
		ID: "mariadb", DisplayName: "MariaDB 11", Image: "mariadb:11", Port: 3306,
		Description: "MySQL-compatible database. Database and user are both named bot.",
		DataPath:    "/var/lib/mysql", Tmpfs: []string{"/run/mysqld"},
		Env: func(pw string) map[string]string {
			return map[string]string{"MARIADB_USER": "bot", "MARIADB_PASSWORD": pw, "MARIADB_DATABASE": "bot", "MARIADB_ROOT_PASSWORD": pw}
		},
		Health: []string{"healthcheck.sh", "--connect", "--innodb_initialized"},
		BotEnv: func(pw string) map[string]string {
			return map[string]string{"MYSQL_URL": "mysql://bot:" + pw + "@mariadb:3306/bot", "MYSQL_HOST": "mariadb", "MYSQL_PORT": "3306",
				"MYSQL_USER": "bot", "MYSQL_PASSWORD": pw, "MYSQL_DATABASE": "bot"}
		},
		DefaultMemory: 384 * mib, MinMemory: 192 * mib, NanoCPUs: 500_000_000, PidsLimit: 256, Password: true,
	},
}

func init() {
	for id, k := range kinds {
		if k.BotEnv == nil {
			panic("addon " + id + " has no bot variables")
		}
		for n := range k.BotEnv("x") {
			k.VarNames = append(k.VarNames, n)
		}
		sort.Strings(k.VarNames)
		kinds[id] = k
	}
}

// Get returns one add-on kind.
func Get(id string) (Kind, bool) { k, ok := kinds[id]; return k, ok }

// List returns every add-on kind, sorted by ID.
func List() []Kind {
	out := make([]Kind, 0, len(kinds))
	for _, k := range kinds {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// MaxMemory bounds one add-on's memory limit.
const MaxMemory = 8 << 30

// ValidateMemory checks an add-on memory limit.
func (k Kind) ValidateMemory(b int64) error {
	if b < k.MinMemory || b > MaxMemory {
		return fmt.Errorf("%s needs between %d MiB and %d MiB of memory", k.DisplayName, k.MinMemory/mib, MaxMemory/mib)
	}
	return nil
}

// BotEnv merges the connection variables of a bot's add-ons. passwords maps
// kind to its generated password. When both SQL databases exist, DATABASE_URL
// points at PostgreSQL.
func BotEnv(kindsInUse []string, passwords map[string]string) map[string]string {
	out := map[string]string{}
	sorted := append([]string(nil), kindsInUse...)
	sort.Strings(sorted)
	for _, id := range sorted {
		k, ok := kinds[id]
		if !ok {
			continue
		}
		for n, v := range k.BotEnv(passwords[id]) {
			out[n] = v
		}
	}
	if _, pg := out["POSTGRES_HOST"]; !pg {
		if u, ok := out["MYSQL_URL"]; ok {
			out["DATABASE_URL"] = u
		}
	}
	return out
}
