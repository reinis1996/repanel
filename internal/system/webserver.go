package system

import (
	"os"
	"path/filepath"

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
	mode = ws.NormalizeMode(mode)
	if err := writePHPPool(d, sysUser); err != nil {
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

// RemoveVhost deletes every generated config file and the PHP-FPM pool.
func (ws *WebServer) RemoveVhost(d models.Domain) error {
	removeNginxVhost(ws.NginxDir, d.Name)
	removeApacheVhost(ws.ApacheDir, d.Name)
	removePHPPool(d)
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

// RebuildWebmail regenerates the shared webmail vhost on the front server from
// the list of enabled domains; with no domains it removes the vhost.
func (ws *WebServer) RebuildWebmail(domains []string) error {
	nginxConf := filepath.Join(nginxConfDir(ws.NginxDir), "webmail.conf")
	apacheConf := filepath.Join(apacheConfDir(ws.ApacheDir), "webmail.conf")
	root := WebmailRoot()

	if len(domains) == 0 || root == "" {
		os.Remove(nginxConf)
		os.Remove(apacheConf)
		return ws.reloadAll()
	}

	if ws.frontIsApache() {
		os.Remove(nginxConf)
		conf, err := renderApacheWebmailVhost(root, DefaultPHPSocket(), domains)
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
		conf, err := renderWebmailVhost(root, DefaultPHPSocket(), domains)
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
