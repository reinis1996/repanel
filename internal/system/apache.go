package system

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/reinis1996/repanel/internal/models"
)

// This file holds the Apache half of the web server integration. Apache runs
// in one of two shapes:
//
//   - direct  (apache-only stack): Apache owns :80/:443, terminates TLS and
//     serves the site, running PHP through the per-domain FPM pool via
//     mod_proxy_fcgi.
//   - backend (nginx-apache stack): Apache listens on 127.0.0.1:<port> over
//     plain HTTP behind nginx, which terminates TLS and proxies to it.
//
// PHP is always handled by the same per-domain FPM pool nginx uses, so a site
// behaves identically whichever server is in front of it.

var apacheDirectTemplate = template.Must(template.New("apachedirect").Parse(`# Managed by RePanel — do not edit, changes will be overwritten.
<VirtualHost *:80>
    ServerName {{.Name}}
    ServerAlias www.{{.Name}}
    DocumentRoot {{.DocumentRoot}}

    <Directory {{.DocumentRoot}}>
        Options -Indexes +FollowSymLinks
        AllowOverride All
        Require all granted
    </Directory>

    <FilesMatch \.php$>
        SetHandler "proxy:unix:{{.PHPSock}}|fcgi://localhost"
    </FilesMatch>
{{- if .SSL}}

    RewriteEngine On
    RewriteCond %{REQUEST_URI} !^/\.well-known/acme-challenge/
    RewriteRule ^ https://%{HTTP_HOST}%{REQUEST_URI} [R=301,L]
{{- end}}

    ErrorLog ${APACHE_LOG_DIR}/{{.Name}}.error.log
    CustomLog ${APACHE_LOG_DIR}/{{.Name}}.access.log combined
</VirtualHost>
{{- if .SSL}}

<VirtualHost *:443>
    ServerName {{.Name}}
    ServerAlias www.{{.Name}}
    DocumentRoot {{.DocumentRoot}}

    <Directory {{.DocumentRoot}}>
        Options -Indexes +FollowSymLinks
        AllowOverride All
        Require all granted
    </Directory>

    <FilesMatch \.php$>
        SetHandler "proxy:unix:{{.PHPSock}}|fcgi://localhost"
    </FilesMatch>

    SSLEngine on
    SSLCertificateFile    {{.CertPath}}
    SSLCertificateKeyFile {{.KeyPath}}

    ErrorLog ${APACHE_LOG_DIR}/{{.Name}}.error.log
    CustomLog ${APACHE_LOG_DIR}/{{.Name}}.access.log combined
</VirtualHost>
{{- end}}
`))

var apacheBackendTemplate = template.Must(template.New("apachebackend").Parse(`# Managed by RePanel — Apache backend behind nginx. Do not edit.
<VirtualHost 127.0.0.1:{{.BackendPort}}>
    ServerName {{.Name}}
    ServerAlias www.{{.Name}}
    DocumentRoot {{.DocumentRoot}}

    <Directory {{.DocumentRoot}}>
        Options -Indexes +FollowSymLinks
        AllowOverride All
        Require all granted
    </Directory>

    <FilesMatch \.php$>
        SetHandler "proxy:unix:{{.PHPSock}}|fcgi://localhost"
    </FilesMatch>

    RemoteIPHeader X-Forwarded-For

    ErrorLog ${APACHE_LOG_DIR}/{{.Name}}.error.log
    CustomLog ${APACHE_LOG_DIR}/{{.Name}}.access.log combined
</VirtualHost>
`))

// apacheConfDir is where the panel writes its per-domain Apache vhosts. The
// installer adds an IncludeOptional for it.
func apacheConfDir(apacheDir string) string { return filepath.Join(apacheDir, "repanel.d") }

func writeApacheFile(apacheDir, name string, tmpl *template.Template, data vhostData) error {
	confDir := apacheConfDir(apacheDir)
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		return err
	}
	var sb strings.Builder
	if err := tmpl.Execute(&sb, data); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(confDir, name+".conf"), []byte(sb.String()), 0o644)
}

// writeApacheDirectVhost writes the front-facing Apache vhost (apache-only stack).
func writeApacheDirectVhost(apacheDir string, data vhostData) error {
	return writeApacheFile(apacheDir, data.Name, apacheDirectTemplate, data)
}

// writeApacheBackendVhost writes the backend Apache vhost (nginx-apache stack).
func writeApacheBackendVhost(apacheDir string, data vhostData) error {
	return writeApacheFile(apacheDir, data.Name, apacheBackendTemplate, data)
}

// writeApacheSuspended writes a 503 vhost for a suspended domain (apache front).
func writeApacheSuspended(apacheDir string, d models.Domain) error {
	conf := fmt.Sprintf(`# Managed by RePanel — domain suspended
<VirtualHost *:80>
    ServerName %s
    ServerAlias www.%s
    Redirect 503 /
    ErrorDocument 503 "Site suspended"
</VirtualHost>
`, d.Name, d.Name)
	confDir := apacheConfDir(apacheDir)
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(confDir, d.Name+".conf"), []byte(conf), 0o644)
}

// removeApacheVhost deletes the generated Apache vhost for a domain.
func removeApacheVhost(apacheDir, name string) {
	os.Remove(filepath.Join(apacheConfDir(apacheDir), name+".conf"))
}

func reloadApache() error {
	bin := ""
	switch {
	case have("apache2ctl"):
		bin = "apache2ctl"
	case have("apachectl"):
		bin = "apachectl"
	default:
		return nil
	}
	if _, err := run(bin, "-t"); err != nil {
		return fmt.Errorf("apache config test failed: %w", err)
	}
	return ReloadService("apache2")
}

var apacheWebmailTemplate = template.Must(template.New("apachewebmail").Parse(`# Managed by RePanel — webmail (Roundcube). Regenerated from panel state.
<VirtualHost *:80>
    ServerName {{.ServerName}}
{{- range .ServerAliases}}
    ServerAlias {{.}}
{{- end}}
    DocumentRoot {{.Root}}

    <Directory {{.Root}}>
        Options -Indexes +FollowSymLinks
        AllowOverride All
        Require all granted
    </Directory>

    <FilesMatch \.php$>
        SetHandler "proxy:unix:{{.PHPSock}}|fcgi://localhost"
    </FilesMatch>

    <DirectoryMatch "^{{.Root}}/(config|temp|logs|bin|SQL|installer)/">
        Require all denied
    </DirectoryMatch>

    ErrorLog ${APACHE_LOG_DIR}/webmail.error.log
    CustomLog ${APACHE_LOG_DIR}/webmail.access.log combined
</VirtualHost>
`))

type apacheWebmailData struct {
	ServerName    string
	ServerAliases []string
	Root          string
	PHPSock       string
}

// renderApacheWebmailVhost renders the shared webmail vhost for an Apache front.
func renderApacheWebmailVhost(root, phpSock string, domains []string) (string, error) {
	hosts := make([]string, len(domains))
	for i, d := range domains {
		hosts[i] = "webmail." + d
	}
	data := apacheWebmailData{Root: root, PHPSock: phpSock}
	if len(hosts) > 0 {
		data.ServerName = hosts[0]
		data.ServerAliases = hosts[1:]
	}
	var sb strings.Builder
	err := apacheWebmailTemplate.Execute(&sb, data)
	return sb.String(), err
}
