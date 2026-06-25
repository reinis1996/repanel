package system

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/reinis1996/repanel/internal/models"
)

// Domain forwarding / parked-domain redirect. When a domain has a RedirectURL
// its vhost is replaced by a pure HTTP redirect to that target (preserving the
// request path), instead of serving a document root. This backs both the
// "domain forwarding" feature and alias/parked domains in redirect mode.

// redirectVhostNginx renders an nginx redirect server block. The ACME challenge
// is still served from the docroot so the redirect host can obtain its own
// certificate. When ssl is set the HTTPS listener redirects too.
func redirectVhostNginx(d models.Domain, ssl bool, certPath, keyPath string) string {
	target := strings.TrimRight(d.RedirectURL, "/")
	code := redirectCode(d.RedirectCode)
	var b strings.Builder
	names := serverNames(d)
	fmt.Fprintf(&b, "# Managed by RePanel — redirect to %s\n", target)
	fmt.Fprintf(&b, "server {\n    listen 80;\n    listen [::]:80;\n    server_name %s;\n", names)
	fmt.Fprintf(&b, "    location /.well-known/acme-challenge/ { root %s; }\n", d.DocumentRoot)
	fmt.Fprintf(&b, "    location / { return %d %s$request_uri; }\n}\n", code, target)
	if ssl && certPath != "" {
		fmt.Fprintf(&b, "\nserver {\n    listen 443 ssl http2;\n    listen [::]:443 ssl http2;\n    server_name %s;\n", names)
		fmt.Fprintf(&b, "    ssl_certificate     %s;\n    ssl_certificate_key %s;\n", certPath, keyPath)
		fmt.Fprintf(&b, "    location / { return %d %s$request_uri; }\n}\n", code, target)
	}
	return b.String()
}

// redirectVhostApache renders an Apache redirect vhost (apache-only stack front).
func redirectVhostApache(d models.Domain, ssl bool, certPath, keyPath string) string {
	target := strings.TrimRight(d.RedirectURL, "/")
	permanent := "permanent"
	if redirectCode(d.RedirectCode) == 302 {
		permanent = "temp"
	}
	var b strings.Builder
	alias := apacheAliasLine(d)
	fmt.Fprintf(&b, "# Managed by RePanel — redirect to %s\n", target)
	fmt.Fprintf(&b, "<VirtualHost *:80>\n    ServerName %s\n%s", d.Name, alias)
	fmt.Fprintf(&b, "    Redirect %s / %s/\n</VirtualHost>\n", permanent, target)
	if ssl && certPath != "" {
		fmt.Fprintf(&b, "\n<VirtualHost *:443>\n    ServerName %s\n%s", d.Name, alias)
		fmt.Fprintf(&b, "    SSLEngine on\n    SSLCertificateFile    %s\n    SSLCertificateKeyFile %s\n", certPath, keyPath)
		fmt.Fprintf(&b, "    Redirect %s / %s/\n</VirtualHost>\n", permanent, target)
	}
	return b.String()
}

func redirectCode(c int) int {
	if c == 302 {
		return 302
	}
	return 301
}

// writeRedirectVhost writes the redirect-only vhost on the front server and
// removes the other server's file and any PHP pool.
func (ws *WebServer) writeRedirectVhost(d models.Domain, certPath, keyPath string) error {
	removePHPPool(d)
	ssl := d.SSL && certPath != ""
	if ws.frontIsApache() {
		removeNginxVhost(ws.NginxDir, d.Name)
		conf := redirectVhostApache(d, ssl, certPath, keyPath)
		if err := writeRawVhost(apacheConfDir(ws.ApacheDir), d.Name, conf); err != nil {
			return err
		}
	} else {
		removeApacheVhost(ws.ApacheDir, d.Name)
		conf := redirectVhostNginx(d, ssl, certPath, keyPath)
		if err := writeRawVhost(nginxConfDir(ws.NginxDir), d.Name, conf); err != nil {
			return err
		}
	}
	return ws.reloadAll()
}

// writeRawVhost writes a pre-rendered config file into a server's conf dir.
func writeRawVhost(confDir, name, content string) error {
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(confDir, name+".conf"), []byte(content), 0o644)
}
