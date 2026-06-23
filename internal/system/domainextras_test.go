package system

import (
	"strings"
	"testing"

	"github.com/reinis1996/repanel/internal/models"
)

func TestSanitizePHPSettings(t *testing.T) {
	// Valid values pass through; junk/injection attempts are dropped to "".
	in := models.PHPSettings{
		MemoryLimit:       "256M",
		UploadMaxFilesize: "10g",                 // lowercased + accepted
		PostMaxSize:       "lol; evil = 1",       // rejected
		MaxExecutionTime:  "30",                  // accepted
		MaxInputTime:      "99999",               // out of range -> dropped
		MaxInputVars:      "1000",                // accepted
		DisableFunctions:  "exec, system; rm -rf", // sanitized to allowed chars only -> rejected (has ';')
	}
	clean, raw := SanitizePHPSettings(in)
	if clean.MemoryLimit != "256M" {
		t.Errorf("memory_limit = %q, want 256M", clean.MemoryLimit)
	}
	if clean.UploadMaxFilesize != "10G" {
		t.Errorf("upload_max_filesize = %q, want 10G", clean.UploadMaxFilesize)
	}
	if clean.PostMaxSize != "" {
		t.Errorf("post_max_size should be rejected, got %q", clean.PostMaxSize)
	}
	if clean.MaxInputTime != "" {
		t.Errorf("max_input_time out of range should be dropped, got %q", clean.MaxInputTime)
	}
	if clean.DisableFunctions != "" {
		t.Errorf("disable_functions with ';' should be rejected, got %q", clean.DisableFunctions)
	}
	// The rendered pool lines must never contain a stray semicolon from input
	// (which could break out of the value into another directive).
	lines := RenderPHPSettings(raw)
	if strings.Contains(lines, ";") || strings.Contains(lines, "evil") {
		t.Errorf("rendered settings leaked unsafe input: %q", lines)
	}
}

func TestRenderPHPSettingsEmptyKeepsDefaults(t *testing.T) {
	// A never-touched domain (blank JSON) must keep the historical 128M ceiling
	// and must NOT emit allow_url_fopen/display_errors (which would override the
	// PHP default).
	got := RenderPHPSettings("")
	if !strings.Contains(got, "upload_max_filesize] = 128M") {
		t.Errorf("missing default upload size: %q", got)
	}
	if strings.Contains(got, "allow_url_fopen") || strings.Contains(got, "display_errors") {
		t.Errorf("blank settings should not emit flag overrides: %q", got)
	}
}

func TestNormalizeProtectedPath(t *testing.T) {
	cases := map[string]string{
		"/admin":      "/admin",
		"admin":       "/admin",
		"/admin/":     "/admin",
		"/":           "",   // whole-site protection rejected (nginx location clash)
		"/a/../b":     "",   // traversal
		"/a b":        "",   // space
		"/a;rm":       "",   // injection char
		"":            "",
	}
	for in, want := range cases {
		if got := NormalizeProtectedPath(in); got != want {
			t.Errorf("NormalizeProtectedPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRenderProtectedNginxSkipsDisabled(t *testing.T) {
	dirs := []ProtectedSpec{
		{ID: 1, Path: "/admin", Realm: "Staff", Disabled: false},
		{ID: 2, Path: "/secret", Realm: "x", Disabled: true},
	}
	got := RenderProtectedNginx("example.com", "", dirs)
	if !strings.Contains(got, "location /admin/") || !strings.Contains(got, "auth_basic") {
		t.Errorf("expected auth block for /admin, got %q", got)
	}
	if strings.Contains(got, "/secret") {
		t.Errorf("disabled dir should be skipped, got %q", got)
	}
}

func TestRenderProtectedNginxProtectsPHP(t *testing.T) {
	dirs := []ProtectedSpec{{ID: 1, Path: "/admin", Realm: "Staff"}}
	// With a PHP socket the prefix must use ^~ (so the global \.php$ regex can't
	// bypass auth) and nest an authed fastcgi location for PHP under the dir.
	got := RenderProtectedNginx("example.com", "/run/php/php8.3-fpm-example_com.sock", dirs)
	if !strings.Contains(got, "location ^~ /admin/") {
		t.Errorf("expected ^~ prefix to stop regex bypass, got %q", got)
	}
	if !strings.Contains(got, "location ~ \\.php$") || !strings.Contains(got, "fastcgi_pass unix:/run/php/php8.3-fpm-example_com.sock") {
		t.Errorf("expected nested authed PHP location, got %q", got)
	}
	// The PHP location must itself carry auth (count auth_basic_user_file twice).
	if strings.Count(got, "auth_basic_user_file") < 2 {
		t.Errorf("PHP sub-location must re-apply auth, got %q", got)
	}
}
