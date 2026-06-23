package auth

import (
	"strings"
	"testing"
	"time"
)

func TestTOTPValidate(t *testing.T) {
	secret, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	step := uint64(time.Now().Unix() / 30)
	code, err := totpAt(secret, step)
	if err != nil {
		t.Fatal(err)
	}
	if !TOTPValidate(secret, code) {
		t.Error("current code should validate")
	}
	if TOTPValidate(secret, "000000") && code != "000000" {
		t.Error("an arbitrary wrong code should not validate")
	}
	if TOTPValidate(secret, "12345") {
		t.Error("a malformed (5-digit) code should not validate")
	}
}

func TestRecoveryCodes(t *testing.T) {
	codes, stored, err := GenerateRecoveryCodes(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != 10 {
		t.Fatalf("expected 10 codes, got %d", len(codes))
	}
	// A valid code is accepted once, then consumed.
	updated, ok := CheckRecoveryCode(stored, codes[3])
	if !ok {
		t.Fatal("a freshly generated code should be accepted")
	}
	if _, ok := CheckRecoveryCode(updated, codes[3]); ok {
		t.Error("a recovery code must not be reusable")
	}
	// Other codes still work against the updated blob.
	if _, ok := CheckRecoveryCode(updated, codes[4]); !ok {
		t.Error("unused codes should remain valid")
	}
	if _, ok := CheckRecoveryCode(stored, "nope-nope"); ok {
		t.Error("an unknown code should be rejected")
	}
}

func TestTOTPURI(t *testing.T) {
	uri := TOTPURI("ABC234", "alice", "RePanel")
	if !strings.HasPrefix(uri, "otpauth://totp/") || !strings.Contains(uri, "secret=ABC234") || !strings.Contains(uri, "issuer=RePanel") {
		t.Errorf("unexpected otpauth URI: %s", uri)
	}
}
