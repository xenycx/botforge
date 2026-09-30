package filesystem

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type zf struct {
	name string
	body string
	mode os.FileMode
}

func makeZip(t *testing.T, files ...zf) *zip.Reader {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range files {
		h := &zip.FileHeader{Name: f.name, Method: zip.Deflate}
		if f.mode != 0 {
			h.SetMode(f.mode)
		}
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		io.WriteString(w, f.body)
	}
	zw.Close()
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	return zr
}

func TestExtractZipHappyPath(t *testing.T) {
	w, dir := setup(t)
	zr := makeZip(t,
		zf{"src/", "", os.ModeDir | 0o755},
		zf{"src/main.js", "console.log(1)", 0o644},
		zf{"run.sh", "#!/bin/sh\n", 0o4755}, // setuid must be dropped, exec kept
		zf{"deep/er/file.txt", "x", 0o600},
	)
	n, err := w.ExtractZip(zr, "app", DefaultExtractLimits)
	if err != nil || n != 3 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if b, _ := w.Read("app/src/main.js", 100); string(b) != "console.log(1)" {
		t.Fatal("content")
	}
	st, err := os.Stat(filepath.Join(dir, "bots", id, "app", "run.sh"))
	if err != nil || st.Mode()&os.ModeSetuid != 0 || st.Mode().Perm()&0o100 == 0 {
		t.Fatalf("mode %v %v", st.Mode(), err)
	}
	es, _ := w.List(".")
	for _, e := range es {
		if strings.HasPrefix(e.Name, ".extract-") {
			t.Fatal("staging directory leaked")
		}
	}
}

func TestExtractZipRejectsMaliciousEntriesAndLeavesWorkspaceUntouched(t *testing.T) {
	bad := map[string][]zf{
		"parent traversal": {{"../evil", "x", 0o644}},
		"deep traversal":   {{"a/../../evil", "x", 0o644}},
		"absolute":         {{"/etc/cron.d/x", "x", 0o644}},
		"backslash":        {{"a\\..\\evil", "x", 0o644}},
		"symlink":          {{"link", "/etc/passwd", os.ModeSymlink | 0o777}},
		"device":           {{"dev", "", os.ModeDevice | 0o644}},
		"nul":              {{"a\x00b", "x", 0o644}},
	}
	for name, files := range bad {
		w, dir := setup(t)
		w.Write("existing.txt", strings.NewReader("keep"), 100)
		files = append([]zf{{"good.txt", "fine", 0o644}}, files...)
		_, err := w.ExtractZip(makeZip(t, files...), ".", DefaultExtractLimits)
		var ae *ErrArchive
		if err == nil || !errors.As(err, &ae) {
			// traversal names may be rejected by IsLocal as ErrArchive; anything else is a failure
			t.Errorf("%s: expected ErrArchive, got %v", name, err)
		}
		if _, err := w.Read("good.txt", 10); err == nil {
			t.Errorf("%s: partial extraction left good.txt behind", name)
		}
		if b, _ := w.Read("existing.txt", 10); string(b) != "keep" {
			t.Errorf("%s: existing file changed", name)
		}
		for _, p := range []string{"evil", "bots/evil", "../evil"} {
			if _, err := os.Stat(filepath.Join(dir, p)); err == nil {
				t.Errorf("%s: file escaped to %s", name, p)
			}
		}
	}
}

func TestExtractZipEnforcesLimits(t *testing.T) {
	w, _ := setup(t)
	many := make([]zf, 0, 50)
	for i := 0; i < 50; i++ {
		many = append(many, zf{name: "f" + string(rune('a'+i%26)) + string(rune('a'+i/26)), body: "x"})
	}
	if _, err := w.ExtractZip(makeZip(t, many...), ".", ExtractLimits{MaxEntries: 10, MaxTotalSize: 1 << 20, MaxFileSize: 1 << 20}); err == nil {
		t.Fatal("entry limit not enforced")
	}
	big := strings.Repeat("A", 1<<20) // compresses to almost nothing: a bomb in miniature
	if _, err := w.ExtractZip(makeZip(t, zf{"bomb.bin", big, 0o644}), ".", ExtractLimits{MaxEntries: 10, MaxTotalSize: 1 << 30, MaxFileSize: 1 << 16}); err == nil {
		t.Fatal("per-file limit not enforced")
	}
	if _, err := w.ExtractZip(makeZip(t, zf{"a", big, 0o644}, zf{"b", big, 0o644}), ".", ExtractLimits{MaxEntries: 10, MaxTotalSize: 1<<20 + 1, MaxFileSize: 1 << 30}); err == nil {
		t.Fatal("total limit not enforced")
	}
	es, _ := w.List(".")
	if len(es) != 0 {
		t.Fatalf("workspace not clean after rejected archives: %+v", es)
	}
}

func TestExtractZipCannotWriteThroughExistingSymlink(t *testing.T) {
	w, dir := setup(t)
	outside := filepath.Join(dir, "outside")
	os.Mkdir(outside, 0o750)
	os.Symlink(outside, filepath.Join(dir, "bots", id, "linkdir"))
	// The archive is innocent; the workspace already contains a symlink to the outside.
	_, err := w.ExtractZip(makeZip(t, zf{"linkdir/pwn.txt", "x", 0o644}), ".", DefaultExtractLimits)
	if err == nil {
		t.Fatal("extraction through a symlink to the outside succeeded")
	}
	if _, e := os.Stat(filepath.Join(outside, "pwn.txt")); e == nil {
		t.Fatal("file written outside the workspace")
	}
	if _, err := w.ExtractZip(makeZip(t, zf{"x.txt", "x", 0o644}), "../out", DefaultExtractLimits); err == nil {
		t.Fatal("destination directory traversal accepted")
	}
}

func TestRenameStatOpenFile(t *testing.T) {
	w, _ := setup(t)
	w.Write("a/b.txt", strings.NewReader("hi"), 10)
	if err := w.Rename("a/b.txt", "c/d.txt"); err != nil {
		t.Fatal(err)
	}
	if b, _ := w.Read("c/d.txt", 10); string(b) != "hi" {
		t.Fatal("moved content")
	}
	if err := w.Rename("c", "c/inner"); err == nil {
		t.Fatal("moved a directory into itself")
	}
	for _, p := range []string{"../x", "/abs"} {
		if err := w.Rename("c/d.txt", p); err == nil {
			t.Errorf("rename to %q accepted", p)
		}
		if err := w.Rename(p, "ok"); err == nil {
			t.Errorf("rename from %q accepted", p)
		}
	}
	e, err := w.Stat("c/d.txt")
	if err != nil || e.IsDir || e.Size != 2 {
		t.Fatalf("%+v %v", e, err)
	}
	f, size, err := w.OpenFile("c/d.txt")
	if err != nil || size != 2 {
		t.Fatal(err)
	}
	f.Close()
	if _, _, err := w.OpenFile("c"); err == nil {
		t.Fatal("opened a directory as a file")
	}
	if _, err := w.Stat("."); err != nil {
		t.Fatal(err)
	}
}
