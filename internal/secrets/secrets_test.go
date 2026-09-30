package secrets

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func ring(t *testing.T) *Keyring {
	t.Helper()
	k, err := NewKeyring("a", map[string][]byte{"a": bytes.Repeat([]byte{1}, 32), "b": bytes.Repeat([]byte{2}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestRoundTripAndFreshNonce(t *testing.T) {
	k := ring(t)
	s1, _ := k.Seal("bot", "TOKEN", []byte("secret"))
	s2, _ := k.Seal("bot", "TOKEN", []byte("secret"))
	if bytes.Equal(s1.Nonce, s2.Nonce) || bytes.Equal(s1.Ciphertext, s2.Ciphertext) {
		t.Fatal("nonce reuse")
	}
	if bytes.Contains(s1.Ciphertext, []byte("secret")) || len(s1.Nonce) != 12 {
		t.Fatal("bad ciphertext")
	}
	pt, err := k.Open("bot", "TOKEN", s1)
	if err != nil || string(pt) != "secret" {
		t.Fatal(err, pt)
	}
}

func TestAADBindsContext(t *testing.T) {
	k := ring(t)
	s, _ := k.Seal("bot", "TOKEN", []byte("v"))
	if _, err := k.Open("other", "TOKEN", s); err == nil {
		t.Fatal("bot id not bound")
	}
	if _, err := k.Open("bot", "OTHER", s); err == nil {
		t.Fatal("name not bound")
	}
	// ambiguity: ("ab","c") must differ from ("a","bc")
	s2, _ := k.Seal("ab", "c", []byte("v"))
	if _, err := k.Open("a", "bc", s2); err == nil {
		t.Fatal("aad ambiguous")
	}
	s.Ciphertext[0] ^= 1
	if _, err := k.Open("bot", "TOKEN", s); err == nil {
		t.Fatal("tamper undetected")
	}
}

func TestRotationKeepsOldKeys(t *testing.T) {
	old := ring(t)
	s, _ := old.Seal("bot", "N", []byte("v"))
	rotated, _ := NewKeyring("b", map[string][]byte{"a": bytes.Repeat([]byte{1}, 32), "b": bytes.Repeat([]byte{2}, 32)})
	if pt, err := rotated.Open("bot", "N", s); err != nil || string(pt) != "v" {
		t.Fatal(err)
	}
	s2, _ := rotated.Seal("bot", "N", []byte("v"))
	if s2.KeyID != "b" {
		t.Fatal("not using active key")
	}
	missing, _ := NewKeyring("b", map[string][]byte{"b": bytes.Repeat([]byte{2}, 32)})
	if _, err := missing.Open("bot", "N", s); err == nil {
		t.Fatal("expected missing key error")
	}
}

func TestLoadDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "keys")
	if _, err := LoadDir(dir, "k1", false); err == nil {
		t.Fatal("expected error without keys in production mode")
	}
	k, err := LoadDir(dir, "k1", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := k.Seal("b", "n", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := GenerateKeyFile(dir, "k1"); err == nil {
		t.Fatal("must not overwrite key")
	}
	os.Chmod(filepath.Join(dir, "k1.key"), 0o644)
	if _, err := LoadDir(dir, "k1", false); err == nil {
		t.Fatal("permissive key file accepted")
	}
}
