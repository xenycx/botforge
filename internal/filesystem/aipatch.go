package filesystem

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"sort"

	"github.com/google/uuid"
)

// PatchFile is one text-only, revision-checked AI change. BeforeRevision is
// empty only for a newly-created path; After nil deletes the path.
type PatchFile struct {
	Path           string
	BeforeRevision string
	After          []byte
	Mode           fs.FileMode
}

// ApplyPatch stages and swaps a complete AI change set through the existing
// crash-recovery journal. The caller must Finish only after its database state
// is durable; otherwise Rollback returns every file to its prior state.
func (w *Workspace) ApplyPatch(files []PatchFile, maxFile, maxTotal int64) (*Commit, error) {
	if len(files) == 0 || len(files) > 32 {
		return nil, fmt.Errorf("AI patch must contain 1 to 32 files")
	}
	seen := map[string]bool{}
	var total int64
	for i := range files {
		p, err := clean(files[i].Path)
		if err != nil || p == "." {
			return nil, ErrInvalidPath
		}
		files[i].Path = p
		if seen[p] {
			return nil, fmt.Errorf("duplicate patch path %s", p)
		}
		seen[p] = true
		if int64(len(files[i].After)) > maxFile {
			return nil, ErrTooLarge
		}
		total += int64(len(files[i].After))
		if total > maxTotal {
			return nil, ErrTooLarge
		}
		exists, e := w.Exists(p)
		if e != nil {
			return nil, e
		}
		if files[i].BeforeRevision == "" {
			if exists {
				return nil, fmt.Errorf("%w: %s was created after the AI snapshot", ErrPatchConflict, p)
			}
		} else {
			if !exists {
				return nil, fmt.Errorf("%w: %s was removed after the AI snapshot", ErrPatchConflict, p)
			}
			rev, e := w.Revision(p)
			if e != nil {
				return nil, e
			}
			if rev != files[i].BeforeRevision {
				return nil, fmt.Errorf("%w: %s changed after the AI snapshot", ErrPatchConflict, p)
			}
		}
	}
	id := uuid.NewString()
	stage := path.Join(".ai-stage-" + id)
	old := path.Join(".ai-old-" + id)
	if err := w.root.Mkdir(stage, 0o700); err != nil {
		return nil, err
	}
	cleanup := func() { _ = w.root.RemoveAll(stage); _ = w.root.RemoveAll(old) }
	var incoming []string
	for _, f := range files {
		if f.After == nil {
			continue
		}
		incoming = append(incoming, f.Path)
		if err := w.Write(path.Join(stage, f.Path), bytes.NewReader(f.After), maxFile); err != nil {
			cleanup()
			return nil, err
		}
		mode := f.Mode
		if mode == 0 {
			mode = 0o640
		}
		if err := w.root.Chmod(path.Join(stage, f.Path), mode.Perm()); err != nil {
			cleanup()
			return nil, err
		}
	}
	// Deep paths first avoids moving a parent before a child; callers normally
	// submit files only, but deterministic ordering keeps recovery auditable.
	sort.Slice(files, func(i, j int) bool { return len(files[i].Path) > len(files[j].Path) })
	c, err := w.m.begin(w.root, w.id, "ai_patch", stage, old, incoming)
	if err != nil {
		cleanup()
		return nil, err
	}
	fail := func(e error) (*Commit, error) { _ = c.Rollback(); return nil, e }
	for _, f := range files {
		if err := c.moveAside(f.Path); err != nil {
			return fail(err)
		}
	}
	for _, f := range files {
		if f.After == nil {
			continue
		}
		if d := path.Dir(f.Path); d != "." {
			if err := w.mkdirAll(d); err != nil {
				return fail(err)
			}
		}
		if err := c.moveIn(f.Path); err != nil {
			return fail(err)
		}
	}
	if err := c.swapped(); err != nil {
		return fail(err)
	}
	return c, nil
}

var ErrPatchConflict = errors.New("AI patch conflict")

// RevisionOrMissing returns an empty revision for a missing file.
func (w *Workspace) RevisionOrMissing(p string) (string, error) {
	r, e := w.Revision(p)
	if errors.Is(e, os.ErrNotExist) {
		return "", nil
	}
	return r, e
}
