// Package api contains the Fiber routes, middleware and error handling.
package api

import (
	"context"
	"io/fs"
	"log/slog"
	"strings"
	"time"

	fws "github.com/gofiber/contrib/v3/websocket"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/limiter"

	"botpanel/internal/console"
	"botpanel/internal/diag"
	"botpanel/internal/domain"
	"botpanel/internal/filesystem"
	"botpanel/internal/hostmon"
	"botpanel/internal/logbuf"
	"botpanel/internal/pkgmgr"
	"botpanel/internal/runtimes"
	"botpanel/internal/service"
)

// Pinger reports whether a dependency is reachable.
// SFTPInfo describes the embedded SFTP server for the settings page.
type SFTPInfo struct {
	Port        string `json:"port"`
	Fingerprint string `json:"fingerprint"`
}

type Pinger interface {
	PingContext(ctx context.Context) error
}

// Deps are the dependencies of the HTTP server. Docker is deliberately absent:
// no readiness check may claim a dependency that does not exist yet.
type Deps struct {
	Log *slog.Logger
	DB  Pinger
	UI  fs.FS // built static frontend; may be empty

	Auth          *service.AuthService // nil disables the authenticated API (tests/foundation)
	Bots          *service.BotService
	OAuth         *service.OAuthService         // nil disables OAuth routes
	SFTP          *SFTPInfo                     // nil when the SFTP server is disabled
	Analytics     *service.Analytics            // nil disables bot telemetry routes
	PublicURL     string                        // externally reachable origin, handed to bots
	Registry      *pkgmgr.Registry              // package registry client; default used when nil
	Stats         StatsSource                   // Docker stats stream; nil disables live gauges
	Backups       *service.BackupService        // nil disables the backup routes
	Deploy        *service.DeployService        // nil disables GitHub deployment routes
	Ops           *service.Operations           // nil disables operation history routes
	Audit         *service.Audit                // nil disables the activity record
	Schedules     *service.Scheduler            // nil disables scheduled actions
	MFA           *service.MFAService           // nil disables two-step sign-in
	Tokens        *service.TokenService         // nil disables the automation API
	Health        *service.HealthService        // nil disables application health and alert rules
	Settings      *service.SettingsService      // nil disables the setup wizard and panel settings
	Mail          *service.MailService          // nil disables email (alerts, invitations, notices)
	Resets        *service.PasswordResetService // nil disables "Forgot password?"
	MailPrefs     MailPrefs                     // per-account alert-email switch; nil hides it
	Sites         *service.SiteService          // nil (or not started) disables static site hosting
	AI            *service.AIService            // nil disables the AI operator
	Env           *service.PanelEnvService      // nil disables the environment editor
	Host          *hostmon.Monitor              // nil disables the host monitoring routes
	Logs          *logbuf.Buffer                // nil disables the panel log viewer
	SetupCodeFile string                        // shown by the setup wizard
	OnSetupDone   func()                        // called after the first administrator is created
	Catalog       *runtimes.Catalog
	SecureCookies bool   // Secure flag on the session cookie (production)
	ProxyHeader   string // trusted client-IP header from a loopback reverse proxy; empty = none
	MetricsToken  string // bearer token for /metrics; empty disables it

	Nodes        NodeStore           // nil disables node/telemetry routes
	Files        *filesystem.Manager // nil disables the file manager
	MaxUpload    int64               // bytes per upload; default 32 MiB
	Console      *console.Service    // nil disables the WebSocket console
	ConsoleLimit *console.Limiter
	BaseCtx      context.Context // cancelled on shutdown to end WebSocket sessions

	// Checks are extra readiness probes (e.g. Docker when the local runner is on).
	Checks []Check
	// RunnerReady reports whether the runner can act now; bots waiting on an
	// unavailable runner are shown as such. Nil means always ready.
	RunnerReady func(ctx context.Context) error
	// BuildMemory is the runner's default builder memory (shown to users).
	BuildMemory int64
	// Diagnostics runs the administrator health report; nil disables it.
	Diagnostics func(ctx context.Context) diag.Report
}

// Check is a named readiness probe.
type Check struct {
	Name string
	Fn   func(ctx context.Context) error
}

type server struct {
	log           *slog.Logger
	files         *filesystem.Manager
	maxUpload     int64
	nodes         NodeStore
	console       *console.Service
	consoleLimit  *console.Limiter
	baseCtx       context.Context
	auth          *service.AuthService
	bots          *service.BotService
	oauth         *service.OAuthService
	sftp          *SFTPInfo
	analytics     *service.Analytics
	publicURL     string
	registry      *pkgmgr.Registry
	stats         StatsSource
	backups       *service.BackupService
	deploy        *service.DeployService
	ops           *service.Operations
	audit         *service.Audit
	schedules     *service.Scheduler
	mfa           *service.MFAService
	tokens        *service.TokenService
	health        *service.HealthService
	settings      *service.SettingsService
	mail          *service.MailService
	resets        *service.PasswordResetService
	mailPrefs     MailPrefs
	sites         *service.SiteService
	ai            *service.AIService
	env           *service.PanelEnvService
	host          *hostmon.Monitor
	logs          *logbuf.Buffer
	setupCodeFile string
	onSetupDone   func()
	statsLimit    *console.Limiter
	disk          diskCache
	catalog       *runtimes.Catalog
	secureCookies bool
	runnerReady   func(ctx context.Context) error
	buildMemory   int64
	diagnostics   func(ctx context.Context) diag.Report
}

const readyTimeout = 2 * time.Second

// New builds the Fiber application.
func New(d Deps) *fiber.App {
	maxUpload := d.MaxUpload
	if maxUpload <= 0 {
		maxUpload = defaultMaxUpload
	}
	cfg := fiber.Config{
		AppName:      "botpanel",
		ErrorHandler: errorHandler(d.Log),
		// Bodies are streamed so uploads never sit in memory; every route other
		// than the two upload routes is capped at 1 MiB by smallBody below.
		StreamRequestBody: true,
		BodyLimit:         int(maxUpload) + 1<<20,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		ReadBufferSize:    4096,
		Concurrency:       4096,
	}
	if d.ProxyHeader != "" {
		// Only a proxy on this host may set the header; remote peers cannot spoof it.
		cfg.ProxyHeader, cfg.TrustProxy = d.ProxyHeader, true
		cfg.TrustProxyConfig = fiber.TrustProxyConfig{Loopback: true}
	}
	app := fiber.New(cfg)

	metrics := newPanelMetrics()
	if d.Host != nil && d.Host.HTTP == nil {
		d.Host.HTTP = metrics.stats
	}
	app.Use(securityHeaders, metrics.observe, smallBody)
	app.Get("/metrics", metrics.handler(d))

	v1 := app.Group("/api/v1")
	v1.Get("/healthz", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})
	v1.Get("/readyz", func(c fiber.Ctx) error {
		ctx, cancel := context.WithTimeout(c.Context(), readyTimeout)
		defer cancel()
		checks := fiber.Map{}
		ok := true
		probe := func(name string, fn func(context.Context) error) {
			if err := fn(ctx); err != nil {
				d.Log.Warn("readiness check failed", "check", name, "err", err)
				checks[name], ok = "unavailable", false
			} else {
				checks[name] = "ok"
			}
		}
		probe("database", d.DB.PingContext)
		for _, ch := range d.Checks {
			probe(ch.Name, ch.Fn)
		}
		if !ok {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"status": "unavailable", "checks": checks})
		}
		return c.JSON(fiber.Map{"status": "ok", "checks": checks})
	})
	if d.Auth != nil && d.Bots != nil {
		s := &server{log: d.Log, auth: d.Auth, bots: d.Bots, oauth: d.OAuth, sftp: d.SFTP, analytics: d.Analytics, publicURL: d.PublicURL, registry: d.Registry, stats: d.Stats, backups: d.Backups, deploy: d.Deploy, ops: d.Ops, audit: d.Audit, schedules: d.Schedules, mfa: d.MFA, tokens: d.Tokens, health: d.Health, settings: d.Settings, mail: d.Mail, resets: d.Resets, mailPrefs: d.MailPrefs, sites: d.Sites, ai: d.AI, env: d.Env, host: d.Host, logs: d.Logs, setupCodeFile: d.SetupCodeFile, onSetupDone: d.OnSetupDone, statsLimit: console.NewLimiter(0, 0, 0), catalog: d.Catalog, secureCookies: d.SecureCookies,
			console: d.Console, consoleLimit: d.ConsoleLimit, baseCtx: d.BaseCtx, nodes: d.Nodes, files: d.Files, maxUpload: d.MaxUpload,
			runnerReady: d.RunnerReady, buildMemory: d.BuildMemory, diagnostics: d.Diagnostics}
		if s.buildMemory == 0 {
			s.buildMemory = 768 << 20
		}
		if s.maxUpload <= 0 {
			s.maxUpload = defaultMaxUpload
		}
		if s.registry == nil {
			s.registry = &pkgmgr.Registry{}
		}
		if s.baseCtx == nil {
			s.baseCtx = context.Background()
		}
		if s.consoleLimit == nil {
			s.consoleLimit = console.NewLimiter(0, 0, 0)
		}
		s.routes(v1)
	}
	// Unknown API paths are JSON errors, never the HTML fallback.
	app.All("/api/v1/*", func(c fiber.Ctx) error { return fiber.ErrNotFound })
	app.All("/api", func(c fiber.Ctx) error { return fiber.ErrNotFound })
	app.All("/api/*", func(c fiber.Ctx) error { return fiber.ErrNotFound })

	app.Use(staticHandler(d.UI))
	return app
}

// smallBody caps request bodies at 1 MiB (and rejects unknown-length bodies)
// everywhere except the streaming upload routes.
func smallBody(c fiber.Ctx) error {
	if c.Method() == fiber.MethodGet || c.Method() == fiber.MethodHead {
		return c.Next()
	}
	p := c.Path()
	if (c.Method() == fiber.MethodPut && strings.HasSuffix(p, "/files/content")) ||
		(c.Method() == fiber.MethodPost && strings.HasSuffix(p, "/files/extract")) ||
		(c.Method() == fiber.MethodPost && strings.HasPrefix(p, "/api/v1/sites/") && strings.HasSuffix(p, "/upload")) {
		return c.Next()
	}
	switch n := c.RequestCtx().Request.Header.ContentLength(); {
	case n < 0:
		return fiber.NewError(fiber.StatusLengthRequired, "a Content-Length is required")
	case n > 1<<20:
		return fiber.NewError(fiber.StatusRequestEntityTooLarge, "request body too large")
	}
	return c.Next()
}

func securityHeaders(c fiber.Ctx) error {
	c.Set("X-Content-Type-Options", "nosniff")
	c.Set("X-Frame-Options", "DENY")
	c.Set("Referrer-Policy", "same-origin")
	return c.Next()
}

// MailPrefs stores each account's alert-email switch.
type MailPrefs interface {
	UserEmailAlerts(ctx context.Context, userID string) (bool, error)
	SetUserEmailAlerts(ctx context.Context, userID string, on bool) error
	UserEmailNews(ctx context.Context, userID string) (bool, error)
	SetUserEmailNews(ctx context.Context, userID string, on bool) error
}

func (s *server) routes(v1 fiber.Router) {
	v1.Use(s.checkOrigin)
	v1.Post("/auth/login", limiter.New(limiter.Config{
		Max: 10, Expiration: time.Minute,
		KeyGenerator: func(c fiber.Ctx) string { return c.IP() },
		LimitReached: func(c fiber.Ctx) error {
			return fiber.NewError(fiber.StatusTooManyRequests, "too many login attempts; try again later")
		},
	}), s.login)

	oauthLimit := limiter.New(limiter.Config{
		Max: 30, Expiration: time.Minute,
		KeyGenerator: func(c fiber.Ctx) string { return c.IP() },
		LimitReached: func(c fiber.Ctx) error {
			return fiber.NewError(fiber.StatusTooManyRequests, "too many sign-in attempts; try again later")
		},
	})
	if s.deploy != nil {
		// GitHub authenticates deliveries with an HMAC, not a session, so there
		// is no CSRF token; the limit caps signature guessing.
		hookLimit := limiter.New(limiter.Config{
			Max: 120, Expiration: time.Minute,
			KeyGenerator: func(c fiber.Ctx) string { return c.IP() },
			LimitReached: func(c fiber.Ctx) error {
				return fiber.NewError(fiber.StatusTooManyRequests, "too many requests")
			},
		})
		v1.Post("/webhooks/github", hookLimit, s.githubWebhook)
	}
	if s.mfa != nil {
		v1.Post("/auth/mfa", limiter.New(limiter.Config{
			Max: 10, Expiration: time.Minute,
			KeyGenerator: func(c fiber.Ctx) string { return "mfa:" + c.IP() },
			LimitReached: func(c fiber.Ctx) error {
				return fiber.NewError(fiber.StatusTooManyRequests, "too many attempts; try again in a minute")
			},
		}), s.completeMFA)
	}
	if s.settings != nil {
		setupLimit := limiter.New(limiter.Config{
			Max: 10, Expiration: time.Minute,
			KeyGenerator: func(c fiber.Ctx) string { return "setup:" + c.IP() },
			LimitReached: func(c fiber.Ctx) error {
				return fiber.NewError(fiber.StatusTooManyRequests, "too many attempts; try again in a minute")
			},
		})
		v1.Get("/setup/status", s.setupStatus)
		v1.Post("/setup/check", setupLimit, s.setupCheck)
		v1.Post("/setup/complete", setupLimit, s.setupComplete)
	}
	if s.resets != nil {
		resetLimit := limiter.New(limiter.Config{
			Max: 10, Expiration: 10 * time.Minute,
			KeyGenerator: func(c fiber.Ctx) string { return "reset:" + c.IP() },
			LimitReached: func(c fiber.Ctx) error {
				return fiber.NewError(fiber.StatusTooManyRequests, "too many password reset attempts; try again in a few minutes")
			},
		})
		v1.Get("/auth/password-reset", s.resetAvailable)
		v1.Post("/auth/password-reset/request", resetLimit, s.requestPasswordReset)
		v1.Post("/auth/password-reset/confirm", resetLimit, s.confirmPasswordReset)
	}
	v1.Get("/auth/providers", s.oauthProviders)
	v1.Get("/registration", s.registrationStatus)
	v1.Post("/registration/preview", oauthLimit, s.previewAccountInvite)
	v1.Post("/registration", oauthLimit, s.registerAccount)
	v1.Get("/auth/:provider/login", oauthLimit, s.oauthLogin)
	v1.Get("/auth/:provider/callback", oauthLimit, s.oauthCallback)

	if s.analytics != nil {
		// Bots authenticate with a bearer key, not a session, so there is no
		// CSRF; the per-IP limit caps key guessing, the per-bot limit caps volume.
		botLimit := limiter.New(limiter.Config{
			Max: 300, Expiration: time.Minute,
			KeyGenerator: func(c fiber.Ctx) string { return c.IP() },
			LimitReached: func(c fiber.Ctx) error {
				return fiber.NewError(fiber.StatusTooManyRequests, "too many requests")
			},
		})
		v1.Post("/bot-telemetry", botLimit, s.botAuth, s.botTelemetryPost)
		v1.Get("/bot-telemetry/ws", botLimit, s.botAuth, fws.New(s.botTelemetryWS, fws.Config{
			ReadBufferSize: 1024, WriteBufferSize: 1024, AllowEmptyOrigin: true,
		}))
	}

	if s.tokens != nil {
		s.automationRoutes(v1)
	}
	authed := v1.Group("", s.requireAuth, s.auditMW)
	if s.ai != nil {
		s.aiRoutes(authed)
	}
	registryLimit := limiter.New(limiter.Config{
		Max: 60, Expiration: time.Minute,
		KeyGenerator: func(c fiber.Ctx) string { return "reg:" + currentUser(c).ID },
		LimitReached: func(c fiber.Ctx) error {
			return fiber.NewError(fiber.StatusTooManyRequests, "too many registry lookups; try again shortly")
		},
	})
	authed.Get("/templates", s.listTemplates)
	if s.deploy != nil {
		authed.Get("/me/github/repos", s.githubRepos)
		authed.Get("/me/github/branches", s.githubBranches)
		authed.Get("/bots/:id/github", s.getGitHub)
		authed.Put("/bots/:id/github", s.putGitHub)
		authed.Delete("/bots/:id/github", s.deleteGitHub)
		authed.Post("/bots/:id/github/deploy", s.deployGitHub)
		authed.Get("/bots/:id/github/preview", s.previewGitHub)
		authed.Get("/me/github/owners", s.githubOwners)
		authed.Get("/bots/:id/github/push-plan", s.pushPlan)
		authed.Post("/bots/:id/github/publish", s.publishGitHub)
		authed.Post("/bots/:id/github/push", s.pushGitHub)
	}
	if s.tokens != nil {
		authed.Get("/me/tokens", s.listTokens)
		authed.Post("/me/tokens", s.createToken)
		authed.Delete("/me/tokens/:id", s.deleteToken)
	}
	authed.Get("/me/capacity", s.capacity)
	authed.Get("/me/sftp", s.sftpInfo)
	authed.Get("/me/api-keys", s.listAPIKeys)
	authed.Post("/me/api-keys", s.createAPIKey)
	authed.Delete("/me/api-keys/:id", s.deleteAPIKey)
	authed.Get("/me/connections", s.listConnections)
	authed.Post("/me/connections/:provider/start", s.startConnection)
	authed.Delete("/me/connections/:provider", s.deleteConnection)
	authed.Post("/auth/logout", s.logout)
	passwordLimit := limiter.New(limiter.Config{
		Max: 10, Expiration: time.Minute,
		KeyGenerator: func(c fiber.Ctx) string { return "pw:" + currentUser(c).ID },
		LimitReached: func(c fiber.Ctx) error {
			return fiber.NewError(fiber.StatusTooManyRequests, "too many attempts; try again in a minute")
		},
	})
	authed.Get("/me/sessions", s.listSessions)
	authed.Put("/me/profile", s.updateProfile)
	if s.mailPrefs != nil {
		authed.Put("/me/email-alerts", s.putEmailAlerts)
		authed.Put("/me/email-news", s.putEmailNews)
	}
	authed.Get("/users/:id/avatar", s.userAvatar)
	authed.Delete("/me/sessions/:sid", s.revokeSession)
	authed.Post("/me/sessions/revoke-others", s.revokeOtherSessions)
	authed.Post("/me/password", passwordLimit, s.changePassword)
	if s.mfa != nil {
		authed.Get("/me/mfa", s.mfaStatus)
		authed.Post("/me/mfa/setup", passwordLimit, s.mfaSetup)
		authed.Post("/me/mfa/enable", passwordLimit, s.mfaEnable)
		authed.Post("/me/mfa/disable", passwordLimit, s.mfaDisable)
		authed.Post("/me/mfa/recovery-codes", passwordLimit, s.mfaRecoveryCodes)
	}
	authed.Get("/auth/me", s.me)
	authed.Get("/runtimes", s.listRuntimes)

	authed.Get("/workspaces", s.listWorkspaces)
	authed.Post("/workspaces", s.createWorkspace)
	authed.Get("/workspaces/:wid", s.getWorkspace)
	authed.Patch("/workspaces/:wid", s.patchWorkspace)
	authed.Delete("/workspaces/:wid", s.deleteWorkspace)
	authed.Put("/workspaces/:wid/members", s.addWorkspaceMember)
	authed.Patch("/workspaces/:wid/members/:uid", s.patchWorkspaceMember)
	authed.Delete("/workspaces/:wid/members/:uid", s.removeWorkspaceMember)
	authed.Put("/bots/:id/workspace", s.moveBot)
	authed.Get("/admin/workspaces", s.requireAdmin, s.adminListWorkspaces)
	authed.Get("/admin/workspaces/:wid", s.requireAdmin, s.adminGetWorkspace)
	authed.Get("/admin/users/:id", s.requireAdmin, s.adminGetUser)

	authed.Get("/sites-info", s.sitesInfo)
	if s.sites.Enabled() {
		authed.Get("/bots/:id/site", s.getBotSite)
		authed.Post("/bots/:id/site", s.createBotSite)
		authed.Get("/sites", s.listSites)
		authed.Post("/sites", s.createSite)
		authed.Get("/sites/:sid", s.getSite)
		authed.Patch("/sites/:sid", s.patchSite)
		authed.Delete("/sites/:sid", s.deleteSite)
		authed.Post("/sites/:sid/upload", s.uploadSite)
		authed.Get("/sites/:sid/files", s.listFiles)
		authed.Get("/sites/:sid/files/content", s.readFile)
		authed.Put("/sites/:sid/files/content", s.writeFile)
		authed.Delete("/sites/:sid/files", s.deleteFile)
		authed.Post("/sites/:sid/files/mkdir", s.mkdirFile)
		authed.Post("/sites/:sid/files/move", s.moveFile)
		authed.Post("/sites/:sid/files/extract", s.extractZip)
		authed.Post("/sites/:sid/files/publish", s.publishSiteFiles)
		authed.Post("/sites/:sid/deploy", s.deploySite)
		authed.Post("/sites/:sid/releases/:rid/activate", s.activateRelease)
		authed.Post("/sites/:sid/domains", s.addSiteDomain)
		authed.Post("/sites/:sid/domains/:domain/verify", s.verifySiteDomain)
		authed.Delete("/sites/:sid/domains/:domain", s.removeSiteDomain)
		authed.Get("/admin/sites", s.requireAdmin, s.adminListSites)
		authed.Patch("/admin/sites/:sid", s.requireAdmin, s.adminPatchSite)
		authed.Get("/admin/site-base-domains", s.requireAdmin, s.adminListBaseDomains)
		authed.Post("/admin/site-base-domains", s.requireAdmin, s.adminAddBaseDomain)
		authed.Post("/admin/site-base-domains/:domain/verify", s.requireAdmin, s.adminVerifyBaseDomain)
		authed.Post("/admin/site-base-domains/:domain/move-sites", s.requireAdmin, s.adminMoveBaseDomainSites)
		authed.Patch("/admin/site-base-domains/:domain", s.requireAdmin, s.adminPatchBaseDomain)
		authed.Delete("/admin/site-base-domains/:domain", s.requireAdmin, s.adminDeleteBaseDomain)
	}

	admin := authed.Group("/users", s.requireAdmin)
	admin.Post("", s.createUser)
	admin.Get("", s.listUsers)
	admin.Patch("/:id", s.patchUser)
	authed.Get("/admin/account-invites", s.requireAdmin, s.listAccountInvites)
	authed.Post("/admin/account-invites", s.requireAdmin, s.createAccountInvite)
	authed.Delete("/admin/account-invites/:id", s.requireAdmin, s.deleteAccountInvite)

	if s.diagnostics != nil {
		authed.Get("/admin/diagnostics", s.requireAdmin, s.getDiagnostics)
	}
	if s.settings != nil {
		authed.Get("/admin/settings", s.requireAdmin, s.getSettings)
		authed.Put("/admin/settings", s.requireAdmin, s.putSettings)
		if s.mail != nil {
			authed.Post("/admin/settings/mail/test", s.requireAdmin, s.testMail)
			authed.Get("/admin/mail/audience", s.requireAdmin, s.mailAudience)
			authed.Post("/admin/mail/announcements", s.requireAdmin, s.sendAnnouncement)
		}
	}
	if s.env != nil {
		authed.Get("/admin/environment", s.requireAdmin, s.getEnvironment)
		authed.Put("/admin/environment", s.requireAdmin, s.putEnvironment)
		authed.Post("/admin/environment/restart", s.requireAdmin, s.restartPanel)
	}
	if s.nodes != nil {
		nodes := authed.Group("/nodes", s.requireAdmin)
		nodes.Get("", s.listNodes)
		nodes.Get("/:id/telemetry", s.nodeTelemetry)
		nodes.Get("/:id/history", s.nodeHistory)
	}
	if s.host != nil {
		authed.Get("/admin/host", s.requireAdmin, s.hostSnapshot)
		authed.Get("/admin/host/bots", s.requireAdmin, s.hostBots)
	}
	if s.logs != nil {
		authed.Get("/admin/logs", s.requireAdmin, s.panelLogs)
	}

	authed.Post("/bots", s.createBot)
	authed.Post("/bots/batch", s.batchBots)
	authed.Get("/bots", s.listBots)
	authed.Get("/bots/:id", s.getBot)
	authed.Patch("/bots/:id", s.patchBot)
	authed.Delete("/bots/:id", s.deleteBot)
	authed.Post("/bots/:id/start", s.lifecycle(func(b *service.BotService, c fiber.Ctx, id string) (domain.Bot, error) {
		return b.Start(c.Context(), currentUser(c), id)
	}))
	authed.Post("/bots/:id/stop", s.lifecycle(func(b *service.BotService, c fiber.Ctx, id string) (domain.Bot, error) {
		return b.Stop(c.Context(), currentUser(c), id)
	}))
	authed.Post("/bots/:id/restart", s.lifecycle(func(b *service.BotService, c fiber.Ctx, id string) (domain.Bot, error) {
		return b.Restart(c.Context(), currentUser(c), id)
	}))
	authed.Post("/bots/:id/kill", s.lifecycle(func(b *service.BotService, c fiber.Ctx, id string) (domain.Bot, error) {
		return b.Kill(c.Context(), currentUser(c), id)
	}))
	authed.Put("/bots/:id/ports", s.setPorts)
	authed.Put("/bots/:id/tags", s.setTags)
	authed.Put("/bots/:id/favorite", s.setFavorite)
	if s.console != nil {
		authed.Get("/bots/:id/console", s.consoleGuard, fws.New(s.consoleWS, fws.Config{
			ReadBufferSize: 1024, WriteBufferSize: 4096, AllowEmptyOrigin: true,
		}))
	}
	if s.files != nil {
		authed.Get("/bots/:id/stats/stream", s.statsStream)
		authed.Get("/bots/:id/packages", s.listPackages)
		authed.Put("/bots/:id/packages", s.editPackages)
		authed.Get("/bots/:id/packages/search", registryLimit, s.searchPackages)
		authed.Get("/bots/:id/packages/latest", registryLimit, s.latestPackage)
		authed.Get("/bots/:id/files", s.listFiles)
		authed.Get("/bots/:id/files/content", s.readFile)
		authed.Put("/bots/:id/files/content", s.writeFile)
		authed.Delete("/bots/:id/files", s.deleteFile)
		authed.Post("/bots/:id/files/mkdir", s.mkdirFile)
		authed.Post("/bots/:id/files/move", s.moveFile)
		authed.Post("/bots/:id/files/extract", s.extractZip)
	}
	if s.analytics != nil {
		authed.Get("/sdk/:lang", s.sdkFile)
		authed.Get("/bots/:id/analytics", s.botAnalytics)
		authed.Delete("/bots/:id/widgets/:key", s.deleteBotWidget)
		authed.Post("/bots/:id/telemetry-key", s.rotateTelemetryKey)
		authed.Delete("/bots/:id/telemetry-key", s.revokeTelemetryKey)
	}
	if s.backups != nil {
		authed.Get("/bots/:id/backups", s.listBackups)
		authed.Post("/bots/:id/backups", s.createBackup)
		authed.Get("/bots/:id/backups/:bid/download", s.downloadBackup)
		authed.Post("/bots/:id/backups/:bid/restore", s.restoreBackup)
		authed.Delete("/bots/:id/backups/:bid", s.deleteBackup)
		authed.Patch("/bots/:id/backups/:bid", s.patchBackup)
		authed.Post("/bots/:id/backups/:bid/verify", s.verifyBackup)
	}
	if s.ops != nil {
		authed.Get("/operations", s.listActivity)
		authed.Get("/bots/:id/operations", s.listBotOperations)
		authed.Get("/bots/:id/operations/:op", s.getOperation)
		authed.Get("/bots/:id/operations/:op/output", s.operationOutput)
	}
	if s.health != nil {
		authed.Get("/bots/:id/health", s.getHealth)
		authed.Get("/bots/:id/health-probe", s.getHealthProbe)
		authed.Put("/bots/:id/health-probe", s.putHealthProbe)
		authed.Put("/bots/:id/alerts", s.putAlerts)
		authed.Post("/bots/:id/alerts/test", s.testAlert)
	}
	if s.schedules != nil {
		authed.Get("/schedules/preview", s.previewSchedule)
		authed.Get("/bots/:id/schedules", s.listSchedules)
		authed.Post("/bots/:id/schedules", s.createSchedule)
		authed.Patch("/bots/:id/schedules/:sid", s.patchSchedule)
		authed.Delete("/bots/:id/schedules/:sid", s.deleteSchedule)
		authed.Post("/bots/:id/schedules/:sid/run", s.runSchedule)
	}
	if s.audit != nil {
		authed.Get("/activity/changes", s.visibleAudit)
		authed.Get("/bots/:id/changes", s.botAudit)
	}
	authed.Post("/bots/:id/transfer", s.transferBot)
	authed.Get("/bots/:id/invites", s.listInvites)
	authed.Post("/bots/:id/invites", s.createInvite)
	authed.Delete("/bots/:id/invites/:iid", s.deleteInvite)
	authed.Post("/invites/preview", s.previewInvite)
	authed.Post("/invites/accept", s.acceptInvite)
	authed.Get("/bots/:id/users", s.listSubUsers)
	authed.Put("/bots/:id/users", s.shareBot)
	authed.Delete("/bots/:id/users/:uid", s.unshareBot)
	authed.Get("/bots/:id/env", s.listEnv)
	authed.Put("/bots/:id/env", s.setEnv)
	authed.Post("/bots/:id/env/:name/reveal", s.revealEnv)
	authed.Delete("/bots/:id/env/:name", s.deleteEnv)
}

// currentPublicURL follows changes made on the settings page.
func (s *server) currentPublicURL() string {
	if u := s.oauth.CurrentPublicURL(); u != "" {
		return u
	}
	return s.publicURL
}
