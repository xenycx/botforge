package api

import (
	"strings"

	"github.com/gofiber/fiber/v3"

	"botpanel/internal/domain"
	"botpanel/internal/service"
)

type siteDTO struct {
	ID             string  `json:"id"`
	WorkspaceID    string  `json:"workspace_id"`
	WorkspaceName  string  `json:"workspace_name"`
	OwnerID        string  `json:"owner_id"`
	OwnerEmail     string  `json:"owner_email"`
	Name           string  `json:"name"`
	Slug           string  `json:"slug"`
	URL            string  `json:"url"`
	SPA            bool    `json:"spa"`
	CleanURLs      bool    `json:"clean_urls"`
	CurrentRelease *string `json:"current_release"`
	ReleaseBytes   int64   `json:"release_bytes"`
	Disabled       bool    `json:"disabled"`
	Domains        int     `json:"domains"`
	RepoFullName   *string `json:"repo_full_name"`
	RepoBranch     *string `json:"repo_branch"`
	RepoRoot       string  `json:"repo_root"`
	CreatedAtMS    int64   `json:"created_at_ms"`
	UpdatedAtMS    int64   `json:"updated_at_ms"`
}

func (s *server) toSite(st domain.Site) siteDTO {
	return siteDTO{st.ID, st.WorkspaceID, st.WorkspaceName, st.OwnerID, st.OwnerEmail, st.Name, st.Slug, s.sites.SiteURL(st.Slug),
		st.SPA, st.CleanURLs, st.CurrentRelease, st.ReleaseBytes, st.Disabled, st.Domains, st.RepoFullName, st.RepoBranch, st.RepoRoot,
		st.CreatedAtMS, st.UpdatedAtMS}
}

func (s *server) siteList(list []domain.Site) []siteDTO {
	out := make([]siteDTO, len(list))
	for i, st := range list {
		out[i] = s.toSite(st)
	}
	return out
}

// sitesIn lists a workspace's sites for the administrator workspace view
// (empty when hosting is off).
func (s *server) sitesIn(c fiber.Ctx, workspaceID string) []siteDTO {
	if !s.sites.Enabled() {
		return []siteDTO{}
	}
	list, err := s.sites.ListWorkspace(c.Context(), currentUser(c), workspaceID)
	if err != nil {
		return []siteDTO{}
	}
	return s.siteList(list)
}

type domainDTO struct {
	Domain          string  `json:"domain"`
	URL             string  `json:"url"`
	Verified        bool    `json:"verified"`
	VerifiedAtMS    *int64  `json:"verified_at_ms"`
	LastCheckedAtMS *int64  `json:"last_checked_at_ms"`
	LastError       *string `json:"last_error"`
	TXTName         string  `json:"txt_name"`
	TXTValue        string  `json:"txt_value"`
	// The record that routes visitors: CNAME to a host, or A/AAAA to an
	// address ("" = this server's public address, unknown to the panel).
	RecordType   string `json:"record_type"`
	RecordTarget string `json:"record_target"`
}

func (s *server) toDomain(d domain.SiteDomain, slug string) domainDTO {
	name, value := service.VerificationRecord(d)
	kind, target := s.sites.TrafficRecord(slug)
	return domainDTO{d.Domain, s.sites.DomainURL(d.Domain), d.VerifiedAtMS != nil, d.VerifiedAtMS, d.LastCheckedAtMS, d.LastError,
		name, value, kind, target}
}

type releaseDTO struct {
	ID          string  `json:"id"`
	Source      string  `json:"source"`
	SourceLabel *string `json:"source_label"`
	Files       int     `json:"files"`
	Bytes       int64   `json:"bytes"`
	Actor       *string `json:"actor"`
	Current     bool    `json:"current"`
	CreatedAtMS int64   `json:"created_at_ms"`
}

// sitesInfo tells the interface whether hosting is on and how addresses look.
func (s *server) sitesInfo(c fiber.Ctx) error {
	if !s.sites.Enabled() {
		return c.JSON(fiber.Map{"enabled": false})
	}
	return c.JSON(fiber.Map{"enabled": true, "domain": s.sites.SitesDomain(), "example_url": s.sites.SiteURL("example"),
		"max_bytes": s.sites.MaxBytes, "max_domains": service.MaxDomainsPerSite})
}

func (s *server) listSites(c fiber.Ctx) error {
	list, err := s.sites.List(c.Context(), currentUser(c))
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"sites": s.siteList(list)})
}

func (s *server) createSite(c fiber.Ctx) error {
	var in struct {
		Name        string `json:"name"`
		Slug        string `json:"slug"`
		WorkspaceID string `json:"workspace_id"`
		SPA         bool   `json:"spa"`
	}
	if err := decode(c, &in); err != nil {
		return err
	}
	st, err := s.sites.Create(c.Context(), currentUser(c), service.CreateSiteInput{Name: in.Name, Slug: in.Slug, WorkspaceID: in.WorkspaceID, SPA: in.SPA})
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(s.toSite(st))
}

func (s *server) getSite(c fiber.Ctx) error {
	d, err := s.sites.Get(c.Context(), currentUser(c), strings.Clone(c.Params("sid")))
	if err != nil {
		return err
	}
	domains := make([]domainDTO, len(d.Domains))
	for i, x := range d.Domains {
		domains[i] = s.toDomain(x, d.Site.Slug)
	}
	releases := make([]releaseDTO, len(d.Releases))
	for i, r := range d.Releases {
		releases[i] = releaseDTO{r.ID, r.Source, r.SourceLabel, r.Files, r.Bytes, r.ActorEmail,
			d.Site.CurrentRelease != nil && *d.Site.CurrentRelease == r.ID, r.CreatedAtMS}
	}
	return c.JSON(fiber.Map{"site": s.toSite(d.Site), "role": d.Role, "domains": domains, "releases": releases,
		"deploy": fiber.Map{"running": d.Job.Running, "last_error": d.Job.LastError, "finished_at_ms": d.Job.FinishedMS}})
}

func (s *server) patchSite(c fiber.Ctx) error {
	var in struct {
		Name        *string `json:"name"`
		SPA         *bool   `json:"spa"`
		CleanURLs   *bool   `json:"clean_urls"`
		WorkspaceID *string `json:"workspace_id"`
		Repo        *struct {
			FullName string `json:"full_name"`
			Branch   string `json:"branch"`
			RootDir  string `json:"root_dir"`
			Clear    bool   `json:"clear"`
		} `json:"repo"`
	}
	if err := decode(c, &in); err != nil {
		return err
	}
	up := service.UpdateSiteInput{Name: in.Name, SPA: in.SPA, CleanURLs: in.CleanURLs, WorkspaceID: in.WorkspaceID}
	if in.Repo != nil {
		up.Repo = &service.RepoInput{FullName: in.Repo.FullName, Branch: in.Repo.Branch, RootDir: in.Repo.RootDir, Clear: in.Repo.Clear}
	}
	if _, err := s.sites.Update(c.Context(), currentUser(c), strings.Clone(c.Params("sid")), up); err != nil {
		return err
	}
	return s.getSite(c)
}

func (s *server) deleteSite(c fiber.Ctx) error {
	if err := s.sites.Delete(c.Context(), currentUser(c), strings.Clone(c.Params("sid"))); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// uploadSite publishes a ZIP archive (the raw request body) as a new release.
func (s *server) uploadSite(c fiber.Ctx) error {
	if err := s.checkLength(c); err != nil {
		return err
	}
	r, err := s.sites.Upload(c.Context(), currentUser(c), strings.Clone(c.Params("sid")), bodyReader(c))
	if err != nil {
		return mapFSError(err)
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"release": r.ID, "files": r.Files, "bytes": r.Bytes})
}

func (s *server) deploySite(c fiber.Ctx) error {
	if err := s.sites.Deploy(c.Context(), currentUser(c), strings.Clone(c.Params("sid"))); err != nil {
		return err
	}
	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{"status": "queued"})
}

func (s *server) activateRelease(c fiber.Ctx) error {
	if err := s.sites.Activate(c.Context(), currentUser(c), strings.Clone(c.Params("sid")), strings.Clone(c.Params("rid"))); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (s *server) addSiteDomain(c fiber.Ctx) error {
	var in struct {
		Domain string `json:"domain"`
	}
	if err := decode(c, &in); err != nil {
		return err
	}
	id := strings.Clone(c.Params("sid"))
	d, err := s.sites.AddDomain(c.Context(), currentUser(c), id, in.Domain)
	if err != nil {
		return err
	}
	st, err := s.sites.Get(c.Context(), currentUser(c), id)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(s.toDomain(d, st.Site.Slug))
}

func (s *server) verifySiteDomain(c fiber.Ctx) error {
	id := strings.Clone(c.Params("sid"))
	d, err := s.sites.VerifyDomain(c.Context(), currentUser(c), id, strings.Clone(c.Params("domain")))
	if err != nil {
		return err
	}
	st, err := s.sites.Get(c.Context(), currentUser(c), id)
	if err != nil {
		return err
	}
	return c.JSON(s.toDomain(d, st.Site.Slug))
}

func (s *server) removeSiteDomain(c fiber.Ctx) error {
	if err := s.sites.RemoveDomain(c.Context(), currentUser(c), strings.Clone(c.Params("sid")), strings.Clone(c.Params("domain"))); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (s *server) adminListSites(c fiber.Ctx) error {
	list, err := s.sites.ListAll(c.Context(), currentUser(c))
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"sites": s.siteList(list)})
}

func (s *server) adminPatchSite(c fiber.Ctx) error {
	var in struct {
		Disabled *bool `json:"disabled"`
	}
	if err := decode(c, &in); err != nil {
		return err
	}
	if in.Disabled == nil {
		return domain.Invalid("nothing to change")
	}
	if err := s.sites.SetDisabled(c.Context(), currentUser(c), strings.Clone(c.Params("sid")), *in.Disabled); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}
