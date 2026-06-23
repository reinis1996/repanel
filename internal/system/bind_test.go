package system

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reinis1996/repanel/internal/models"
)

func TestParseSlaveIPs(t *testing.T) {
	got := ParseSlaveIPs("198.51.100.2, 203.0.113.9 not-an-ip\t2001:db8::1")
	want := []string{"198.51.100.2", "203.0.113.9", "2001:db8::1"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("ParseSlaveIPs = %v, want %v", got, want)
	}
	if len(ParseSlaveIPs("")) != 0 {
		t.Error("empty string should yield no IPs")
	}
}

func TestWriteZoneSecondaryNSAndTransfer(t *testing.T) {
	dir := t.TempDir()
	zone := models.DNSZone{
		ID:   1,
		Name: "example.com",
		Records: []models.DNSRecord{
			{Name: "@", Type: "A", Value: "203.0.113.10", TTL: 3600},
		},
	}
	slaves := []string{"198.51.100.2", "203.0.113.9"}
	if err := WriteZone(dir, zone, "ns1.example.com.", "ns2.example.com.", "hostmaster.example.com.", slaves); err != nil {
		t.Fatal(err)
	}

	zoneData, err := os.ReadFile(filepath.Join(dir, "repanel-zones", "db.example.com"))
	if err != nil {
		t.Fatal(err)
	}
	zs := string(zoneData)
	if !strings.Contains(zs, "@ IN NS ns1.example.com.") {
		t.Errorf("primary NS missing:\n%s", zs)
	}
	if !strings.Contains(zs, "@ IN NS ns2.example.com.") {
		t.Errorf("secondary NS missing:\n%s", zs)
	}

	confData, err := os.ReadFile(filepath.Join(dir, "named.conf.repanel"))
	if err != nil {
		t.Fatal(err)
	}
	cs := string(confData)
	if !strings.Contains(cs, `allow-transfer { 198.51.100.2; 203.0.113.9; }`) {
		t.Errorf("allow-transfer missing/wrong:\n%s", cs)
	}
	if !strings.Contains(cs, `also-notify { 198.51.100.2; 203.0.113.9; }`) {
		t.Errorf("also-notify missing/wrong:\n%s", cs)
	}
}

// TestWriteZoneNoSlavesPlain verifies the named.conf block stays minimal when
// no secondaries are configured (no transfer clause leaks in).
func TestWriteZoneNoSlavesPlain(t *testing.T) {
	dir := t.TempDir()
	zone := models.DNSZone{ID: 1, Name: "plain.test"}
	if err := WriteZone(dir, zone, "", "", "", nil); err != nil {
		t.Fatal(err)
	}
	conf, err := os.ReadFile(filepath.Join(dir, "named.conf.repanel"))
	if err != nil {
		t.Fatal(err)
	}
	cs := string(conf)
	if strings.Contains(cs, "allow-transfer") || strings.Contains(cs, "also-notify") {
		t.Errorf("transfer clause should be absent:\n%s", cs)
	}
	if !strings.Contains(cs, `zone "plain.test" { type master; file "`) {
		t.Errorf("zone block missing:\n%s", cs)
	}
}
