package system

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"
)

// Web database administration via Adminer — a single-file PHP database client.
// The panel downloads adminer to a managed document root and serves it on
// dbadmin.<panel-hostname> through a generated vhost running on the shared PHP
// pool, mirroring the webmail integration. Operators reach it per database from
// the Databases page (Adminer reads the target db/user from query parameters).

const adminerURL = "https://github.com/vrana/adminer/releases/download/v4.8.1/adminer-4.8.1.php"

// AdminerRoot is the document root the Adminer file is installed into.
func AdminerRoot() string { return "/usr/share/repanel/dbadmin" }

// AdminerInstalled reports whether the Adminer file is present.
func AdminerInstalled() bool {
	_, err := os.Stat(filepath.Join(AdminerRoot(), "index.php"))
	return err == nil
}

// AdminerHost is the hostname Adminer is served on, derived from the panel
// hostname. Returns "" when the panel hostname is unset (so the UI hides it).
func AdminerHost(panelHostname string) string {
	panelHostname = strings.TrimSpace(panelHostname)
	if panelHostname == "" {
		return ""
	}
	return "dbadmin." + panelHostname
}

// InstallAdminer downloads the Adminer PHP file into the managed document root.
func InstallAdminer() error {
	root := AdminerRoot()
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Get(adminerURL)
	if err != nil {
		return fmt.Errorf("download Adminer: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download Adminer: unexpected status %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, "index.php"), body, 0o644)
}

var adminerVhostTemplate = template.Must(template.New("adminer").Parse(`# Managed by RePanel — Adminer (web database admin).
server {
    listen 80;
    listen [::]:80;
    server_name {{.Host}};

{{- if .SSL}}
    location /.well-known/acme-challenge/ { root {{.Root}}; }
    location / { return 301 https://$host$request_uri; }
}

server {
    listen 443 ssl;
    listen [::]:443 ssl;
    http2 on;
    server_name {{.Host}};

    ssl_certificate     {{.CertPath}};
    ssl_certificate_key {{.KeyPath}};
{{- end}}

    root {{.Root}};
    index index.php;

    access_log /var/log/nginx/dbadmin.access.log;
    error_log  /var/log/nginx/dbadmin.error.log;

    location / { try_files $uri /index.php; }

    location ~ \.php$ {
        include fastcgi_params;
        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
        fastcgi_pass unix:{{.PHPSock}};
    }
}
`))

type adminerVhostData struct {
	Host     string
	Root     string
	PHPSock  string
	SSL      bool
	CertPath string
	KeyPath  string
}

// WriteAdminerVhost (re)writes the Adminer vhost for the given host on the nginx
// front and reloads nginx. With host == "" it removes the vhost instead.
func (ws *WebServer) WriteAdminerVhost(host, certPath, keyPath string) error {
	confPath := filepath.Join(nginxConfDir(ws.NginxDir), "dbadmin.conf")
	if host == "" || !AdminerInstalled() {
		os.Remove(confPath)
		return reloadNginx()
	}
	if err := os.MkdirAll(nginxConfDir(ws.NginxDir), 0o755); err != nil {
		return err
	}
	var sb strings.Builder
	if err := adminerVhostTemplate.Execute(&sb, adminerVhostData{
		Host:     host,
		Root:     AdminerRoot(),
		PHPSock:  DefaultPHPSocket(),
		SSL:      certPath != "",
		CertPath: certPath,
		KeyPath:  keyPath,
	}); err != nil {
		return err
	}
	if err := os.WriteFile(confPath, []byte(sb.String()), 0o644); err != nil {
		return err
	}
	return reloadNginx()
}
