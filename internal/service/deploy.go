package service

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"botpanel/internal/domain"
	"botpanel/internal/events"
	"botpanel/internal/filesystem"
	"botpanel/internal/github"
	"botpanel/internal/secrets"
)

// DeployService deploys a bot's source from GitHub into its workspace, on
// demand and on push (webhook). Repository code is only ever downloaded as a
// tarball and unpacked with the contained extractor; nothing from a repository
// executes on the host.
type DeployService struct {
	Bots      *BotService
	OAuth     *OAuthService
	Files     *filesystem.Manager
	GH        *github.Client
	Keys      *secrets.Keyring
	Alerts    *AlertService // optional
	PublicURL string
	Limits    filesystem.BackupLimits
	Log       *slog.Logger
	Now       func() time.Time
	Ops       *Operations // optional: deployment history
	// MinFreeDisk is the free space a deployment needs to start.
	MinFreeDisk int64

	baseCtx context.Context
	sem     chan struct{}
	mu      sync.Mutex
	jobs    map[string]*jobState
	seen    map[string]time.Time // recent webhook delivery ids
	wg      sync.WaitGroup
}

// Start sets the context deploy jobs run under (they outlive HTTP requests).
func (d *DeployService) Start(ctx context.Context) {
	d.baseCtx = ctx
	d.sem = make(chan struct{}, 2)
}

// Wait blocks until running jobs finish.
func (d *DeployService) Wait() { d.wg.Wait() }

func (d *DeployService) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

func (d *DeployService) store() Store { return d.Bots.Store }

func (d *DeployService) ctx() context.Context {
	if d.baseCtx != nil {
		return d.baseCtx
	}
	return context.Background()
}

// WebhookURL is where GitHub delivers push events.
func (d *DeployService) WebhookURL() string { return d.publicURL() + "/api/v1/webhooks/github" }

// publicURL follows runtime changes made on the settings page.
func (d *DeployService) publicURL() string {
	if u := d.OAuth.CurrentPublicURL(); u != "" {
		return u
	}
	return d.PublicURL
}

// ready refuses GitHub work while GitHub sign-in is not configured.
func (d *DeployService) ready() error {
	if !d.OAuth.Enabled("github") {
		return domain.Invalid("GitHub sign-in is not configured on this panel; an administrator can set it up under Administration → Panel settings")
	}
	return nil
}

func secretNS(botID string) string { return secrets.GitHubNS(botID) }

// ---- listing ----

func (d *DeployService) token(ctx context.Context, userID string) (string, error) {
	t, err := d.OAuth.GitHubToken(ctx, userID)
	if errors.Is(err, domain.ErrNotFound) {
		return "", domain.Invalid("connect your GitHub account in Settings first")
	}
	return t, err
}

func ghError(err error) error {
	switch {
	case errors.Is(err, github.ErrNotFound), errors.Is(err, github.ErrUnauthorized), errors.Is(err, github.ErrInvalid),
		errors.Is(err, github.ErrConflict):
		return domain.Invalid(err.Error())
	}
	return err
}

// Repos lists the repositories visible to the user's GitHub token.
func (d *DeployService) Repos(ctx context.Context, actor domain.User) ([]github.Repo, error) {
	if err := d.ready(); err != nil {
		return nil, err
	}
	tok, err := d.token(ctx, actor.ID)
	if err != nil {
		return nil, err
	}
	r, err := d.GH.Repos(ctx, tok)
	return r, ghError(err)
}

// Branches lists a repository's branches.
func (d *DeployService) Branches(ctx context.Context, actor domain.User, fullName string) ([]string, error) {
	if err := d.ready(); err != nil {
		return nil, err
	}
	tok, err := d.token(ctx, actor.ID)
	if err != nil {
		return nil, err
	}
	b, err := d.GH.Branches(ctx, tok, fullName)
	return b, ghError(err)
}

// ---- configuration ----

// ConfigureInput selects what to deploy.
type ConfigureInput struct {
	FullName   string
	Branch     string
	RootDir    string
	AutoDeploy bool
}

// RepoView is a bot's GitHub source as shown to users. Secret is set only in
// the response that created it, and only when GitHub could not be given the
// webhook automatically.
type RepoView struct {
	FullName       string
	Branch         string
	RootDir        string
	Private        bool
	AutoDeploy     bool
	HookCreated    bool
	WebhookURL     string
	Secret         string
	LastSHA        string
	LastDeployedMS int64
	LastError      string
	Deploying      bool
}

func cleanRoot(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "." || s == "/" {
		return "", nil
	}
	if len(s) > 200 || strings.ContainsAny(s, "\\\x00") {
		return "", domain.Invalid("invalid root directory")
	}
	c := strings.Trim(path.Clean("/"+s), "/")
	if c == "" || !filepath.IsLocal(c) {
		return "", domain.Invalid("invalid root directory")
	}
	return c, nil
}

func (d *DeployService) view(r domain.GitHubRepo) RepoView {
	v := RepoView{FullName: r.FullName, Branch: r.Branch, RootDir: r.RootDir, Private: r.Private, AutoDeploy: r.AutoDeploy,
		HookCreated: r.HookID != nil, WebhookURL: d.WebhookURL(), Deploying: d.isRunning(r.BotID)}
	if r.LastSHA != nil {
		v.LastSHA = *r.LastSHA
	}
	if r.LastDeployedMS != nil {
		v.LastDeployedMS = *r.LastDeployedMS
	}
	if r.LastError != nil {
		v.LastError = *r.LastError
	}
	return v
}

func (d *DeployService) isRunning(botID string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	j := d.jobs[botID]
	return j != nil && j.running
}

// Get returns a bot's GitHub source (ErrNotFound when none).
func (d *DeployService) Get(ctx context.Context, actor domain.User, botID string) (RepoView, error) {
	if _, err := d.Bots.Authorize(ctx, actor, botID, domain.PermEditFiles); err != nil {
		return RepoView{}, err
	}
	r, err := d.store().GetGitHubRepo(ctx, botID)
	if err != nil {
		return RepoView{}, err
	}
	return d.view(r), nil
}

// Configure links (or re-links) a bot to a repository, using the acting user's
// GitHub token, and creates or removes the push webhook according to AutoDeploy.
func (d *DeployService) Configure(ctx context.Context, actor domain.User, botID string, in ConfigureInput) (RepoView, error) {
	if err := d.ready(); err != nil {
		return RepoView{}, err
	}
	if _, err := d.Bots.Authorize(ctx, actor, botID, domain.PermFullAdmin); err != nil {
		return RepoView{}, err
	}
	return d.configure(ctx, actor, botID, in)
}

func (d *DeployService) configure(ctx context.Context, actor domain.User, botID string, in ConfigureInput) (RepoView, error) {
	if !github.FullNameRe.MatchString(in.FullName) {
		return RepoView{}, domain.Invalid("repository must look like owner/name")
	}
	if !github.ValidBranch(in.Branch) {
		return RepoView{}, domain.Invalid("invalid branch name")
	}
	root, err := cleanRoot(in.RootDir)
	if err != nil {
		return RepoView{}, err
	}
	if in.AutoDeploy && d.publicURL() == "" {
		return RepoView{}, domain.Invalid("auto-deploy needs BOTPANEL_PUBLIC_URL to be configured so GitHub can reach the panel")
	}
	tok, err := d.token(ctx, actor.ID)
	if err != nil {
		return RepoView{}, err
	}
	repo, err := d.GH.GetRepo(ctx, tok, in.FullName)
	if err != nil {
		return RepoView{}, ghError(err)
	}
	if _, err := d.GH.BranchSHA(ctx, tok, in.FullName, in.Branch); err != nil {
		return RepoView{}, ghError(err)
	}

	now := d.now().UnixMilli()
	cur, err := d.store().GetGitHubRepo(ctx, botID)
	exists := err == nil
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return RepoView{}, err
	}
	row := domain.GitHubRepo{BotID: botID, TokenUserID: actor.ID, FullName: repo.FullName, Branch: in.Branch, RootDir: root,
		Private: repo.Private, AutoDeploy: in.AutoDeploy, CreatedAtMS: now, UpdatedAtMS: now}
	var secretPlain string
	if exists {
		row.SecretCipher, row.SecretNonce, row.SecretKeyID = cur.SecretCipher, cur.SecretNonce, cur.SecretKeyID
		row.LastSHA, row.LastDeployedMS, row.LastError, row.CreatedAtMS = cur.LastSHA, cur.LastDeployedMS, cur.LastError, cur.CreatedAtMS
		row.HookID = cur.HookID
		// A different repository (or token owner) invalidates the old hook.
		if cur.HookID != nil && (!strings.EqualFold(cur.FullName, repo.FullName) || !in.AutoDeploy) {
			if ot, err := d.OAuth.GitHubToken(ctx, cur.TokenUserID); err == nil {
				if err := d.GH.DeleteHook(ctx, ot, cur.FullName, *cur.HookID); err != nil {
					d.warn("remove old webhook", err)
				}
			}
			row.HookID = nil
		}
		if !strings.EqualFold(cur.FullName, repo.FullName) {
			row.LastSHA, row.LastDeployedMS, row.LastError = nil, nil, nil
		}
	} else {
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			return RepoView{}, err
		}
		secretPlain = hex.EncodeToString(raw)
		sl, err := d.Keys.Seal(secretNS(botID), secrets.GitHubSecretName, []byte(secretPlain))
		if err != nil {
			return RepoView{}, err
		}
		row.SecretCipher, row.SecretNonce, row.SecretKeyID = sl.Ciphertext, sl.Nonce, sl.KeyID
	}
	// The row (with its secret) must exist before GitHub sends its first ping.
	if err := d.store().UpsertGitHubRepo(ctx, row); err != nil {
		return RepoView{}, err
	}

	if in.AutoDeploy && row.HookID == nil {
		secret, err := d.secret(row)
		if err != nil {
			return RepoView{}, err
		}
		id, err := d.GH.CreateHook(ctx, tok, repo.FullName, d.WebhookURL(), secret)
		if err != nil {
			// Not fatal: the user can add the webhook by hand with the values shown.
			d.warn("create webhook", err)
			v := d.view(row)
			v.Secret = secret
			return v, nil
		}
		row.HookID = &id
		row.UpdatedAtMS = d.now().UnixMilli()
		if err := d.store().UpsertGitHubRepo(ctx, row); err != nil {
			return RepoView{}, err
		}
	}
	v := d.view(row)
	if secretPlain != "" && in.AutoDeploy && row.HookID == nil {
		v.Secret = secretPlain
	}
	return v, nil
}

func (d *DeployService) secret(r domain.GitHubRepo) (string, error) {
	pt, err := d.Keys.Open(secretNS(r.BotID), secrets.GitHubSecretName, secrets.Sealed{Ciphertext: r.SecretCipher, Nonce: r.SecretNonce, KeyID: r.SecretKeyID})
	return string(pt), err
}

// Unlink removes the repository link and its webhook.
func (d *DeployService) Unlink(ctx context.Context, actor domain.User, botID string) error {
	if _, err := d.Bots.Authorize(ctx, actor, botID, domain.PermFullAdmin); err != nil {
		return err
	}
	// Unlinking wins over a deployment in progress.
	pctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	_ = d.Bots.Coord.Preempt(pctx, botID)
	cancel()
	d.removeHook(ctx, botID)
	return d.store().DeleteGitHubRepo(ctx, botID)
}

// removeHook deletes the GitHub webhook of a bot, best effort.
func (d *DeployService) removeHook(ctx context.Context, botID string) {
	r, err := d.store().GetGitHubRepo(ctx, botID)
	if err != nil || r.HookID == nil {
		return
	}
	if tok, err := d.OAuth.GitHubToken(ctx, r.TokenUserID); err == nil {
		if err := d.GH.DeleteHook(ctx, tok, r.FullName, *r.HookID); err != nil {
			d.warn("remove webhook", err)
		}
	}
}

// BeforeBotDelete is wired to BotService.BeforeDelete.
func (d *DeployService) BeforeBotDelete(ctx context.Context, botID string) { d.removeHook(ctx, botID) }

// CreateFromGitHub creates a bot and links it to a repository; the first
// deploy runs in the background. If linking fails the bot is deleted again.
func (d *DeployService) CreateFromGitHub(ctx context.Context, actor domain.User, in CreateBotInput, gh ConfigureInput) (domain.Bot, RepoView, error) {
	if err := d.ready(); err != nil {
		return domain.Bot{}, RepoView{}, err
	}
	in.SourceType, in.TemplateID = "github", nil
	b, err := d.Bots.Create(ctx, actor, in)
	if err != nil {
		return domain.Bot{}, RepoView{}, err
	}
	v, err := d.configure(ctx, actor, b.ID, gh)
	if err != nil {
		_ = d.Bots.Delete(ctx, actor, b.ID)
		return domain.Bot{}, RepoView{}, err
	}
	d.enqueue(b.ID, DeployRequest{Trigger: "initial", ActorID: &actor.ID})
	return b, v, nil
}

// ---- deploying ----

// DeployRequest selects what to deploy. SHA empty = the branch head.
type DeployRequest struct {
	Trigger string // manual | push | initial | api
	ActorID *string
	SHA     string // a specific commit (redeploy/rollback)
}

// Deploy starts a deployment of the configured branch (or of req.SHA) in the
// background.
func (d *DeployService) Deploy(ctx context.Context, actor domain.User, botID string, sha string) error {
	return d.DeployAs(ctx, actor, botID, sha, "manual")
}

// DeployAs is Deploy with an explicit trigger (manual, schedule, api).
func (d *DeployService) DeployAs(ctx context.Context, actor domain.User, botID, sha, trigger string) error {
	if err := d.ready(); err != nil {
		return err
	}
	if _, err := d.Bots.Authorize(ctx, actor, botID, domain.PermEditFiles); err != nil {
		return err
	}
	if _, err := d.store().GetGitHubRepo(ctx, botID); err != nil {
		return err
	}
	if sha != "" && !github.ValidSHA(sha) {
		return domain.Invalid("choose a full commit id")
	}
	if err := diskPreflight(d.Files, d.MinFreeDisk); err != nil {
		return err
	}
	if err := d.Bots.FilesBlocked(botID); err != nil {
		if what, _ := d.Bots.Coord.Active(botID); what != "A deployment" {
			return err
		}
	}
	id := actor.ID
	d.enqueue(botID, DeployRequest{Trigger: trigger, ActorID: &id, SHA: sha})
	return nil
}

// Preview compares the deployed commit with the branch head, so users can
// see what a deployment would change before starting it.
func (d *DeployService) Preview(ctx context.Context, actor domain.User, botID string) (github.Comparison, error) {
	if err := d.ready(); err != nil {
		return github.Comparison{}, err
	}
	if _, err := d.Bots.Authorize(ctx, actor, botID, domain.PermEditFiles); err != nil {
		return github.Comparison{}, err
	}
	repo, err := d.store().GetGitHubRepo(ctx, botID)
	if err != nil {
		return github.Comparison{}, err
	}
	tok, err := d.OAuth.GitHubToken(ctx, repo.TokenUserID)
	if err != nil && !(errors.Is(err, domain.ErrNotFound) && !repo.Private) {
		return github.Comparison{}, domain.Invalid("the GitHub account used for this deployment is no longer connected")
	}
	head, err := d.GH.BranchSHA(ctx, tok, repo.FullName, repo.Branch)
	if err != nil {
		return github.Comparison{}, ghError(err)
	}
	if repo.LastSHA == nil || *repo.LastSHA == head {
		return github.Comparison{HeadSHA: head, Commits: []github.Commit{}, Files: []github.ChangedFile{}}, nil
	}
	cmp, err := d.GH.Compare(ctx, tok, repo.FullName, *repo.LastSHA, head)
	return cmp, ghError(err)
}

type jobState struct {
	running bool
	next    *DeployRequest // the newest request that arrived while running
}

// enqueue runs a deployment, coalescing requests that arrive while one is
// running into a single follow-up that uses the newest request.
func (d *DeployService) enqueue(botID string, req DeployRequest) {
	d.mu.Lock()
	if d.jobs == nil {
		d.jobs = map[string]*jobState{}
	}
	j := d.jobs[botID]
	if j == nil {
		j = &jobState{}
		d.jobs[botID] = j
	}
	if j.running {
		r := req
		j.next = &r
		d.mu.Unlock()
		return
	}
	j.running = true
	d.mu.Unlock()

	if d.sem == nil {
		d.sem = make(chan struct{}, 2)
	}
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		for {
			d.sem <- struct{}{}
			d.run(d.ctx(), botID, req)
			<-d.sem
			d.mu.Lock()
			if j.next == nil || d.ctx().Err() != nil {
				j.running, j.next = false, nil
				delete(d.jobs, botID)
				d.mu.Unlock()
				return
			}
			req, j.next = *j.next, nil
			d.mu.Unlock()
		}
	}()
}

const deployTimeout = 15 * time.Minute

// errSuperseded ends a deployment because newer intent (Stop, Delete, Unlink
// or a changed repository link) replaced it.
type errSuperseded struct{ why string }

func (e errSuperseded) Error() string { return e.why }

func (d *DeployService) run(base context.Context, botID string, req DeployRequest) {
	ctx, cancel := context.WithTimeout(base, deployTimeout)
	defer cancel()
	repo, err := d.store().GetGitHubRepo(ctx, botID)
	if err != nil {
		return
	}
	bot, err := d.store().GetBot(ctx, botID)
	if err != nil || bot.DesiredState == domain.DesiredDeleted {
		return
	}
	kind := domain.OpDeploy
	if req.SHA != "" {
		kind = domain.OpRollback
	}
	label := repo.FullName + "@" + repo.Branch
	op := d.Ops.Begin(ctx, OpStart{BotID: botID, Kind: kind, Trigger: req.Trigger, ActorID: req.ActorID, SourceLabel: &label})
	fctx, fcancel := context.WithTimeout(context.WithoutCancel(base), 10*time.Second)
	defer fcancel()

	claim, cctx, err := d.Bots.Coord.Claim(ctx, botID, "A deployment", true)
	if err != nil {
		d.Ops.Finish(fctx, op, domain.OpFailed, "busy", err.Error(), nil)
		return
	}
	defer claim.Release()

	sha, n, err := d.deployOnce(cctx, repo, req.SHA, op)
	if err != nil {
		var sup errSuperseded
		if errors.As(err, &sup) || (cctx.Err() != nil && ctx.Err() == nil) {
			why := sup.why
			if why == "" {
				why = "cancelled by a newer action on this bot"
			}
			d.Ops.Finish(fctx, op, domain.OpCancelled, "superseded", why, nil)
			return
		}
		msg := deployMessage(err)
		d.warn("deploy failed", err)
		_ = d.store().RecordDeploy(fctx, botID, nil, &msg, d.now().UnixMilli())
		d.Ops.Finish(fctx, op, domain.OpFailed, "deploy_failed", msg, nil)
		if d.Alerts.Wants(fctx, botID, "deploy") {
			d.Alerts.Send(fctx, bot.OwnerID, "❌ Deploy failed: "+bot.Name, label+": "+msg)
		}
		return
	}
	claim.Release() // deployOnce recorded the commit together with the files
	// A running bot restarts on the new code (this also re-runs its build
	// step). The condition is evaluated atomically against CURRENT intent, so
	// a Stop that arrived during the download is never undone.
	restarted := false
	if d.Bots.Notifier != nil {
		d.Ops.Stage(fctx, op, "Restarting")
		if nb, changed, err := d.store().RestartIfRunning(fctx, botID, d.now().UnixMilli()); err == nil && changed {
			restarted = true
			d.Bots.Bus.Publish(events.Status{BotID: botID, DesiredState: nb.DesiredState, ObservedState: nb.ObservedState,
				Generation: nb.Generation, ObservedGeneration: nb.ObservedGeneration})
			d.Bots.Notifier.Notify(botID)
		}
	}
	msg := fmt.Sprintf("Deployed %s (%d files)", sha[:7], n)
	if restarted {
		msg += "; the bot is restarting on the new code"
	}
	d.Ops.Finish(fctx, op, domain.OpSucceeded, "", msg, map[string]any{"files": n, "sha": sha, "restarted": restarted})
	if d.Alerts.Wants(fctx, botID, "deploy") {
		d.Alerts.Send(fctx, bot.OwnerID, "✅ Deployed "+bot.Name,
			fmt.Sprintf("%s · %s · %d files (%s)", label, sha[:7], n, req.Trigger))
	}
}

const maxTarball = 1 << 30

// deployOnce downloads and applies one commit. Before the workspace changes,
// it re-reads the bot and its repository link: a deleted bot, a removed or
// changed link, or a cancelled claim stops the deployment without touching
// any file.
func (d *DeployService) deployOnce(ctx context.Context, repo domain.GitHubRepo, want string, op string) (string, int, error) {
	d.Ops.Stage(ctx, op, "Resolving the commit")
	tok, err := d.OAuth.GitHubToken(ctx, repo.TokenUserID)
	if err != nil {
		if !repo.Private && errors.Is(err, domain.ErrNotFound) {
			tok = "" // public repository: works without a token
		} else {
			return "", 0, domain.Invalid("the GitHub account used for this deployment is no longer connected")
		}
	}
	sha := want
	if sha == "" {
		if sha, err = d.GH.BranchSHA(ctx, tok, repo.FullName, repo.Branch); err != nil {
			return "", 0, ghError(err)
		}
	}
	d.Ops.Source(ctx, op, sha, "")
	d.Ops.Stage(ctx, op, "Downloading "+sha[:7])
	body, err := d.GH.Tarball(ctx, tok, repo.FullName, sha)
	if err != nil {
		return "", 0, ghError(err)
	}
	defer body.Close()
	w, err := d.Files.Open(repo.BotID)
	if err != nil {
		return "", 0, err
	}
	defer w.Close()
	lim := d.Limits
	if lim.MaxBytes == 0 {
		lim = filesystem.DefaultBackupLimits
	}
	d.Ops.Stage(ctx, op, "Unpacking and replacing files")
	n, commit, err := w.DeployTarGzCommit(io.LimitReader(body, maxTarball), repo.RootDir, lim, func() error {
		// Runs after the archive is fully validated in staging and before
		// the first workspace entry is replaced.
		if ctx.Err() != nil {
			return errSuperseded{"cancelled by a newer action on this bot"}
		}
		cur, err := d.store().GetBot(ctx, repo.BotID)
		if err != nil || cur.DesiredState == domain.DesiredDeleted {
			return errSuperseded{"the bot was deleted"}
		}
		now, err := d.store().GetGitHubRepo(ctx, repo.BotID)
		if err != nil {
			return errSuperseded{"the repository was unlinked"}
		}
		if !strings.EqualFold(now.FullName, repo.FullName) || now.Branch != repo.Branch || now.RootDir != repo.RootDir {
			return errSuperseded{"the repository link changed; deploy again to use the new settings"}
		}
		return nil
	})
	if err != nil {
		return sha, n, err
	}
	// Record the new commit before the previous files are dropped: a crash
	// in between rolls the files back to match the recorded commit.
	fctx, cancel := bg(ctx)
	defer cancel()
	if err := d.store().RecordDeploy(fctx, repo.BotID, &sha, nil, d.now().UnixMilli()); err != nil {
		if rerr := commit.Rollback(); rerr != nil {
			d.warn("roll back files after a failed record", rerr)
		}
		return sha, 0, err
	}
	if err := commit.Finish(); err != nil {
		d.warn("clean up previous files", err)
	}
	return sha, n, nil
}

func deployMessage(err error) string {
	var ae *filesystem.ErrArchive
	var ve *domain.ValidationError
	switch {
	case errors.As(err, &ae):
		return ae.Msg
	case errors.As(err, &ve):
		return ve.Msg
	case errors.Is(err, context.DeadlineExceeded):
		return "the deployment timed out"
	}
	return "the deployment failed; see the panel logs"
}

func (d *DeployService) warn(msg string, err error) {
	if d.Log != nil {
		d.Log.Warn("deploy: "+msg, "err", err)
	}
}

// ---- webhook ----

// ErrBadSignature is returned for every webhook that cannot be authenticated,
// whether the repository is unknown or the signature is wrong, so callers learn
// nothing about which repositories are configured.
var ErrBadSignature = errors.New("invalid webhook signature")

func validSignature(secret string, body []byte, header string) bool {
	hexsig, ok := strings.CutPrefix(header, "sha256=")
	if !ok {
		return false
	}
	want, err := hex.DecodeString(hexsig)
	if err != nil {
		return false
	}
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(body)
	return hmac.Equal(m.Sum(nil), want)
}

// HandleWebhook authenticates and processes a GitHub delivery. It returns a
// short result ("pong", "queued", "ignored") or ErrBadSignature.
func (d *DeployService) HandleWebhook(ctx context.Context, event, delivery, signature string, body []byte) (string, error) {
	var p struct {
		Ref        string `json:"ref"`
		After      string `json:"after"`
		Deleted    bool   `json:"deleted"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	}
	if json.Unmarshal(body, &p) != nil || !github.FullNameRe.MatchString(p.Repository.FullName) {
		return "", ErrBadSignature
	}
	cands, err := d.store().ListAutoDeployRepos(ctx, p.Repository.FullName)
	if err != nil {
		return "", err
	}
	var verified []domain.GitHubRepo
	for _, c := range cands {
		if sec, err := d.secret(c); err == nil && validSignature(sec, body, signature) {
			verified = append(verified, c)
		}
	}
	if len(verified) == 0 {
		return "", ErrBadSignature
	}
	switch event {
	case "ping":
		return "pong", nil
	case "push":
	default:
		return "ignored", nil
	}
	if delivery != "" && d.duplicate(delivery) {
		return "ignored", nil
	}
	branch, ok := strings.CutPrefix(p.Ref, "refs/heads/")
	if !ok || p.Deleted {
		return "ignored", nil
	}
	queued := false
	for _, c := range verified {
		// A commit the panel pushed from this bot's own files is already
		// deployed; redeploying it would only restart the bot.
		if c.Branch == branch && (c.LastSHA == nil || *c.LastSHA != p.After) {
			d.enqueue(c.BotID, DeployRequest{Trigger: "push"})
			queued = true
		}
	}
	if !queued {
		return "ignored", nil
	}
	return "queued", nil
}

// duplicate remembers recent delivery ids so a GitHub redelivery does not
// deploy twice.
func (d *DeployService) duplicate(id string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.seen == nil {
		d.seen = map[string]time.Time{}
	}
	now := d.now()
	if len(d.seen) > 2000 {
		for k, t := range d.seen {
			if now.Sub(t) > time.Hour {
				delete(d.seen, k)
			}
		}
	}
	if _, ok := d.seen[id]; ok {
		return true
	}
	d.seen[id] = now
	return false
}
