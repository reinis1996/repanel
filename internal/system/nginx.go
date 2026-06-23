package system

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/reinis1996/repanel/internal/models"
)

// This file holds the nginx half of the web server integration: the per-domain
// server-block templates and file-only writers. Orchestration (which files to
// write for a given stack/mode and when to reload) lives in webserver.go.

// nginxDirectTemplate renders an nginx server block that serves the site
// directly, passing PHP to the per-domain FPM pool. SSL section is included
// only when a certificate has been issued.
var nginxDirectTemplate = template.Must(template.New("vhost").Parse(`# Managed by RePanel — do not edit, changes will be overwritten.
server {
    listen 80;
    listen [::]:80;
    server_name {{.Name}} www.{{.Name}};

{{- if .SSL}}
    location /.well-known/acme-challenge/ { root {{.DocumentRoot}}; }
    location / { return 301 https://$host$request_uri; }
}

server {
    listen 443 ssl;
    listen [::]:443 ssl;
    http2 on;
    server_name {{.Name}} www.{{.Name}};

    ssl_certificate     {{.CertPath}};
    ssl_certificate_key {{.KeyPath}};
{{- end}}

    root {{.DocumentRoot}};
    index index.php index.html index.htm;

    access_log /var/log/nginx/{{.Name}}.access.log;
    error_log  /var/log/nginx/{{.Name}}.error.log;

    client_max_body_size 128m;
{{- if .WAFEnabled}}

    modsecurity on;
    modsecurity_rules_file {{.WAFRulesFile}};
{{- end}}
{{- if .NginxExtra}}

    # --- Custom directives (RePanel) ---
{{.NginxExtra}}
    # --- End custom directives ---
{{- end}}
{{- if .ProtectedNginx}}
{{.ProtectedNginx}}
{{- end}}

    location / {
        try_files $uri $uri/ /index.php?$query_string;
    }

    location ~ \.php$ {
        try_files $uri =404;
        include fastcgi_params;
        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
        fastcgi_pass unix:{{.PHPSock}};
    }

    location ~ /\.(?!well-known) { deny all; }
}
`))

// nginxProxyTemplate renders an nginx front that terminates HTTP(S) and reverse
// proxies to an Apache backend on 127.0.0.1:{{.BackendPort}}. When ServeStatic
// is true nginx serves static files itself and proxies only what it cannot find
// (the "nginx + Apache" mode); otherwise every request is proxied to Apache
// (the "Apache only" mode, where Apache serves both static and PHP).
var nginxProxyTemplate = template.Must(template.New("vhostproxy").Parse(`# Managed by RePanel — do not edit, changes will be overwritten.
server {
    listen 80;
    listen [::]:80;
    server_name {{.Name}} www.{{.Name}};

{{- if .SSL}}
    location /.well-known/acme-challenge/ { root {{.DocumentRoot}}; }
    location / { return 301 https://$host$request_uri; }
}

server {
    listen 443 ssl;
    listen [::]:443 ssl;
    http2 on;
    server_name {{.Name}} www.{{.Name}};

    ssl_certificate     {{.CertPath}};
    ssl_certificate_key {{.KeyPath}};
{{- end}}

    root {{.DocumentRoot}};

    access_log /var/log/nginx/{{.Name}}.access.log;
    error_log  /var/log/nginx/{{.Name}}.error.log;

    client_max_body_size 128m;
{{- if .WAFEnabled}}

    modsecurity on;
    modsecurity_rules_file {{.WAFRulesFile}};
{{- end}}
{{- if .NginxExtra}}

    # --- Custom directives (RePanel) ---
{{.NginxExtra}}
    # --- End custom directives ---
{{- end}}
{{- if .ProtectedNginx}}
{{.ProtectedNginx}}
{{- end}}

    location /.well-known/acme-challenge/ { root {{.DocumentRoot}}; }
{{- if .ServeStatic}}
    index index.php index.html index.htm;

    location / {
        try_files $uri $uri/ @backend;
    }

    location ~ \.php$ {
{{.ProxyPass}}
    }

    location @backend {
{{.ProxyPass}}
    }
{{- else}}
    location / {
{{.ProxyPass}}
    }
{{- end}}
}
`))

type vhostData struct {
	Name         string
	DocumentRoot string
	PHPVersion   string
	PoolName     string
	PHPSock      string
	SSL          bool
	CertPath     string
	KeyPath      string
	ServeStatic  bool
	BackendPort  int
	ProxyPass    string // pre-rendered proxy_pass block (nginx-apache stack)
	NginxExtra   string // admin custom directives injected into the nginx server block
	ApacheExtra  string // admin custom directives injected into the Apache vhost
	ProtectedNginx  string // pre-rendered password-protected location blocks (nginx)
	ProtectedApache string // pre-rendered password-protected directory blocks (Apache)
	WAFEnabled   bool   // inject the ModSecurity enable directives
	WAFRulesFile string // per-domain ModSecurity rules file
}

// phpPoolTemplate gives every domain an isolated PHP-FPM pool running as the
// owning system user, the same isolation model DirectAdmin uses.
var phpPoolTemplate = template.Must(template.New("pool").Parse(`; Managed by RePanel
[{{.PoolName}}]
user = {{.SysUser}}
group = {{.SysUser}}
listen = {{.PHPSock}}
listen.owner = www-data
listen.group = www-data
pm = ondemand
pm.max_children = 10
pm.process_idle_timeout = 30s
php_admin_value[open_basedir] = {{.DocumentRoot}}:/tmp
{{.PHPSettings}}
{{- if .PHPExtra}}
; --- Custom directives (RePanel) ---
{{.PHPExtra}}
{{- end}}
`))

// poolName converts a domain to a safe pool/socket identifier.
func poolName(domain string) string {
	return strings.NewReplacer(".", "_", "-", "_").Replace(domain)
}

// phpSocket returns the per-domain PHP-FPM socket path for a domain.
func phpSocket(d models.Domain) string {
	return fmt.Sprintf("/run/php/php%s-fpm-%s.sock", d.PHPVersion, poolName(d.Name))
}

// PHPSocket exposes the per-domain PHP-FPM socket path for callers outside this
// file (e.g. rendering protected-directory PHP handling).
func PHPSocket(d models.Domain) string { return phpSocket(d) }

// proxyPassBlock renders the indented proxy_pass directives (with no trailing
// newline) that send a request to the Apache backend on the loopback port.
func proxyPassBlock(backendPort int) string {
	return fmt.Sprintf("        proxy_pass http://127.0.0.1:%d;\n"+
		"        proxy_set_header Host $host;\n"+
		"        proxy_set_header X-Real-IP $remote_addr;\n"+
		"        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;\n"+
		"        proxy_set_header X-Forwarded-Proto $scheme;", backendPort)
}

// vhostDataFor builds the template data shared by the nginx and apache writers.
func vhostDataFor(d models.Domain, ssl bool, certPath, keyPath string, backendPort int, serveStatic bool) vhostData {
	return vhostData{
		Name:         d.Name,
		DocumentRoot: d.DocumentRoot,
		PHPVersion:   d.PHPVersion,
		PoolName:     poolName(d.Name),
		PHPSock:      phpSocket(d),
		SSL:          ssl,
		CertPath:     certPath,
		KeyPath:      keyPath,
		ServeStatic:  serveStatic,
		BackendPort:  backendPort,
		ProxyPass:    proxyPassBlock(backendPort),
		NginxExtra:   indentConfig(d.NginxConf, "    "),
		ApacheExtra:  indentConfig(d.ApacheConf, "    "),
		ProtectedNginx:  d.ProtectedNginx,
		ProtectedApache: d.ProtectedApache,
	}
}

// indentConfig trims trailing whitespace and prefixes each non-empty line of an
// admin override block with indent, so injected directives sit tidily inside the
// generated server/vhost block. Returns "" for blank input.
func indentConfig(s, indent string) string {
	s = strings.TrimRight(s, "\n\r \t")
	if strings.TrimSpace(s) == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		ln = strings.TrimRight(ln, "\r")
		if strings.TrimSpace(ln) == "" {
			lines[i] = ""
		} else {
			lines[i] = indent + strings.TrimLeft(ln, " \t")
		}
	}
	return strings.Join(lines, "\n")
}

// nginxConfDir is where the panel writes its per-domain nginx server blocks.
func nginxConfDir(nginxDir string) string { return filepath.Join(nginxDir, "repanel.d") }

// renderPHPPool builds the FPM pool config for a domain.
func renderPHPPool(d models.Domain, sysUser string) (string, error) {
	var pb strings.Builder
	err := phpPoolTemplate.Execute(&pb, map[string]string{
		"PoolName":     poolName(d.Name),
		"SysUser":      sysUser,
		"PHPSock":      phpSocket(d),
		"DocumentRoot": d.DocumentRoot,
		"PHPSettings":  RenderPHPSettings(d.PHPSettings),
		"PHPExtra":     indentConfig(d.PHPConf, ""),
	})
	return pb.String(), err
}

// writePHPPool writes (or refreshes) the domain's isolated PHP-FPM pool and
// reloads FPM. It is a no-op when the PHP version is not installed. When the
// owning account has cgroup limits the pool is hosted by a per-account FPM master
// (so PHP is capped by the account slice); otherwise it uses the shared master.
// See phpfpm.go.
func writePHPPool(d models.Domain, sysUser string) error {
	poolDir := fmt.Sprintf("/etc/php/%s/fpm/pool.d", d.PHPVersion)
	if st, err := os.Stat(poolDir); err != nil || !st.IsDir() {
		return nil
	}
	content, err := renderPHPPool(d, sysUser)
	if err != nil {
		return err
	}
	return applyPHPPool(d, content)
}

// phpFpmTest runs `php-fpm<ver> -t` to validate pool configuration, returning the
// first line of its complaint on failure. It is a no-op when the binary is absent.
func phpFpmTest(version string) error {
	bin := "php-fpm" + version
	if !have(bin) {
		return nil
	}
	if _, errOut, err := runCapture(30*time.Second, "", "", bin, "-t"); err != nil {
		msg := strings.TrimSpace(errOut)
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("php-fpm config test failed: %s", firstLine(msg))
	}
	return nil
}

// removePHPPool deletes the domain's PHP-FPM pool (from whichever master hosts
// it) and reloads. See phpfpm.go.
func removePHPPool(d models.Domain) {
	removePHPPoolFiles(d)
}

// writeNginxDirectVhost writes the direct (nginx serves + FPM) server block.
func writeNginxDirectVhost(nginxDir string, data vhostData) error {
	return writeNginxFile(nginxDir, data.Name, nginxDirectTemplate, data)
}

// writeNginxProxyVhost writes the reverse-proxy server block (nginx front,
// Apache backend).
func writeNginxProxyVhost(nginxDir string, data vhostData) error {
	return writeNginxFile(nginxDir, data.Name, nginxProxyTemplate, data)
}

func writeNginxFile(nginxDir, name string, tmpl *template.Template, data vhostData) error {
	confDir := nginxConfDir(nginxDir)
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		return err
	}
	var sb strings.Builder
	if err := tmpl.Execute(&sb, data); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(confDir, name+".conf"), []byte(sb.String()), 0o644)
}

// nodeVhostTemplate fronts a Node application: nginx terminates HTTP(S), serves
// the ACME challenge from the docroot, and reverse-proxies everything else to the
// app on its loopback port (the systemd unit in nodeapp.go sets PORT). WebSocket
// upgrades are passed through.
var nodeVhostTemplate = template.Must(template.New("nodevhost").Parse(`# Managed by RePanel — Node app, do not edit, changes will be overwritten.
server {
    listen 80;
    listen [::]:80;
    server_name {{.Name}} www.{{.Name}};
    location /.well-known/acme-challenge/ { root {{.DocumentRoot}}; }
{{- if .SSL}}
    location / { return 301 https://$host$request_uri; }
}

server {
    listen 443 ssl;
    listen [::]:443 ssl;
    http2 on;
    server_name {{.Name}} www.{{.Name}};

    ssl_certificate     {{.CertPath}};
    ssl_certificate_key {{.KeyPath}};

    location /.well-known/acme-challenge/ { root {{.DocumentRoot}}; }
{{- end}}

    access_log /var/log/nginx/{{.Name}}.access.log;
    error_log  /var/log/nginx/{{.Name}}.error.log;
    client_max_body_size 128m;
{{- if .WAFEnabled}}

    modsecurity on;
    modsecurity_rules_file {{.WAFRulesFile}};
{{- end}}

    location / {
        proxy_pass http://127.0.0.1:{{.Port}};
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
`))

type nodeVhostData struct {
	Name         string
	DocumentRoot string
	SSL          bool
	CertPath     string
	KeyPath      string
	Port         int
	WAFEnabled   bool
	WAFRulesFile string
}

// writeNginxNodeVhost writes the reverse-proxy server block for a Node app.
func writeNginxNodeVhost(nginxDir string, data nodeVhostData) error {
	confDir := nginxConfDir(nginxDir)
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		return err
	}
	var sb strings.Builder
	if err := nodeVhostTemplate.Execute(&sb, data); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(confDir, data.Name+".conf"), []byte(sb.String()), 0o644)
}

// writeNginxSuspended writes a 503 server block for a suspended domain.
func writeNginxSuspended(nginxDir string, d models.Domain) error {
	conf := fmt.Sprintf(`# Managed by RePanel — domain suspended
server {
    listen 80;
    listen [::]:80;
    server_name %s www.%s;
    location / { return 503; }
}
`, d.Name, d.Name)
	confDir := nginxConfDir(nginxDir)
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(confDir, d.Name+".conf"), []byte(conf), 0o644)
}

// removeNginxVhost deletes the generated nginx server block for a domain.
func removeNginxVhost(nginxDir, name string) {
	os.Remove(filepath.Join(nginxConfDir(nginxDir), name+".conf"))
}

func reloadNginx() error {
	if !have("nginx") {
		return nil
	}
	if _, err := run("nginx", "-t"); err != nil {
		return fmt.Errorf("nginx config test failed: %w", err)
	}
	return ReloadService("nginx")
}

// PHPVersions detects installed PHP-FPM versions from /etc/php.
func PHPVersions() []string {
	entries, err := os.ReadDir("/etc/php")
	if err != nil {
		return []string{"8.3"}
	}
	var versions []string
	for _, e := range entries {
		if e.IsDir() {
			if _, err := os.Stat(filepath.Join("/etc/php", e.Name(), "fpm")); err == nil {
				versions = append(versions, e.Name())
			}
		}
	}
	if len(versions) == 0 {
		versions = []string{"8.3"}
	}
	return versions
}
