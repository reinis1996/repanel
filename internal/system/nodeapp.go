package system

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
	"time"

	"github.com/reinis1996/repanel/internal/models"
)

// Per-domain Node.js application runtime. The app runs as the owning tenant under
// a systemd unit on a panel-allocated loopback port (exposed via the PORT env);
// nginx reverse-proxies the domain to it (see the node vhost in nginx.go). The
// isolation level matches a PHP site (own user, ProtectSystem=strict, resource
// caps) but with network enabled, since the app serves HTTP and may reach DBs.

// NodeAppPortLow/High bound the loopback port range the panel allocates to Node
// apps. The firewall isolates this range to nginx (www-data) and the panel
// (root) so one tenant cannot reach another tenant's app directly (see
// EnsureNodeAppIsolation).
const (
	NodeAppPortLow  = 30000
	NodeAppPortHigh = 39999
)

var validEnvKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func nodeUnit(domain string) string     { return "repanel-app-" + poolName(domain) }
func nodeUnitFile(domain string) string { return nodeUnit(domain) + ".service" }
func nodeUnitPath(domain string) string {
	return filepath.Join("/etc/systemd/system", nodeUnitFile(domain))
}

// nodeAppDir resolves the application root (relative to the domain directory).
func nodeAppDir(d models.Domain, appRoot string) (string, error) {
	return ResolveJailed(filepath.Dir(d.DocumentRoot), appRoot)
}

// nodeSandboxProps hardens a Node app's unit to (at least) PHP-site parity:
// non-root tenant user, read-only system with only the app dir writable, private
// /tmp, hidden home dirs, resource caps. Network stays enabled.
func nodeSandboxProps(appDir string) []string {
	props := []string{
		"NoNewPrivileges=yes",
		"PrivateTmp=yes",
		"ProtectSystem=strict",
		"ProtectHome=yes",
		"ReadWritePaths=" + appDir,
		"ProtectControlGroups=yes",
		"ProtectKernelTunables=yes",
		"ProtectKernelModules=yes",
		"RestrictSUIDSGID=yes",
		"LockPersonality=yes",
		"RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6",
		"MemoryMax=512M",
		"CPUQuota=80%",
		"TasksMax=256",
	}
	if functionsHideProc() {
		props = append(props, "ProtectProc=invisible")
	}
	return props
}

type nodeUnitData struct {
	Domain, Version, SysUser, AppDir, Bin, Startup, Slice string
	Port                                                  int
	Env                                                   []string // KEY=VALUE
	Props                                                 []string
}

var nodeUnitTemplate = template.Must(template.New("nodeunit").Parse(`# Managed by RePanel — Node app for {{.Domain}}. Do not edit.
[Unit]
Description=RePanel Node app {{.Domain}} (Node {{.Version}})
After=network.target
StartLimitIntervalSec=60
StartLimitBurst=3

[Service]
User={{.SysUser}}
Group={{.SysUser}}
Slice={{.Slice}}
WorkingDirectory={{.AppDir}}
Environment=PORT={{.Port}}
Environment=NODE_ENV=production
{{- range .Env}}
Environment="{{.}}"
{{- end}}
ExecStart={{.Bin}} {{.Startup}}
Restart=on-failure
RestartSec=2
{{- range .Props}}
{{.}}
{{- end}}

[Install]
WantedBy=multi-user.target
`))

// WriteNodeApp (re)writes the systemd unit for a domain's Node app and starts it.
// A crash-looping app (e.g. no code uploaded yet) is bounded by StartLimitBurst,
// so it stops retrying and shows as failed rather than hammering the host.
func WriteNodeApp(d models.Domain, sysUser, appRoot, startup string, env map[string]string) error {
	if !Linux() {
		return nil
	}
	if !validSysName.MatchString(sysUser) {
		return fmt.Errorf("invalid system user")
	}
	bin, ok := NodeBinary(d.NodeVersion)
	if !ok {
		return fmt.Errorf("Node %s is not installed on this server", d.NodeVersion)
	}
	appDir, err := nodeAppDir(d, appRoot)
	if err != nil {
		return fmt.Errorf("application root escapes the domain directory")
	}
	if _, err := nodeAppDir(d, filepath.Join(appRoot, startup)); err != nil {
		return fmt.Errorf("startup file escapes the application directory")
	}

	envLines := make([]string, 0, len(env))
	for k, v := range env {
		if validEnvKey.MatchString(k) && !strings.ContainsAny(v, "\n\r\"") {
			envLines = append(envLines, k+"="+v)
		}
	}
	var sb strings.Builder
	if err := nodeUnitTemplate.Execute(&sb, nodeUnitData{
		Domain: d.Name, Version: d.NodeVersion, SysUser: sysUser, AppDir: appDir,
		Bin: bin, Startup: startup, Port: d.NodePort, Env: envLines, Props: nodeSandboxProps(appDir),
		Slice: AccountSliceName(d.UserID),
	}); err != nil {
		return err
	}
	if err := os.WriteFile(nodeUnitPath(d.Name), []byte(sb.String()), 0o644); err != nil {
		return err
	}
	if !have("systemctl") {
		return nil
	}
	if _, err := run("systemctl", "daemon-reload"); err != nil {
		return err
	}
	run("systemctl", "reset-failed", nodeUnitFile(d.Name))
	_, err = run("systemctl", "enable", "--now", nodeUnitFile(d.Name))
	return err
}

// RemoveNodeApp stops and removes a domain's Node app unit.
func RemoveNodeApp(d models.Domain) {
	if have("systemctl") {
		run("systemctl", "disable", "--now", nodeUnitFile(d.Name))
	}
	os.Remove(nodeUnitPath(d.Name))
	if have("systemctl") {
		run("systemctl", "daemon-reload")
	}
}

// RestartNodeApp restarts the app (used after a code change or npm install).
func RestartNodeApp(d models.Domain) error {
	if !have("systemctl") {
		return nil
	}
	run("systemctl", "reset-failed", nodeUnitFile(d.Name))
	_, err := run("systemctl", "restart", nodeUnitFile(d.Name))
	return err
}

// StopNodeApp stops the app without removing its unit.
func StopNodeApp(d models.Domain) {
	if have("systemctl") {
		run("systemctl", "disable", "--now", nodeUnitFile(d.Name))
	}
}

// NodeAppRunning reports whether the app's unit is active.
func NodeAppRunning(d models.Domain) bool {
	if !have("systemctl") {
		return false
	}
	_, err := run("systemctl", "is-active", "--quiet", nodeUnitFile(d.Name))
	return err == nil
}

// NodeAppLogs returns the last lines of the app's journald output.
func NodeAppLogs(d models.Domain) string {
	if !have("journalctl") {
		return ""
	}
	out, _ := run("journalctl", "-u", nodeUnitFile(d.Name), "--no-pager", "-n", "200")
	return out
}

// WriteSampleApp scaffolds a dependency-free "it works" Node server into the app
// root so a freshly created Node domain serves something immediately. It never
// overwrites an existing startup file, and hands the files to the tenant.
func WriteSampleApp(d models.Domain, sysUser, appRoot, startup string) error {
	if !Linux() {
		return nil
	}
	if startup == "" {
		startup = "app.js"
	}
	appDir, err := nodeAppDir(d, appRoot)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return err
	}
	target := filepath.Join(appDir, startup)
	if _, err := os.Stat(target); err == nil {
		return nil // an app is already there — leave it untouched
	}
	page := fmt.Sprintf(sampleNodePage, d.Name, d.Name, d.NodeVersion)
	pageJS, _ := json.Marshal(page) // a JSON string is a valid JS string literal
	if err := os.WriteFile(target, []byte(fmt.Sprintf(sampleNodeApp, string(pageJS))), 0o644); err != nil {
		return err
	}
	pkg := filepath.Join(appDir, "package.json")
	if _, err := os.Stat(pkg); os.IsNotExist(err) {
		name := strings.ReplaceAll(d.Name, ".", "-")
		os.WriteFile(pkg, []byte(fmt.Sprintf(samplePackageJSON, name, startup, startup)), 0o644)
	}
	if validSysName.MatchString(sysUser) {
		run("chown", "-R", sysUser+":"+sysUser, appDir)
	}
	return nil
}

const sampleNodeApp = `// Sample app generated by RePanel — replace with your own application.
// Listen on the port RePanel provides via the PORT environment variable.
const http = require('http');
const port = process.env.PORT || 3000;
const page = %s;
http.createServer((req, res) => {
  res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' });
  res.end(page);
}).listen(port, () => console.log('RePanel sample app listening on port ' + port));
`

const sampleNodePage = `<!doctype html>
<html><head><meta charset="utf-8"><title>%s</title>
<style>body{font-family:system-ui;display:flex;align-items:center;justify-content:center;height:100vh;margin:0;background:#f4f6fa;color:#24344d}
.card{text-align:center;padding:3rem;background:#fff;border-radius:12px;box-shadow:0 4px 24px rgba(0,0,0,.06)}
code{background:#eef2f7;padding:.15rem .4rem;border-radius:5px}</style>
</head><body><div class="card"><h1>%s</h1>
<p>Your Node.js app is running on RePanel (Node %s).</p>
<p>Edit <code>app.js</code> and click <strong>Restart</strong> to deploy your own application.</p>
</div></body></html>
`

const samplePackageJSON = `{
  "name": "%s",
  "version": "1.0.0",
  "private": true,
  "main": "%s",
  "scripts": {
    "start": "node %s"
  }
}
`

// NpmInstall runs `npm install` in the app directory as the tenant.
func NpmInstall(d models.Domain, sysUser, appRoot string) (string, error) {
	npm, ok := NodeNpmBinary(d.NodeVersion)
	if !ok {
		return "", fmt.Errorf("npm for Node %s is not installed", d.NodeVersion)
	}
	appDir, err := nodeAppDir(d, appRoot)
	if err != nil {
		return "", fmt.Errorf("application root escapes the domain directory")
	}
	name, args := npm, []string{"install"}
	if Linux() && validSysName.MatchString(sysUser) && have("sudo") {
		name, args = "sudo", []string{"-n", "-u", sysUser, "--", npm, "install"}
	}
	out, errOut, err := runCapture(10*time.Minute, appDir, "", name, args...)
	if err != nil {
		if d := strings.TrimSpace(errOut); d != "" {
			return out, fmt.Errorf("%s", d)
		}
		return out, err
	}
	if errOut != "" {
		out += "\n" + errOut // npm writes progress to stderr
	}
	return strings.TrimSpace(out), nil
}
