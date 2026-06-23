package system

import (
	"strings"
	"testing"

	"github.com/reinis1996/repanel/internal/models"
)

func TestValidWAFMode(t *testing.T) {
	for _, m := range []string{"on", "detection"} {
		if !validWAFMode(m) {
			t.Errorf("%q should be valid", m)
		}
	}
	for _, m := range []string{"", "On", "block", "off"} {
		if validWAFMode(m) {
			t.Errorf("%q should be rejected", m)
		}
	}
}

func TestWAFEngineMode(t *testing.T) {
	if wafEngineMode("on") != "On" {
		t.Error("on -> On")
	}
	if wafEngineMode("detection") != "DetectionOnly" {
		t.Error("detection -> DetectionOnly")
	}
	if wafEngineMode("anything-else") != "On" {
		t.Error("unknown mode should default to blocking (On)")
	}
}

// When the WAF is enabled the generated vhost must carry the connector directives;
// when it is not, they must be absent.
func TestVhostWAFInjection(t *testing.T) {
	d := models.Domain{Name: "example.com", DocumentRoot: "/srv/x", PHPVersion: "8.3"}

	on := vhostDataFor(d, false, "", "", 8080, false)
	on.WAFEnabled = true
	on.WAFRulesFile = "/etc/repanel/modsec/example.com.conf"

	var nginxOut strings.Builder
	if err := nginxDirectTemplate.Execute(&nginxOut, on); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(nginxOut.String(), "modsecurity on;") ||
		!strings.Contains(nginxOut.String(), "modsecurity_rules_file /etc/repanel/modsec/example.com.conf;") {
		t.Fatalf("nginx vhost missing WAF directives:\n%s", nginxOut.String())
	}

	var apacheOut strings.Builder
	if err := apacheDirectTemplate.Execute(&apacheOut, on); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(apacheOut.String(), "<IfModule security2_module>") ||
		!strings.Contains(apacheOut.String(), "Include /etc/repanel/modsec/example.com.conf") {
		t.Fatalf("apache vhost missing WAF directives:\n%s", apacheOut.String())
	}

	// Disabled: no ModSecurity directives leak into the config.
	off := vhostDataFor(d, false, "", "", 8080, false)
	var offOut strings.Builder
	nginxDirectTemplate.Execute(&offOut, off)
	if strings.Contains(offOut.String(), "modsecurity") {
		t.Errorf("disabled WAF should emit no directives:\n%s", offOut.String())
	}
}
