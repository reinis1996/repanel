package system

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/reinis1996/repanel/internal/models"
)

// WebServer ties the nginx and Apache integrations together. The panel runs in
// one of three stacks, chosen at install time, and within the combined stack
// each domain picks its own mode:
//
//	stack "nginx"         every site is served directly by nginx (+ PHP-FPM).
//	stack "apache"        every site is served directly by Apache (+ PHP-FPM).
//	stack "nginx-apache"  nginx fronts :80/:443; each domain is one of
//	                      - "nginx":        nginx serves it directly (no Apache),
//	                      - "apache":       nginx proxies everything to Apache,
//	                      - "nginx-apache": nginx serves static, proxies PHP to
//	                                        Apache.
//
// All the per-domain writers in nginx.go / apache.go are file-only (no reload);
// WriteVhost decides which files a (stack, mode) needs, writes them, removes the
// other server's stale file, and reloads whichever servers are running.
type WebServer struct {
	Stack       Stack
	NginxDir    string
	ApacheDir   string
	BackendPort int // Apache backend port in the nginx-apache stack
}

type Stack string

const (
	StackNginx       Stack = "nginx"
	StackApache      Stack = "apache"
	StackNginxApache Stack = "nginx-apache"
)

// Per-domain web modes.
const (
	ModeNginx       = "nginx"        // nginx serves directly
	ModeApache      = "apache"       // Apache serves (directly, or via nginx proxy)
	ModeNginxApache = "nginx-apache" // nginx static + Apache for PHP
)

// NormalizeStack maps an arbitrary string to a known stack, defaulting to nginx.
func NormalizeStack(s string) Stack {
	switch Stack(s) {
	case StackApache:
		return StackApache
	case StackNginxApache:
		return StackNginxApache
	default:
		return StackNginx
	}
}

// NewWebServer builds a WebServer for the configured stack.
func NewWebServer(stack, nginxDir, apacheDir string, backendPort int) *WebServer {
	if backendPort <= 0 {
		backendPort = 8080
	}
	return &WebServer{
		Stack:       NormalizeStack(stack),
		NginxDir:    nginxDir,
		ApacheDir:   apacheDir,
		BackendPort: backendPort,
	}
}

// Modes returns the per-domain modes selectable in the current stack.
func (ws *WebServer) Modes() []string {
	switch ws.Stack {
	case StackApache:
		return []string{ModeApache}
	case StackNginxApache:
		return []string{ModeNginx, ModeApache, ModeNginxApache}
	default:
		return []string{ModeNginx}
	}
}

// DefaultMode is the mode assigned to new domains in the current stack.
func (ws *WebServer) DefaultMode() string { return ws.Modes()[0] }

// NormalizeMode coerces a requested mode to one valid for the current stack,
// falling back to the stack default for empty or out-of-stack values.
func (ws *WebServer) NormalizeMode(mode string) string {
	for _, m := range ws.Modes() {
		if m == mode {
			return mode
		}
	}
	return ws.DefaultMode()
}

// Info reports the stack, selectable modes and default for the UI/API.
func (ws *WebServer) Info() models.WebServerInfo {
	return models.WebServerInfo{Stack: string(ws.Stack), Modes: ws.Modes(), Default: ws.DefaultMode()}
}

// frontIsApache reports whether Apache owns :80/:443 (apache-only stack).
func (ws *WebServer) frontIsApache() bool { return ws.Stack == StackApache }

// FrontIsApache reports whether Apache is the front server (apache-only stack),
// which determines which ModSecurity connector the WAF uses.
func (ws *WebServer) FrontIsApache() bool { return ws.frontIsApache() }

// Subsystems reports which per-site config subsystems are active (and therefore
// editable) for a domain given the stack and its mode. Node apps are nginx
// reverse-proxies with no injectable vhost or PHP pool, so none apply.
func (ws *WebServer) Subsystems(d models.Domain) (nginx, apache, php bool) {
	if d.Runtime == "node" {
		return false, false, false
	}
	php = true
	mode := ws.NormalizeMode(d.WebMode)
	switch ws.Stack {
	case StackNginx:
		nginx = true
	case StackApache:
		apache = true
	case StackNginxApache:
		nginx = true
		if mode == ModeApache || mode == ModeNginxApache {
			apache = true
		}
	}
	return nginx, apache, php
}

// AccessLogDir is the front server's log directory; traffic accounting reads
// the per-domain <name>.access.log files there.
func (ws *WebServer) AccessLogDir() string {
	if ws.frontIsApache() {
		return "/var/log/apache2"
	}
	return NginxLogDir
}

// WriteVhost regenerates every config file a domain needs for the given mode,
// removes the other server's stale file, refreshes the PHP-FPM pool and reloads
// the running servers.
func (ws *WebServer) WriteVhost(d models.Domain, sysUser, certPath, keyPath, mode string) error {
	// A forwarding domain (or parked alias in redirect mode) serves only an HTTP
	// redirect — no document root, PHP pool or backend vhost.
	if d.RedirectURL != "" {
		return ws.writeRedirectVhost(d, certPath, keyPath)
	}
	// Node apps are nginx-fronted reverse proxies; the systemd unit (nodeapp.go)
	// runs the app. No PHP-FPM pool or Apache vhost is involved.
	if d.Runtime == "node" {
		return ws.writeNodeVhost(d, certPath, keyPath)
	}
	mode = ws.NormalizeMode(mode)
	if err := writePHPPool(d, sysUser); err != nil {
		return err
	}
	wafFile, err := ws.ensureWAF(d)
	if err != nil {
		return err
	}
	ssl := d.SSL && certPath != ""

	// Decide the file layout for this (stack, mode).
	var (
		wantNginx, nginxProxy, serveStatic bool
		wantApache, apacheBackend          bool
	)
	switch ws.Stack {
	case StackNginx:
		wantNginx = true
	case StackApache:
		wantApache = true // direct
	case StackNginxApache:
		switch mode {
		case ModeNginx:
			wantNginx = true
		case ModeApache:
			wantNginx, nginxProxy, serveStatic = true, true, false
			wantApache, apacheBackend = true, true
		case ModeNginxApache:
			wantNginx, nginxProxy, serveStatic = true, true, true
			wantApache, apacheBackend = true, true
		}
	}

	data := vhostDataFor(d, ssl, certPath, keyPath, ws.BackendPort, serveStatic)
	data.WAFEnabled = wafFile != ""
	data.WAFRulesFile = wafFile

	if wantNginx {
		var err error
		if nginxProxy {
			err = writeNginxProxyVhost(ws.NginxDir, data)
		} else {
			err = writeNginxDirectVhost(ws.NginxDir, data)
		}
		if err != nil {
			return err
		}
	} else {
		removeNginxVhost(ws.NginxDir, d.Name)
	}

	if wantApache {
		var err error
		if apacheBackend {
			err = writeApacheBackendVhost(ws.ApacheDir, data)
		} else {
			err = writeApacheDirectVhost(ws.ApacheDir, data)
		}
		if err != nil {
			return err
		}
	} else {
		removeApacheVhost(ws.ApacheDir, d.Name)
	}

	return ws.reloadAll()
}

// writeNodeVhost writes the nginx reverse-proxy vhost for a Node app, clearing
// any leftover PHP config from a previous runtime, and reloads nginx.
func (ws *WebServer) writeNodeVhost(d models.Domain, certPath, keyPath string) error {
	removeApacheVhost(ws.ApacheDir, d.Name)
	removePHPPool(d)
	wafFile, err := ws.ensureWAF(d)
	if err != nil {
		return err
	}
	if err := writeNginxNodeVhost(ws.NginxDir, nodeVhostData{
		Name:         d.Name,
		ServerNames:  serverNames(d),
		DocumentRoot: d.DocumentRoot,
		SSL:          d.SSL && certPath != "",
		CertPath:     certPath,
		KeyPath:      keyPath,
		Port:         d.NodePort,
		WAFEnabled:   wafFile != "",
		WAFRulesFile: wafFile,
	}); err != nil {
		return err
	}
	return reloadNginx()
}

// ensureWAF writes (or removes) a domain's ModSecurity rules file and reports the
// path to inject into its vhost. It returns "" — i.e. the WAF is not wired in —
// when the domain has it disabled or the connector for the active front server is
// not installed, so a stale flag can never emit a directive nginx/Apache can't
// parse.
func (ws *WebServer) ensureWAF(d models.Domain) (string, error) {
	if !d.WAFEnabled || !WAFModuleAvailable(ws.frontIsApache()) {
		RemoveDomainWAF(d.Name)
		return "", nil
	}
	return WriteDomainWAF(d)
}

// RemoveVhost deletes every generated config file and the PHP-FPM pool.
func (ws *WebServer) RemoveVhost(d models.Domain) error {
	removeNginxVhost(ws.NginxDir, d.Name)
	removeApacheVhost(ws.ApacheDir, d.Name)
	removePHPPool(d)
	RemoveDomainWAF(d.Name)
	return ws.reloadAll()
}

// WriteSuspendedVhost replaces the site with a 503 on the front server and
// removes the backend's file.
func (ws *WebServer) WriteSuspendedVhost(d models.Domain) error {
	if ws.frontIsApache() {
		removeNginxVhost(ws.NginxDir, d.Name)
		if err := writeApacheSuspended(ws.ApacheDir, d); err != nil {
			return err
		}
	} else {
		removeApacheVhost(ws.ApacheDir, d.Name)
		if err := writeNginxSuspended(ws.NginxDir, d); err != nil {
			return err
		}
	}
	return ws.reloadAll()
}

// RebuildWebmail regenerates the webmail vhost on the front server from the list
// of enabled domains (each with its certificate, when one exists); with no hosts
// it removes the vhost.
func (ws *WebServer) RebuildWebmail(hosts []WebmailHost) error {
	nginxConf := filepath.Join(nginxConfDir(ws.NginxDir), "webmail.conf")
	apacheConf := filepath.Join(apacheConfDir(ws.ApacheDir), "webmail.conf")
	root := WebmailRoot()

	if len(hosts) == 0 || root == "" {
		os.Remove(nginxConf)
		os.Remove(apacheConf)
		return ws.reloadAll()
	}

	if ws.frontIsApache() {
		os.Remove(nginxConf)
		conf, err := renderApacheWebmailVhost(root, DefaultPHPSocket(), hosts)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(apacheConf), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(apacheConf, []byte(conf), 0o644); err != nil {
			return err
		}
	} else {
		os.Remove(apacheConf)
		conf, err := renderWebmailVhost(root, DefaultPHPSocket(), hosts)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(nginxConf), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(nginxConf, []byte(conf), 0o644); err != nil {
			return err
		}
	}
	return ws.reloadAll()
}

// WriteDefaultVhost installs a catch-all "default server" on the nginx front so
// requests whose Host matches no domain or function — most importantly the bare
// server IP — are dropped (444) instead of falling through to whichever tenant
// vhost nginx would otherwise treat as the default. Without it, the first/only
// :443 vhost (e.g. a freshly created function) answers for the IP.
//
// The distribution's stock default site is removed first so our default_server
// is unambiguous (nginx rejects duplicate default_server). certPath/keyPath
// secure the HTTPS catch-all (the panel's own self-signed cert is fine); when
// empty only the HTTP catch-all is written. It is a no-op on the apache-only
// stack, where nginx is not the front (and functions are not served).
func (ws *WebServer) WriteDefaultVhost(certPath, keyPath string) error {
	if ws.Stack == StackApache || !have("nginx") {
		return nil
	}
	// Drop the stock Debian/Ubuntu default site to avoid a duplicate default_server.
	os.Remove("/etc/nginx/sites-enabled/default")

	// A plain 404 with a short body (rather than nginx's "444 = drop connection",
	// which surfaces as ERR_HTTP2_PROTOCOL_ERROR / ERR_EMPTY_RESPONSE in browsers)
	// so an unmatched host gets a clean, obviously-intentional response.
	const body = `"No site is configured for this address."`
	var sb strings.Builder
	sb.WriteString(`# Managed by RePanel — default catch-all. Unmatched hosts (e.g. the bare
# server IP) must not be served a tenant's site, so they get a 404 here.
server {
    listen 80 default_server;
    listen [::]:80 default_server;
    server_name _;
    default_type text/plain;
    return 404 ` + body + `;
}
`)
	if certPath != "" && keyPath != "" {
		fmt.Fprintf(&sb, `server {
    listen 443 ssl default_server;
    listen [::]:443 ssl default_server;
    http2 on;
    server_name _;
    ssl_certificate     %s;
    ssl_certificate_key %s;
    default_type text/plain;
    return 404 %s;
}
`, certPath, keyPath, body)
	}

	confDir := nginxConfDir(ws.NginxDir)
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(confDir, "00-default.conf"), []byte(sb.String()), 0o644); err != nil {
		return err
	}
	return reloadNginx()
}

// reloadAll reloads whichever servers the stack runs.
func (ws *WebServer) reloadAll() error {
	var firstErr error
	if ws.Stack != StackApache {
		if err := reloadNginx(); err != nil {
			firstErr = err
		}
	}
	if ws.Stack != StackNginx {
		if err := reloadApache(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
