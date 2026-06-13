package system

import (
	"crypto/x509"
	"encoding/base64"
	"strings"
	"testing"
)

func TestGenerateDKIMKey(t *testing.T) {
	key, err := GenerateDKIMKey()
	if err != nil {
		t.Fatal(err)
	}
	if key.Selector != DKIMSelector {
		t.Errorf("selector = %q, want %q", key.Selector, DKIMSelector)
	}
	if !strings.HasPrefix(key.PublicTXT, "v=DKIM1; k=rsa; p=") {
		t.Fatalf("unexpected TXT prefix: %q", key.PublicTXT)
	}
	if !strings.Contains(key.PrivatePEM, "RSA PRIVATE KEY") {
		t.Fatal("private key is not PEM-encoded")
	}
	// The published key must be a parseable RSA public key.
	b64 := strings.TrimPrefix(key.PublicTXT, "v=DKIM1; k=rsa; p=")
	der, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatalf("public key base64: %v", err)
	}
	if _, err := x509.ParsePKIXPublicKey(der); err != nil {
		t.Fatalf("public key DER: %v", err)
	}
}

func TestFormatTXT(t *testing.T) {
	// Short value: single quoted string.
	if got := formatTXT("v=spf1 a mx ~all"); got != `"v=spf1 a mx ~all"` {
		t.Errorf("short TXT = %q", got)
	}
	// Already-quoted value passes through untouched.
	if got := formatTXT(`"already quoted"`); got != `"already quoted"` {
		t.Errorf("quoted TXT = %q", got)
	}
	// Long value (e.g. a DKIM key) is split into <=255-byte quoted chunks.
	long := strings.Repeat("a", 600)
	got := formatTXT(long)
	if !strings.HasPrefix(got, "( ") || !strings.HasSuffix(got, " )") {
		t.Fatalf("long TXT not parenthesised: %.20q...", got)
	}
	chunks := strings.Count(got, `"`) / 2
	if chunks != 3 { // 255 + 255 + 90
		t.Errorf("expected 3 chunks, got %d", chunks)
	}
	for _, c := range strings.Fields(strings.Trim(got, "( )")) {
		if len(strings.Trim(c, `"`)) > 255 {
			t.Errorf("chunk exceeds 255 bytes: %d", len(c))
		}
	}
}
