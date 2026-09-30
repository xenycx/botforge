package service

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"botpanel/internal/domain"
	"botpanel/internal/events"
	"botpanel/internal/filesystem"
	"botpanel/internal/runtimes"
	"botpanel/internal/secrets"
	"botpanel/internal/templates"
)

// Workspaces is the filesystem surface used for bot lifecycle.
type Workspaces interface {
	Create(botID string) error
	Remove(botID string) error
}

// Notifier tells the runner that a bot's intent changed. The persisted desired
// state is the durable record; notification only reduces latency.
type Notifier interface{ Notify(botID string) }

// Purger removes everything belonging to a bot marked deleted (containers,
// workspace, database row). It must be idempotent.
type Purger interface {
	Purge(ctx context.Context, botID string) error
}

// BotService implements bot configuration rules and ownership checks.
type BotService struct {
	Store        Store
	Catalog      *runtimes.Catalog
	Keys         *secrets.Keyring
	Workspaces   Workspaces
	Limits       Limits
	LocalNode    string
	Now          func() time.Time
	Notifier     Notifier                                // nil disables lifecycle requests
	Purger       Purger                                  // nil: workspace+row cleanup inline (no runner, no containers)
	Bus          *events.Bus                             // optional: status fan-out to live views
	Killer       Killer                                  // optional: immediate SIGKILL
	OnDelete     func(botID string)                      // optional: called after a bot is deleted (e.g. purge its backups)
	BeforeDelete func(ctx context.Context, botID string) // optional best-effort cleanup (e.g. remove the GitHub webhook)
	Files        *filesystem.Manager                     // optional: needed to seed templates
	Coord        *Coordinator                            // per-bot operation reservations (nil = none)
}

// FilesBlocked reports (as a BusyError) whether a deployment or restore is
// replacing the bot's files, so edits from the panel or SFTP must wait.
func (s *BotService) FilesBlocked(botID string) error { return s.Coord.Blocked(botID) }

// Killer force-stops a bot's containers without a graceful period.
type Killer interface {
	Kill(ctx context.Context, botID string) error
}

// CreateBotInput describes a new bot. Zero resource values use runtime defaults.
type CreateBotInput struct {
	Name        string
	Runtime     string
	Argv        []string
	MemoryBytes int64
	NanoCPUs    int64
	PidsLimit   int64
	NodeID      string
	SourceType  string  // manual (default) | template | github
	TemplateID  *string // set for SourceType template
	// Env are initial variables (e.g. the template's DISCORD_TOKEN), sealed
	// like any other; invalid names or values reject the whole request.
	Env map[string]string
}

// UpdateBotInput is a partial configuration change; nil fields are unchanged.
type UpdateBotInput struct {
	Name        *string
	Argv        *[]string
	MemoryBytes *int64
	NanoCPUs    *int64
	PidsLimit   *int64

	Runtime    *string
	Entrypoint *[]string // empty slice clears it

	NetworkEnabled *bool
	BandwidthKbps  *int64 // 0 clears
	AutoBackup     *bool

	RestartPolicy           *string
	RestartMaxAttempts      *int64
	RestartBackoffInitialMS *int64
	RestartBackoffMaxMS     *int64
}

// EnvView is the masked view of a variable.
type EnvView struct {
	Name        string
	UpdatedAtMS int64
}

func (s *BotService) now() int64 {
	if s.Now != nil {
		return s.Now().UnixMilli()
	}
	return time.Now().UnixMilli()
}

// Permission requirements for loadPerm besides the domain.Perm* bits.
const (
	permAny       = 0  // any access at all (owner, admin or any sub-user grant)
	permOwnerOnly = -1 // owner or administrator only
)

// Authorize returns the bot if the actor holds perm on it. Bots the actor has no
// access to at all are indistinguishable from nonexistent ones (ErrNotFound) so
// IDs cannot be probed; a sub-user lacking perm gets ErrForbidden.
func (s *BotService) Authorize(ctx context.Context, actor domain.User, id string, perm int) (domain.Bot, error) {
	return s.loadPerm(ctx, actor, id, perm, false)
}

func (s *BotService) load(ctx context.Context, actor domain.User, id string, allowDeleted bool) (domain.Bot, error) {
	return s.loadPerm(ctx, actor, id, permOwnerOnly, allowDeleted)
}

func (s *BotService) loadPerm(ctx context.Context, actor domain.User, id string, perm int, allowDeleted bool) (domain.Bot, error) {
	if u, err := uuid.Parse(id); err != nil || u.String() != id {
		return domain.Bot{}, domain.ErrNotFound
	}
	b, err := s.Store.GetBot(ctx, id)
	if err != nil {
		return domain.Bot{}, err
	}
	if b.DesiredState == domain.DesiredDeleted && !allowDeleted {
		return domain.Bot{}, domain.ErrNotFound
	}
	if b.OwnerID == actor.ID || actor.IsAdmin() {
		return b, nil
	}
	mask, err := s.Store.GetSubUserPermissions(ctx, id, actor.ID)
	if err != nil {
		return domain.Bot{}, err // ErrNotFound: no grant at all
	}
	if perm == permOwnerOnly || (perm != permAny && !domain.HasPerm(mask, perm)) {
		return domain.Bot{}, domain.ErrForbidden
	}
	return b, nil
}

// Permissions returns the actor's permission mask on a bot they can access.
func (s *BotService) Permissions(ctx context.Context, actor domain.User, b domain.Bot) int {
	if b.OwnerID == actor.ID || actor.IsAdmin() {
		return domain.PermAll
	}
	mask, _ := s.Store.GetSubUserPermissions(ctx, b.ID, actor.ID)
	if mask&domain.PermFullAdmin != 0 {
		return domain.PermAll
	}
	return mask
}

func (s *BotService) Create(ctx context.Context, actor domain.User, in CreateBotInput) (domain.Bot, error) {
	name, err := validateName(in.Name)
	if err != nil {
		return domain.Bot{}, err
	}
	if len(in.Env) > maxEnvVars {
		return domain.Bot{}, domain.Invalid(fmt.Sprintf("at most %d environment variables per bot", maxEnvVars))
	}
	for n, v := range in.Env {
		if err := validateEnvName(n, false); err != nil {
			return domain.Bot{}, err
		}
		if err := validateEnvValue(v); err != nil {
			return domain.Bot{}, err
		}
	}
	var seed []templates.File
	if in.TemplateID != nil {
		t, ok := templates.Get(*in.TemplateID)
		if !ok {
			return domain.Bot{}, domain.Invalid("unknown template")
		}
		if s.Files == nil {
			return domain.Bot{}, domain.Invalid("templates are not available")
		}
		var err error
		if seed, err = templates.Files(t.ID); err != nil {
			return domain.Bot{}, err
		}
		in.Runtime, in.SourceType = t.Runtime, "template"
		if in.Argv == nil && len(t.Argv) > 0 {
			in.Argv = append([]string(nil), t.Argv...)
		}
	}
	rt, ok := s.Catalog.Get(in.Runtime)
	if !ok {
		return domain.Bot{}, domain.Invalid("unknown runtime")
	}
	argv := in.Argv
	if argv == nil {
		argv = append([]string(nil), rt.DefaultArgv...)
	}
	if err := validateArgv(rt, argv); err != nil {
		return domain.Bot{}, err
	}
	mem, cpu, pids := in.MemoryBytes, in.NanoCPUs, in.PidsLimit
	if mem == 0 {
		mem = rt.Defaults.MemoryBytes
	}
	if cpu == 0 {
		cpu = rt.Defaults.NanoCPUs
	}
	if pids == 0 {
		pids = rt.Defaults.PidsLimit
	}
	if err := s.Limits.validateResources(rt, mem, cpu, pids); err != nil {
		return domain.Bot{}, err
	}
	if err := s.checkUserBudget(ctx, actor, 1, mem); err != nil {
		return domain.Bot{}, err
	}
	nodeID := in.NodeID
	if nodeID == "" {
		nodeID = s.LocalNode
	}
	node, err := s.Store.GetNode(ctx, nodeID)
	if err != nil || !node.Enabled {
		return domain.Bot{}, domain.Invalid("node is unavailable")
	}

	now := s.now()
	b := domain.Bot{
		ID: uuid.NewString(), OwnerID: actor.ID, NodeID: nodeID, Name: name, Runtime: rt.ID, ImageRef: rt.ImageRef(),
		Argv: argv, MemoryBytes: mem, NanoCPUs: cpu, PidsLimit: pids,
		DesiredState: domain.DesiredStopped, ObservedState: "stopped", CreatedAtMS: now, UpdatedAtMS: now,
		SourceType: in.SourceType, TemplateID: in.TemplateID, RestartPolicy: domain.RestartOnFailure,
	}
	// Workspace first: a leftover directory is harmless and reconcilable, whereas
	// a row without a workspace would be a broken bot.
	if err := s.Workspaces.Create(b.ID); err != nil {
		return domain.Bot{}, fmt.Errorf("create workspace: %w", err)
	}
	if err := s.Store.CreateBot(ctx, b); err != nil {
		_ = s.Workspaces.Remove(b.ID)
		return domain.Bot{}, err
	}
	if len(seed) > 0 {
		if err := s.seed(b.ID, seed); err != nil {
			_ = s.Store.MarkBotDeleted(ctx, b.ID, s.now())
			_ = s.Workspaces.Remove(b.ID)
			_ = s.Store.DeleteBotRow(ctx, b.ID)
			return domain.Bot{}, fmt.Errorf("seed template: %w", err)
		}
	}
	if len(in.Env) > 0 {
		if err := s.putEnv(ctx, b, in.Env, false); err != nil {
			_ = s.Store.MarkBotDeleted(ctx, b.ID, s.now())
			_ = s.Workspaces.Remove(b.ID)
			_ = s.Store.DeleteBotRow(ctx, b.ID)
			return domain.Bot{}, err
		}
	}
	return b, nil
}

func (s *BotService) Get(ctx context.Context, actor domain.User, id string) (domain.Bot, error) {
	return s.loadPerm(ctx, actor, id, permAny, false)
}

// List returns the actor's bots (all bots for administrators).
func (s *BotService) List(ctx context.Context, actor domain.User) ([]domain.Bot, error) {
	var all []domain.Bot
	var err error
	if actor.IsAdmin() {
		all, err = s.Store.ListBots(ctx, "")
	} else {
		all, err = s.Store.ListBotsForUser(ctx, actor.ID)
	}
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, b := range all {
		if b.DesiredState != domain.DesiredDeleted {
			out = append(out, b)
		}
	}
	return out, nil
}

// Update changes configuration of a stopped bot.
func (s *BotService) Update(ctx context.Context, actor domain.User, id string, in UpdateBotInput) (domain.Bot, error) {
	b, err := s.loadPerm(ctx, actor, id, domain.PermFullAdmin, false)
	if err != nil {
		return domain.Bot{}, err
	}
	old := b
	rt, ok := s.Catalog.Get(b.Runtime)
	if !ok {
		return domain.Bot{}, fmt.Errorf("runtime %q missing from catalog", b.Runtime)
	}
	if in.Runtime != nil && *in.Runtime != b.Runtime {
		nrt, ok := s.Catalog.Get(*in.Runtime)
		if !ok {
			return domain.Bot{}, domain.Invalid("unknown runtime")
		}
		rt = nrt
		b.Runtime, b.ImageRef = nrt.ID, nrt.ImageRef()
		if in.Argv == nil { // the old command rarely fits another language
			b.Argv = append([]string(nil), nrt.DefaultArgv...)
		}
		if in.Entrypoint == nil {
			b.Entrypoint = nil
		}
	}
	if in.Name != nil {
		if b.Name, err = validateName(*in.Name); err != nil {
			return domain.Bot{}, err
		}
	}
	if in.Argv != nil {
		b.Argv = *in.Argv
	}
	if in.Entrypoint != nil {
		b.Entrypoint = append([]string(nil), *in.Entrypoint...)
	}
	if err := validateStartup(rt, b.Argv, b.Entrypoint); err != nil {
		return domain.Bot{}, err
	}
	if in.MemoryBytes != nil {
		b.MemoryBytes = *in.MemoryBytes
	}
	if in.NanoCPUs != nil {
		b.NanoCPUs = *in.NanoCPUs
	}
	if in.PidsLimit != nil {
		b.PidsLimit = *in.PidsLimit
	}
	if err := s.Limits.validateResources(rt, b.MemoryBytes, b.NanoCPUs, b.PidsLimit); err != nil {
		return domain.Bot{}, err
	}
	if in.MemoryBytes != nil {
		owner, err := s.Store.GetUserByID(ctx, b.OwnerID)
		if err != nil {
			return domain.Bot{}, err
		}
		if err := s.checkUserBudget(ctx, owner, 0, b.MemoryBytes-old.MemoryBytes); err != nil {
			return domain.Bot{}, err
		}
	}
	if in.NetworkEnabled != nil {
		if !*in.NetworkEnabled && len(b.Ports) > 0 {
			return domain.Bot{}, domain.Invalid("remove the published ports before disabling networking")
		}
		b.NetworkDisabled = !*in.NetworkEnabled
	}
	if in.BandwidthKbps != nil {
		if *in.BandwidthKbps == 0 {
			b.BandwidthKbps = nil
		} else if *in.BandwidthKbps < 8 || *in.BandwidthKbps > 10_000_000 {
			return domain.Bot{}, domain.Invalid("bandwidth must be between 8 and 10000000 kbit/s (0 for unlimited)")
		} else {
			v := *in.BandwidthKbps
			b.BandwidthKbps = &v
		}
	}
	if in.AutoBackup != nil {
		b.AutoBackupOff = !*in.AutoBackup
	}
	if err := applyRestart(&b, in); err != nil {
		return domain.Bot{}, err
	}
	if err := s.Store.UpdateBotConfig(ctx, b, s.now()); err != nil {
		return domain.Bot{}, err
	}
	return s.Store.GetBot(ctx, id)
}

func applyRestart(b *domain.Bot, in UpdateBotInput) error {
	if in.RestartPolicy != nil {
		switch *in.RestartPolicy {
		case domain.RestartNever, domain.RestartOnFailure:
			b.RestartPolicy = *in.RestartPolicy
		default:
			return domain.Invalid("restart policy must be never or on_failure")
		}
	}
	if in.RestartMaxAttempts != nil {
		if *in.RestartMaxAttempts < 0 || *in.RestartMaxAttempts > 100 {
			return domain.Invalid("max restart attempts must be between 0 (unlimited) and 100")
		}
		b.RestartMaxAttempts = *in.RestartMaxAttempts
	}
	if in.RestartBackoffInitialMS != nil {
		b.RestartBackoffInitialMS = *in.RestartBackoffInitialMS
	}
	if in.RestartBackoffMaxMS != nil {
		b.RestartBackoffMaxMS = *in.RestartBackoffMaxMS
	}
	if b.RestartBackoffInitialMS < 100 || b.RestartBackoffMaxMS > 3_600_000 || b.RestartBackoffMaxMS < b.RestartBackoffInitialMS {
		return domain.Invalid("restart backoff must satisfy 100 ms <= initial <= max <= 3600000 ms")
	}
	return nil
}

// Delete marks the bot deleted, then tears it down (containers, workspace, row).
// If cleanup fails the row stays marked deleted so a retry or the reconciler
// can finish the job.
func (s *BotService) Delete(ctx context.Context, actor domain.User, id string) error {
	b, err := s.load(ctx, actor, id, true)
	if err != nil {
		return err
	}
	if s.Purger == nil && b.ContainerID != nil {
		return domain.ErrRunnerUnavailable // a container exists but nothing can remove it
	}
	// Deletion wins over a running deployment/restore/backup: cancel it and
	// wait for it to let go of the workspace before tearing it down.
	pctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	err = s.Coord.Preempt(pctx, id)
	cancel()
	if err != nil {
		return domain.Invalid("another operation is still finishing on this bot; try deleting again in a moment")
	}
	if s.BeforeDelete != nil {
		s.BeforeDelete(ctx, id)
	}
	if err := s.Store.MarkBotDeleted(ctx, id, s.now()); err != nil {
		return err
	}
	if s.Purger != nil {
		if err := s.Purger.Purge(ctx, id); err != nil {
			return err
		}
	} else {
		if err := s.Workspaces.Remove(id); err != nil {
			return fmt.Errorf("remove workspace: %w", err)
		}
		if err := s.Store.DeleteBotRow(ctx, id); err != nil {
			return err
		}
	}
	if s.OnDelete != nil {
		s.OnDelete(id)
	}
	return nil
}

// Start records intent to run the bot. Repeated requests for a bot that is
// already wanted running return the current generation unchanged.
func (s *BotService) Start(ctx context.Context, actor domain.User, id string) (domain.Bot, error) {
	return s.setIntent(ctx, actor, id, domain.DesiredRunning, false)
}

// Stop records intent to stop the bot.
func (s *BotService) Stop(ctx context.Context, actor domain.User, id string) (domain.Bot, error) {
	return s.setIntent(ctx, actor, id, domain.DesiredStopped, false)
}

// Restart replaces the container: it always advances the generation.
func (s *BotService) Restart(ctx context.Context, actor domain.User, id string) (domain.Bot, error) {
	return s.setIntent(ctx, actor, id, domain.DesiredRunning, true)
}

func (s *BotService) setIntent(ctx context.Context, actor domain.User, id, desired string, force bool) (domain.Bot, error) {
	if s.Notifier == nil {
		return domain.Bot{}, domain.ErrRunnerUnavailable
	}
	b, err := s.loadPerm(ctx, actor, id, domain.PermPower, false)
	if err != nil {
		return domain.Bot{}, err
	}
	if desired == domain.DesiredRunning {
		if err := s.Coord.Blocked(id); err != nil {
			return domain.Bot{}, err
		}
		node, err := s.Store.GetNode(ctx, b.NodeID)
		if err != nil || !node.Enabled {
			return domain.Bot{}, domain.Invalid("node is unavailable")
		}
		if _, ok := s.Catalog.Get(b.Runtime); !ok {
			return domain.Bot{}, domain.Invalid("runtime is no longer available")
		}
	}
	// A bot that is meant to run but has settled in a terminal state (a clean
	// exit, a crash with no restart left, or the restart policy said stop) needs
	// an explicit retry; "already wanted running" would otherwise ignore Start.
	// An unsettled generation (start still pending) stays idempotent.
	if desired == domain.DesiredRunning && !force && b.DesiredState == domain.DesiredRunning &&
		b.ObservedGeneration == b.Generation && (b.ObservedState == "stopped" || b.ObservedState == "failed") {
		force = true
	}
	nb, changed, err := s.Store.SetDesiredWithin(ctx, id, desired, force, s.now(), s.Limits.NodeMemoryBytes)
	if err != nil {
		return domain.Bot{}, err
	}
	if changed {
		s.Bus.Publish(events.Status{BotID: id, DesiredState: nb.DesiredState, ObservedState: nb.ObservedState,
			Generation: nb.Generation, ObservedGeneration: nb.ObservedGeneration})
		s.Notifier.Notify(id)
	}
	return nb, nil
}

// ListEnv returns masked variables (names only).
func (s *BotService) ListEnv(ctx context.Context, actor domain.User, id string) ([]EnvView, error) {
	if _, err := s.loadPerm(ctx, actor, id, domain.PermManageEnv, false); err != nil {
		return nil, err
	}
	rows, err := s.Store.ListEnv(ctx, id)
	if err != nil {
		return nil, err
	}
	out := make([]EnvView, len(rows))
	for i, r := range rows {
		out[i] = EnvView{r.Name, r.UpdatedAtMS}
	}
	return out, nil
}

// SetEnv encrypts and upserts variables on a stopped bot.
func (s *BotService) SetEnv(ctx context.Context, actor domain.User, id string, vars map[string]string) error {
	b, err := s.loadPerm(ctx, actor, id, domain.PermManageEnv, false)
	if err != nil {
		return err
	}
	return s.putEnv(ctx, b, vars, false)
}

// putEnv validates, seals and stores variables. System callers may set the
// reserved BOTPANEL_* names that users cannot.
func (s *BotService) putEnv(ctx context.Context, b domain.Bot, vars map[string]string, system bool) error {
	if len(vars) == 0 {
		return domain.Invalid("no variables provided")
	}
	if err := s.Coord.Blocked(b.ID); err != nil {
		return err
	}
	names := make([]string, 0, len(vars))
	for n, v := range vars {
		if err := validateEnvName(n, system); err != nil {
			return err
		}
		if err := validateEnvValue(v); err != nil {
			return err
		}
		names = append(names, n)
	}
	sort.Strings(names)
	existing, err := s.Store.ListEnv(ctx, b.ID)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for _, e := range existing {
		have[e.Name] = true
	}
	total := len(existing)
	for _, n := range names {
		if !have[n] {
			total++
		}
	}
	if total > maxEnvVars {
		return domain.Invalid(fmt.Sprintf("at most %d environment variables per bot", maxEnvVars))
	}
	rows := make([]domain.EnvVar, 0, len(names))
	for _, n := range names {
		sealed, err := s.Keys.Seal(b.ID, n, []byte(vars[n]))
		if err != nil {
			return err
		}
		rows = append(rows, domain.EnvVar{BotID: b.ID, Name: n, Ciphertext: sealed.Ciphertext, Nonce: sealed.Nonce, KeyID: sealed.KeyID})
	}
	return s.Store.UpsertEnv(ctx, b.ID, rows, s.now())
}

// DeleteEnv removes one variable from a stopped bot.
func (s *BotService) DeleteEnv(ctx context.Context, actor domain.User, id, name string) error {
	if _, err := s.loadPerm(ctx, actor, id, domain.PermManageEnv, false); err != nil {
		return err
	}
	if err := s.Coord.Blocked(id); err != nil {
		return err
	}
	return s.Store.DeleteEnv(ctx, id, name, s.now())
}

// DecryptEnv returns plaintext variables for container construction. It is
// not exposed over the API; only the runner calls it.
func (s *BotService) DecryptEnv(ctx context.Context, botID string) (map[string]string, error) {
	rows, err := s.Store.ListEnv(ctx, botID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		pt, err := s.Keys.Open(botID, r.Name, secrets.Sealed{Ciphertext: r.Ciphertext, Nonce: r.Nonce, KeyID: r.KeyID})
		if err != nil {
			return nil, fmt.Errorf("variable %s: %w", r.Name, err)
		}
		out[r.Name] = string(pt)
	}
	return out, nil
}

// seed writes template files into a new bot's workspace.
func (s *BotService) seed(botID string, files []templates.File) error {
	w, err := s.Files.Open(botID)
	if err != nil {
		return err
	}
	defer w.Close()
	for _, f := range files {
		if err := w.Write(f.Path, bytes.NewReader(f.Data), 1<<20); err != nil {
			return fmt.Errorf("%s: %w", f.Path, err)
		}
	}
	return nil
}

// RevealEnv returns one variable's plaintext to a user with the env permission.
// Values are masked everywhere else; this is an explicit, single-value request.
func (s *BotService) RevealEnv(ctx context.Context, actor domain.User, id, name string) (string, error) {
	b, err := s.loadPerm(ctx, actor, id, domain.PermManageEnv, false)
	if err != nil {
		return "", err
	}
	rows, err := s.Store.ListEnv(ctx, b.ID)
	if err != nil {
		return "", err
	}
	for _, r := range rows {
		if r.Name == name {
			pt, err := s.Keys.Open(b.ID, r.Name, secrets.Sealed{Ciphertext: r.Ciphertext, Nonce: r.Nonce, KeyID: r.KeyID})
			return string(pt), err
		}
	}
	return "", domain.ErrNotFound
}

// StopOwnedBy records stopped intent for every bot a user owns (offboarding:
// disabling an account does not by itself stop its bots). Admin only.
func (s *BotService) StopOwnedBy(ctx context.Context, actor domain.User, ownerID string) (int, error) {
	if !actor.IsAdmin() {
		return 0, domain.ErrForbidden
	}
	if s.Notifier == nil {
		return 0, nil
	}
	all, err := s.Store.ListBots(ctx, ownerID)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, b := range all {
		if b.DesiredState != domain.DesiredRunning {
			continue
		}
		nb, changed, err := s.Store.SetDesired(ctx, b.ID, domain.DesiredStopped, false, s.now())
		if err != nil {
			return n, err
		}
		if changed {
			n++
			s.Bus.Publish(events.Status{BotID: b.ID, DesiredState: nb.DesiredState, ObservedState: nb.ObservedState,
				Generation: nb.Generation, ObservedGeneration: nb.ObservedGeneration})
			s.Notifier.Notify(b.ID)
		}
	}
	return n, nil
}

// checkUserBudget refuses adding bots or memory beyond a user's budget.
// Administrators are exempt: they operate the host.
func (s *BotService) checkUserBudget(ctx context.Context, owner domain.User, addBots int, addMemory int64) error {
	if owner.IsAdmin() || (s.Limits.MaxBotsPerUser == 0 && s.Limits.UserMemoryBytes == 0) {
		return nil
	}
	n, mem, err := s.Store.OwnerUsage(ctx, owner.ID)
	if err != nil {
		return err
	}
	if s.Limits.MaxBotsPerUser > 0 && addBots > 0 && n+addBots > s.Limits.MaxBotsPerUser {
		return domain.Invalid(fmt.Sprintf("your account can have at most %d bots; delete one first", s.Limits.MaxBotsPerUser))
	}
	if s.Limits.UserMemoryBytes > 0 && addMemory > 0 && mem+addMemory > s.Limits.UserMemoryBytes {
		return domain.Invalid(fmt.Sprintf("your bots may use at most %d MiB of memory in total; %d MiB is already assigned",
			s.Limits.UserMemoryBytes>>20, mem>>20))
	}
	return nil
}

// Capacity is a user's usage against the configured budgets.
type Capacity struct {
	Bots, MaxBots            int
	Memory, MaxMemory        int64
	NodeRunning              int   // admins only
	NodeReserved, NodeBudget int64 // admins only
	Exempt                   bool  // administrators are not limited per user
}

// Capacity reports the actor's usage and, for administrators, the node's.
func (s *BotService) Capacity(ctx context.Context, actor domain.User) (Capacity, error) {
	n, mem, err := s.Store.OwnerUsage(ctx, actor.ID)
	if err != nil {
		return Capacity{}, err
	}
	c := Capacity{Bots: n, MaxBots: s.Limits.MaxBotsPerUser, Memory: mem, MaxMemory: s.Limits.UserMemoryBytes, Exempt: actor.IsAdmin()}
	if actor.IsAdmin() {
		c.NodeRunning, c.NodeReserved, err = s.Store.NodeReserved(ctx, s.LocalNode)
		c.NodeBudget = s.Limits.NodeMemoryBytes
	}
	return c, err
}
