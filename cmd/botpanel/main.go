// Command botpanel runs the API, the embedded UI and the optional local runner.
//
//	botpanel                       serve
//	botpanel create-admin EMAIL    create an administrator (password from
//	                               BOTPANEL_ADMIN_PASSWORD or a hidden prompt)
//	botpanel reset-password EMAIL  set a new password (BOTPANEL_ADMIN_PASSWORD or a
//	                               hidden prompt) and sign the account out everywhere
//	botpanel reset-mfa EMAIL       remove two-step sign-in from an account
//	botpanel keygen [ID]           create an encryption key file in the key dir
//	botpanel backup [--include-keys] DEST
//	botpanel backup-verify DIR     check a backup against its manifest checksums
//	botpanel restore [--force] [--restore-keys] SRC   (panel must be stopped)
//	botpanel verify                trial-decrypt every stored environment value
//	botpanel reseal                re-encrypt every sealed value with the active key
//	                               (panel stopped; resumable)
//	botpanel doctor                read-only health checks (Docker, disk, keys, ...)
//	botpanel env [reset [NAME]]    show, or drop, the environment overrides saved
//	                               from the administration page (recovery path)
//	botpanel version               print the build version
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"sync/atomic"
	"syscall"
	"time"
	_ "time/tzdata" // schedules need time zones on hosts without a zoneinfo database

	"github.com/gofiber/fiber/v3"
	"golang.org/x/term"

	"botpanel/internal/api"
	"botpanel/internal/auth"
	"botpanel/internal/backup"
	"botpanel/internal/config"
	"botpanel/internal/console"
	"botpanel/internal/diag"
	"botpanel/internal/docker"
	"botpanel/internal/domain"
	"botpanel/internal/events"
	"botpanel/internal/filesystem"
	"botpanel/internal/github"
	"botpanel/internal/hostmon"
	"botpanel/internal/logbuf"
	"botpanel/internal/migrations"
	"botpanel/internal/oauth"
	"botpanel/internal/oplog"
	"botpanel/internal/runner"
	"botpanel/internal/runtimes"
	"botpanel/internal/secrets"
	"botpanel/internal/service"
	"botpanel/internal/sftpd"
	"botpanel/internal/sitehost"
	"botpanel/internal/store/sqlite"
	"botpanel/internal/telemetry"
	"botpanel/internal/webui"
	rtdefaults "botpanel/runtimes"
)

// restartRequested is set when an administrator restarts the panel from the
// browser; main then exits with restartExitCode so the supervisor starts it
// again (a clean exit would be treated as a deliberate stop by systemd's
// Restart=on-failure).
var restartRequested atomic.Bool

const restartExitCode = 75 // EX_TEMPFAIL

func main() {
	logs := logbuf.New(logbuf.DefaultCapacity)
	log := slog.New(logs.Handler(slog.NewJSONHandler(os.Stderr, nil)))
	var err error
	switch {
	case len(os.Args) >= 2 && os.Args[1] == "create-admin":
		err = createAdmin(os.Args[2:])
	case len(os.Args) >= 2 && os.Args[1] == "reset-password":
		err = resetPassword(os.Args[2:])
	case len(os.Args) >= 2 && os.Args[1] == "reset-mfa":
		err = resetMFA(os.Args[2:])
	case len(os.Args) >= 2 && os.Args[1] == "keygen":
		err = keygen(os.Args[2:])
	case len(os.Args) >= 2 && os.Args[1] == "backup":
		err = backupCmd(os.Args[2:])
	case len(os.Args) >= 2 && os.Args[1] == "backup-verify":
		err = backupVerifyCmd(os.Args[2:])
	case len(os.Args) >= 2 && os.Args[1] == "restore":
		err = restoreCmd(os.Args[2:])
	case len(os.Args) >= 2 && os.Args[1] == "reseal":
		err = resealCmd()
	case len(os.Args) >= 2 && os.Args[1] == "verify":
		err = verifyCmd()
	case len(os.Args) >= 2 && os.Args[1] == "version":
		fmt.Println("botpanel", version)
	case len(os.Args) >= 2 && os.Args[1] == "doctor":
		err = doctorCmd()
	case len(os.Args) >= 2 && os.Args[1] == "env":
		err = envCmd(os.Args[2:])
	case len(os.Args) == 1:
		err = serve(log, logs)
	default:
		err = fmt.Errorf("unknown command %q (see the package comment for usage)", os.Args[1])
	}
	if err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
	if restartRequested.Load() {
		log.Info("exiting so the supervisor restarts the panel", "code", restartExitCode)
		os.Exit(restartExitCode)
	}
}

func openDB(ctx context.Context, cfg config.Config) (*sqlite.DB, error) {
	db, err := sqlite.Open(ctx, cfg.DBPath, cfg.DBMaxOpenConns)
	if err != nil {
		return nil, err
	}
	if err := db.Migrate(ctx, migrations.FS); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	if err := db.EnsureLocalNode(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("register local node: %w", err)
	}
	return db, nil
}

func keygen(args []string) error {
	cfg, err := config.LoadEnv()
	if err != nil {
		return err
	}
	id := cfg.ActiveKeyID
	if len(args) > 0 {
		id = args[0]
	}
	if err := secrets.GenerateKeyFile(cfg.KeyDir, id); err != nil {
		return err
	}
	fmt.Printf("created %s/%s.key (mode 0600). Back it up separately from the database; without it, stored environment values cannot be decrypted.\n", cfg.KeyDir, id)
	return nil
}

func createAdmin(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: botpanel create-admin EMAIL")
	}
	cfg, err := config.LoadEnv()
	if err != nil {
		return err
	}
	pw := os.Getenv("BOTPANEL_ADMIN_PASSWORD")
	if pw == "" {
		fmt.Fprint(os.Stderr, "Password: ")
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return fmt.Errorf("read password (set BOTPANEL_ADMIN_PASSWORD when not on a terminal): %w", err)
		}
		pw = string(b)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := openDB(ctx, cfg)
	if err != nil {
		return err
	}
	defer db.Close()
	svc := &service.AuthService{Store: db, Hasher: auth.NewHasher(1), TTL: cfg.SessionTTL}
	u, err := svc.CreateUser(ctx, args[0], pw, domain.RoleAdmin)
	if err != nil {
		return err
	}
	fmt.Printf("created administrator %s (%s)\n", u.Email, u.ID)
	return nil
}

// readSecret returns BOTPANEL_ADMIN_PASSWORD or reads a hidden line.
func readSecret(prompt string) (string, error) {
	if pw := os.Getenv("BOTPANEL_ADMIN_PASSWORD"); pw != "" {
		return pw, nil
	}
	fmt.Fprint(os.Stderr, prompt)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read password (set BOTPANEL_ADMIN_PASSWORD when not on a terminal): %w", err)
	}
	return string(b), nil
}

// resetPassword is the recovery path for a locked-out account (including
// the only administrator). It needs shell access to the host.
func resetPassword(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: botpanel reset-password EMAIL")
	}
	cfg, err := config.LoadEnv()
	if err != nil {
		return err
	}
	pw, err := readSecret("New password: ")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := openDB(ctx, cfg)
	if err != nil {
		return err
	}
	defer db.Close()
	svc := &service.AuthService{Store: db, Hasher: auth.NewHasher(1), TTL: cfg.SessionTTL}
	u, err := svc.ResetPassword(ctx, args[0], pw)
	if err != nil {
		return err
	}
	fmt.Printf("password changed for %s; every session of this account was signed out\n", u.Email)
	return nil
}

// resetMFA removes two-step sign-in from an account (lost phone and lost
// recovery codes). It needs shell access to the host.
func resetMFA(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: botpanel reset-mfa EMAIL")
	}
	cfg, err := config.LoadEnv()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := openDB(ctx, cfg)
	if err != nil {
		return err
	}
	defer db.Close()
	svc := &service.MFAService{Store: db, Auth: &service.AuthService{Store: db}}
	u, err := svc.Reset(ctx, args[0])
	if err != nil {
		return err
	}
	fmt.Printf("two-step sign-in removed for %s; every session of this account was signed out\n", u.Email)
	return nil
}

func backupCmd(args []string) error {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	incl := fs.Bool("include-keys", false, "also copy encryption keys (store them separately from the rest)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: botpanel backup [--include-keys] DEST")
	}
	cfg, err := config.LoadEnv()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	db, err := openDB(ctx, cfg)
	if err != nil {
		return err
	}
	defer db.Close()
	m, err := backup.Create(ctx, backup.Source{DB: db, DataRoot: cfg.DataRoot, KeyDir: cfg.KeyDir}, fs.Arg(0), *incl, time.Now())
	if err != nil {
		return err
	}
	fmt.Printf("backup written to %s: database snapshot, %d bot workspace(s)\n", fs.Arg(0), m.Bots)
	if m.IncludesKeys {
		fmt.Println("WARNING: this directory contains encryption keys AND the database; protect it accordingly.")
	} else {
		fmt.Printf("Encryption keys were NOT included (key ids in use: %v). Back them up separately;\nwithout them stored environment values cannot be restored.\n", m.KeyIDs)
	}
	return nil
}

func backupVerifyCmd(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: botpanel backup-verify DIR")
	}
	m, err := backup.Verify(args[0])
	if err != nil {
		return err
	}
	fmt.Printf("backup OK: %d bot workspace(s), keys included: %v\n", m.Bots, m.IncludesKeys)
	return nil
}

func restoreCmd(args []string) error {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	force := fs.Bool("force", false, "replace an existing database and bot directories")
	keys := fs.Bool("restore-keys", false, "also restore keys included in the backup")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: botpanel restore [--force] [--restore-keys] SRC")
	}
	cfg, err := config.LoadEnv()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	m, err := backup.Restore(ctx, fs.Arg(0), backup.RestoreOptions{DBPath: cfg.DBPath, DataRoot: cfg.DataRoot, KeyDir: cfg.KeyDir, Force: *force, RestoreKeys: *keys})
	if err != nil {
		return err
	}
	fmt.Printf("restored database and %d bot workspace(s)\n", m.Bots)
	return verifyCmd()
}

// verifyCmd reports whether every stored environment value decrypts with the
// available keys.
func verifyCmd() error {
	cfg, err := config.LoadEnv()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	db, err := openDB(ctx, cfg)
	if err != nil {
		return err
	}
	defer db.Close()
	kr, err := secrets.LoadDir(cfg.KeyDir, cfg.ActiveKeyID, false)
	if err != nil {
		// No usable keys at all: still report what is missing.
		fmt.Printf("no usable encryption keys in %s: %v\n", cfg.KeyDir, err)
		kr, _ = secrets.NewKeyring("none", map[string][]byte{"none": make([]byte, 32)})
	}
	rep, err := backup.VerifyEnv(ctx, db, kr)
	if err != nil {
		return err
	}
	fmt.Printf("sealed values (environment, OAuth tokens, webhooks): %d total, %d decrypt correctly\n", rep.Total, rep.OK)
	if len(rep.MissingKeys) > 0 {
		fmt.Printf("MISSING KEYS (restore these key files): %v\n", rep.MissingKeys)
	}
	if rep.Failed > 0 {
		fmt.Printf("%d value(s) failed authentication (wrong key material or tampering)\n", rep.Failed)
	}
	if !rep.Healthy() {
		return errors.New("some environment values cannot be decrypted")
	}
	return nil
}

// resealCmd moves every sealed value (environment variables, OAuth tokens,
// Discord webhooks, GitHub webhook secrets, TOTP secrets) to the active key,
// so an old key can eventually be retired. Old keys stay needed for per-bot
// backup archives made before the reseal; the command does not touch those.
func resealCmd() error {
	cfg, err := config.LoadEnv()
	if err != nil {
		return err
	}
	unlock, err := lockInstallation(cfg.DBPath) // the panel must be stopped
	if err != nil {
		return err
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	db, err := openDB(ctx, cfg)
	if err != nil {
		return err
	}
	defer db.Close()
	kr, err := secrets.LoadDir(cfg.KeyDir, cfg.ActiveKeyID, false)
	if err != nil {
		return fmt.Errorf("encryption keys: %w", err)
	}
	st, err := db.Reseal(ctx, func(ns, name, keyID string, ct, nonce []byte) ([]byte, []byte, string, bool, error) {
		out, changed, err := kr.Reseal(ns, name, secrets.Sealed{Ciphertext: ct, Nonce: nonce, KeyID: keyID})
		return out.Ciphertext, out.Nonce, out.KeyID, changed, err
	})
	fmt.Printf("sealed values: %d checked, %d re-encrypted with key %q, %d could not be decrypted\n", st.Seen, st.Changed, kr.Active(), st.Failed)
	if err != nil {
		return err
	}
	if st.Failed > 0 {
		return errors.New("some values could not be decrypted (missing key files?); run botpanel verify for details")
	}
	fmt.Println("Keep the old key files while per-bot backups made before now exist: their environment is sealed with the key of that time.")
	return nil
}

func loadCatalog(cfg config.Config) (*runtimes.Catalog, error) {
	var fsys fs.FS = rtdefaults.FS
	if cfg.RuntimesDir != "" {
		fsys = os.DirFS(cfg.RuntimesDir)
	}
	return runtimes.Load(fsys)
}

func serve(log *slog.Logger, logs *logbuf.Buffer) error {
	// A soft ceiling makes the collector work harder near the footprint target.
	// It is not a hard cap (stacks, cgo-free runtime overhead and mapped files
	// are outside it); an explicit GOMEMLIMIT always wins.
	if os.Getenv("GOMEMLIMIT") == "" {
		debug.SetMemoryLimit(48 << 20)
	}
	cfg, err := config.LoadEnv()
	if err != nil {
		return fmt.Errorf("configuration: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// One panel per installation: a second process on the same database would
	// reconcile the same containers and corrupt each other's decisions.
	unlock, err := lockInstallation(cfg.DBPath)
	if err != nil {
		return err
	}
	defer unlock()
	started := time.Now()

	startCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	db, err := openDB(startCtx, cfg)
	if err != nil {
		return err
	}
	defer db.Close()
	keys, err := secrets.LoadDir(cfg.KeyDir, cfg.ActiveKeyID, !cfg.Production)
	if err != nil {
		return fmt.Errorf("encryption keys: %w", err)
	}
	// Everything above is read from the environment alone (database, keys); the
	// settings an administrator saved on the Environment page apply from here on.
	envSvc := &service.PanelEnvService{Store: db, Keys: keys, Base: processEnv, Log: log,
		CanRestart: supervised(), Restart: func() { restartRequested.Store(true); stop() }}
	cfg = effectiveConfig(startCtx, cfg, envSvc, log)
	catalog, err := loadCatalog(cfg)
	if err != nil {
		return fmt.Errorf("runtime catalog: %w", err)
	}
	wsm, err := filesystem.NewManager(cfg.DataRoot)
	if err != nil {
		return fmt.Errorf("data root: %w", err)
	}
	defer wsm.Close()
	// Before anything can touch a workspace: roll back restores and
	// deployments a crash interrupted, so no bot starts on half-replaced files.
	if rolled, err := wsm.Recover(); err != nil {
		log.Error("workspace recovery incomplete; affected files are kept for inspection", "err", err)
	} else if len(rolled) > 0 {
		log.Warn("rolled back interrupted restores or deployments", "bots", rolled)
	}
	owner := cfg.WorkspaceOwner
	if owner == "" {
		owner = cfg.ContainerUser
	}
	if uid, gid, err := runner.ParseUser(owner); err == nil {
		wsm.SetOwner(uid, gid) // files the panel writes are usable by the bot immediately
	}

	authSvc := &service.AuthService{Store: db, Hasher: auth.NewHasher(cfg.HashWorkers), TTL: cfg.SessionTTL}
	botSvc := &service.BotService{
		Store: db, Catalog: catalog, Keys: keys, Workspaces: wsm, LocalNode: domain.LocalNodeID,
		Limits: service.Limits{MinMemoryBytes: config.MinBotMemory, MaxMemoryBytes: cfg.MaxBotMemory,
			MinNanoCPUs: config.MinBotCPUs, MaxNanoCPUs: cfg.MaxBotCPUs,
			PortMin: cfg.PortMin, PortMax: cfg.PortMax, PortPublicBind: cfg.PortPublicBind,
			MaxBotsPerUser: cfg.MaxBotsPerUser, UserMemoryBytes: cfg.UserMemoryBytes, NodeMemoryBytes: cfg.NodeMemoryBytes},
	}
	// OAuth always exists: providers come from the environment or from the
	// settings page, and can change while the panel runs.
	oauthSvc := &service.OAuthService{Store: db, Auth: authSvc, Keys: keys, Providers: map[string]oauth.Provider{},
		PublicURL: cfg.PublicURL, AllowSignup: cfg.OAuthAllowSignup, States: oauth.NewStateStore(), Log: log}
	settingsSvc := &service.SettingsService{Store: db, Keys: keys, OAuth: oauthSvc, Auth: authSvc, Log: log,
		Env: service.EnvSettings{PublicURL: cfg.PublicURL, GitHubID: cfg.GitHubClientID, GitHubSecret: cfg.GitHubSecret,
			DiscordID: cfg.DiscordClientID, DiscordSecret: cfg.DiscordSecret, AllowSignup: cfg.OAuthAllowSignup,
			AllowSignupSet: cfg.OAuthAllowSignupSet, Production: cfg.Production}}
	if err := settingsSvc.Apply(startCtx); err != nil {
		return fmt.Errorf("panel settings: %w", err)
	}
	setupCodeFile := filepath.Join(filepath.Dir(cfg.DBPath), "setup-code")
	if need, err := settingsSvc.SetupNeeded(startCtx); err == nil && need {
		code := settingsSvc.SetupCode()
		if err := os.WriteFile(setupCodeFile, []byte(code+"\n"), 0o600); err != nil {
			log.Warn("could not write the setup code file", "err", err)
		}
		log.Warn("FIRST-RUN SETUP: open the panel in a browser and enter this setup code", "setup_code", code, "file", setupCodeFile)
	} else {
		_ = os.Remove(setupCodeFile)
	}
	cancel()

	bus := events.NewBus()
	botSvc.Bus = bus
	botSvc.Files = wsm
	botSvc.Coord = &service.Coordinator{}
	opLogs, err := oplog.New(filepath.Join(filepath.Dir(cfg.DBPath), "oplogs"), oplog.DefaultMax)
	if err != nil {
		return fmt.Errorf("operation logs: %w", err)
	}
	ops := &service.Operations{Store: db, Bots: botSvc, Logs: opLogs, Log: log}
	ops.Recover(ctx)
	audit := &service.Audit{Store: db, Bots: botSvc, Log: log}
	go func() {
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for {
			if err := audit.Prune(ctx); err != nil && ctx.Err() == nil {
				log.Warn("prune activity record", "err", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
	// Alerts and GitHub deployments are always wired; they report clearly
	// when Discord or GitHub is not configured (yet).
	alerts := &service.AlertService{Store: db, Keys: keys, Bots: db, Log: log, Prefs: db.GetAlertPrefs}
	go alerts.Watch(ctx, bus)
	deploySvc := &service.DeployService{Bots: botSvc, OAuth: oauthSvc, Files: wsm, GH: &github.Client{}, Keys: keys,
		Alerts: alerts, PublicURL: cfg.PublicURL, Log: log, Ops: ops}
	deploySvc.Limits = filesystem.DefaultBackupLimits
	deploySvc.MinFreeDisk = cfg.MinFreeDisk
	deploySvc.Start(ctx)
	botSvc.BeforeDelete = deploySvc.BeforeBotDelete
	var checks []api.Check
	var runnerReady func(context.Context) error
	var runnerStatus func(context.Context) (runner.Status, bool)
	var runnerDone chan struct{}
	var dk *docker.Adapter
	var installID string
	var aiDiagnostic service.DiagnosticRunner
	if cfg.RunnerMode == config.RunnerLocal {
		var err error
		dk, err = docker.New(cfg.DockerHost)
		if err != nil {
			return fmt.Errorf("docker client: %w", err)
		}
		defer dk.Close()
		installID, err = db.InstallationID(ctx, time.Now().UnixMilli())
		if err != nil {
			return fmt.Errorf("installation identity: %w", err)
		}
		rn, err := runner.New(runner.Deps{Store: db, Docker: dk, Env: botSvc, Workspaces: wsm, Catalog: catalog, Log: log, Bus: bus, Builds: ops},
			runner.Options{NodeID: domain.LocalNodeID, InstallID: installID, User: cfg.ContainerUser, WorkspaceOwner: cfg.WorkspaceOwner, Network: cfg.ContainerNetwork,
				Workers: cfg.RunnerWorkers, BuildTimeout: cfg.BuildTimeout, MaxBuilds: cfg.MaxBuilds})
		if err != nil {
			return fmt.Errorf("runner: %w", err)
		}
		botSvc.Notifier, botSvc.Purger, botSvc.Killer = rn, rn, rn
		checks = append(checks, api.Check{Name: "docker", Fn: rn.Check})
		runnerReady = rn.Check
		runnerStatus = func(context.Context) (runner.Status, bool) { return rn.Status(), true }
		runnerDone = make(chan struct{})
		go func() {
			defer close(runnerDone)
			if err := rn.Run(ctx); err != nil {
				log.Error("runner stopped", "err", err)
			}
		}()
		aiDiagnostic = &runner.Diagnostic{Docker: dk, Files: wsm, Catalog: catalog, ScratchRoot: filepath.Join(filepath.Dir(cfg.DBPath), "ai-scratch"),
			User: cfg.ContainerUser, InstallID: installID, NodeID: domain.LocalNodeID, Timeout: 10 * time.Minute,
			MemoryBytes: 768 << 20, NanoCPUs: 1e9, PidsLimit: 256, TmpfsBytes: 128 << 20}
	}

	go pruneSessions(ctx, log, db)

	var consoleSvc *console.Service
	if dk != nil {
		consoleSvc = &console.Service{Src: dk, Bus: bus}
	}
	sampler := &telemetry.Sampler{Store: db, Reader: telemetry.ProcReader{}, NodeID: domain.LocalNodeID,
		DiskPath: cfg.DataRoot, Interval: cfg.TelemetryInterval, Retention: cfg.TelemetryRetention, Log: log}
	go sampler.Run(ctx)

	var statsSrc api.StatsSource
	if dk != nil {
		statsSrc = dk
	}
	var sftpInfo *api.SFTPInfo
	var sftpDone chan struct{}
	if cfg.SFTPListen != "" {
		hostKey, err := sftpd.LoadOrCreateHostKey(cfg.SFTPHostKey)
		if err != nil {
			return fmt.Errorf("sftp host key: %w", err)
		}
		sln, err := net.Listen("tcp", cfg.SFTPListen)
		if err != nil {
			return fmt.Errorf("sftp listen: %w", err)
		}
		_, port, _ := net.SplitHostPort(sln.Addr().String())
		sftpInfo = &api.SFTPInfo{Port: port, Fingerprint: sftpd.Fingerprint(hostKey)}
		log.Info("sftp listening", "addr", sln.Addr().String(), "host_key", sftpInfo.Fingerprint)
		sftpSrv := &sftpd.Server{Auth: authSvc, Bots: botSvc, Files: wsm, HostKey: hostKey, MaxFile: cfg.SFTPMaxFile, Log: log}
		sftpDone = make(chan struct{})
		go func() { defer close(sftpDone); sftpSrv.Serve(ctx, sln) }()
	}

	backupSvc := &service.BackupService{Bots: botSvc, Files: wsm, Keys: keys, Dir: cfg.BackupDir, Keep: cfg.BackupKeep,
		Interval: cfg.BackupInterval, Log: log, Ops: ops, Alerts: alerts, MinFreeDisk: cfg.MinFreeDisk}
	backupSvc.Limits = filesystem.DefaultBackupLimits
	backupSvc.Limits.MaxBytes = cfg.BackupMaxBytes
	if err := backupSvc.Start(ctx); err != nil {
		return fmt.Errorf("backups: %w", err)
	}
	botSvc.OnDelete = backupSvc.PurgeBot
	scheduler := &service.Scheduler{Store: db, Bots: botSvc, Backups: backupSvc, Deploy: deploySvc, Log: log}
	go scheduler.Run(ctx)
	health := &service.HealthService{Store: db, Bots: botSvc, Alerts: alerts, Log: log}
	go health.Run(ctx)
	analytics := &service.Analytics{Store: db, Heartbeat: health.Heartbeat}
	go pruneTelemetry(ctx, log, analytics, cfg.TelemetryRetention)

	var enabledOAuth []string
	for _, p := range []string{"github", "discord"} {
		if oauthSvc != nil && oauthSvc.Enabled(p) {
			enabledOAuth = append(enabledOAuth, p)
		}
	}
	// Static site hosting: its own listener, never the panel's origin.
	var sitesSvc *service.SiteService
	var sitesSrv *http.Server
	if cfg.SitesListen != "" {
		panelURL := oauthSvc.CurrentPublicURL()
		if panelURL == "" {
			panelURL = cfg.PublicURL
		}
		panelHost := ""
		if u, err := url.Parse(panelURL); err == nil {
			panelHost = u.Hostname()
		}
		sitesSvc = &service.SiteService{Store: db, Bots: botSvc, OAuth: oauthSvc, GH: &github.Client{}, Dir: cfg.SitesDir,
			BaseURL: cfg.SitesBaseURL, DNSTarget: cfg.SitesDNSTarget, PanelHost: panelHost, MaxBytes: cfg.SiteMaxBytes,
			MaxPerUser: cfg.MaxSitesPerUser, Log: log}
		if err := sitesSvc.Start(ctx); err != nil {
			return err
		}
		sln, err := net.Listen("tcp", cfg.SitesListen)
		if err != nil {
			return fmt.Errorf("sites listen: %w", err)
		}
		sitesSrv = sitehost.NewServer(&sitehost.Handler{Sites: sitesSvc, Log: log})
		log.Info("sites listening", "addr", sln.Addr().String(), "base_url", cfg.SitesBaseURL)
		go func() {
			if err := sitesSrv.Serve(sln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Error("sites server", "err", err)
			}
		}()
	}
	migs, _ := fs.Glob(migrations.FS, "*.sql")
	dsrc := diagSources{cfg: cfg, db: db, files: wsm, catalog: catalog, keys: keys, runnerStatus: runnerStatus,
		oauth: enabledOAuth, knownSchema: migrationCount(migs)}
	diagnostics := func(ctx context.Context) diag.Report { return diag.Run(ctx, version, started, probes(dsrc)) }
	var aiLogs service.LogTail
	if dk != nil {
		aiLogs = dk
	}
	monitor := &hostmon.Monitor{Proc: telemetry.ProcReader{}, Store: db, Users: db, Runner: runnerStatus, Logs: logs, Version: version,
		Started: started, DBPath: cfg.DBPath, DataRoot: cfg.DataRoot, BackupDir: cfg.BackupDir, NodeBudget: cfg.NodeMemoryBytes,
		WorkspaceUsage: wsm.Usage}
	if cfg.SitesListen != "" {
		monitor.SitesDir = cfg.SitesDir
	}
	if dk != nil {
		monitor.Stats, monitor.Containers = dk, dk
	}
	aiSvc := &service.AIService{Store: db, Keys: keys, Bots: botSvc, Sites: sitesSvc, Files: wsm, Ops: ops, Audit: audit, Diagnostic: aiDiagnostic, Logs: aiLogs, Log: log}
	aiSvc.Start(ctx)
	app := api.New(api.Deps{Diagnostics: diagnostics, Log: log, Deploy: deploySvc, Backups: backupSvc, Stats: statsSrc, SFTP: sftpInfo, Analytics: analytics, PublicURL: cfg.PublicURL, DB: db, UI: webui.FS(), Auth: authSvc, Bots: botSvc, OAuth: oauthSvc,
		Catalog: catalog, SecureCookies: cfg.Production, ProxyHeader: cfg.ProxyHeader, MetricsToken: cfg.MetricsToken, Checks: checks, Nodes: db, Files: wsm, MaxUpload: cfg.MaxUploadBytes,
		Console: consoleSvc, BaseCtx: ctx, RunnerReady: runnerReady, Ops: ops, Audit: audit, Schedules: scheduler,
		MFA: &service.MFAService{Store: db, Keys: keys, Auth: authSvc}, Tokens: &service.TokenService{Store: db, Bots: botSvc}, Health: health,
		Settings: settingsSvc, Sites: sitesSvc, AI: aiSvc, Env: envSvc, Host: monitor, Logs: logs, SetupCodeFile: setupCodeFile, OnSetupDone: func() { _ = os.Remove(setupCodeFile) }})
	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return err
	}
	log.Info("listening", "addr", ln.Addr().String(), "db", cfg.DBPath, "runner", cfg.RunnerMode)

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- app.Listener(ln, fiber.ListenConfig{DisableStartupMessage: true})
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down")
	shCtx, shCancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer shCancel()
	if err := app.ShutdownWithContext(shCtx); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("shutdown: %w", err)
	}
	if sitesSrv != nil {
		_ = sitesSrv.Shutdown(shCtx)
		sitesSvc.Wait()
	}
	backupSvc.Wait()
	if deploySvc != nil {
		deploySvc.Wait()
	}
	if sftpDone != nil {
		select {
		case <-sftpDone:
		case <-shCtx.Done():
		}
	}
	if runnerDone != nil {
		select {
		case <-runnerDone: // ctx is cancelled; workers drain
		case <-shCtx.Done():
		}
	}
	return nil
}

// pruneTelemetry enforces bot telemetry retention hourly.
func pruneTelemetry(ctx context.Context, log *slog.Logger, a *service.Analytics, retention time.Duration) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		if err := a.Prune(ctx, retention); err != nil && ctx.Err() == nil {
			log.Warn("prune bot telemetry", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// pruneSessions deletes expired sessions in bounded batches.
func pruneSessions(ctx context.Context, log *slog.Logger, db *sqlite.DB) {
	t := time.NewTicker(15 * time.Minute)
	defer t.Stop()
	for {
		for {
			n, err := db.PruneSessions(ctx, time.Now().UnixMilli(), 500)
			if err != nil {
				log.Warn("prune sessions", "err", err)
				break
			}
			if n < 500 {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// lockInstallation takes an exclusive lock next to the database for the life
// of the process.
func lockInstallation(dbPath string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o750); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(dbPath+".lock", os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("installation lock: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("another BotForge process is already using %s; stop it first", dbPath)
	}
	return func() { f.Close() }, nil
}

// doctorCmd runs the diagnostics without starting the panel.
func doctorCmd() error {
	cfg, err := config.LoadEnv()
	if err != nil {
		return fmt.Errorf("configuration: %w", err)
	}
	fmt.Println("configuration: ok")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	db, err := openDB(ctx, cfg)
	if err != nil {
		return err
	}
	defer db.Close()
	keys, _ := secrets.LoadDir(cfg.KeyDir, cfg.ActiveKeyID, false)
	catalog, err := loadCatalog(cfg)
	if err != nil {
		return err
	}
	wsm, err := filesystem.NewManager(cfg.DataRoot)
	if err != nil {
		return err
	}
	defer wsm.Close()
	src := diagSources{cfg: cfg, db: db, files: wsm, catalog: catalog, keys: keys}
	if cfg.RunnerMode == config.RunnerLocal {
		dk, err := docker.New(cfg.DockerHost)
		if err == nil {
			defer dk.Close()
			src.runnerStatus = func(ctx context.Context) (runner.Status, bool) {
				caps, err := dk.Capabilities(ctx)
				if err == nil {
					err = caps.Validate()
				}
				return runner.Status{Ready: err, Capabilities: caps, Workers: cfg.RunnerWorkers}, true
			}
		}
	}
	migs, _ := fs.Glob(migrations.FS, "*.sql")
	src.knownSchema = migrationCount(migs)
	r := diag.Run(ctx, version, time.Now(), probes(src))
	for _, c := range r.Checks {
		mark := map[diag.Status]string{diag.OK: "ok  ", diag.Warn: "WARN", diag.Fail: "FAIL", diag.Info: "info"}[c.Status]
		fmt.Printf("%s  %-22s %s\n", mark, c.Title, c.Detail)
		if c.Fix != "" && c.Status != diag.OK {
			fmt.Printf("      %-22s -> %s\n", "", c.Fix)
		}
	}
	if fail, _ := r.Summary(); fail > 0 {
		return fmt.Errorf("%d check(s) failed", fail)
	}
	return nil
}
