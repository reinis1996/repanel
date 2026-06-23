package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Native RFC 6238 TOTP (30-second step, 6 digits, SHA-1 — the universal
// authenticator-app default), plus one-time recovery codes. Implemented in-house
// to avoid a new dependency.

const totpDigits = 6

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// GenerateTOTPSecret returns a fresh base32-encoded secret (160 bits).
func GenerateTOTPSecret() (string, error) {
	buf := make([]byte, 20)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return b32.EncodeToString(buf), nil
}

// totpAt computes the TOTP code for a secret at a unix time-step counter.
func totpAt(secret string, counter uint64) (string, error) {
	key, err := b32.DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return "", err
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	code := (uint32(sum[offset]&0x7f)<<24 | uint32(sum[offset+1])<<16 |
		uint32(sum[offset+2])<<8 | uint32(sum[offset+3])) % 1_000_000
	return fmt.Sprintf("%0*d", totpDigits, code), nil
}

// TOTPValidate reports whether code matches the secret within ±1 time step,
// tolerating modest clock skew. The comparison is constant-time.
func TOTPValidate(secret, code string) bool {
	code = strings.TrimSpace(code)
	if len(code) != totpDigits {
		return false
	}
	step := time.Now().Unix() / 30
	for _, c := range []uint64{uint64(step - 1), uint64(step), uint64(step + 1)} {
		want, err := totpAt(secret, c)
		if err != nil {
			return false
		}
		if subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return true
		}
	}
	return false
}

// TOTPURI builds the otpauth:// URI an authenticator app enrolls from.
func TOTPURI(secret, account, issuer string) string {
	label := url.PathEscape(issuer + ":" + account)
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", issuer)
	q.Set("digits", fmt.Sprint(totpDigits))
	q.Set("period", "30")
	return "otpauth://totp/" + label + "?" + q.Encode()
}

// GenerateRecoveryCodes returns n human-friendly one-time codes and the JSON blob
// of their hashes to persist (the plaintext is shown to the user only once).
func GenerateRecoveryCodes(n int) (codes []string, stored string, err error) {
	hashes := make([]string, 0, n)
	for i := 0; i < n; i++ {
		buf := make([]byte, 5)
		if _, err := rand.Read(buf); err != nil {
			return nil, "", err
		}
		code := strings.ToLower(hex.EncodeToString(buf)) // 10 hex chars
		codes = append(codes, code[:5]+"-"+code[5:])
		hashes = append(hashes, recoveryHash(codes[i]))
	}
	b, _ := json.Marshal(hashes)
	return codes, string(b), nil
}

func recoveryHash(code string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(strings.ToLower(code))))
	return hex.EncodeToString(sum[:])
}

// CheckRecoveryCode consumes a recovery code: if code matches one of the stored
// hashes it returns the updated JSON (with that code removed) and true.
func CheckRecoveryCode(storedJSON, code string) (updated string, ok bool) {
	var hashes []string
	if json.Unmarshal([]byte(storedJSON), &hashes) != nil {
		return storedJSON, false
	}
	want := recoveryHash(code)
	for i, h := range hashes {
		if subtle.ConstantTimeCompare([]byte(h), []byte(want)) == 1 {
			hashes = append(hashes[:i], hashes[i+1:]...)
			b, _ := json.Marshal(hashes)
			return string(b), true
		}
	}
	return storedJSON, false
}
