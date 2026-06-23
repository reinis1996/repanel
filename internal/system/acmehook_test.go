package system

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleZone = `; Managed by RePanel — zone example.com
$TTL 3600
$ORIGIN example.com.
@ IN SOA ns1.example.com. hostmaster.example.com. ( 2024010100 3600 600 1209600 3600 )
@ IN NS ns1.example.com.
@ 3600 IN A 203.0.113.1
`

func writeZoneFixture(t *testing.T) (bindDir, zoneFile string) {
	t.Helper()
	bindDir = t.TempDir()
	zonesDir := filepath.Join(bindDir, "repanel-zones")
	if err := os.MkdirAll(zonesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	zoneFile = filepath.Join(zonesDir, "db.example.com")
	if err := os.WriteFile(zoneFile, []byte(sampleZone), 0o644); err != nil {
		t.Fatal(err)
	}
	return bindDir, zoneFile
}

func TestACMEHookAuthAndCleanup(t *testing.T) {
	bindDir, zoneFile := writeZoneFixture(t)

	// Two validations (wildcard + apex) accumulate as separate TXT records.
	if err := ACMEHookAuth(bindDir, "example.com", "token-AAA"); err != nil {
		t.Fatal(err)
	}
	if err := ACMEHookAuth(bindDir, "example.com", "token-BBB"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(zoneFile)
	s := string(got)
	if strings.Count(s, "_acme-challenge") != 2 {
		t.Fatalf("expected 2 challenge records:\n%s", s)
	}
	if !strings.Contains(s, `_acme-challenge 60 IN TXT "token-AAA"`) || !strings.Contains(s, `token-BBB`) {
		t.Fatalf("challenge records missing:\n%s", s)
	}
	if strings.Contains(s, "2024010100") {
		t.Error("SOA serial should have been bumped")
	}

	// Cleanup removes only the matching record.
	if err := ACMEHookCleanup(bindDir, "example.com", "token-AAA"); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(zoneFile)
	s = string(got)
	if strings.Contains(s, "token-AAA") {
		t.Error("token-AAA should have been removed")
	}
	if !strings.Contains(s, "token-BBB") {
		t.Error("token-BBB should remain")
	}
}

func TestACMEHookRejectsUnmanagedDomain(t *testing.T) {
	bindDir, _ := writeZoneFixture(t)
	if err := ACMEHookAuth(bindDir, "not-hosted.com", "x"); err == nil {
		t.Error("expected error for a domain with no managed zone")
	}
	if err := ACMEHookAuth(bindDir, "../escape", "x"); err == nil {
		t.Error("expected error for a path-traversal domain")
	}
}

func TestBumpZoneSerial(t *testing.T) {
	out := bumpZoneSerial(sampleZone)
	if !strings.Contains(out, "( 2024010101 ") {
		t.Errorf("serial not incremented:\n%s", out)
	}
}
