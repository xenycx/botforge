package api

import (
	"errors"
	"io/fs"
	"strings"

	"github.com/gofiber/fiber/v3"

	"botpanel/internal/domain"
	"botpanel/internal/filesystem"
	"botpanel/internal/pkgmgr"
)

// pkgCtx authorizes the caller (edit-files permission) and resolves the
// manifest handling for the bot's runtime.
func (s *server) pkgCtx(c fiber.Ctx) (*filesystem.Workspace, *pkgmgr.Ecosystem, error) {
	if s.files == nil {
		return nil, nil, fiber.ErrNotFound
	}
	b, err := s.bots.Authorize(c.Context(), currentUser(c), strings.Clone(c.Params("id")), domain.PermEditFiles)
	if err != nil {
		return nil, nil, err
	}
	if !isSafeMethod(c.Method()) {
		if err := s.bots.FilesBlocked(b.ID); err != nil {
			return nil, nil, err
		}
	}
	w, err := s.files.Open(b.ID)
	if err != nil {
		return nil, nil, mapFSError(err)
	}
	eco, err := pkgmgr.ForRuntime(b.Runtime, func(n string) bool { ok, _ := w.Exists(n); return ok })
	if err != nil {
		w.Close()
		return nil, nil, err
	}
	return w, eco, nil
}

func mapPkgError(err error) error {
	var pe *pkgmgr.ParseError
	switch {
	case errors.Is(err, pkgmgr.ErrUnsupported):
		return domain.Invalid("the visual package manager is not available for this runtime")
	case errors.As(err, &pe):
		return domain.Invalid(pe.Error())
	}
	return err
}

type depsResponse struct {
	Supported bool                `json:"supported"`
	Ecosystem string              `json:"ecosystem,omitempty"`
	File      string              `json:"file,omitempty"`
	Exists    bool                `json:"exists"`
	Groups    []string            `json:"groups,omitempty"`
	Deps      []pkgmgr.Dependency `json:"deps"`
}

func (s *server) listPackages(c fiber.Ctx) error {
	w, eco, err := s.pkgCtx(c)
	if errors.Is(err, pkgmgr.ErrUnsupported) {
		return c.JSON(depsResponse{Supported: false, Deps: []pkgmgr.Dependency{}})
	}
	if err != nil {
		return err
	}
	defer w.Close()
	res := depsResponse{Supported: true, Ecosystem: eco.ID, File: eco.File, Groups: eco.Groups, Deps: []pkgmgr.Dependency{}}
	data, err := w.Read(eco.File, pkgmgr.MaxManifestBytes)
	if errors.Is(err, fs.ErrNotExist) {
		return c.JSON(res)
	}
	if err != nil {
		return mapFSError(err)
	}
	res.Exists = true
	if res.Deps, err = eco.Parse(data); err != nil {
		return mapPkgError(err)
	}
	if res.Deps == nil {
		res.Deps = []pkgmgr.Dependency{}
	}
	return c.JSON(res)
}

// newManifests are created when a project has none yet (where that is safe).
var newManifests = map[string]string{
	"package.json":     "{\n  \"name\": \"bot\",\n  \"version\": \"1.0.0\",\n  \"private\": true\n}\n",
	"requirements.txt": "",
}

func (s *server) editPackages(c fiber.Ctx) error {
	var in struct {
		Ops []pkgmgr.Op `json:"ops"`
	}
	if err := decode(c, &in); err != nil {
		return err
	}
	w, eco, err := s.pkgCtx(c)
	if err != nil {
		return mapPkgError(err)
	}
	defer w.Close()
	data, err := w.Read(eco.File, pkgmgr.MaxManifestBytes)
	if errors.Is(err, fs.ErrNotExist) {
		tpl, ok := newManifests[eco.File]
		if !ok {
			return domain.Invalid(eco.File + " does not exist yet; create it in the file manager first")
		}
		data, err = []byte(tpl), nil
	}
	if err != nil {
		return mapFSError(err)
	}
	out, err := eco.Apply(data, in.Ops)
	if err != nil {
		var pe *pkgmgr.ParseError
		if errors.As(err, &pe) {
			return mapPkgError(err)
		}
		return domain.Invalid(err.Error()) // op validation / conflicts are client errors
	}
	if err := w.Write(eco.File, strings.NewReader(string(out)), pkgmgr.MaxManifestBytes); err != nil {
		return mapFSError(err)
	}
	deps, err := eco.Parse(out)
	if err != nil {
		return mapPkgError(err)
	}
	if deps == nil {
		deps = []pkgmgr.Dependency{}
	}
	return c.JSON(depsResponse{Supported: true, Ecosystem: eco.ID, File: eco.File, Exists: true, Groups: eco.Groups, Deps: deps})
}

func (s *server) searchPackages(c fiber.Ctx) error {
	w, eco, err := s.pkgCtx(c)
	if err != nil {
		return mapPkgError(err)
	}
	w.Close()
	res, err := s.registry.Search(c.Context(), eco.ID, strings.Clone(c.Query("q")))
	if err != nil {
		return registryError(err)
	}
	return c.JSON(fiber.Map{"results": res})
}

func (s *server) latestPackage(c fiber.Ctx) error {
	w, eco, err := s.pkgCtx(c)
	if err != nil {
		return mapPkgError(err)
	}
	w.Close()
	v, err := s.registry.Latest(c.Context(), eco.ID, strings.Clone(c.Query("name")))
	if err != nil {
		return registryError(err)
	}
	return c.JSON(fiber.Map{"version": v})
}

// registryError separates the caller's mistakes (400) from upstream trouble (502).
func registryError(err error) error {
	m := err.Error()
	if strings.HasPrefix(m, "registry lookup failed") || strings.Contains(m, "status ") || strings.Contains(m, "deadline") || strings.Contains(m, "dial") {
		return fiber.NewError(fiber.StatusBadGateway, "the package registry could not be reached")
	}
	return domain.Invalid(m)
}
