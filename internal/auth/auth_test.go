package auth

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestPasswordRoundTrip(t *testing.T) {
	h := NewHasher(2)
	ctx := context.Background()
	phc, err := h.Hash(ctx, "correct horse battery")
	if err != nil || !strings.HasPrefix(phc, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatal(err, phc)
	}
	if ok, err := h.Verify(ctx, "correct horse battery", phc); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if ok, _ := h.Verify(ctx, "wrong", phc); ok {
		t.Fatal("wrong password accepted")
	}
	for _, bad := range []string{"", "plain", "$argon2id$v=19$m=999999999,t=2,p=1$AAAA$AAAA", "$bcrypt$x$y$z$w"} {
		if ok, err := h.Verify(ctx, "x", bad); ok || err == nil {
			t.Errorf("%q: ok=%v err=%v", bad, ok, err)
		}
	}
}

func TestHasherBoundsConcurrency(t *testing.T) {
	h := NewHasher(1)
	h.sem <- struct{}{} // occupy the only slot
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := h.Hash(ctx, "x"); err == nil {
		t.Fatal("expected context error while saturated")
	}
}

func TestTokens(t *testing.T) {
	tok, hash, err := NewToken()
	if err != nil || len(hash) != 32 || !bytes.Equal(hash, HashToken(tok)) {
		t.Fatal(err)
	}
	tok2, _, _ := NewToken()
	if tok == tok2 {
		t.Fatal("tokens repeat")
	}
	if !CSRFValid(tok, CSRFToken(tok)) || CSRFValid(tok, CSRFToken(tok2)) || CSRFValid(tok, "") {
		t.Fatal("csrf validation wrong")
	}
	if CSRFToken(tok) == tok {
		t.Fatal("csrf must not equal session token")
	}
}
