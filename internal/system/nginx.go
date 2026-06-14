package system

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

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
{{- define "ppass" -}}
        proxy_pass http://127.0.0.1:{{.BackendPort}};
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
{{- end -}}
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

    location /.well-known/acme-challenge/ { root {{.DocumentRoot}}; }
{{- if .ServeStatic}}
    index index.php index.html index.htm;

    location / {
        try_files $uri $uri/ @backend;
    }

    location ~ \.php$ {
{{template "ppass" .}}
    }

    location @backend {
{{template "ppass" .}}
    }
{{- else}}
    location / {
{{template "ppass" .}}
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
php_admin_value[upload_max_filesize] = 128M
php_admin_value[post_max_size] = 128M
`))

// poolName converts a domain to a safe pool/socket identifier.
func poolName(domain string) string {
	return strings.NewReplacer(".", "_", "-", "_").Replace(domain)
}

// phpSocket returns the per-domain PHP-FPM socket path for a domain.
func phpSocket(d models.Domain) string {
	return fmt.Sprintf("/run/php/php%s-fpm-%s.sock", d.PHPVersion, poolName(d.Name))
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
	}
}

// nginxConfDir is where the panel writes its per-domain nginx server blocks.
func nginxConfDir(nginxDir string) string { return filepath.Join(nginxDir, "repanel.d") }

// writePHPPool writes (or refreshes) the domain's isolated PHP-FPM pool and
// reloads FPM. It is a no-op when the PHP version's pool directory is absent.
func writePHPPool(d models.Domain, sysUser string) error {
	poolDir := fmt.Sprintf("/etc/php/%s/fpm/pool.d", d.PHPVersion)
	st, err := os.Stat(poolDir)
	if err != nil || !st.IsDir() {
		return nil
	}
	var pb strings.Builder
	if err := phpPoolTemplate.Execute(&pb, map[string]string{
		"PoolName":     poolName(d.Name),
		"SysUser":      sysUser,
		"PHPSock":      phpSocket(d),
		"DocumentRoot": d.DocumentRoot,
	}); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(poolDir, "repanel-"+poolName(d.Name)+".conf"), []byte(pb.String()), 0o644); err != nil {
		return err
	}
	ReloadService("php" + d.PHPVersion + "-fpm")
	return nil
}

// removePHPPool deletes the domain's PHP-FPM pool file and reloads FPM.
func removePHPPool(d models.Domain) {
	os.Remove(fmt.Sprintf("/etc/php/%s/fpm/pool.d/repanel-%s.conf", d.PHPVersion, poolName(d.Name)))
	ReloadService("php" + d.PHPVersion + "-fpm")
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
