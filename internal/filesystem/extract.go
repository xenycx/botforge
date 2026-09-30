package filesystem

import (
	"archive/zip"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// ExtractLimits bound archive extraction against zip bombs and abuse.
type ExtractLimits struct {
	MaxEntries   int
	MaxTotalSize int64 // total uncompressed bytes
	MaxFileSize  int64
}

// DefaultExtractLimits are conservative defaults.
var DefaultExtractLimits = ExtractLimits{MaxEntries: 20000, MaxTotalSize: 512 << 20, MaxFileSize: 256 << 20}

// ErrArchive marks a rejected archive; the message is safe to show to users.
type ErrArchive struct{ Msg string }

func (e *ErrArchive) Error() string { return e.Msg }

func reject(format string, a ...any) error { return &ErrArchive{Msg: fmt.Sprintf(format, a...)} }

// ExtractZip unpacks zr under dir (a workspace-relative directory, "." for the
// root). It never writes outside the workspace: entry names must be relative
// and free of "..", symlinks and special files are rejected, and setuid/setgid
// bits are dropped. Files are first written to a private staging directory and
// only moved into place once the whole archive has validated and unpacked, so a
// rejected or failed archive leaves the workspace unchanged.
func (w *Workspace) ExtractZip(zr *zip.Reader, dir string, lim ExtractLimits) (files int, err error) {
	if dir != "." {
		if dir, err = clean(dir); err != nil {
			return 0, err
		}
	}
	if len(zr.File) > lim.MaxEntries {
		return 0, reject("archive has too many entries (limit %d)", lim.MaxEntries)
	}
	// Pass 1: validate names and types, and total the DECLARED sizes.
	type entry struct {
		f    *zip.File
		name string
		dir  bool
	}
	var entries []entry
	var declared uint64
	for _, f := range zr.File {
		name := strings.TrimSuffix(f.Name, "/")
		if name == "" {
			continue
		}
		if strings.ContainsAny(name, "\\\x00") || strings.HasPrefix(name, "/") {
			return 0, reject("unsafe path in archive: %q", f.Name)
		}
		name = path.Clean(name)
		if !filepath.IsLocal(name) {
			return 0, reject("unsafe path in archive: %q", f.Name)
		}
		mode := f.Mode()
		switch {
		case mode.IsDir():
		case mode.IsRegular():
			if f.UncompressedSize64 > uint64(lim.MaxFileSize) {
				return 0, reject("file %q exceeds the per-file limit", f.Name)
			}
			declared += f.UncompressedSize64
		default:
			return 0, reject("archive contains a link or special file (%q); these are not allowed", f.Name)
		}
		entries = append(entries, entry{f, name, mode.IsDir()})
	}
	if declared > uint64(lim.MaxTotalSize) {
		return 0, reject("archive expands beyond the %d byte limit", lim.MaxTotalSize)
	}

	// Pass 2: extract into a private staging directory.
	var rnd [6]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return 0, err
	}
	stage := ".extract-" + hex.EncodeToString(rnd[:])
	if err := w.root.Mkdir(stage, 0o700); err != nil {
		return 0, err
	}
	defer w.root.RemoveAll(stage)

	var written int64
	var staged []entry
	for _, e := range entries {
		dst := path.Join(stage, e.name)
		if e.dir {
			if err := w.root.MkdirAll(dst, 0o750); err != nil {
				return 0, err
			}
			staged = append(staged, e)
			continue
		}
		if err := w.root.MkdirAll(path.Dir(dst), 0o750); err != nil {
			return 0, err
		}
		rc, err := e.f.Open()
		if err != nil {
			return 0, reject("cannot read %q: %v", e.f.Name, err)
		}
		perm := fs.FileMode(0o640)
		if e.f.Mode()&0o111 != 0 {
			perm = 0o750
		}
		out, err := w.root.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
		if err != nil {
			rc.Close()
			return 0, err
		}
		// Enforce limits on ACTUAL decompressed bytes; headers can lie.
		n, cerr := io.Copy(out, io.LimitReader(rc, lim.MaxFileSize+1))
		rc.Close()
		if cerr2 := out.Close(); cerr == nil {
			cerr = cerr2
		}
		if cerr != nil {
			return 0, reject("cannot unpack %q: %v", e.f.Name, cerr)
		}
		written += n
		if n > lim.MaxFileSize || written > lim.MaxTotalSize {
			return 0, reject("archive expands beyond its limits")
		}
		staged = append(staged, e)
	}

	// Pass 3: move into place.
	for _, e := range staged {
		final := e.name
		if dir != "." {
			final = path.Join(dir, e.name)
		}
		src := path.Join(stage, e.name)
		if e.dir {
			if err := w.mkdirAll(final); err != nil {
				return files, err
			}
			continue
		}
		if err := w.mkdirAll(path.Dir(final)); err != nil {
			return files, err
		}
		w.chown(src)
		if err := w.root.Rename(src, final); err != nil {
			return files, fmt.Errorf("place %q: %w", e.name, err)
		}
		files++
	}
	return files, nil
}
