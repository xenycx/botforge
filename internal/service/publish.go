package service

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"sync"
	"time"

	"botpanel/internal/domain"
	"botpanel/internal/filesystem"
	"botpanel/internal/github"
)

// Pushing a bot's files to GitHub. The files are read from the workspace
// and sent through the Git Data API with the acting user's own token (never
// the token of whoever linked the repository): pushing is write access to
// their GitHub account. Nothing from the workspace is executed.

// PushPlan previews what a push would send.
func (d *DeployService) PushPlan(ctx context.Context, actor domain.User, botID string) (filesystem.PushSet, error) {
	if _, err := d.Bots.Authorize(ctx, actor, botID, domain.PermEditFiles); err != nil {
		return filesystem.PushSet{}, err
	}
	w, err := d.Files.Open(botID)
	if err != nil {
		return filesystem.PushSet{}, err
	}
	defer w.Close()
	set, err := w.PushSet(filesystem.DefaultPushLimits)
	var ae *filesystem.ErrArchive
	if errors.As(err, &ae) {
		return filesystem.PushSet{}, domain.Invalid(ae.Msg)
	}
	return set, err
}

// Owners lists where the actor may create repositories.
func (d *DeployService) Owners(ctx context.Context, actor domain.User) ([]github.Owner, error) {
	if err := d.ready(); err != nil {
		return nil, err
	}
	tok, err := d.OAuth.GitHubPushToken(ctx, actor.ID)
	if err != nil {
		return nil, err
	}
	o, err := d.GH.Owners(ctx, tok)
	return o, ghError(err)
}

// PublishInput describes a new repository for a bot's files.
type PublishInput struct {
	Owner       string // "" or the user's login: their account; otherwise an organization
	Name        string
	Description string
	Private     bool
	AutoDeploy  bool // link with a push webhook afterwards
}

// Publish creates a GitHub repository, then (in the background, recorded as
// a "publish" operation) pushes the bot's files to it and links the bot to
// it, so later deployments come from the repository.
func (d *DeployService) Publish(ctx context.Context, actor domain.User, botID string, in PublishInput) (github.Repo, error) {
	if err := d.ready(); err != nil {
		return github.Repo{}, err
	}
	b, err := d.Bots.Authorize(ctx, actor, botID, domain.PermFullAdmin)
	if err != nil {
		return github.Repo{}, err
	}
	if cur, err := d.store().GetGitHubRepo(ctx, botID); err == nil {
		return github.Repo{}, domain.Invalid("this bot is already linked to " + cur.FullName + "; push to it instead")
	} else if !errors.Is(err, domain.ErrNotFound) {
		return github.Repo{}, err
	}
	name := strings.TrimSpace(in.Name)
	if !github.ValidRepoName(name) {
		return github.Repo{}, domain.Invalid("repository names use letters, digits, '.', '-' and '_' (up to 100)")
	}
	if in.AutoDeploy && d.publicURL() == "" {
		return github.Repo{}, domain.Invalid("auto-deploy needs BOTPANEL_PUBLIC_URL to be configured so GitHub can reach the panel")
	}
	tok, err := d.OAuth.GitHubPushToken(ctx, actor.ID)
	if err != nil {
		return github.Repo{}, err
	}
	// Refuse before creating anything if the files cannot be pushed.
	set, err := d.PushPlan(ctx, actor, botID)
	if err != nil {
		return github.Repo{}, err
	}
	if len(set.Files) == 0 {
		return github.Repo{}, domain.Invalid("the bot has no files to publish")
	}
	org := strings.TrimSpace(in.Owner)
	if org != "" {
		login, err := d.GH.Viewer(ctx, tok)
		if err != nil {
			return github.Repo{}, ghError(err)
		}
		if strings.EqualFold(org, login) {
			org = ""
		}
	}
	desc := strings.TrimSpace(in.Description)
	if desc == "" {
		desc = b.Name + " (published from BotForge)"
	}
	repo, err := d.GH.CreateRepo(ctx, tok, org, name, desc, in.Private)
	if errors.Is(err, github.ErrConflict) {
		return github.Repo{}, domain.Invalid("a repository with that name already exists there")
	}
	if err != nil {
		return github.Repo{}, ghError(err)
	}
	d.startPush(botID, pushJob{actor: actor, full: repo.FullName, branch: repo.DefaultBranch, token: tok,
		message: "Initial commit from BotForge", publish: &PublishInput{AutoDeploy: in.AutoDeploy}})
	return repo, nil
}

// Push commits the bot's current files to its linked repository and branch
// (inside the linked root directory, leaving the rest of the repository
// alone). The branch must not move meanwhile: nothing is ever force-pushed.
func (d *DeployService) Push(ctx context.Context, actor domain.User, botID, message string) error {
	if err := d.ready(); err != nil {
		return err
	}
	if _, err := d.Bots.Authorize(ctx, actor, botID, domain.PermEditFiles); err != nil {
		return err
	}
	link, err := d.store().GetGitHubRepo(ctx, botID)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.Invalid("link a repository or publish the bot to a new one first")
	}
	if err != nil {
		return err
	}
	tok, err := d.OAuth.GitHubPushToken(ctx, actor.ID)
	if err != nil {
		return err
	}
	message = strings.TrimSpace(message)
	if message == "" {
		message = "Update from BotForge"
	}
	if len(message) > 2000 {
		return domain.Invalid("the commit message is too long")
	}
	if what, busy := d.Bots.Coord.Active(botID); busy {
		return domain.Invalid(what + " is in progress; push when it has finished")
	}
	d.startPush(botID, pushJob{actor: actor, full: link.FullName, branch: link.Branch, root: link.RootDir, token: tok, message: message})
	return nil
}

type pushJob struct {
	actor   domain.User
	full    string
	branch  string
	root    string
	token   string
	message string
	publish *PublishInput // set for a new repository: link the bot afterwards
}

func (d *DeployService) startPush(botID string, j pushJob) {
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		if d.sem == nil {
			d.sem = make(chan struct{}, 2)
		}
		d.sem <- struct{}{}
		defer func() { <-d.sem }()
		d.runPush(botID, j)
	}()
}

func (d *DeployService) runPush(botID string, j pushJob) {
	base := d.ctx()
	ctx, cancel := context.WithTimeout(base, 20*time.Minute)
	defer cancel()
	label := j.full + "@" + j.branch
	op := d.Ops.Begin(ctx, OpStart{BotID: botID, Kind: domain.OpPublish, Trigger: "manual", ActorID: &j.actor.ID, SourceLabel: &label})
	fctx, fcancel := context.WithTimeout(context.WithoutCancel(base), 10*time.Second)
	defer fcancel()
	fail := func(err error) {
		msg := deployMessage(err)
		if msg == "the deployment failed; see the panel logs" {
			msg = "the push failed; see the panel logs"
		}
		var ve *domain.ValidationError
		if !errors.As(err, &ve) && !errors.Is(err, context.DeadlineExceeded) {
			d.warn("push failed", err)
		}
		d.Ops.Finish(fctx, op, domain.OpFailed, "publish_failed", msg, nil)
	}
	claim, cctx, err := d.Bots.Coord.Claim(ctx, botID, "A push to GitHub", false)
	if err != nil {
		d.Ops.Finish(fctx, op, domain.OpFailed, "busy", err.Error(), nil)
		return
	}
	defer claim.Release()
	res, err := d.pushOnce(cctx, botID, j, op)
	if err != nil {
		fail(err)
		return
	}
	if j.publish != nil {
		d.Ops.Stage(fctx, op, "Linking the repository")
		if _, err := d.configure(cctx, j.actor, botID, ConfigureInput{FullName: j.full, Branch: j.branch, AutoDeploy: j.publish.AutoDeploy}); err != nil {
			fail(domain.Invalid("the files were pushed, but linking the repository failed: " + deployMessage(err)))
			return
		}
	}
	// The workspace is exactly this commit: record it as deployed.
	if res.commit != "" {
		_ = d.store().RecordDeploy(fctx, botID, &res.commit, nil, d.now().UnixMilli())
	}
	msg := res.summary(j.full, j.branch)
	d.Ops.Finish(fctx, op, domain.OpSucceeded, "", msg, map[string]any{"sha": res.commit, "files": res.files,
		"uploaded": res.uploaded, "url": "https://github.com/" + j.full})
}

type pushResult struct {
	commit   string // "" when GitHub already had exactly these files
	files    int
	uploaded int
}

func (r pushResult) summary(full, branch string) string {
	if r.commit == "" {
		return fmt.Sprintf("%s@%s already has these %d files; nothing to push", full, branch, r.files)
	}
	return fmt.Sprintf("Pushed %d files (%d changed) to %s@%s as %s", r.files, r.uploaded, full, branch, r.commit[:7])
}

func (d *DeployService) pushOnce(ctx context.Context, botID string, j pushJob, op string) (pushResult, error) {
	d.Ops.Stage(ctx, op, "Reading the branch")
	// A repository created a moment ago may need a few seconds before its
	// first commit is visible.
	var head string
	var err error
	for attempt := 0; attempt < 8; attempt++ {
		head, err = d.GH.BranchHead(ctx, j.token, j.full, j.branch)
		if !errors.Is(err, github.ErrNotFound) || j.publish == nil {
			break
		}
		select {
		case <-ctx.Done():
			return pushResult{}, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	create := false
	switch {
	case errors.Is(err, github.ErrNotFound) && j.publish == nil:
		create = true // a new branch in an existing repository
	case err != nil:
		return pushResult{}, ghError(err)
	}
	existing := map[string]github.TreeEntry{}
	var headTree string
	if head != "" {
		tree, files, err := d.GH.CommitFiles(ctx, j.token, j.full, head)
		if err != nil {
			return pushResult{}, ghError(err)
		}
		headTree = tree
		for _, f := range files {
			existing[f.Path] = f
		}
	}

	d.Ops.Stage(ctx, op, "Collecting files")
	w, err := d.Files.Open(botID)
	if err != nil {
		return pushResult{}, err
	}
	defer w.Close()
	set, err := w.PushSet(filesystem.DefaultPushLimits)
	if err != nil {
		return pushResult{}, err
	}
	if len(set.Files) == 0 {
		return pushResult{}, domain.Invalid("the bot has no files to push")
	}

	// Files outside the linked root directory stay as they are.
	prefix := ""
	if j.root != "" {
		prefix = j.root + "/"
	}
	var entries []github.TreeEntry
	if prefix != "" {
		for p, e := range existing {
			if !strings.HasPrefix(p, prefix) {
				entries = append(entries, e)
			}
		}
	}

	d.Ops.Stage(ctx, op, fmt.Sprintf("Uploading changed files (%d)", len(set.Files)))
	type item struct {
		i     int
		entry github.TreeEntry
		err   error
		fresh bool
	}
	jobs := make(chan int)
	results := make(chan item)
	var wg sync.WaitGroup
	for n := 0; n < 4; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				f := set.Files[i]
				it := item{i: i}
				var content []byte
				mode := "100644"
				if f.Link != "" {
					content, mode = []byte(f.Link), "120000"
				} else {
					content, it.err = w.Read(f.Path, filesystem.DefaultPushLimits.MaxFile)
					if f.Exec {
						mode = "100755"
					}
				}
				if it.err == nil {
					repoPath := path.Join(j.root, f.Path)
					sha := github.BlobSHA(content)
					if old, ok := existing[repoPath]; !ok || old.SHA != sha {
						if sha, it.err = d.GH.CreateBlob(ctx, j.token, j.full, content); it.err == nil {
							it.fresh = true
						}
					}
					it.entry = github.TreeEntry{Path: repoPath, Mode: mode, Type: "blob", SHA: sha}
				}
				results <- it
			}
		}()
	}
	go func() {
		defer close(jobs)
		for i := range set.Files {
			select {
			case jobs <- i:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() { wg.Wait(); close(results) }()
	res := pushResult{files: len(set.Files)}
	var firstErr error
	got := make([]github.TreeEntry, len(set.Files))
	for it := range results {
		if it.err != nil && firstErr == nil {
			firstErr = it.err
		}
		got[it.i] = it.entry
		if it.fresh {
			res.uploaded++
		}
	}
	if firstErr != nil {
		return pushResult{}, ghError(firstErr)
	}
	if err := ctx.Err(); err != nil {
		return pushResult{}, err
	}
	entries = append(entries, got...)

	d.Ops.Stage(ctx, op, "Creating the commit")
	tree, err := d.GH.CreateTree(ctx, j.token, j.full, entries)
	if err != nil {
		return pushResult{}, ghError(err)
	}
	if tree == headTree {
		return res, nil // identical content: no empty commit
	}
	var parents []string
	if head != "" {
		parents = []string{head}
	}
	commit, err := d.GH.CreateCommit(ctx, j.token, j.full, j.message, tree, parents)
	if err != nil {
		return pushResult{}, ghError(err)
	}
	if err := d.GH.MoveBranch(ctx, j.token, j.full, j.branch, commit, create); err != nil {
		if errors.Is(err, github.ErrConflict) {
			return pushResult{}, domain.Invalid("the branch changed on GitHub while pushing; deploy or pull those changes first, then push again")
		}
		return pushResult{}, ghError(err)
	}
	res.commit = commit
	return res, nil
}
