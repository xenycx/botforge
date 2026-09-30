package oplog

import (
	"bytes"
	"strings"
	"testing"
)

const id = "0b5e0b2c-1c1a-4f3b-9c55-8d7c1a2b3c4d"

func TestLiveReadsAreIncremental(t *testing.T) {
	s, err := New(t.TempDir(), 16)
	if err != nil {
		t.Fatal(err)
	}
	w, _ := s.Open(id)
	w.Write([]byte("hello "))
	c, err := s.Read(id, 0, 0)
	if err != nil || string(c.Data) != "hello " || !c.Live || c.Next != 6 {
		t.Fatalf("first read: %+v %v", c, err)
	}
	w.Write([]byte("world"))
	c, _ = s.Read(id, c.Next, 0)
	if string(c.Data) != "world" || c.Truncated || c.Next != 11 {
		t.Fatalf("second read: %+v", c)
	}
}

func TestRingKeepsTheTailAndReportsTruncation(t *testing.T) {
	s, _ := New(t.TempDir(), 8)
	w, _ := s.Open(id)
	for _, p := range []string{"abc", "defg", "hijkl", "m"} {
		w.Write([]byte(p))
	}
	c, _ := s.Read(id, 0, 0)
	if string(c.Data) != "fghijklm" || !c.Truncated || c.Total != 13 || c.Next != 13 {
		t.Fatalf("ring: %+v %q", c, c.Data)
	}
	w.Write([]byte(strings.Repeat("z", 20))) // larger than the ring
	c, _ = s.Read(id, 13, 0)
	if !bytes.Equal(c.Data, []byte("zzzzzzzz")) || !c.Truncated {
		t.Fatalf("oversized write: %+v", c)
	}
}

func TestClosedLogPersistsWithOffsets(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir, 8)
	w, _ := s.Open(id)
	w.Write([]byte("0123456789AB"))
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	s2, _ := New(dir, 8) // a restarted panel reads the same file
	c, err := s2.Read(id, 6, 3)
	if err != nil || string(c.Data) != "6789"[:3] || c.Live || c.Next != 9 || c.Total != 12 {
		t.Fatalf("persisted: %+v %v", c, err)
	}
	if c, _ := s2.Read(id, 0, 0); !c.Truncated || string(c.Data) != "456789AB" {
		t.Fatalf("persisted tail: %+v %q", c, c.Data)
	}
	s2.Sweep(map[string]bool{})
	if _, err := s2.Read(id, 0, 0); err != ErrUnknown {
		t.Fatalf("sweep: %v", err)
	}
}

func TestRejectsPathLikeIDs(t *testing.T) {
	s, _ := New(t.TempDir(), 8)
	if _, err := s.Open("../../etc/passwd"); err == nil {
		t.Fatal("accepted a path as an id")
	}
	if _, err := s.Read("../x", 0, 0); err == nil {
		t.Fatal("read accepted a path")
	}
}
