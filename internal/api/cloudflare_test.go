package api

import (
	"testing"

	"github.com/reinis1996/repanel/internal/models"
)

func TestCFNameMapping(t *testing.T) {
	zone := "example.com"
	cases := []struct{ label, fqdn string }{
		{"@", "example.com"},
		{"www", "www.example.com"},
		{"a.b", "a.b.example.com"},
	}
	for _, c := range cases {
		if got := cfFQDN(c.label, zone); got != c.fqdn {
			t.Errorf("cfFQDN(%q) = %q, want %q", c.label, got, c.fqdn)
		}
		if got := cfLabel(c.fqdn, zone); got != c.label {
			t.Errorf("cfLabel(%q) = %q, want %q", c.fqdn, got, c.label)
		}
	}
	// Apex FQDN with trailing dot and case differences still map to "@".
	if cfLabel("Example.com.", zone) != "@" {
		t.Error("apex with trailing dot/case should map to @")
	}
}

func TestCFRecordRoundTrip(t *testing.T) {
	zone := "example.com"

	// A record, proxied: exports proxied with automatic TTL.
	a := models.DNSRecord{Name: "@", Type: "A", Value: "1.2.3.4", TTL: 3600, Proxied: true}
	cf := repToCF(a, zone)
	if cf.Name != "example.com" || cf.Content != "1.2.3.4" || !cf.Proxied || cf.TTL != 1 {
		t.Errorf("A export wrong: %+v", cf)
	}
	back, ok := cfToRep(cf, zone)
	if !ok || back.Name != "@" || back.Value != "1.2.3.4" || !back.Proxied || back.TTL != 3600 {
		t.Errorf("A import wrong: %+v", back)
	}

	// MX: priority carried separately; trailing dot added on import, stripped on export.
	mx := models.DNSRecord{Name: "@", Type: "MX", Value: "mail.example.com.", TTL: 3600, Priority: 10}
	cfmx := repToCF(mx, zone)
	if cfmx.Content != "mail.example.com" || cfmx.Priority == nil || *cfmx.Priority != 10 {
		t.Errorf("MX export wrong: %+v (prio=%v)", cfmx, cfmx.Priority)
	}
	backmx, _ := cfToRep(cfmx, zone)
	if backmx.Value != "mail.example.com." || backmx.Priority != 10 {
		t.Errorf("MX import wrong: %+v", backmx)
	}

	// Unmanaged type (NS) is skipped on import.
	if _, ok := cfToRep(repToCF(models.DNSRecord{Name: "@", Type: "NS", Value: "ns1.x."}, zone), zone); ok {
		t.Error("NS should not be a managed (synced) type")
	}
}

func TestCFTTL(t *testing.T) {
	cases := map[int]int{0: 1, 30: 60, 3600: 3600}
	for in, want := range cases {
		if got := cfTTL(in); got != want {
			t.Errorf("cfTTL(%d) = %d, want %d", in, got, want)
		}
	}
}
