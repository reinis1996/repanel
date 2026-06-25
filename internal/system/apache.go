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
{{- if .AliasLine}}
    ServerAlias {{.AliasLine}}
{{- end}}
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
{{- if .WAFEnabled}}

    <IfModule security2_module>
        Include {{.WAFRulesFile}}
    </IfModule>
{{- end}}
{{- if .ApacheExtra}}

    # --- Custom directives (RePanel) ---
{{.ApacheExtra}}
    # --- End custom directives ---
{{- end}}
{{- if .ProtectedApache}}
{{.ProtectedApache}}
{{- end}}

    ErrorLog ${APACHE_LOG_DIR}/{{.Name}}.error.log
    CustomLog ${APACHE_LOG_DIR}/{{.Name}}.access.log combined
</VirtualHost>
{{- if .SSL}}

<VirtualHost *:443>
    ServerName {{.Name}}
{{- if .AliasLine}}
    ServerAlias {{.AliasLine}}
{{- end}}
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
{{- if .WAFEnabled}}

    <IfModule security2_module>
        Include {{.WAFRulesFile}}
    </IfModule>
{{- end}}
{{- if .ApacheExtra}}

    # --- Custom directives (RePanel) ---
{{.ApacheExtra}}
    # --- End custom directives ---
{{- end}}
{{- if .ProtectedApache}}
{{.ProtectedApache}}
{{- end}}

    ErrorLog ${APACHE_LOG_DIR}/{{.Name}}.error.log
    CustomLog ${APACHE_LOG_DIR}/{{.Name}}.access.log combined
</VirtualHost>
{{- end}}
`))

var apacheBackendTemplate = template.Must(template.New("apachebackend").Parse(`# Managed by RePanel — Apache backend behind nginx. Do not edit.
<VirtualHost 127.0.0.1:{{.BackendPort}}>
    ServerName {{.Name}}
{{- if .AliasLine}}
    ServerAlias {{.AliasLine}}
{{- end}}
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
{{- if .WAFEnabled}}

    <IfModule security2_module>
        Include {{.WAFRulesFile}}
    </IfModule>
{{- end}}
{{- if .ApacheExtra}}

    # --- Custom directives (RePanel) ---
{{.ApacheExtra}}
    # --- End custom directives ---
{{- end}}
{{- if .ProtectedApache}}
{{.ProtectedApache}}
{{- end}}

    ErrorLog ${APACHE_LOG_DIR}/{{.Name}}.error.log
    CustomLog ${APACHE_LOG_DIR}/{{.Name}}.access.log combined
</VirtualHost>
`))

// apacheAliasLine renders the indented "ServerAlias ..." directive (with a
// trailing newline) for a domain's alternative hostnames, or "" when it has
// none. Used by the hand-built (non-template) Apache vhosts.
func apacheAliasLine(d models.Domain) string {
	if len(d.Aliases) == 0 {
		return ""
	}
	return "    ServerAlias " + strings.Join(d.Aliases, " ") + "\n"
}

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
%s    Redirect 503 /
    ErrorDocument 503 "Site suspended"
</VirtualHost>
`, d.Name, apacheAliasLine(d))
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

// One vhost per enabled domain so each webmail.<domain> can present its own
// certificate. The :80 vhost always serves the ACME challenge from the parent
// domain's docroot; with a certificate it redirects to HTTPS, otherwise it
// serves Roundcube directly.
var apacheWebmailTemplate = template.Must(template.New("apachewebmail").Parse(`# Managed by RePanel — webmail (Roundcube). Regenerated from panel state.
{{- $root := .Root}}{{- $php := .PHPSock}}
{{- range .Hosts}}
<VirtualHost *:80>
    ServerName webmail.{{.Domain}}

    Alias "/.well-known/acme-challenge" "{{.DocRoot}}/.well-known/acme-challenge"
    <Directory "{{.DocRoot}}/.well-known/acme-challenge">
        Require all granted
    </Directory>
{{- if .CertPath}}
    RewriteEngine On
    RewriteCond %{REQUEST_URI} !^/\.well-known/acme-challenge/
    RewriteRule ^ https://%{HTTP_HOST}%{REQUEST_URI} [R=301,L]
</VirtualHost>

<VirtualHost *:443>
    ServerName webmail.{{.Domain}}

    SSLEngine on
    SSLCertificateFile {{.CertPath}}
    SSLCertificateKeyFile {{.KeyPath}}
{{- end}}

    DocumentRoot {{$root}}

    <Directory {{$root}}>
        Options -Indexes +FollowSymLinks
        AllowOverride All
        Require all granted
    </Directory>

    <FilesMatch \.php$>
        SetHandler "proxy:unix:{{$php}}|fcgi://localhost"
    </FilesMatch>

    <DirectoryMatch "^{{$root}}/(config|temp|logs|bin|SQL|installer)/">
        Require all denied
    </DirectoryMatch>

    ErrorLog ${APACHE_LOG_DIR}/webmail.error.log
    CustomLog ${APACHE_LOG_DIR}/webmail.access.log combined
</VirtualHost>
{{- end}}
`))

type apacheWebmailData struct {
	Root    string
	PHPSock string
	Hosts   []WebmailHost
}

// renderApacheWebmailVhost renders one webmail vhost per enabled domain.
func renderApacheWebmailVhost(root, phpSock string, hosts []WebmailHost) (string, error) {
	var sb strings.Builder
	err := apacheWebmailTemplate.Execute(&sb, apacheWebmailData{Root: root, PHPSock: phpSock, Hosts: hosts})
	return sb.String(), err
}
