package system

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderWebmailVhost(t *testing.T) {
	conf, err := renderWebmailVhost("/var/lib/roundcube", "/run/php/php8.3-fpm.sock", []WebmailHost{
		{Domain: "example.com", DocRoot: "/var/www/u/example.com/public_html"},
		{Domain: "test.org", DocRoot: "/var/www/u/test.org/public_html",
			CertPath: "/etc/le/test.org/fullchain.pem", KeyPath: "/etc/le/test.org/privkey.pem"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// One server block per host now (so each can have its own certificate).
	if !strings.Contains(conf, "server_name webmail.example.com;") ||
		!strings.Contains(conf, "server_name webmail.test.org;") {
		t.Errorf("per-host server_name not rendered as expected:\n%s", conf)
	}
	// The host without a cert serves over plain HTTP; the one with a cert gets an
	// HTTPS server block and a redirect.
	if strings.Contains(conf, "ssl_certificate     /etc/le/example.com") {
		t.Errorf("example.com should have no TLS block:\n%s", conf)
	}
	if !strings.Contains(conf, "ssl_certificate     /etc/le/test.org/fullchain.pem;") {
		t.Errorf("test.org TLS block missing:\n%s", conf)
	}
	if !strings.Contains(conf, "location /.well-known/acme-challenge/ { root /var/www/u/example.com/public_html; }") {
		t.Errorf("acme-challenge location not served from domain docroot:\n%s", conf)
	}
	if !strings.Contains(conf, "root /var/lib/roundcube;") {
		t.Errorf("root not rendered:\n%s", conf)
	}
	if !strings.Contains(conf, "fastcgi_pass unix:/run/php/php8.3-fpm.sock;") {
		t.Errorf("php socket not rendered:\n%s", conf)
	}
	// Roundcube internals must be denied.
	if !strings.Contains(conf, "location ~ ^/(config|temp|logs|bin|SQL|installer)/ { deny all; }") {
		t.Errorf("missing deny rules:\n%s", conf)
	}
}

// TestRebuildWebmailVhostRemovesWhenEmpty verifies that disabling the last
// domain removes the vhost file rather than leaving a server block with an
// empty server_name (which nginx rejects).
func TestRebuildWebmailVhostRemovesWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	confPath := filepath.Join(dir, "repanel.d", "webmail.conf")
	if err := os.MkdirAll(filepath.Dir(confPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(confPath, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws := NewWebServer("nginx", dir, dir, 8080)
	if err := ws.RebuildWebmail(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(confPath); !os.IsNotExist(err) {
		t.Errorf("webmail.conf should have been removed, stat err = %v", err)
	}
}
