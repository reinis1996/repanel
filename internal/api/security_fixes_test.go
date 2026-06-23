package api

import (
	"net/http"
	"testing"
	"time"
)

// F-06: mail local parts that could traverse the maildir path must be rejected.
func TestValidMailLocalPart(t *testing.T) {
	bad := []string{"../../etc", "a/b", "..", ".hidden", "trailing.", "a b", "a:b", ""}
	for _, l := range bad {
		if validMailLocalPart(l) {
			t.Errorf("local part %q should be rejected", l)
		}
	}
	for _, l := range []string{"john", "john.doe", "a_b", "user+tag", "abc123"} {
		if !validMailLocalPart(l) {
			t.Errorf("local part %q should be accepted", l)
		}
	}
}

// F-18: read-only tokens may only use safe HTTP methods.
func TestSafeMethod(t *testing.T) {
	for _, m := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		if !safeMethod(m) {
			t.Errorf("%s should be safe", m)
		}
	}
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		if safeMethod(m) {
			t.Errorf("%s should be unsafe", m)
		}
	}
}

// DNS-related settings rendered into zone files must reject control characters
// (newline injection into the SOA/NS section) and obviously malformed values.
func TestSettingError(t *testing.T) {
	bad := map[string]string{
		"ns1":         "ns1.example.com\n@ IN NS evil.com.",
		"admin_email": "admin@example.com\ninjected",
		"server_ip":   "1.2.3.4\n",
		"ns2":         "not a hostname",
		"server_ip2":  "", // placeholder, replaced below
	}
	delete(bad, "server_ip2")
	bad["server_ip"] = "999.999.999.999"
	bad["admin_email"] = "no-at-sign"
	for k, v := range bad {
		if settingError(k, v) == "" {
			t.Errorf("settingError(%q, %q) should reject", k, v)
		}
	}
	// Newlines are rejected for every key, even ones without a format check.
	if settingError("panel_hostname", "host\nname") == "" {
		t.Error("control characters must be rejected for all settings")
	}
	good := map[string]string{
		"ns1":             "ns1.example.com",
		"ns2":             "ns2.example.com.", // trailing dot tolerated
		"server_ip":       "203.0.113.10",
		"admin_email":     "admin@example.com",
		"panel_hostname":  "panel.example.com",
		"backup_schedule": "daily",
	}
	for k, v := range good {
		if msg := settingError(k, v); msg != "" {
			t.Errorf("settingError(%q, %q) should accept, got %q", k, v, msg)
		}
	}
}

// F-08: the login limiter locks out after the configured number of failures.
func TestLoginLimiter(t *testing.T) {
	l := newLoginLimiter(3, time.Minute)
	ip := "203.0.113.7"
	for i := 0; i < 3; i++ {
		if ok, _ := l.allowed(ip); !ok {
			t.Fatalf("attempt %d should be allowed", i)
		}
		l.fail(ip)
	}
	if ok, retry := l.allowed(ip); ok || retry <= 0 {
		t.Errorf("should be locked out after 3 failures (ok=%v retry=%v)", ok, retry)
	}
	l.success(ip) // a valid login clears the counter
	if ok, _ := l.allowed(ip); !ok {
		t.Errorf("counter should reset after success")
	}
}
