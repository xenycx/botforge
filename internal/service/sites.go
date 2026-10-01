package service

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/net/idna"

	"botpanel/internal/domain"
	"botpanel/internal/filesystem"
	"botpanel/internal/github"
)

// SiteStore is the persistence surface of static site hosting.
type SiteStore interface {
	CreateSite(ctx context.Context, s domain.Site) error
	GetSite(ctx context.Context, id string) (domain.Site, error)
	GetSiteForBot(ctx context.Context, botID string) (domain.Site, error)
	ListSitesForUser(ctx context.Context, userID string) ([]domain.Site, error)
	ListAllSites(ctx context.Context) ([]domain.Site, error)
	ListWorkspaceSites(ctx context.Context, workspaceID string) ([]domain.Site, error)
	CountOwnedSites(ctx context.Context, userID string) (int, error)
	UpdateSite(ctx context.Context, s domain.Site) error
	SetSiteDisabled(ctx context.Context, id string, disabled bool, nowMS int64) error
	ActivateRelease(ctx context.Context, siteID, releaseID string, nowMS int64) error
	DeleteSite(ctx context.Context, id string) error
	InsertRelease(ctx context.Context, r domain.SiteRelease) error
	ListReleases(ctx context.Context, siteID string) ([]domain.SiteRelease, error)
	DeleteRelease(ctx context.Context, siteID, releaseID string) error
	ClaimDomain(ctx context.Context, d domain.SiteDomain) error
	ListDomains(ctx context.Context, siteID string) ([]domain.SiteDomain, error)
	ListVerifiedDomains(ctx context.Context) ([]domain.SiteDomain, error)
	GetDomain(ctx context.Context, siteID, name string) (domain.SiteDomain, error)
	RecordDomainCheck(ctx context.Context, name string, verifiedAt *int64, errMsg *string, nowMS int64) error
	DeleteDomain(ctx context.Context, siteID, name string) error
	SiteRoutes(ctx context.Context) (bySlug, byDomain map[string]domain.SiteRoute, err error)
	PublicSitePage(ctx context.Context, siteID string) (domain.PublicSitePage, error)
}

// TXTResolver looks up DNS TXT records (net.Resolver satisfies it).
type TXTResolver interface {
	LookupTXT(ctx context.Context, name string) ([]string, error)
}

// Site hosting limits that are not configurable.
const (
	MaxDomainsPerSite = 10
	keepReleases      = 5 // the serving release plus up to four for rollback
	maxSiteFiles      = 20000
	domainTXTPrefix   = "_botforge-verify."
	domainTXTValue    = "botforge-verify="
)

// SiteService manages static sites. Files are served by the separate sites
// listener (package sitehost), never by the panel's own origin: sites contain
// arbitrary user HTML and scripts.
type SiteService struct {
	Store      SiteStore
	Bots       *BotService
	OAuth      *OAuthService  // optional: GitHub deployments
	GH         *github.Client // optional: GitHub deployments
	Dir        string         // one directory per site, one sub-directory per release
	BaseURL    string         // e.g. https://sites.example.com; a site is served at <slug>.<host>
	DNSTarget  string         // CNAME target shown for custom domains; default: the BaseURL host
	PanelHost  string         // the panel's own host name, never assignable to a site
	MaxBytes   int64          // largest release
	MaxPerUser int            // sites an account may create; 0 = unlimited
	Resolver   TXTResolver    // nil: net.DefaultResolver
	Log        *slog.Logger
	Now        func() time.Time

	root    *filesystem.Manager
	drafts  *filesystem.Manager
	baseCtx context.Context
	wg      sync.WaitGroup

	mu       sync.RWMutex
	bySlug   map[string]domain.SiteRoute
	byDomain map[string]domain.SiteRoute

	jobMu   sync.Mutex
	jobs    map[string]*SiteJob
	draftMu sync.Mutex
}

// SiteJob is the state of a site's GitHub deployment (in memory).
type SiteJob struct {
	Running    bool
	Again      bool
	LastError  string
	FinishedMS int64
}

func (s *SiteService) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *SiteService) warn(msg string, err error) {
	if s.Log != nil {
		s.Log.Warn("sites: "+msg, "err", err)
	}
}

// Start opens the sites directory, removes release directories no database
// row refers to (an interrupted upload), loads the routing table and keeps it
// and custom-domain checks fresh until ctx ends.
func (s *SiteService) Start(ctx context.Context) error {
	m, err := filesystem.NewManager(s.Dir)
	if err != nil {
		return fmt.Errorf("sites directory: %w", err)
	}
	s.root, s.baseCtx = m, ctx
	s.drafts, err = filesystem.NewManager(filepath.Join(s.Dir, "drafts"))
	if err != nil {
		return fmt.Errorf("site drafts directory: %w", err)
	}
	s.sweep(ctx)
	if err := s.Reload(ctx); err != nil {
		return err
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		reload := time.NewTicker(30 * time.Second)
		recheck := time.NewTicker(6 * time.Hour)
		defer reload.Stop()
		defer recheck.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-reload.C:
				if err := s.Reload(ctx); err != nil && ctx.Err() == nil {
					s.warn("reload routes", err)
				}
			case <-recheck.C:
				s.recheckDomains(ctx)
			}
		}
	}()
	return nil
}

// Wait blocks until background work (deployments, refreshes) has stopped.
func (s *SiteService) Wait() { s.wg.Wait() }

// Enabled reports whether site hosting is configured.
func (s *SiteService) Enabled() bool { return s != nil && s.root != nil }

// sweep deletes directories of sites and releases that no longer exist.
func (s *SiteService) sweep(ctx context.Context) {
	sites, err := s.Store.ListAllSites(ctx)
	if err != nil {
		s.warn("sweep", err)
		return
	}
	known := map[string]bool{}
	for _, st := range sites {
		known[st.ID] = true
	}
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if _, err := uuid.Parse(e.Name()); err != nil || !e.IsDir() {
			continue
		}
		if !known[e.Name()] {
			_ = s.root.Remove(e.Name())
			continue
		}
		rels, err := s.Store.ListReleases(ctx, e.Name())
		if err != nil {
			continue
		}
		keep := map[string]bool{}
		for _, r := range rels {
			keep[r.ID] = true
		}
		sub, err := os.ReadDir(filepath.Join(s.Dir, e.Name()))
		if err != nil {
			continue
		}
		for _, r := range sub {
			if _, err := uuid.Parse(r.Name()); err == nil && r.IsDir() && !keep[r.Name()] {
				_ = os.RemoveAll(filepath.Join(s.Dir, e.Name(), r.Name()))
			}
		}
	}
}

// Reload rebuilds the host routing table from the database.
func (s *SiteService) Reload(ctx context.Context) error {
	bySlug, byDomain, err := s.Store.SiteRoutes(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.bySlug, s.byDomain = bySlug, byDomain
	s.mu.Unlock()
	return nil
}

func (s *SiteService) reloadSoon() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Reload(ctx); err != nil {
		s.warn("reload routes", err)
	}
}

// SitesDomain is the host part of BaseURL.
func (s *SiteService) SitesDomain() string {
	u, err := url.Parse(s.BaseURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// SiteURL is a site's default address.
func (s *SiteService) SiteURL(slug string) string {
	u, err := url.Parse(s.BaseURL)
	if err != nil || u.Host == "" {
		return ""
	}
	host := slug + "." + u.Hostname()
	if p := u.Port(); p != "" {
		host += ":" + p
	}
	return u.Scheme + "://" + host
}

// DomainURL is the address of a custom domain, on the same scheme as sites.
func (s *SiteService) DomainURL(name string) string {
	scheme := "https"
	if u, err := url.Parse(s.BaseURL); err == nil && u.Scheme != "" {
		scheme = u.Scheme
	}
	return scheme + "://" + name
}

// TrafficRecord is the DNS record that sends a custom domain's visitors to
// this server: a CNAME to a host name, or an A/AAAA record when the only
// known target is an address. An empty target means "this server's public
// address" (unknown to the panel).
func (s *SiteService) TrafficRecord(slug string) (kind, target string) {
	t := s.DNSTarget
	if t == "" {
		if d := s.SitesDomain(); d != "" && d != "localhost" {
			t = slug + "." + d
		} else {
			t = s.PanelHost
		}
	}
	if ip := net.ParseIP(t); ip != nil {
		if ip.To4() == nil {
			return "AAAA", t
		}
		return "A", t
	}
	if t == "" || t == "localhost" {
		return "A", ""
	}
	return "CNAME", t
}

// Resolve maps a request host (with or without port) to a site route.
func (s *SiteService) Resolve(host string) (domain.SiteRoute, bool) {
	host = normalizeHost(host)
	s.mu.RLock()
	defer s.mu.RUnlock()
	if base := s.SitesDomain(); base != "" {
		if slug, ok := strings.CutSuffix(host, "."+base); ok && !strings.Contains(slug, ".") {
			r, ok := s.bySlug[slug]
			return r, ok
		}
	}
	r, ok := s.byDomain[host]
	return r, ok
}

// TLSAllowed tells an on-demand TLS proxy (Caddy's "ask") whether to obtain
// a certificate for host: only for hosts a live site answers on.
func (s *SiteService) TLSAllowed(host string) bool {
	r, ok := s.Resolve(host)
	return ok && !r.Disabled
}

// ReleaseRoot opens a release directory for serving.
func (s *SiteService) ReleaseRoot(siteID, releaseID string) (*os.Root, error) {
	if _, err := uuid.Parse(siteID); err != nil {
		return nil, domain.ErrNotFound
	}
	if _, err := uuid.Parse(releaseID); err != nil {
		return nil, domain.ErrNotFound
	}
	return os.OpenRoot(filepath.Join(s.Dir, siteID, releaseID))
}

func normalizeHost(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	if host, _, err := net.SplitHostPort(h); err == nil {
		h = host
	}
	return strings.TrimSuffix(h, ".")
}

// ---- authorization ----

// siteRole returns the actor's workspace role for a site; administrators act
// as owners. Sites outside the actor's workspaces are ErrNotFound.
func (s *SiteService) siteRole(ctx context.Context, actor domain.User, id string) (domain.Site, string, error) {
	if _, err := uuid.Parse(id); err != nil {
		return domain.Site{}, "", domain.ErrNotFound
	}
	st, err := s.Store.GetSite(ctx, id)
	if err != nil {
		return domain.Site{}, "", err
	}
	if actor.IsAdmin() {
		return st, domain.WorkspaceOwner, nil
	}
	role, err := s.Bots.Store.WorkspaceRole(ctx, st.WorkspaceID, actor.ID)
	if err != nil {
		return domain.Site{}, "", err
	}
	if role == "" {
		return domain.Site{}, "", domain.ErrNotFound
	}
	return st, role, nil
}

func (s *SiteService) requireSite(ctx context.Context, actor domain.User, id, min string) (domain.Site, string, error) {
	st, role, err := s.siteRole(ctx, actor, id)
	if err != nil {
		return st, role, err
	}
	if domain.WorkspaceRoleRank(role) < domain.WorkspaceRoleRank(min) {
		return st, role, domain.ErrForbidden
	}
	return st, role, nil
}

// Authorize exposes the same site-role check to trusted subsystems such as the
// AI operator. HTTP handlers must not duplicate or weaken this boundary.
func (s *SiteService) Authorize(ctx context.Context, actor domain.User, id, minRole string) (domain.Site, string, error) {
	return s.requireSite(ctx, actor, id, minRole)
}

// ---- reading ----

// SiteDetail is everything the site page shows.
type SiteDetail struct {
	Site     domain.Site
	Role     string
	Domains  []domain.SiteDomain
	Releases []domain.SiteRelease
	Job      SiteJob
}

// List returns the sites in the actor's workspaces.
func (s *SiteService) List(ctx context.Context, actor domain.User) ([]domain.Site, error) {
	return s.Store.ListSitesForUser(ctx, actor.ID)
}

// ListAll returns every site (administrators only).
func (s *SiteService) ListAll(ctx context.Context, actor domain.User) ([]domain.Site, error) {
	if !actor.IsAdmin() {
		return nil, domain.ErrForbidden
	}
	return s.Store.ListAllSites(ctx)
}

// ListWorkspace returns one workspace's sites to its members.
func (s *SiteService) ListWorkspace(ctx context.Context, actor domain.User, workspaceID string) ([]domain.Site, error) {
	if _, err := s.Bots.workspaceRole(ctx, actor, workspaceID); err != nil {
		return nil, err
	}
	return s.Store.ListWorkspaceSites(ctx, workspaceID)
}

// Get returns a site with its domains, releases and deployment state.
func (s *SiteService) Get(ctx context.Context, actor domain.User, id string) (SiteDetail, error) {
	st, role, err := s.siteRole(ctx, actor, id)
	if err != nil {
		return SiteDetail{}, err
	}
	ds, err := s.Store.ListDomains(ctx, id)
	if err != nil {
		return SiteDetail{}, err
	}
	rs, err := s.Store.ListReleases(ctx, id)
	if err != nil {
		return SiteDetail{}, err
	}
	return SiteDetail{Site: st, Role: role, Domains: ds, Releases: rs, Job: s.job(id)}, nil
}

func (s *SiteService) job(id string) SiteJob {
	s.jobMu.Lock()
	defer s.jobMu.Unlock()
	if j := s.jobs[id]; j != nil {
		return *j
	}
	return SiteJob{}
}

// ---- creating and changing ----

var (
	slugRe        = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{1,38}[a-z0-9])$`)
	slugRun       = regexp.MustCompile(`[^a-z0-9]+`)
	reservedSlugs = map[string]bool{"www": true, "api": true, "admin": true, "panel": true, "mail": true, "ftp": true, "sites": true,
		"static": true, "assets": true, "cdn": true, "app": true, "status": true, "docs": true, "localhost": true, "botforge": true}
)

// CreateSiteInput describes a new site. Slug "" derives one from the name.
type CreateSiteInput struct {
	Name        string
	Slug        string
	WorkspaceID string
	SPA         bool
	BotID       string // optional: creates the one public page attached to this bot
	Mode        string // files (default) | page
}

func slugFrom(name string) string {
	s := strings.Trim(slugRun.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(s) > 30 {
		s = strings.Trim(s[:30], "-")
	}
	if len(s) < 3 || reservedSlugs[s] {
		s = strings.Trim("site-"+s, "-")
	}
	return s
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Create makes an empty site in a workspace where the actor is at least a
// developer.
func (s *SiteService) Create(ctx context.Context, actor domain.User, in CreateSiteInput) (domain.Site, error) {
	if !s.Enabled() {
		return domain.Site{}, domain.Invalid("site hosting is not enabled on this panel")
	}
	name, err := validateName(in.Name)
	if err != nil {
		return domain.Site{}, err
	}
	wsID := ""
	var botID *string
	if in.BotID != "" {
		b, e := s.Bots.Authorize(ctx, actor, in.BotID, domain.PermEditFiles)
		if e != nil {
			return domain.Site{}, e
		}
		wsID, botID = b.WorkspaceID, &b.ID
		if in.WorkspaceID != "" && in.WorkspaceID != wsID {
			return domain.Site{}, domain.Invalid("a bot page must stay in the bot's workspace")
		}
		if _, e := s.Store.GetSiteForBot(ctx, b.ID); e == nil {
			return domain.Site{}, domain.Invalid("this bot already has a public page")
		} else if !errors.Is(e, domain.ErrNotFound) {
			return domain.Site{}, e
		}
	} else if wsID, err = s.Bots.creatableWorkspace(ctx, actor, in.WorkspaceID); err != nil {
		return domain.Site{}, err
	}
	if s.MaxPerUser > 0 && !actor.IsAdmin() {
		n, err := s.Store.CountOwnedSites(ctx, actor.ID)
		if err != nil {
			return domain.Site{}, err
		}
		if n >= s.MaxPerUser {
			return domain.Site{}, domain.Invalid(fmt.Sprintf("your account can have at most %d sites; delete one first", s.MaxPerUser))
		}
	}
	slug, auto := strings.ToLower(strings.TrimSpace(in.Slug)), false
	if slug == "" {
		slug, auto = slugFrom(name), true
	}
	if !slugRe.MatchString(slug) || strings.Contains(slug, "--") {
		return domain.Site{}, domain.Invalid("the address must be 3-40 lower-case letters, digits and single hyphens, starting and ending with a letter or digit")
	}
	if reservedSlugs[slug] {
		return domain.Site{}, domain.Invalid("that address is reserved; choose another")
	}
	now := s.now().UnixMilli()
	mode := in.Mode
	if mode == "" {
		mode = "files"
	}
	if mode != "files" && mode != "page" {
		return domain.Site{}, domain.Invalid("site mode must be page or files")
	}
	if mode == "page" && botID == nil {
		return domain.Site{}, domain.Invalid("a generated public page must be attached to a bot")
	}
	st := domain.Site{ID: uuid.NewString(), WorkspaceID: wsID, OwnerID: actor.ID, BotID: botID, Name: name, Slug: slug, SPA: in.SPA, CleanURLs: true,
		Mode: mode, PageTitle: name, PageDescription: "A Discord community powered by " + name + ".", PageTheme: "midnight", PageAccent: "#5865f2",
		CreatedAtMS: now, UpdatedAtMS: now}
	for attempt := 0; ; attempt++ {
		err = s.Store.CreateSite(ctx, st)
		if !errors.Is(err, domain.ErrConflict) {
			break
		}
		if !auto || attempt >= 4 {
			return domain.Site{}, domain.Invalid("that address is already taken; choose another")
		}
		base := slug
		if len(base) > 34 {
			base = strings.Trim(base[:34], "-")
		}
		st.Slug = base + "-" + randHex(2)
	}
	if err != nil {
		return domain.Site{}, err
	}
	s.reloadSoon()
	return s.Store.GetSite(ctx, st.ID)
}

// RepoInput links a GitHub repository (Clear removes the link).
type RepoInput struct {
	FullName string
	Branch   string
	RootDir  string
	Clear    bool
}

// UpdateSiteInput is a partial change; nil fields are unchanged.
type UpdateSiteInput struct {
	Name            *string
	SPA             *bool
	CleanURLs       *bool
	WorkspaceID     *string
	Repo            *RepoInput
	Mode            *string
	PageTitle       *string
	PageDescription *string
	PageTheme       *string
	PageAccent      *string
	PageHTML        *string
	PageCSS         *string
	WidgetsPublic   *bool
}

var hexColorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func cleanPageText(v string, max int, field string) (string, error) {
	v = strings.TrimSpace(v)
	if len(v) > max {
		return "", domain.Invalid(fmt.Sprintf("%s must be at most %d characters", field, max))
	}
	if strings.ContainsRune(v, 0) {
		return "", domain.Invalid(field + " contains an invalid character")
	}
	return v, nil
}

// Update changes a site's settings (developer and up; moving to another
// workspace needs admin here and developer there).
func (s *SiteService) Update(ctx context.Context, actor domain.User, id string, in UpdateSiteInput) (domain.Site, error) {
	st, role, err := s.requireSite(ctx, actor, id, domain.WorkspaceDeveloper)
	if err != nil {
		return domain.Site{}, err
	}
	if in.Name != nil {
		if st.Name, err = validateName(*in.Name); err != nil {
			return domain.Site{}, err
		}
	}
	if in.SPA != nil {
		st.SPA = *in.SPA
	}
	if in.CleanURLs != nil {
		st.CleanURLs = *in.CleanURLs
	}
	if in.Mode != nil {
		if *in.Mode != "files" && *in.Mode != "page" {
			return domain.Site{}, domain.Invalid("site mode must be page or files")
		}
		if *in.Mode == "page" && st.BotID == nil {
			return domain.Site{}, domain.Invalid("only a bot-linked site can use page mode")
		}
		st.Mode = *in.Mode
	}
	if in.PageTitle != nil {
		if st.PageTitle, err = cleanPageText(*in.PageTitle, 80, "page title"); err != nil {
			return domain.Site{}, err
		}
	}
	if in.PageDescription != nil {
		if st.PageDescription, err = cleanPageText(*in.PageDescription, 500, "page description"); err != nil {
			return domain.Site{}, err
		}
	}
	if in.PageTheme != nil {
		if *in.PageTheme != "midnight" && *in.PageTheme != "daylight" && *in.PageTheme != "system" {
			return domain.Site{}, domain.Invalid("page theme must be midnight, daylight, or system")
		}
		st.PageTheme = *in.PageTheme
	}
	if in.PageAccent != nil {
		if !hexColorRe.MatchString(*in.PageAccent) {
			return domain.Site{}, domain.Invalid("page accent must be a six-digit hex color")
		}
		st.PageAccent = strings.ToLower(*in.PageAccent)
	}
	if in.PageHTML != nil {
		if st.PageHTML, err = cleanPageText(*in.PageHTML, 65536, "custom HTML"); err != nil {
			return domain.Site{}, err
		}
	}
	if in.PageCSS != nil {
		if st.PageCSS, err = cleanPageText(*in.PageCSS, 32768, "custom CSS"); err != nil {
			return domain.Site{}, err
		}
	}
	if in.WidgetsPublic != nil {
		st.WidgetsPublic = *in.WidgetsPublic
	}
	if in.WorkspaceID != nil && *in.WorkspaceID != st.WorkspaceID {
		if domain.WorkspaceRoleRank(role) < domain.WorkspaceRoleRank(domain.WorkspaceAdmin) {
			return domain.Site{}, domain.ErrForbidden
		}
		if st.WorkspaceID, err = s.Bots.creatableWorkspace(ctx, actor, *in.WorkspaceID); err != nil {
			return domain.Site{}, err
		}
	}
	if in.Repo != nil {
		if in.Repo.Clear {
			st.RepoFullName, st.RepoBranch, st.RepoRoot, st.RepoTokenUser = nil, nil, "", nil
		} else if err := s.linkRepo(ctx, actor, &st, *in.Repo); err != nil {
			return domain.Site{}, err
		}
	}
	st.UpdatedAtMS = s.now().UnixMilli()
	if err := s.Store.UpdateSite(ctx, st); err != nil {
		return domain.Site{}, err
	}
	s.reloadSoon()
	return s.Store.GetSite(ctx, id)
}

// GetForBot returns a bot's attached site to anyone who may view that bot.
// Absence is returned as ErrNotFound so the UI can offer creation.
func (s *SiteService) GetForBot(ctx context.Context, actor domain.User, botID string) (SiteDetail, error) {
	if _, err := s.Bots.Authorize(ctx, actor, botID, domain.PermViewConsole); err != nil {
		return SiteDetail{}, err
	}
	st, err := s.Store.GetSiteForBot(ctx, botID)
	if err != nil {
		return SiteDetail{}, err
	}
	return s.Get(ctx, actor, st.ID)
}

// PublicPage returns the anonymous, deliberately redacted page model used by
// the separate sites listener.
func (s *SiteService) PublicPage(ctx context.Context, siteID string) (domain.PublicSitePage, error) {
	return s.Store.PublicSitePage(ctx, siteID)
}

// linkRepo validates a repository link with the actor's GitHub access (a
// public repository works without a connected account).
func (s *SiteService) linkRepo(ctx context.Context, actor domain.User, st *domain.Site, in RepoInput) error {
	if s.GH == nil || s.OAuth == nil {
		return domain.Invalid("GitHub deployments are not available on this panel")
	}
	if !github.FullNameRe.MatchString(in.FullName) {
		return domain.Invalid("repository must look like owner/name")
	}
	if !github.ValidBranch(in.Branch) {
		return domain.Invalid("invalid branch name")
	}
	root, err := cleanRoot(in.RootDir)
	if err != nil {
		return err
	}
	tok, err := s.OAuth.GitHubToken(ctx, actor.ID)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	repo, err := s.GH.GetRepo(ctx, tok, in.FullName)
	if err != nil {
		return ghError(err)
	}
	if _, err := s.GH.BranchSHA(ctx, tok, repo.FullName, in.Branch); err != nil {
		return ghError(err)
	}
	full, branch := repo.FullName, in.Branch
	st.RepoFullName, st.RepoBranch, st.RepoRoot = &full, &branch, root
	st.RepoTokenUser = nil
	if tok != "" {
		st.RepoTokenUser = &actor.ID
	}
	return nil
}

// Delete removes a site, its domains and every release (workspace admin).
func (s *SiteService) Delete(ctx context.Context, actor domain.User, id string) error {
	if _, _, err := s.requireSite(ctx, actor, id, domain.WorkspaceAdmin); err != nil {
		return err
	}
	if err := s.Store.DeleteSite(ctx, id); err != nil {
		return err
	}
	s.reloadSoon()
	if err := s.root.Remove(id); err != nil {
		s.warn("remove site files", err)
	}
	if s.drafts != nil {
		_ = s.drafts.Remove(id)
	}
	return nil
}

// Draft opens an editable, contained file workspace for a site. The first
// visit seeds it from the serving release; if the site is empty it starts with
// a small, useful index page. Draft changes are private until PublishDraft.
func (s *SiteService) Draft(ctx context.Context, actor domain.User, id string) (*filesystem.Workspace, error) {
	st, _, err := s.requireSite(ctx, actor, id, domain.WorkspaceDeveloper)
	if err != nil {
		return nil, err
	}
	if s.drafts == nil {
		return nil, domain.Invalid("site file editing is not available")
	}
	s.draftMu.Lock()
	defer s.draftMu.Unlock()
	if w, err := s.drafts.Open(id); err == nil {
		return w, nil
	}
	if err := s.drafts.Create(id); err != nil {
		if w, openErr := s.drafts.Open(id); openErr == nil {
			return w, nil
		}
		return nil, err
	}
	dst, err := s.drafts.Open(id)
	if err != nil {
		return nil, err
	}
	if st.CurrentRelease == nil {
		seed := `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>` + htmlEscape(st.Name) + `</title><link rel="stylesheet" href="styles.css"></head><body><main><h1>` + htmlEscape(st.Name) + `</h1><p>Your site is ready to shape.</p></main></body></html>`
		if err := dst.Write("index.html", bytes.NewBufferString(seed), 1<<20); err != nil {
			dst.Close()
			_ = s.drafts.Remove(id)
			return nil, err
		}
		_ = dst.Write("styles.css", bytes.NewBufferString("body{margin:0;min-height:100vh;display:grid;place-items:center;font:16px/1.5 system-ui;background:#0d1220;color:#f4f7ff}main{max-width:42rem;padding:2rem}h1{font-size:clamp(3rem,10vw,7rem);line-height:.9}"), 1<<20)
		return dst, nil
	}
	if err := s.copyReleaseToDraft(st.ID, *st.CurrentRelease, dst); err != nil {
		dst.Close()
		_ = s.drafts.Remove(id)
		return nil, err
	}
	return dst, nil
}

func htmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;")
	return r.Replace(s)
}

func (s *SiteService) copyReleaseToDraft(siteID, releaseID string, dst *filesystem.Workspace) error {
	m, err := filesystem.NewManager(filepath.Join(s.Dir, siteID))
	if err != nil {
		return err
	}
	defer m.Close()
	src, err := m.Open(releaseID)
	if err != nil {
		return err
	}
	defer src.Close()
	_, lim := s.limits()
	var buf bytes.Buffer
	if _, _, err := src.WriteTarGz(&buf, nil, lim); err != nil {
		return err
	}
	_, err = dst.RestoreTarGz(&buf, lim)
	return err
}

// PublishDraft snapshots the private editable workspace into a new immutable
// release and switches the live site atomically.
func (s *SiteService) PublishDraft(ctx context.Context, actor domain.User, id string) (domain.SiteRelease, error) {
	st, _, err := s.requireSite(ctx, actor, id, domain.WorkspaceDeveloper)
	if err != nil {
		return domain.SiteRelease{}, err
	}
	draft, err := s.Draft(ctx, actor, id)
	if err != nil {
		return domain.SiteRelease{}, err
	}
	defer draft.Close()
	rel, dst, done, err := s.newRelease(id)
	if err != nil {
		return domain.SiteRelease{}, err
	}
	ok := false
	defer func() { done(ok) }()
	_, lim := s.limits()
	var buf bytes.Buffer
	if _, _, err := draft.WriteTarGz(&buf, nil, lim); err != nil {
		return domain.SiteRelease{}, err
	}
	if _, err := dst.RestoreTarGz(&buf, lim); err != nil {
		return domain.SiteRelease{}, err
	}
	label := "File editor"
	r, err := s.finish(ctx, st, rel, dst, "upload", &label, &actor.ID)
	if err != nil {
		return domain.SiteRelease{}, err
	}
	ok = true
	return r, nil
}

// SetDisabled suspends or restores a site (panel administrators only).
func (s *SiteService) SetDisabled(ctx context.Context, actor domain.User, id string, disabled bool) error {
	if !actor.IsAdmin() {
		return domain.ErrForbidden
	}
	if _, err := uuid.Parse(id); err != nil {
		return domain.ErrNotFound
	}
	if err := s.Store.SetSiteDisabled(ctx, id, disabled, s.now().UnixMilli()); err != nil {
		return err
	}
	s.reloadSoon()
	return nil
}

// ---- releases ----

// newRelease creates an empty release directory and returns its workspace
// handle. done releases the handles; with keep false it also deletes the
// directory.
func (s *SiteService) newRelease(siteID string) (id string, w *filesystem.Workspace, done func(keep bool), err error) {
	m, err := filesystem.NewManager(filepath.Join(s.Dir, siteID))
	if err != nil {
		return "", nil, nil, err
	}
	id = uuid.NewString()
	if err := m.Create(id); err != nil {
		m.Close()
		return "", nil, nil, err
	}
	w, err = m.Open(id)
	if err != nil {
		_ = m.Remove(id)
		m.Close()
		return "", nil, nil, err
	}
	done = func(keep bool) {
		w.Close()
		if !keep {
			_ = m.Remove(id)
		}
		m.Close()
	}
	return id, w, done, nil
}

func (s *SiteService) limits() (filesystem.ExtractLimits, filesystem.BackupLimits) {
	max := s.MaxBytes
	if max <= 0 {
		max = 100 << 20
	}
	return filesystem.ExtractLimits{MaxEntries: maxSiteFiles, MaxTotalSize: max, MaxFileSize: max},
		filesystem.BackupLimits{MaxEntries: maxSiteFiles, MaxBytes: max, MaxFile: max}
}

// Upload makes a release from a ZIP archive and serves it. A single
// top-level folder (a zipped dist/) is unwrapped.
func (s *SiteService) Upload(ctx context.Context, actor domain.User, id string, body io.Reader) (domain.SiteRelease, error) {
	st, _, err := s.requireSite(ctx, actor, id, domain.WorkspaceDeveloper)
	if err != nil {
		return domain.SiteRelease{}, err
	}
	rel, w, done, err := s.newRelease(st.ID)
	if err != nil {
		return domain.SiteRelease{}, err
	}
	ok := false
	defer func() { done(ok) }()
	elim, _ := s.limits()
	const tmp = ".upload.zip"
	if err := w.Write(tmp, body, elim.MaxTotalSize); err != nil {
		if errors.Is(err, filesystem.ErrTooLarge) {
			return domain.SiteRelease{}, domain.Invalid(fmt.Sprintf("the archive is larger than the %d MiB limit", elim.MaxTotalSize>>20))
		}
		return domain.SiteRelease{}, err
	}
	f, size, err := w.OpenFile(tmp)
	if err != nil {
		return domain.SiteRelease{}, err
	}
	zr, err := zip.NewReader(f, size)
	if err != nil {
		f.Close()
		return domain.SiteRelease{}, domain.Invalid("not a valid zip archive")
	}
	_, err = w.ExtractZip(zr, ".", elim)
	f.Close()
	if err != nil {
		var ae *filesystem.ErrArchive
		if errors.As(err, &ae) {
			return domain.SiteRelease{}, domain.Invalid(ae.Msg)
		}
		return domain.SiteRelease{}, err
	}
	if err := w.Remove(tmp); err != nil {
		return domain.SiteRelease{}, err
	}
	r, err := s.finish(ctx, st, rel, w, "upload", nil, &actor.ID)
	if err != nil {
		return domain.SiteRelease{}, err
	}
	ok = true
	return r, nil
}

// finish counts a staged release, records it, serves it and prunes old ones.
func (s *SiteService) finish(ctx context.Context, st domain.Site, rel string, w *filesystem.Workspace, source string, label, actorID *string) (domain.SiteRelease, error) {
	if _, err := w.FlattenSingleDir(); err != nil {
		return domain.SiteRelease{}, err
	}
	_ = w.Remove(filesystem.DeployManifest) // written by tarball deploys; not part of the site
	files, bytes, err := w.Usage()
	if err != nil {
		return domain.SiteRelease{}, err
	}
	if files == 0 {
		return domain.SiteRelease{}, domain.Invalid("there are no files to publish")
	}
	r := domain.SiteRelease{ID: rel, SiteID: st.ID, Source: source, SourceLabel: label, Files: files, Bytes: bytes,
		ActorID: actorID, CreatedAtMS: s.now().UnixMilli()}
	if err := s.Store.InsertRelease(ctx, r); err != nil {
		return domain.SiteRelease{}, err
	}
	if err := s.Store.ActivateRelease(ctx, st.ID, rel, r.CreatedAtMS); err != nil {
		return domain.SiteRelease{}, err
	}
	s.reloadSoon()
	s.prune(ctx, st.ID)
	return r, nil
}

// prune keeps the newest releases (the serving one is never removed).
func (s *SiteService) prune(ctx context.Context, siteID string) {
	rs, err := s.Store.ListReleases(ctx, siteID)
	if err != nil || len(rs) <= keepReleases {
		return
	}
	st, err := s.Store.GetSite(ctx, siteID)
	if err != nil {
		return
	}
	for _, r := range rs[keepReleases:] {
		if st.CurrentRelease != nil && *st.CurrentRelease == r.ID {
			continue
		}
		if err := s.Store.DeleteRelease(ctx, siteID, r.ID); err != nil {
			s.warn("prune release", err)
			continue
		}
		if err := os.RemoveAll(filepath.Join(s.Dir, siteID, r.ID)); err != nil {
			s.warn("remove release files", err)
		}
	}
}

// Activate serves an earlier release again (rollback).
func (s *SiteService) Activate(ctx context.Context, actor domain.User, id, releaseID string) error {
	if _, _, err := s.requireSite(ctx, actor, id, domain.WorkspaceDeveloper); err != nil {
		return err
	}
	if _, err := uuid.Parse(releaseID); err != nil {
		return domain.ErrNotFound
	}
	if err := s.Store.ActivateRelease(ctx, id, releaseID, s.now().UnixMilli()); err != nil {
		return err
	}
	s.reloadSoon()
	return nil
}

// Deploy publishes the linked repository's branch head in the background.
// Requests during a running deployment coalesce into one follow-up.
func (s *SiteService) Deploy(ctx context.Context, actor domain.User, id string) error {
	st, _, err := s.requireSite(ctx, actor, id, domain.WorkspaceDeveloper)
	if err != nil {
		return err
	}
	if st.RepoFullName == nil || st.RepoBranch == nil {
		return domain.Invalid("link a GitHub repository first")
	}
	if s.GH == nil || s.OAuth == nil {
		return domain.Invalid("GitHub deployments are not available on this panel")
	}
	s.jobMu.Lock()
	if s.jobs == nil {
		s.jobs = map[string]*SiteJob{}
	}
	j := s.jobs[id]
	if j == nil {
		j = &SiteJob{}
		s.jobs[id] = j
	}
	if j.Running {
		j.Again = true
		s.jobMu.Unlock()
		return nil
	}
	j.Running, j.LastError = true, ""
	s.jobMu.Unlock()
	actorID := actor.ID
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			err := s.deployOnce(id, &actorID)
			s.jobMu.Lock()
			j.LastError, j.FinishedMS = "", s.now().UnixMilli()
			if err != nil {
				j.LastError = deployMessage(err)
				s.warn("deploy site", err)
			}
			if !j.Again || s.ctx().Err() != nil {
				j.Running, j.Again = false, false
				s.jobMu.Unlock()
				return
			}
			j.Again = false
			s.jobMu.Unlock()
		}
	}()
	return nil
}

func (s *SiteService) ctx() context.Context {
	if s.baseCtx != nil {
		return s.baseCtx
	}
	return context.Background()
}

func (s *SiteService) deployOnce(id string, actorID *string) error {
	ctx, cancel := context.WithTimeout(s.ctx(), 10*time.Minute)
	defer cancel()
	st, err := s.Store.GetSite(ctx, id)
	if err != nil {
		return err
	}
	if st.RepoFullName == nil || st.RepoBranch == nil {
		return domain.Invalid("the repository was unlinked")
	}
	tok := ""
	if st.RepoTokenUser != nil {
		if tok, err = s.OAuth.GitHubToken(ctx, *st.RepoTokenUser); err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
	}
	sha, err := s.GH.BranchSHA(ctx, tok, *st.RepoFullName, *st.RepoBranch)
	if err != nil {
		return ghError(err)
	}
	body, err := s.GH.Tarball(ctx, tok, *st.RepoFullName, sha)
	if err != nil {
		return ghError(err)
	}
	defer body.Close()
	rel, w, done, err := s.newRelease(st.ID)
	if err != nil {
		return err
	}
	_, blim := s.limits()
	if _, err := w.DeployTarGz(io.LimitReader(body, maxTarball), st.RepoRoot, blim); err != nil {
		done(false)
		return err
	}
	label := fmt.Sprintf("%s@%s %s", *st.RepoFullName, *st.RepoBranch, sha[:7])
	_, err = s.finish(ctx, st, rel, w, "github", &label, actorID)
	done(err == nil)
	return err
}

// ---- custom domains ----

// NormalizeDomain validates a host name typed by a user and returns its
// lower-case ASCII (punycode) form.
func NormalizeDomain(in string) (string, error) {
	d := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(in)), ".")
	if strings.ContainsAny(d, "/:@ ") {
		return "", domain.Invalid("enter only the host name, such as www.example.com")
	}
	a, err := idna.Lookup.ToASCII(d)
	if err != nil || net.ParseIP(a) != nil {
		return "", domain.Invalid("that is not a valid domain name")
	}
	labels := strings.Split(a, ".")
	if len(a) < 4 || len(a) > 253 || len(labels) < 2 {
		return "", domain.Invalid("enter a full domain name, such as www.example.com")
	}
	for _, l := range labels {
		if l == "" || len(l) > 63 || strings.HasPrefix(l, "-") || strings.HasSuffix(l, "-") ||
			strings.ContainsFunc(l, func(r rune) bool { return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') }) {
			return "", domain.Invalid("that is not a valid domain name")
		}
	}
	if tld := labels[len(labels)-1]; strings.Trim(tld, "0123456789") == "" || tld == "localhost" || tld == "local" || tld == "internal" {
		return "", domain.Invalid("use a public domain name")
	}
	return a, nil
}

// VerificationRecord is the DNS TXT record proving control of a domain.
func VerificationRecord(d domain.SiteDomain) (name, value string) {
	return domainTXTPrefix + d.Domain, domainTXTValue + d.Token
}

// AddDomain attaches a custom domain; it is served once verified.
func (s *SiteService) AddDomain(ctx context.Context, actor domain.User, id, name string) (domain.SiteDomain, error) {
	if _, _, err := s.requireSite(ctx, actor, id, domain.WorkspaceDeveloper); err != nil {
		return domain.SiteDomain{}, err
	}
	d, err := NormalizeDomain(name)
	if err != nil {
		return domain.SiteDomain{}, err
	}
	if p := normalizeHost(s.PanelHost); p != "" && (d == p || strings.HasSuffix(d, "."+p)) {
		return domain.SiteDomain{}, domain.Invalid("the panel's own domain (and its subdomains) cannot host a site")
	}
	if base := s.SitesDomain(); base != "" && (d == base || strings.HasSuffix(d, "."+base)) {
		return domain.SiteDomain{}, domain.Invalid("addresses under " + base + " are assigned automatically; change the site address instead")
	}
	existing, err := s.Store.ListDomains(ctx, id)
	if err != nil {
		return domain.SiteDomain{}, err
	}
	if len(existing) >= MaxDomainsPerSite {
		return domain.SiteDomain{}, domain.Invalid(fmt.Sprintf("a site can have at most %d domains", MaxDomainsPerSite))
	}
	sd := domain.SiteDomain{Domain: d, SiteID: id, Token: randHex(16), CreatedAtMS: s.now().UnixMilli()}
	if err := s.Store.ClaimDomain(ctx, sd); err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return domain.SiteDomain{}, domain.Invalid("that domain is already verified for another site on this panel")
		}
		return domain.SiteDomain{}, err
	}
	return sd, nil
}

// VerifyDomain checks the TXT record now and, when it matches, starts serving
// the domain.
func (s *SiteService) VerifyDomain(ctx context.Context, actor domain.User, id, name string) (domain.SiteDomain, error) {
	if _, _, err := s.requireSite(ctx, actor, id, domain.WorkspaceDeveloper); err != nil {
		return domain.SiteDomain{}, err
	}
	d, err := s.Store.GetDomain(ctx, id, strings.ToLower(name))
	if err != nil {
		return domain.SiteDomain{}, err
	}
	ok, msg := s.checkTXT(ctx, d)
	now := s.now().UnixMilli()
	var at *int64
	var errMsg *string
	if ok {
		at = &now
	} else {
		errMsg = &msg
	}
	if err := s.Store.RecordDomainCheck(ctx, d.Domain, at, errMsg, now); err != nil {
		return domain.SiteDomain{}, err
	}
	s.reloadSoon()
	return s.Store.GetDomain(ctx, id, d.Domain)
}

func (s *SiteService) checkTXT(ctx context.Context, d domain.SiteDomain) (bool, string) {
	r := s.Resolver
	if r == nil {
		r = net.DefaultResolver
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	name, want := VerificationRecord(d)
	recs, err := r.LookupTXT(ctx, name)
	if err != nil {
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
			return false, "No TXT record found at " + name + " yet; DNS changes can take several minutes to appear"
		}
		return false, "The DNS lookup failed; try again in a minute"
	}
	for _, v := range recs {
		if strings.TrimSpace(v) == want {
			return true, ""
		}
	}
	return false, "A TXT record exists at " + name + " but none has the expected value"
}

// recheckDomains re-verifies verified domains and reports problems (a failed
// re-check does not stop serving: DNS hiccups must not take sites down).
func (s *SiteService) recheckDomains(ctx context.Context) {
	ds, err := s.Store.ListVerifiedDomains(ctx)
	if err != nil {
		s.warn("list domains", err)
		return
	}
	for _, d := range ds {
		if ctx.Err() != nil {
			return
		}
		ok, msg := s.checkTXT(ctx, d)
		var errMsg *string
		if !ok {
			errMsg = &msg
		}
		if err := s.Store.RecordDomainCheck(ctx, d.Domain, nil, errMsg, s.now().UnixMilli()); err != nil {
			s.warn("record domain check", err)
		}
	}
}

// RemoveDomain detaches a domain.
func (s *SiteService) RemoveDomain(ctx context.Context, actor domain.User, id, name string) error {
	if _, _, err := s.requireSite(ctx, actor, id, domain.WorkspaceDeveloper); err != nil {
		return err
	}
	if err := s.Store.DeleteDomain(ctx, id, strings.ToLower(name)); err != nil {
		return err
	}
	s.reloadSoon()
	return nil
}
