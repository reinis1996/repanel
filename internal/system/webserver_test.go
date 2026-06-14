package system

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reinis1996/repanel/internal/models"
)

func TestModesPerStack(t *testing.T) {
	cases := map[string][]string{
		"nginx":        {ModeNginx},
		"apache":       {ModeApache},
		"nginx-apache": {ModeNginx, ModeApache, ModeNginxApache},
		"bogus":        {ModeNginx}, // unknown stacks fall back to nginx
	}
	for stack, want := range cases {
		ws := NewWebServer(stack, "n", "a", 8080)
		got := ws.Modes()
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("stack %q: modes = %v, want %v", stack, got, want)
		}
		if ws.DefaultMode() != want[0] {
			t.Errorf("stack %q: default = %q, want %q", stack, ws.DefaultMode(), want[0])
		}
	}
}

func TestNormalizeModeFallback(t *testing.T) {
	ws := NewWebServer("nginx-apache", "n", "a", 8080)
	if m := ws.NormalizeMode("apache"); m != ModeApache {
		t.Errorf("valid mode coerced: %q", m)
	}
	if m := ws.NormalizeMode(""); m != ModeNginx {
		t.Errorf("empty mode should default to nginx, got %q", m)
	}
	if m := ws.NormalizeMode("garbage"); m != ModeNginx {
		t.Errorf("invalid mode should default to nginx, got %q", m)
	}
	// A single-server stack only ever yields its one mode.
	apacheOnly := NewWebServer("apache", "n", "a", 8080)
	if m := apacheOnly.NormalizeMode("nginx"); m != ModeApache {
		t.Errorf("apache stack should coerce to apache, got %q", m)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// In the combined stack, the "apache" mode proxies everything to an Apache
// backend, so it writes both an nginx proxy vhost and a plain-HTTP Apache
// backend vhost.
func TestWriteVhostCombinedApacheMode(t *testing.T) {
	nginxDir, apacheDir := t.TempDir(), t.TempDir()
	ws := NewWebServer("nginx-apache", nginxDir, apacheDir, 8081)
	d := models.Domain{Name: "example.com", DocumentRoot: "/var/www/u/example.com/public_html", PHPVersion: "8.3"}

	if err := ws.WriteVhost(d, "u", "", "", ModeApache); err != nil {
		t.Fatal(err)
	}
	nginxConf := read(t, filepath.Join(nginxDir, "repanel.d", "example.com.conf"))
	if !strings.Contains(nginxConf, "proxy_pass http://127.0.0.1:8081;") {
		t.Errorf("nginx vhost should proxy to backend:\n%s", nginxConf)
	}
	if strings.Contains(nginxConf, "try_files $uri $uri/ @backend") {
		t.Errorf("apache mode should proxy everything, not serve static:\n%s", nginxConf)
	}
	apacheConf := read(t, filepath.Join(apacheDir, "repanel.d", "example.com.conf"))
	if !strings.Contains(apacheConf, "<VirtualHost 127.0.0.1:8081>") {
		t.Errorf("apache backend should listen on the backend port, no TLS:\n%s", apacheConf)
	}
	if strings.Contains(apacheConf, "SSLEngine") {
		t.Errorf("apache backend must not terminate TLS:\n%s", apacheConf)
	}
}

// The "nginx-apache" mode serves static from nginx and proxies PHP to Apache.
func TestWriteVhostCombinedBothMode(t *testing.T) {
	nginxDir, apacheDir := t.TempDir(), t.TempDir()
	ws := NewWebServer("nginx-apache", nginxDir, apacheDir, 8080)
	d := models.Domain{Name: "site.test", DocumentRoot: "/srv/site", PHPVersion: "8.3"}

	if err := ws.WriteVhost(d, "u", "", "", ModeNginxApache); err != nil {
		t.Fatal(err)
	}
	nginxConf := read(t, filepath.Join(nginxDir, "repanel.d", "site.test.conf"))
	if !strings.Contains(nginxConf, "try_files $uri $uri/ @backend;") {
		t.Errorf("both mode should serve static via try_files:\n%s", nginxConf)
	}
	if !exists(filepath.Join(apacheDir, "repanel.d", "site.test.conf")) {
		t.Errorf("both mode should write an apache backend vhost")
	}
}

// Switching a domain from a proxied mode back to nginx-direct must remove the
// now-stale Apache backend file.
func TestWriteVhostSwitchRemovesStaleApache(t *testing.T) {
	nginxDir, apacheDir := t.TempDir(), t.TempDir()
	ws := NewWebServer("nginx-apache", nginxDir, apacheDir, 8080)
	d := models.Domain{Name: "switch.test", DocumentRoot: "/srv/x", PHPVersion: "8.3"}

	if err := ws.WriteVhost(d, "u", "", "", ModeApache); err != nil {
		t.Fatal(err)
	}
	apacheConf := filepath.Join(apacheDir, "repanel.d", "switch.test.conf")
	if !exists(apacheConf) {
		t.Fatal("apache backend vhost should exist after apache mode")
	}
	if err := ws.WriteVhost(d, "u", "", "", ModeNginx); err != nil {
		t.Fatal(err)
	}
	if exists(apacheConf) {
		t.Errorf("apache backend vhost should be removed after switching to nginx mode")
	}
	nginxConf := read(t, filepath.Join(nginxDir, "repanel.d", "switch.test.conf"))
	if !strings.Contains(nginxConf, "fastcgi_pass unix:/run/php/php8.3-fpm-switch_test.sock;") {
		t.Errorf("nginx-direct vhost should pass PHP to the per-domain FPM socket:\n%s", nginxConf)
	}
}

// The apache-only stack writes a front-facing Apache vhost and no nginx file.
func TestWriteVhostApacheStack(t *testing.T) {
	nginxDir, apacheDir := t.TempDir(), t.TempDir()
	ws := NewWebServer("apache", nginxDir, apacheDir, 8080)
	d := models.Domain{Name: "a.test", DocumentRoot: "/srv/a", PHPVersion: "8.3", SSL: true}

	if err := ws.WriteVhost(d, "u", "/c/cert.pem", "/c/key.pem", ModeApache); err != nil {
		t.Fatal(err)
	}
	apacheConf := read(t, filepath.Join(apacheDir, "repanel.d", "a.test.conf"))
	if !strings.Contains(apacheConf, "<VirtualHost *:80>") || !strings.Contains(apacheConf, "<VirtualHost *:443>") {
		t.Errorf("apache front should listen on :80 and :443:\n%s", apacheConf)
	}
	if !strings.Contains(apacheConf, "SSLCertificateFile    /c/cert.pem") {
		t.Errorf("apache front should reference the cert:\n%s", apacheConf)
	}
	if exists(filepath.Join(nginxDir, "repanel.d", "a.test.conf")) {
		t.Errorf("apache stack must not write an nginx vhost")
	}
	if ws.AccessLogDir() != "/var/log/apache2" {
		t.Errorf("apache stack log dir = %q", ws.AccessLogDir())
	}
}
