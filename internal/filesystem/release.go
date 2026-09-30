package filesystem

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"path"
)

// Usage counts the regular files under the workspace and their total size.
// Symlinks are counted as files of size zero; nothing is followed.
func (w *Workspace) Usage() (files int, bytes int64, err error) {
	err = fs.WalkDir(w.root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		files++
		if d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				bytes += info.Size()
			}
		}
		return nil
	})
	return files, bytes, err
}

// FlattenSingleDir lifts the contents of a lone top-level directory to the
// root (an archive of "dist/" becomes the site itself). It does nothing when
// the root holds anything besides that one directory. It reports whether it
// flattened.
func (w *Workspace) FlattenSingleDir() (bool, error) {
	entries, err := w.List(".")
	if err != nil {
		return false, err
	}
	if len(entries) != 1 || !entries[0].IsDir || entries[0].Symlink {
		return false, nil
	}
	var rnd [6]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return false, err
	}
	// Move the directory aside first: it may contain an entry of its own name.
	tmp := ".flatten-" + hex.EncodeToString(rnd[:])
	if err := w.root.Rename(entries[0].Name, tmp); err != nil {
		return false, err
	}
	children, err := w.List(tmp)
	if err != nil {
		return false, err
	}
	for _, c := range children {
		if err := w.root.Rename(path.Join(tmp, c.Name), c.Name); err != nil {
			return false, err
		}
	}
	return true, w.root.RemoveAll(tmp)
}
