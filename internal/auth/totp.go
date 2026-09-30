package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TOTP (RFC 6238): SHA-1, 6 digits, 30-second steps, the form every
// authenticator app accepts.
const (
	totpPeriod = 30
	totpDigits = 6
	// TOTPSkew is how many steps either side of now are accepted (clock drift).
	TOTPSkew = 1
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewTOTPSecret returns 20 random bytes (160 bits, the RFC's recommendation).
func NewTOTPSecret() ([]byte, error) {
	b := make([]byte, 20)
	_, err := rand.Read(b)
	return b, err
}

// TOTPKey is the base32 text users type into an authenticator app.
func TOTPKey(secret []byte) string { return b32.EncodeToString(secret) }

// TOTPURI is the otpauth:// URI authenticator apps import (QR code or link).
func TOTPURI(secret []byte, issuer, account string) string {
	v := url.Values{}
	v.Set("secret", TOTPKey(secret))
	v.Set("issuer", issuer)
	v.Set("algorithm", "SHA1")
	v.Set("digits", "6")
	v.Set("period", "30")
	return "otpauth://totp/" + url.PathEscape(issuer+":"+account) + "?" + v.Encode()
}

// TOTPStep returns the time step for t.
func TOTPStep(t time.Time) int64 { return t.Unix() / totpPeriod }

// TOTPCode computes the code for one step.
func TOTPCode(secret []byte, step int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	m := hmac.New(sha1.New, secret)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	bin := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", totpDigits, bin%1_000_000)
}

// VerifyTOTP checks code against the steps around now and returns the
// matching step. Steps at or before lastUsed are refused, so a code cannot be
// replayed (the caller stores the returned step).
func VerifyTOTP(secret []byte, code string, now time.Time, lastUsed int64) (int64, bool) {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != totpDigits {
		return 0, false
	}
	cur := TOTPStep(now)
	for d := -int64(TOTPSkew); d <= TOTPSkew; d++ {
		step := cur + d
		if step <= lastUsed {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(TOTPCode(secret, step)), []byte(code)) == 1 {
			return step, true
		}
	}
	return 0, false
}

// NewRecoveryCode returns a one-use code like "k7m2-9qxd-4tzp" (60 bits).
func NewRecoveryCode() (string, error) {
	const alphabet = "abcdefghjkmnpqrstuvwxyz23456789" // no 0/o, 1/l/i
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, 0, 14)
	for i, x := range b {
		if i > 0 && i%4 == 0 {
			out = append(out, '-')
		}
		out = append(out, alphabet[int(x)%len(alphabet)])
	}
	return string(out), nil
}

// NormalizeRecoveryCode lowercases and removes spaces and dashes.
func NormalizeRecoveryCode(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.NewReplacer("-", "", " ", "").Replace(s)
}
