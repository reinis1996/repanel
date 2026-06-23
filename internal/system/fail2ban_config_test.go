package system

import (
	"strings"
	"testing"
)

func TestRenderFail2banConfig(t *testing.T) {
	cfg := Fail2banConfig{
		Defaults: Fail2banDefaults{Bantime: "1h", Findtime: "10m", Maxretry: "5"},
		Jails: []Fail2banJailConfig{
			{Name: "sshd", Enabled: true, Maxretry: "3"},
			{Name: "nginx-http-auth", Enabled: false},
		},
	}
	out, err := renderFail2banConfig(cfg)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{
		"[DEFAULT]", "bantime = 1h", "findtime = 10m", "maxretry = 5",
		"[sshd]", "enabled = true", "[nginx-http-auth]", "enabled = false",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered config missing %q:\n%s", want, out)
		}
	}
	// Empty per-jail override must not emit the key.
	if strings.Contains(out, "[nginx-http-auth]\nenabled = false\nmaxretry") {
		t.Errorf("blank maxretry should be omitted:\n%s", out)
	}

	// Round-trip: parsing it back yields the same values.
	secs := parseINISections(out)
	if secs["DEFAULT"]["bantime"] != "1h" || secs["sshd"]["maxretry"] != "3" || secs["sshd"]["enabled"] != "true" {
		t.Errorf("round-trip mismatch: %+v", secs)
	}
}

func TestRenderFail2banConfigValidation(t *testing.T) {
	bad := []Fail2banConfig{
		{Defaults: Fail2banDefaults{Maxretry: "abc"}},
		{Defaults: Fail2banDefaults{Bantime: "1 hour"}},        // space -> invalid
		{Jails: []Fail2banJailConfig{{Name: "x", Bantime: "5z"}}}, // bad unit
		{Jails: []Fail2banJailConfig{{Name: "bad name"}}},         // bad jail name
		{Jails: []Fail2banJailConfig{{Name: "x", Maxretry: "1; rm"}}},
	}
	for i, cfg := range bad {
		if _, err := renderFail2banConfig(cfg); err == nil {
			t.Errorf("case %d: expected validation error, got none", i)
		}
	}

	// Valid time forms accepted.
	for _, v := range []string{"", "600", "10m", "1h", "1d", "1w", "-1", "1mo"} {
		if err := validTime(v); err != nil {
			t.Errorf("validTime(%q) unexpected error: %v", v, err)
		}
	}
}

func TestFilterRegexRoundTrip(t *testing.T) {
	fail := "^auth failed for .* from <HOST>$\n^bad login from <HOST>:\\d+$"
	ignore := "^ignore me <HOST>$"

	content := fail2banFilterMarker + " — custom.\n[Definition]\n" +
		formatFilterValue("failregex", fail) + formatFilterValue("ignoreregex", ignore)

	// Continuation lines must be indented so they don't read as new directives.
	for _, line := range strings.Split(content, "\n") {
		if strings.Contains(line, "bad login") && !strings.HasPrefix(line, " ") {
			t.Errorf("continuation line not indented: %q", line)
		}
	}

	gotFail, gotIgnore := parseFilterRegexes(content)
	if gotFail != fail {
		t.Errorf("failregex round-trip mismatch:\n got %q\nwant %q", gotFail, fail)
	}
	if gotIgnore != ignore {
		t.Errorf("ignoreregex round-trip mismatch:\n got %q\nwant %q", gotIgnore, ignore)
	}
}
