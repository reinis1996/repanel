package system

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderWebmailVhost(t *testing.T) {
	conf, err := renderWebmailVhost("/var/lib/roundcube", "/run/php/php8.3-fpm.sock",
		[]string{"example.com", "test.org"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(conf, "server_name webmail.example.com webmail.test.org;") {
		t.Errorf("server_name not rendered as expected:\n%s", conf)
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
	if err := RebuildWebmailVhost(dir, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(confPath); !os.IsNotExist(err) {
		t.Errorf("webmail.conf should have been removed, stat err = %v", err)
	}
}
