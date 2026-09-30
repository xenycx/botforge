package api

import (
	"errors"
	"strings"

	"github.com/gofiber/fiber/v3"

	"botpanel/internal/domain"
	"botpanel/internal/service"
	"botpanel/internal/templates"
)

func (s *server) listTemplates(c fiber.Ctx) error {
	type tplDTO struct {
		templates.Template
		// From the runtime recipe, so the creation flow can show real needs.
		DefaultMemoryBytes int64 `json:"default_memory_bytes"`
		BuildMemoryBytes   int64 `json:"build_memory_bytes"`
		HasBuild           bool  `json:"has_build"`
	}
	list := templates.List()
	out := make([]tplDTO, 0, len(list))
	for _, t := range list {
		d := tplDTO{Template: t}
		if rt, ok := s.catalog.Get(t.Runtime); ok {
			d.DefaultMemoryBytes, d.BuildMemoryBytes, d.HasBuild = rt.Defaults.MemoryBytes, rt.BuildMemoryBytes, len(rt.BuildArgv) > 0
			if d.HasBuild && d.BuildMemoryBytes == 0 {
				d.BuildMemoryBytes = s.buildMemory
			}
		}
		out = append(out, d)
	}
	return c.JSON(fiber.Map{"templates": out})
}

type repoDTO struct {
	FullName       string `json:"full_name"`
	Branch         string `json:"branch"`
	RootDir        string `json:"root_dir"`
	Private        bool   `json:"private"`
	AutoDeploy     bool   `json:"auto_deploy"`
	HookCreated    bool   `json:"hook_created"`
	WebhookURL     string `json:"webhook_url"`
	Secret         string `json:"secret,omitempty"` // shown once, only for manual webhook setup
	LastSHA        string `json:"last_sha"`
	LastDeployedMS int64  `json:"last_deployed_at_ms"`
	LastError      string `json:"last_error"`
	Deploying      bool   `json:"deploying"`
}

func toRepo(v service.RepoView) repoDTO {
	return repoDTO{v.FullName, v.Branch, v.RootDir, v.Private, v.AutoDeploy, v.HookCreated, v.WebhookURL, v.Secret,
		v.LastSHA, v.LastDeployedMS, v.LastError, v.Deploying}
}

func (s *server) githubRepos(c fiber.Ctx) error {
	rs, err := s.deploy.Repos(c.Context(), currentUser(c))
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"repos": rs})
}

func (s *server) githubBranches(c fiber.Ctx) error {
	bs, err := s.deploy.Branches(c.Context(), currentUser(c), strings.Clone(c.Query("repo")))
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"branches": bs})
}

func (s *server) getGitHub(c fiber.Ctx) error {
	v, err := s.deploy.Get(c.Context(), currentUser(c), strings.Clone(c.Params("id")))
	if errors.Is(err, domain.ErrNotFound) {
		if _, aerr := s.bots.Authorize(c.Context(), currentUser(c), strings.Clone(c.Params("id")), domain.PermEditFiles); aerr == nil {
			return c.JSON(fiber.Map{"linked": false}) // the bot exists but has no repository
		}
	}
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"linked": true, "repo": toRepo(v)})
}

func (s *server) putGitHub(c fiber.Ctx) error {
	var in githubIn
	if err := decode(c, &in); err != nil {
		return err
	}
	v, err := s.deploy.Configure(c.Context(), currentUser(c), strings.Clone(c.Params("id")),
		service.ConfigureInput{FullName: in.FullName, Branch: in.Branch, RootDir: in.RootDir, AutoDeploy: in.AutoDeploy})
	if err != nil {
		return err
	}
	c.Set("Cache-Control", "no-store")
	return c.JSON(fiber.Map{"linked": true, "repo": toRepo(v)})
}

func (s *server) deleteGitHub(c fiber.Ctx) error {
	if err := s.deploy.Unlink(c.Context(), currentUser(c), strings.Clone(c.Params("id"))); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (s *server) deployGitHub(c fiber.Ctx) error {
	var in struct {
		SHA string `json:"sha"` // optional: a specific commit (redeploy or roll back)
	}
	if len(c.Body()) > 0 {
		if err := decode(c, &in); err != nil {
			return err
		}
	}
	if err := s.deploy.Deploy(c.Context(), currentUser(c), strings.Clone(c.Params("id")), strings.Clone(in.SHA)); err != nil {
		return err
	}
	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{"status": "queued"})
}

// previewGitHub shows what deploying the branch head would change.
func (s *server) previewGitHub(c fiber.Ctx) error {
	cmp, err := s.deploy.Preview(c.Context(), currentUser(c), strings.Clone(c.Params("id")))
	if err != nil {
		return err
	}
	return c.JSON(cmp)
}

// githubWebhook receives push events. It answers 401 for anything it cannot
// authenticate, without saying why.
func (s *server) githubWebhook(c fiber.Ctx) error {
	body := append([]byte(nil), c.Body()...) // request memory is reused after the handler returns
	res, err := s.deploy.HandleWebhook(c.Context(), strings.Clone(c.Get("X-GitHub-Event")), strings.Clone(c.Get("X-GitHub-Delivery")),
		strings.Clone(c.Get("X-Hub-Signature-256")), body)
	if errors.Is(err, service.ErrBadSignature) {
		return fiber.NewError(fiber.StatusUnauthorized, "invalid signature")
	}
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{"status": res})
}
