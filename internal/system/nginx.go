package system

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/repanel/repanel/internal/models"
)

// vhostTemplate renders an nginx server block per domain, in the style of the
// per-domain config files Plesk generates. SSL section is included only when
// a certificate has been issued.
var vhostTemplate = template.Must(template.New("vhost").Parse(`# Managed by RePanel — do not edit, changes will be overwritten.
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
        fastcgi_pass unix:/run/php/php{{.PHPVersion}}-fpm-{{.PoolName}}.sock;
    }

    location ~ /\.(?!well-known) { deny all; }
}
`))

type vhostData struct {
	Name         string
	DocumentRoot string
	PHPVersion   string
	PoolName     string
	SSL          bool
	CertPath     string
	KeyPath      string
}

// phpPoolTemplate gives every domain an isolated PHP-FPM pool running as the
// owning system user, the same isolation model DirectAdmin uses.
var phpPoolTemplate = template.Must(template.New("pool").Parse(`; Managed by RePanel
[{{.PoolName}}]
user = {{.SysUser}}
group = {{.SysUser}}
listen = /run/php/php{{.PHPVersion}}-fpm-{{.PoolName}}.sock
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

// WriteVhost writes the nginx server block and PHP-FPM pool for a domain and
// reloads both services. sysUser is the unix account that owns the files.
func WriteVhost(nginxDir string, d models.Domain, sysUser, certPath, keyPath string) error {
	confDir := filepath.Join(nginxDir, "repanel.d")
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		return err
	}
	data := vhostData{
		Name:         d.Name,
		DocumentRoot: d.DocumentRoot,
		PHPVersion:   d.PHPVersion,
		PoolName:     poolName(d.Name),
		SSL:          d.SSL && certPath != "",
		CertPath:     certPath,
		KeyPath:      keyPath,
	}
	var sb strings.Builder
	if err := vhostTemplate.Execute(&sb, data); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(confDir, d.Name+".conf"), []byte(sb.String()), 0o644); err != nil {
		return err
	}

	// PHP-FPM pool
	poolDir := fmt.Sprintf("/etc/php/%s/fpm/pool.d", d.PHPVersion)
	if st, err := os.Stat(poolDir); err == nil && st.IsDir() {
		var pb strings.Builder
		if err := phpPoolTemplate.Execute(&pb, map[string]string{
			"PoolName":     data.PoolName,
			"SysUser":      sysUser,
			"PHPVersion":   d.PHPVersion,
			"DocumentRoot": d.DocumentRoot,
		}); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(poolDir, "repanel-"+data.PoolName+".conf"), []byte(pb.String()), 0o644); err != nil {
			return err
		}
		ReloadService("php" + d.PHPVersion + "-fpm")
	}
	return reloadNginx()
}

// RemoveVhost deletes the generated nginx + PHP-FPM config for a domain.
func RemoveVhost(nginxDir string, d models.Domain) error {
	os.Remove(filepath.Join(nginxDir, "repanel.d", d.Name+".conf"))
	os.Remove(fmt.Sprintf("/etc/php/%s/fpm/pool.d/repanel-%s.conf", d.PHPVersion, poolName(d.Name)))
	ReloadService("php" + d.PHPVersion + "-fpm")
	return reloadNginx()
}

// WriteSuspendedVhost replaces the site with a 503 page when suspended.
func WriteSuspendedVhost(nginxDir string, d models.Domain) error {
	conf := fmt.Sprintf(`# Managed by RePanel — domain suspended
server {
    listen 80;
    listen [::]:80;
    server_name %s www.%s;
    location / { return 503; }
}
`, d.Name, d.Name)
	confDir := filepath.Join(nginxDir, "repanel.d")
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(confDir, d.Name+".conf"), []byte(conf), 0o644); err != nil {
		return err
	}
	return reloadNginx()
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
