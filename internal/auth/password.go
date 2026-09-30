// Package auth implements password hashing, session tokens and CSRF tokens.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"runtime/debug"
	"strings"
	"sync/atomic"

	"golang.org/x/crypto/argon2"
)

const (
	memoryKiB = 19 * 1024
	iters     = 2
	threads   = 1
	saltLen   = 16
	keyLen    = 32
)

// Hasher hashes and verifies Argon2id passwords, bounding concurrency because
// each operation transiently uses memoryKiB of RAM above the idle baseline.
type Hasher struct {
	sem    chan struct{}
	active atomic.Int32
}

// NewHasher allows at most maxConcurrent simultaneous operations.
func NewHasher(maxConcurrent int) *Hasher {
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	return &Hasher{sem: make(chan struct{}, maxConcurrent)}
}

func (h *Hasher) acquire(ctx context.Context) error {
	select {
	case h.sem <- struct{}{}:
		h.active.Add(1)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// release frees the slot. Argon2id allocates a fresh memoryKiB block per call
// and Go keeps that garbage resident for a long time; without this, a handful
// of logins inflates RSS by tens of MiB. When the last in-flight operation
// finishes, collect and hand the memory back to the OS immediately.
func (h *Hasher) release() {
	<-h.sem
	if h.active.Add(-1) == 0 {
		debug.FreeOSMemory()
	}
}

// Hash returns a PHC-format Argon2id hash.
func (h *Hasher) Hash(ctx context.Context, password string) (string, error) {
	if err := h.acquire(ctx); err != nil {
		return "", err
	}
	defer h.release()
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, iters, memoryKiB, threads, keyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, memoryKiB, iters, threads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// Verify checks password against a PHC hash in constant time.
func (h *Hasher) Verify(ctx context.Context, password, phc string) (bool, error) {
	var version int
	var m, t uint32
	var p uint8
	parts := strings.Split(phc, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errors.New("unsupported hash format")
	}
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, errors.New("unsupported argon2 version")
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false, errors.New("bad argon2 parameters")
	}
	// Refuse absurd parameters from a corrupted/hostile row.
	if m > 256*1024 || t > 10 || p == 0 || p > 8 {
		return false, errors.New("argon2 parameters out of range")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, err
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return false, errors.New("bad hash")
	}
	if err := h.acquire(ctx); err != nil {
		return false, err
	}
	defer h.release()
	got := argon2.IDKey([]byte(password), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
