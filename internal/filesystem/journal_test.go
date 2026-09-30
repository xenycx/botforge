package filesystem

import (
	"archive/tar"
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// snapshot maps every file and symlink under root to its content or target.
func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == root {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			l, _ := os.Readlink(p)
			out[rel] = "-> " + l
		case !d.IsDir():
			b, _ := os.ReadFile(p)
			out[rel] = string(b)
		}
		return nil
	})
	return out
}

func noLeftovers(t *testing.T, root string) {
	t.Helper()
	ents, _ := os.ReadDir(root)
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".restore-") || strings.HasPrefix(e.Name(), ".deploy-") {
			t.Fatalf("leftover %s", e.Name())
		}
	}
	if js, _ := os.ReadDir(filepath.Join(filepath.Dir(root), journalDir)); len(js) != 0 {
		t.Fatalf("leftover journal %v", js[0].Name())
	}
}

func restoreArchive(t *testing.T) []byte {
	return targz(t, []tar.Header{
		{Name: "a.txt", Typeflag: tar.TypeReg, Mode: 0o644},
		{Name: "dir/", Typeflag: tar.TypeDir, Mode: 0o755},
		{Name: "dir/c.txt", Typeflag: tar.TypeReg, Mode: 0o644},
		{Name: "new.txt", Typeflag: tar.TypeReg, Mode: 0o644},
		{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "a.txt"},
	}, []string{"new-a", "", "new-c", "new", ""})
}

func seedOld(t *testing.T, root string) {
	put(t, root, "a.txt", "old-a")
	put(t, root, "dir/b.txt", "old-b")
	put(t, root, "keep.txt", "old-keep")
	put(t, root, "node_modules/x/i.js", "deps")
}

// A crash at any point of a restore commit, and after the commit before the
// caller finished, recovers to exactly the previous files.
func TestRestoreCommitSurvivesACrashAtEveryStep(t *testing.T) {
	defer func() { crashAfter = -1 }()
	for k := 0; ; k++ {
		w, root := newWS(t)
		seedOld(t, root)
		before := snapshot(t, root)
		crashAfter = k
		_, c, err := w.RestoreTarGzCommit(bytes.NewReader(restoreArchive(t)), DefaultBackupLimits)
		crashed := errors.Is(err, errSimulatedCrash)
		crashAfter = -1
		if !crashed && err != nil {
			t.Fatalf("k=%d: %v", k, err)
		}
		if !crashed {
			_ = c // the commit completed; the panel dies before Finish
		}
		rolled, err := w.m.Recover()
		if err != nil || len(rolled) != 1 {
			t.Fatalf("k=%d: recover %v %v", k, rolled, err)
		}
		if got := snapshot(t, root); !reflect.DeepEqual(got, before) {
			t.Fatalf("k=%d: after recovery\n got %v\nwant %v", k, got, before)
		}
		noLeftovers(t, root)
		if !crashed {
			if k < 3 {
				t.Fatalf("the commit finished after only %d steps", k)
			}
			return
		}
	}
}

func TestRestoreCommitFinishAndRollback(t *testing.T) {
	w, root := newWS(t)
	seedOld(t, root)
	before := snapshot(t, root)
	_, c, err := w.RestoreTarGzCommit(bytes.NewReader(restoreArchive(t)), DefaultBackupLimits)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Rollback(); err != nil {
		t.Fatal(err)
	}
	if got := snapshot(t, root); !reflect.DeepEqual(got, before) {
		t.Fatalf("rollback: %v", got)
	}
	noLeftovers(t, root)

	_, c, err = w.RestoreTarGzCommit(bytes.NewReader(restoreArchive(t)), DefaultBackupLimits)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Finish(); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"a.txt": "new-a", "dir/c.txt": "new-c", "new.txt": "new", "link": "-> a.txt", "node_modules/x/i.js": "deps"}
	if got := snapshot(t, root); !reflect.DeepEqual(got, want) {
		t.Fatalf("finish:\n got %v\nwant %v", got, want)
	}
	noLeftovers(t, root)
	// A finished commit leaves nothing for recovery.
	if rolled, err := w.m.Recover(); err != nil || len(rolled) != 0 {
		t.Fatalf("recover after finish: %v %v", rolled, err)
	}
}

func TestDeployCommitSurvivesACrashAtEveryStep(t *testing.T) {
	defer func() { crashAfter = -1 }()
	first := map[string]string{"index.js": "v1", "src/a.js": "a", "old.js": "old"}
	second := map[string]string{"index.js": "v2", "src/b.js": "b", "src/a.js": "a2"}
	for k := 0; ; k++ {
		w, root := newWS(t)
		if _, err := w.DeployTarGz(bytes.NewReader(repoTar(t, first)), "", DefaultBackupLimits); err != nil {
			t.Fatal(err)
		}
		put(t, root, "data/db.sqlite", "precious")
		before := snapshot(t, root)
		crashAfter = k
		_, _, err := w.DeployTarGzCommit(bytes.NewReader(repoTar(t, second)), "", DefaultBackupLimits, nil)
		crashed := errors.Is(err, errSimulatedCrash)
		crashAfter = -1
		if !crashed && err != nil {
			t.Fatalf("k=%d: %v", k, err)
		}
		if _, err := w.m.Recover(); err != nil {
			t.Fatalf("k=%d: recover: %v", k, err)
		}
		if got := snapshot(t, root); !reflect.DeepEqual(got, before) {
			t.Fatalf("k=%d: after recovery\n got %v\nwant %v", k, got, before)
		}
		noLeftovers(t, root)
		if !crashed {
			return
		}
	}
}
