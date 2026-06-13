package auth

import (
	"strings"
	"testing"
)

func TestNewAPIToken(t *testing.T) {
	token, hash, prefix, err := NewAPIToken()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(token, TokenPrefix) {
		t.Fatalf("token %q missing prefix %q", token, TokenPrefix)
	}
	if prefix != token[:len(TokenPrefix)+8] {
		t.Fatalf("display prefix %q is not a prefix of the token", prefix)
	}
	if hash == token || hash != HashToken(token) {
		t.Fatalf("hash should be a stable SHA-256 of the token, not the token itself")
	}

	// Two tokens must differ (random) and hash deterministically.
	token2, _, _, _ := NewAPIToken()
	if token2 == token {
		t.Fatal("two generated tokens collided")
	}
	if HashToken(token) != hash {
		t.Fatal("HashToken is not deterministic")
	}
}
