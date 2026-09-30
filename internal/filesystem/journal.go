package filesystem

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Crash-safe workspace replacement.
//
// A restore or deployment first unpacks and validates everything in a staging
// directory inside the workspace. The commit then moves entries with
// rename(2), which is atomic per entry on one filesystem, but a whole commit
// is many renames. To survive a panel crash in the middle, every entry that is
// replaced is moved aside into a sibling "old" directory rather than deleted,
// and a journal outside the workspace records the plan before the first
// rename. On the next start, Recover rolls every unfinished commit back to the
// complete previous state; only after the caller also recorded its database
// changes is the journal marked done and the old copies deleted.
//
// Journals live in <data root>/.journal/<bot id>.json: bots cannot write there,
// so a bot cannot forge one. Every operation stays inside the workspace root.

const journalDir = ".journal"

// crashAfter simulates process death in tests: after that many commit steps
// the commit stops without rolling back, as if the panel had been killed.
// -1 (the default) disables it.
var crashAfter = -1

var errSimulatedCrash = errors.New("simulated crash")

// step is called before every rename and phase change of a commit.
func (c *Commit) step() error {
	switch {
	case crashAfter == 0:
		return errSimulatedCrash
	case crashAfter > 0:
		crashAfter--
	}
	return nil
}

// Journal phases.
const (
	phaseSwapping = "swapping" // renames may be partly done: roll back
	phaseSwapped  = "swapped"  // all renames done, caller not finished: roll back
	phaseDone     = "done"     // committed: only clean up
)

type journal struct {
	Bot   string   `json:"bot"`
	Kind  string   `json:"kind"`  // restore | deploy
	Stage string   `json:"stage"` // staging directory (workspace-relative)
	Old   string   `json:"old"`   // where replaced entries were moved
	Phase string   `json:"phase"`
	In    []string `json:"in"` // entries moved from Stage into place (workspace-relative)
}

func (m *Manager) journalName(botID string) string { return path.Join(journalDir, botID+".json") }

// writeJournal replaces the journal durably: temp file, fsync, rename, fsync
// of the directory.
func (m *Manager) writeJournal(j journal) error {
	if err := m.root.Mkdir(journalDir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	b, err := json.Marshal(j)
	if err != nil {
		return err
	}
	tmp := m.journalName(j.Bot) + ".tmp"
	f, err := m.root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := m.root.Rename(tmp, m.journalName(j.Bot)); err != nil {
		return err
	}
	if d, err := m.root.Open(journalDir); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}

func (m *Manager) removeJournal(botID string) error {
	err := m.root.Remove(m.journalName(botID))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// Commit is a replaced workspace whose previous contents are still kept. The
// caller records its database changes and then calls Finish, or calls Rollback
// to return to the previous files. If the panel dies first, Recover rolls it
// back on the next start.
type Commit struct {
	m    *Manager
	root *os.Root
	j    journal
	done bool
}

// Finish makes the commit permanent and deletes the previous contents.
func (c *Commit) Finish() error {
	if c == nil || c.done {
		return nil
	}
	c.done = true
	c.j.Phase = phaseDone
	if err := c.m.writeJournal(c.j); err != nil {
		return err
	}
	_ = c.root.RemoveAll(c.j.Old)
	_ = c.root.RemoveAll(c.j.Stage)
	return c.m.removeJournal(c.j.Bot)
}

// Rollback restores the previous contents (after a failed database update).
func (c *Commit) Rollback() error {
	if c == nil || c.done || crashAfter == 0 { // a simulated crash leaves everything as it is
		return nil
	}
	c.done = true
	return c.m.rollback(c.root, c.j)
}

// begin records the plan before the first rename.
// moveIn renames a staged entry into place.
func (c *Commit) moveIn(p string) error {
	if err := c.step(); err != nil {
		return err
	}
	return c.root.Rename(path.Join(c.j.Stage, p), p)
}

func (m *Manager) begin(root *os.Root, botID, kind, stage, old string, in []string) (*Commit, error) {
	if m == nil {
		return nil, errors.New("workspace has no manager")
	}
	if err := root.Mkdir(old, 0o700); err != nil {
		return nil, err
	}
	j := journal{Bot: botID, Kind: kind, Stage: stage, Old: old, Phase: phaseSwapping, In: in}
	if err := m.writeJournal(j); err != nil {
		_ = root.RemoveAll(old)
		return nil, err
	}
	return &Commit{m: m, root: root, j: j}, nil
}

// swapped marks every rename as done.
func (c *Commit) swapped() error {
	if err := c.step(); err != nil {
		return err
	}
	c.j.Phase = phaseSwapped
	return c.m.writeJournal(c.j)
}

// moveAside moves an existing entry p into Old (creating parents), so it can be
// put back. Missing entries are fine.
func (c *Commit) moveAside(p string) error {
	if _, err := c.root.Lstat(p); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err := c.step(); err != nil {
		return err
	}
	if dir := path.Dir(p); dir != "." {
		if err := mkdirAllIn(c.root, path.Join(c.j.Old, dir)); err != nil {
			return err
		}
	}
	return c.root.Rename(p, path.Join(c.j.Old, p))
}

// rollback undoes a partial or complete swap: entries that were moved in go
// back to staging, then everything moved aside returns to its place.
func (m *Manager) rollback(root *os.Root, j journal) error {
	var firstErr error
	keep := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	for i := len(j.In) - 1; i >= 0; i-- {
		p := j.In[i]
		staged := path.Join(j.Stage, p)
		if _, err := root.Lstat(staged); err == nil {
			continue // never moved
		}
		if _, err := root.Lstat(p); err != nil {
			continue
		}
		if dir := path.Dir(staged); dir != "." {
			keep(mkdirAllIn(root, dir))
		}
		keep(root.Rename(p, staged))
	}
	// Put the previous entries back, deepest paths first within each walk.
	var moved []string
	_ = fs.WalkDir(root.FS(), j.Old, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == j.Old {
			return nil
		}
		rel := strings.TrimPrefix(p, j.Old+"/")
		if _, err := root.Lstat(rel); errors.Is(err, fs.ErrNotExist) {
			moved = append(moved, rel)
			if d.IsDir() {
				return fs.SkipDir // the whole directory goes back in one rename
			}
			return nil
		}
		// The name exists (a directory created by the commit, or a parent):
		// descend and restore its children.
		return nil
	})
	for _, rel := range moved {
		if dir := path.Dir(rel); dir != "." {
			keep(mkdirAllIn(root, dir))
		}
		keep(root.Rename(path.Join(j.Old, rel), rel))
	}
	if firstErr != nil {
		return fmt.Errorf("roll back %s of bot %s: %w", j.Kind, j.Bot, firstErr)
	}
	_ = root.RemoveAll(j.Old)
	_ = root.RemoveAll(j.Stage)
	return m.removeJournal(j.Bot)
}

// Recover finishes or rolls back commits a crash interrupted. It runs at
// startup, before any runner or job can touch the workspaces, and returns the
// bots whose files were rolled back.
func (m *Manager) Recover() ([]string, error) {
	ents, err := fs.ReadDir(m.root.FS(), journalDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var rolled []string
	var firstErr error
	for _, e := range ents {
		name := e.Name()
		if strings.HasSuffix(name, ".tmp") {
			_ = m.root.Remove(path.Join(journalDir, name)) // never became the journal
			continue
		}
		botID, ok := strings.CutSuffix(name, ".json")
		if !ok || checkID(botID) != nil {
			continue
		}
		b, err := fs.ReadFile(m.root.FS(), path.Join(journalDir, name))
		var j journal
		if err != nil || json.Unmarshal(b, &j) != nil || j.Bot != botID || !validJournalDir(j.Stage) || !validJournalDir(j.Old) {
			continue // unreadable: leave it for an operator rather than guess
		}
		root, err := m.root.OpenRoot(botID)
		if errors.Is(err, fs.ErrNotExist) {
			_ = m.removeJournal(botID) // the bot is gone
			continue
		}
		if err != nil {
			firstErr = errors.Join(firstErr, err)
			continue
		}
		if j.Phase == phaseDone {
			_ = root.RemoveAll(j.Old)
			_ = root.RemoveAll(j.Stage)
			err = m.removeJournal(botID)
		} else {
			err = m.rollback(root, j)
			if err == nil {
				rolled = append(rolled, botID)
			}
		}
		root.Close()
		firstErr = errors.Join(firstErr, err)
	}
	return rolled, firstErr
}

// validJournalDir accepts only the staging names this package creates.
func validJournalDir(p string) bool {
	return filepath.IsLocal(p) && !strings.Contains(p, "/") &&
		(strings.HasPrefix(p, ".restore-") || strings.HasPrefix(p, ".deploy-"))
}
