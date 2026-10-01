package addons

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

// DataRoot holds add-on data directories: <Dir>/<bot id>/<kind>. They live
// outside the bot's workspace, so the bot's files, deployments and file
// manager never touch database files.
type DataRoot struct {
	Dir string // absolute
}

func (d DataRoot) botDir(botID string) (string, error) {
	if d.Dir == "" {
		return "", errors.New("add-on data directory is not configured")
	}
	if u, err := uuid.Parse(botID); err != nil || u.String() != botID {
		return "", errors.New("invalid bot id")
	}
	return filepath.Join(d.Dir, botID), nil
}

// Path returns an add-on's data directory without creating it.
func (d DataRoot) Path(botID, kind string) (string, error) {
	dir, err := d.botDir(botID)
	if err != nil {
		return "", err
	}
	if _, ok := Get(kind); !ok {
		return "", fmt.Errorf("unknown add-on %q", kind)
	}
	return filepath.Join(dir, kind), nil
}

// Ensure creates an add-on's data directory owned by uid:gid (the container
// user as seen by the host) and returns its path.
func (d DataRoot) Ensure(botID, kind string, uid, gid int) (string, error) {
	p, err := d.Path(botID, kind)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(p, 0o700); err != nil {
		return "", err
	}
	st, err := os.Lstat(p)
	if err != nil {
		return "", err
	}
	if !st.IsDir() {
		return "", fmt.Errorf("add-on data path %s is not a directory", p)
	}
	if err := os.Lchown(p, uid, gid); err != nil {
		// Without CAP_CHOWN this only works when uid:gid is the panel's own.
		return "", fmt.Errorf("add-on data directory cannot be given to the container user %d:%d: %w", uid, gid, err)
	}
	return p, nil
}

// Remove deletes one add-on's data. A missing directory is not an error.
func (d DataRoot) Remove(botID, kind string) error {
	p, err := d.Path(botID, kind)
	if err != nil {
		return err
	}
	return removeAll(p)
}

// RemoveBot deletes every add-on's data of a bot.
func (d DataRoot) RemoveBot(botID string) error {
	dir, err := d.botDir(botID)
	if err != nil {
		if d.Dir == "" {
			return nil
		}
		return err
	}
	return removeAll(dir)
}

// Usage returns the bytes used by a bot's add-on data (best effort).
func (d DataRoot) Usage(botID, kind string) int64 {
	p, err := d.Path(botID, kind)
	if err != nil {
		return 0
	}
	var n int64
	_ = filepath.WalkDir(p, func(_ string, e fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if info, err := e.Info(); err == nil && info.Mode().IsRegular() {
			n += info.Size()
		}
		return nil
	})
	return n
}

func removeAll(p string) error {
	err := os.RemoveAll(p)
	if errors.Is(err, fs.ErrPermission) {
		// Database images create directories without owner write access.
		_ = filepath.WalkDir(p, func(q string, e fs.DirEntry, err error) error {
			if err == nil && e.IsDir() {
				_ = os.Chmod(q, 0o700)
			}
			return nil
		})
		err = os.RemoveAll(p)
	}
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
