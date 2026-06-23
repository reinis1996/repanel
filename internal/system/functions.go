package system

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"text/template"
	"time"

	"github.com/reinis1996/repanel/internal/models"
)

// Lambda-style functions. Each function is tenant-supplied handler code in a
// chosen runtime, exposed at a generated hostname and run as the owning tenant's
// system user:
//
//   - php          a dedicated PHP-FPM pool (front controller index.php),
//   - node/python  a per-function systemd unit running a generated bootstrap
//                  HTTP server on a unix socket.
//
// nginx fronts the hostname for every runtime (TLS + reverse proxy / FastCGI).
// Like the rest of this package, every host-touching step degrades to a no-op
// when the relevant service is absent or we are not on Linux, so the panel stays
// usable during development.

const (
	// FunctionSubdomain is the fixed label between the slug and the base domain,
	// mirroring AWS's "<id>.lambda-url.<region>" shape: "<slug>.function-url.<base>".
	FunctionSubdomain = "function-url"

	maxFunctionCodeSize = 1 << 20 // 1 MB of handler source

	// Trigger modes.
	TriggerURL      = "url"      // invoked over HTTP at a generated hostname
	TriggerSchedule = "schedule" // invoked by cron via a generated CLI runner

	functionCronFile = "/etc/cron.d/repanel-functions"
)

var validFnSlug = regexp.MustCompile(`^[a-z0-9]{6,24}$`)

// FunctionRuntimes are the runtimes the panel understands, in display order.
var functionRuntimes = []string{"python", "node", "php"}

// ---- runtime detection ----

// AvailableFunctionRuntimes reports the runtimes (and the versions of each) that
// are actually installed on the host and can therefore back a function.
func AvailableFunctionRuntimes() []models.FunctionRuntime {
	out := []models.FunctionRuntime{}
	if v := sortedKeys(detectPython()); len(v) > 0 {
		out = append(out, models.FunctionRuntime{Runtime: "python", Versions: v})
	}
	if v := sortedKeys(detectNode()); len(v) > 0 {
		out = append(out, models.FunctionRuntime{Runtime: "node", Versions: v})
	}
	if phpv := PHPVersions(); len(phpv) > 0 && havePHPFPM() {
		out = append(out, models.FunctionRuntime{Runtime: "php", Versions: phpv})
	}
	return out
}

// FunctionRuntimeValid reports whether runtime+version is installed and usable.
func FunctionRuntimeValid(runtime, version string) bool {
	for _, r := range AvailableFunctionRuntimes() {
		if r.Runtime != runtime {
			continue
		}
		for _, v := range r.Versions {
			if v == version {
				return true
			}
		}
	}
	return false
}

// runtimeBinary resolves the interpreter path for an exec'd runtime (node /
// python). PHP is served via FPM and has no exec binary here.
func runtimeBinary(runtime, version string) (string, bool) {
	switch runtime {
	case "node":
		bin, ok := detectNode()[version]
		return bin, ok
	case "python":
		bin, ok := detectPython()[version]
		return bin, ok
	}
	return "", false
}

// detectNode maps installed Node major versions to their interpreter path.
func detectNode() map[string]string {
	out := map[string]string{}
	candidates := []string{"node", "nodejs"}
	candidates = append(candidates, scanPathBinaries(regexp.MustCompile(`^node\d+$`))...)
	for _, name := range candidates {
		bin, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		if v := majorVersion(bin); v != "" {
			if _, seen := out[v]; !seen {
				out[v] = bin
			}
		}
	}
	return out
}

// detectPython maps installed Python minor versions (e.g. "3.11") to their path.
func detectPython() map[string]string {
	out := map[string]string{}
	for _, name := range scanPathBinaries(regexp.MustCompile(`^python3\.\d+$`)) {
		bin, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		ver := strings.TrimPrefix(filepath.Base(name), "python")
		if _, seen := out[ver]; !seen {
			out[ver] = bin
		}
	}
	if len(out) == 0 {
		if bin, err := exec.LookPath("python3"); err == nil {
			if v := minorVersion(bin); v != "" {
				out[v] = bin
			}
		}
	}
	return out
}

// majorVersion runs `<bin> --version` and returns the leading major number
// (e.g. "v20.11.1" -> "20"), "" on failure.
func majorVersion(bin string) string {
	out, err := run(bin, "--version")
	if err != nil {
		return ""
	}
	digits := strings.TrimPrefix(strings.TrimSpace(out), "v")
	major, _, _ := strings.Cut(digits, ".")
	if _, err := fmt.Sscanf(major, "%d", new(int)); err != nil {
		return ""
	}
	return major
}

// minorVersion runs `<bin> --version` and returns "major.minor" (e.g. "3.11").
func minorVersion(bin string) string {
	out, err := run(bin, "--version") // "Python 3.11.2"
	if err != nil {
		return ""
	}
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return ""
	}
	parts := strings.Split(fields[len(fields)-1], ".")
	if len(parts) < 2 {
		return ""
	}
	return parts[0] + "." + parts[1]
}

// scanPathBinaries returns absolute paths of binaries in $PATH whose base name
// matches re.
func scanPathBinaries(re *regexp.Regexp) []string {
	var found []string
	seen := map[string]bool{}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if re.MatchString(name) && !seen[name] {
				seen[name] = true
				found = append(found, name)
			}
		}
	}
	return found
}

func havePHPFPM() bool {
	if _, err := os.Stat("/etc/php"); err != nil {
		return false
	}
	return true
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ---- on-disk layout ----

// FunctionDir is the code directory for a function. It lives in a hidden folder
// inside the tenant's home (not in any document root, so it is never web-served)
// rather than under DataDir: function processes run AS the tenant, so the code
// must sit on a path the tenant account can traverse and read — which the home,
// like the per-domain docroots, already provides for both the tenant's own
// processes and www-data.
func FunctionDir(webRoot, sysUser, slug string) string {
	return filepath.Join(webRoot, sysUser, ".functions", slug)
}

// chownFunctionTree hands a function's directory (and the parent .functions
// folder, so the tenant can traverse into it) to the owning tenant on Linux.
func chownFunctionTree(dir, sysUser string) {
	if Linux() && sysUser != "" && validSysName.MatchString(sysUser) {
		run("chown", sysUser+":"+sysUser, filepath.Dir(dir))
		run("chown", "-R", sysUser+":"+sysUser, dir)
	}
}

// functionSocket returns the backend socket nginx connects to.
func functionSocket(fn models.Function) string {
	if fn.Runtime == "php" {
		return "/run/php/repanel-fn-" + fn.Slug + ".sock"
	}
	return filepath.Join("/run", functionRuntimeDir(fn.Slug), "fn.sock")
}

func functionRuntimeDir(slug string) string { return "repanel-fn-" + slug }
func functionUnit(slug string) string       { return "repanel-fn-" + slug + ".service" }
func functionUnitPath(slug string) string {
	return filepath.Join("/etc/systemd/system", functionUnit(slug))
}

func handlerFileName(runtime string) string {
	switch runtime {
	case "node":
		return "handler.js"
	case "php":
		return "handler.php"
	default:
		return "handler.py"
	}
}

func bootstrapFileName(runtime string) string {
	switch runtime {
	case "node":
		return "bootstrap.js"
	case "php":
		return "index.php" // PHP front controller
	default:
		return "bootstrap.py"
	}
}

func runnerFileName(runtime string) string {
	switch runtime {
	case "node":
		return "runner.js"
	case "php":
		return "runner.php"
	default:
		return "runner.py"
	}
}

func invokeFileName(runtime string) string {
	switch runtime {
	case "node":
		return "invoke.js"
	case "php":
		return "invoke.php"
	default:
		return "invoke.py"
	}
}

// phpCliBinary resolves the PHP CLI for a version (used by scheduled functions),
// preferring the versioned binary (e.g. php8.3) over the default php.
func phpCliBinary(version string) (string, bool) {
	for _, name := range []string{"php" + version, "php"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, true
		}
	}
	return "", false
}

// resolveRuntimeBin returns the interpreter that executes a function's CLI
// scripts (invoke/runner): the PHP CLI for php, else the node/python binary.
func resolveRuntimeBin(fn models.Function) (string, bool) {
	if fn.Runtime == "php" {
		return phpCliBinary(fn.Version)
	}
	return runtimeBinary(fn.Runtime, fn.Version)
}

// ---- sandboxing ----
//
// Untrusted tenant code is confined with systemd sandboxing: dropped to the
// tenant uid, a clean environment (no SUDO_*/panel vars), a private /tmp,
// read-only system with only the function dir writable, hidden home dirs,
// no new privileges, memory/CPU/task caps and (by default) no network. The same
// property set is applied to transient runs (test/scheduled, via systemd-run)
// and to the long-running URL units (written into the unit file).

const (
	fnMemoryMax     = "128M"
	fnCPUQuota      = "50%"
	fnTasksMax      = "64"
	fnScheduleMaxSec = 900 // hard cap for a scheduled run
)

// sandboxProperties returns the systemd sandbox properties (KEY=VALUE) applied
// to a function. dir is made the one writable path; allowNetwork keeps the
// network namespace, otherwise it is removed entirely. runtimeMaxSec, when > 0,
// adds a hard wall-clock cap (omitted for long-running URL units). hideProc adds
// the 247+-only /proc-hardening property.
//
// Notes on deliberate omissions:
//   - MemoryDenyWriteExecute is NOT set: it breaks the V8 (Node) and PHP JITs.
//   - ProcSubset=pid is NOT set: it hides /proc/meminfo and /proc/cpuinfo, which
//     V8 and some runtimes read at startup; ProtectProc=invisible already hides
//     other processes, which is the high-value part.
func sandboxProperties(dir string, allowNetwork bool, runtimeMaxSec int, hideProc bool) []string {
	props := []string{
		"NoNewPrivileges=yes",
		"PrivateTmp=yes",
		"PrivateDevices=yes",
		"ProtectSystem=strict",
		"ProtectHome=yes",
		"ProtectControlGroups=yes",
		"ProtectKernelTunables=yes",
		"ProtectKernelModules=yes",
		"ProtectKernelLogs=yes",
		"ProtectClock=yes",
		"ProtectHostname=yes",
		"RestrictSUIDSGID=yes",
		"RestrictRealtime=yes",
		"LockPersonality=yes",
		"RestrictNamespaces=yes",
		"RemoveIPC=yes",
		"SystemCallArchitectures=native",
		"SystemCallFilter=@system-service",
		"UMask=0077",
		"MemoryMax=" + fnMemoryMax,
		"MemorySwapMax=0",
		"CPUQuota=" + fnCPUQuota,
		"TasksMax=" + fnTasksMax,
	}
	if hideProc {
		props = append(props, "ProtectProc=invisible") // systemd >= 247
	}

	// Private root: replace / with an empty read-only tmpfs, then bind back only
	// the runtime and shared libraries (read-only) and the function's own
	// directory (writable). The host root, /home, /etc/passwd, /etc/hostname and
	// everything else simply do not exist in the function's mount namespace, so
	// it can no longer read the host filesystem. Optional binds (leading "-")
	// are ignored when absent. /bin /sbin /lib /lib64 are the usr-merge symlinks
	// the dynamic loader needs (e.g. /lib64/ld-linux-x86-64.so.2).
	props = append(props,
		"TemporaryFileSystem=/:ro",
		"BindReadOnlyPaths=/usr",
		"BindReadOnlyPaths=-/bin",
		"BindReadOnlyPaths=-/sbin",
		"BindReadOnlyPaths=-/lib",
		"BindReadOnlyPaths=-/lib64",
		"BindReadOnlyPaths=-/etc/ld.so.cache",
		"BindReadOnlyPaths=-/etc/ld.so.conf",
		"BindReadOnlyPaths=-/etc/ld.so.conf.d",
		"BindReadOnlyPaths=-/etc/alternatives",
		"BindReadOnlyPaths=-/etc/localtime",
		"BindReadOnlyPaths=-/etc/php", // PHP CLI ini + extension config
		"ReadWritePaths="+dir,
	)

	if allowNetwork {
		// Allow outbound sockets and bind the minimal resolver/CA config so
		// name resolution and TLS work inside the private root.
		props = append(props,
			"RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6",
			"BindReadOnlyPaths=-/etc/resolv.conf",
			"BindReadOnlyPaths=-/etc/hosts",
			"BindReadOnlyPaths=-/etc/nsswitch.conf",
			"BindReadOnlyPaths=-/etc/ssl",
			"BindReadOnlyPaths=-/etc/ca-certificates",
		)
	} else {
		// A private network namespace leaves only loopback, so the function
		// cannot reach the host, other tenants or the internet.
		props = append(props, "PrivateNetwork=yes", "RestrictAddressFamilies=AF_UNIX")
	}
	if runtimeMaxSec > 0 {
		props = append(props, fmt.Sprintf("RuntimeMaxSec=%d", runtimeMaxSec))
	}
	return props
}

// systemdAtLeast reports whether the host's systemd is at least the given major
// version, used to gate properties that older releases reject (a rejected
// property would make systemd-run fail and the function not run).
func systemdAtLeast(major int) bool {
	out, err := run("systemctl", "--version")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "systemd" {
			var v int
			if _, err := fmt.Sscanf(fields[1], "%d", &v); err == nil {
				return v >= major
			}
		}
	}
	return false
}

// functionsHideProc reports whether ProtectProc=invisible is supported (247+).
func functionsHideProc() bool { return systemdAtLeast(247) }

// systemdRunArgv builds the full argv for a one-shot sandboxed run of script as
// the tenant. Property values may contain spaces; passed as single argv elements
// they are safe for exec, and shell-quoted by shellJoin for cron.
func (m *FunctionManager) systemdRunArgv(fn models.Function, sysUser, dir, bin, script string, runtimeMaxSec int, pipe bool) []string {
	home := filepath.Join(m.WebRoot, sysUser)
	argv := []string{"systemd-run", "--collect", "--quiet",
		"--uid=" + sysUser, "--gid=" + sysUser,
		"-p", "WorkingDirectory=" + dir,
		"--setenv=HOME=" + home, "--setenv=USER=" + sysUser, "--setenv=LOGNAME=" + sysUser,
		"--setenv=PATH=/usr/local/bin:/usr/bin:/bin"}
	if pipe {
		argv = append(argv, "--pipe", "--wait")
	}
	for _, p := range sandboxProperties(dir, fn.AllowNetwork, runtimeMaxSec, functionsHideProc()) {
		argv = append(argv, "-p", p)
	}
	return append(argv, "--", bin, script)
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func shellJoin(argv []string) string {
	q := make([]string, len(argv))
	for i, a := range argv {
		q[i] = shellQuote(a)
	}
	return strings.Join(q, " ")
}

// ---- manager ----

// FunctionManager deploys and tears down functions for the configured paths.
type FunctionManager struct {
	WebRoot  string // base for tenant homes that hold function code
	NginxDir string
}

func NewFunctionManager(webRoot, nginxDir string) *FunctionManager {
	return &FunctionManager{WebRoot: webRoot, NginxDir: nginxDir}
}

// Write (re)deploys a function: writes the handler + generated bootstrap, hands
// ownership to the tenant, starts the runtime backend, writes the nginx vhost
// and reloads. Idempotent.
func (m *FunctionManager) Write(fn models.Function, sysUser, code, certPath, keyPath string) error {
	if !validFnSlug.MatchString(fn.Slug) {
		return fmt.Errorf("invalid function slug")
	}
	if !validSysName.MatchString(sysUser) {
		return fmt.Errorf("invalid system user")
	}
	dir := FunctionDir(m.WebRoot, sysUser, fn.Slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := writeFunctionFile(filepath.Join(dir, handlerFileName(fn.Runtime)), []byte(code)); err != nil {
		return err
	}
	// The invoke runner powers the manual "Test" action for every trigger type.
	if err := writeFunctionFile(filepath.Join(dir, invokeFileName(fn.Runtime)), invokeSource(fn.Runtime)); err != nil {
		return err
	}

	// Scheduled functions need only the handler + a CLI runner; the cron.d entry
	// is (re)built separately from the full function set. They have no URL,
	// backend service or vhost.
	if fn.Trigger == TriggerSchedule {
		if err := writeFunctionFile(filepath.Join(dir, runnerFileName(fn.Runtime)), runnerSource(fn.Runtime)); err != nil {
			return err
		}
		chownFunctionTree(dir, sysUser)
		return nil
	}

	if err := writeFunctionFile(filepath.Join(dir, bootstrapFileName(fn.Runtime)), bootstrapSource(fn.Runtime)); err != nil {
		return err
	}
	chownFunctionTree(dir, sysUser)
	if err := m.deployBackend(fn, sysUser, dir); err != nil {
		return err
	}
	if err := m.writeVhost(fn, dir, certPath, keyPath); err != nil {
		return err
	}
	return reloadNginx()
}

// SaveCode writes just the handler source (used when editing a disabled
// function without redeploying).
func (m *FunctionManager) SaveCode(fn models.Function, sysUser, code string) error {
	dir := FunctionDir(m.WebRoot, sysUser, fn.Slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := writeFunctionFile(filepath.Join(dir, handlerFileName(fn.Runtime)), []byte(code)); err != nil {
		return err
	}
	chownFunctionTree(dir, sysUser)
	return nil
}

// ReadCode returns the current handler source for a function.
func (m *FunctionManager) ReadCode(fn models.Function, sysUser string) string {
	b, err := os.ReadFile(filepath.Join(FunctionDir(m.WebRoot, sysUser, fn.Slug), handlerFileName(fn.Runtime)))
	if err != nil {
		return ""
	}
	return string(b)
}

// Disable stops a URL function serving (stop backend, drop vhost). Scheduled
// functions are disabled by simply omitting them from the rebuilt cron.d file,
// which the caller triggers, so there is nothing to do here.
func (m *FunctionManager) Disable(fn models.Function, sysUser string) error {
	if fn.Trigger == TriggerSchedule {
		return nil
	}
	m.stopBackend(fn)
	removeNginxVhost(m.NginxDir, "fn-"+fn.Slug)
	return reloadNginx()
}

// Remove tears a function down completely. For URL functions that means the
// backend (FPM pool / systemd unit) and nginx vhost; the code directory goes for
// both. Scheduled functions are dropped from cron.d by a rebuild the caller runs.
func (m *FunctionManager) Remove(fn models.Function, sysUser string) error {
	urlFn := fn.Trigger != TriggerSchedule
	m.removeArtifacts(fn)
	os.RemoveAll(FunctionDir(m.WebRoot, sysUser, fn.Slug))
	if urlFn {
		return reloadNginx()
	}
	return nil
}

// removeArtifacts removes a URL function's backend (FPM pool / systemd unit) and
// nginx vhost, leaving the code directory in place. It does not reload nginx (the
// caller does). Scheduled functions have no such artifacts.
func (m *FunctionManager) removeArtifacts(fn models.Function) {
	if fn.Trigger == TriggerSchedule {
		return
	}
	m.stopBackend(fn)
	if fn.Runtime == "php" {
		os.Remove(fmt.Sprintf("/etc/php/%s/fpm/pool.d/repanel-fn-%s.conf", fn.Version, fn.Slug))
		ReloadService("php" + fn.Version + "-fpm")
	} else {
		os.Remove(functionUnitPath(fn.Slug))
		if have("systemctl") {
			run("systemctl", "daemon-reload")
		}
	}
	removeNginxVhost(m.NginxDir, "fn-"+fn.Slug)
}

// Teardown removes a function's backend + vhost but keeps its code directory,
// so it can be redeployed with changed settings (runtime, trigger, base, …).
func (m *FunctionManager) Teardown(fn models.Function) { m.removeArtifacts(fn) }

// Reload reloads the web server (used after a re-provision that changed or
// removed a vhost without otherwise deploying one).
func (m *FunctionManager) Reload() error { return reloadNginx() }

// RebuildCron regenerates /etc/cron.d/repanel-functions from every enabled
// scheduled function, mirroring the user-cron model in cron.go. Each entry runs
// the function's CLI runner as the owning tenant's system user.
func (m *FunctionManager) RebuildCron(funcs []models.Function, sysUserFor func(int64) string) error {
	if !Linux() {
		return nil
	}
	var sb strings.Builder
	sb.WriteString("# Managed by RePanel — function schedules. Do not edit.\nSHELL=/bin/sh\nPATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin\n\n")
	for _, fn := range funcs {
		if fn.Trigger != TriggerSchedule || !fn.Enabled {
			continue
		}
		if ValidateCronSchedule(fn.Schedule) != nil {
			continue
		}
		tenant := sysUserFor(fn.UserID)
		if tenant == "" {
			continue
		}
		cronUser, cmd, ok := m.cronEntry(fn, tenant)
		if !ok {
			continue
		}
		fmt.Fprintf(&sb, "%s %s %s\n", fn.Schedule, cronUser, cmd)
	}
	return os.WriteFile(functionCronFile, []byte(sb.String()), 0o644)
}

// cronEntry returns the cron user field and command for a scheduled function.
// With systemd-run available the entry runs as root and systemd-run drops to the
// tenant inside the sandbox — deliberately with no shell redirect, since root
// following a tenant-owned path would be a symlink-attack vector; output goes to
// the journal. Without systemd-run it falls back to running as the tenant, where
// appending to the tenant-owned log is safe.
func (m *FunctionManager) cronEntry(fn models.Function, tenant string) (cronUser, cmd string, ok bool) {
	dir := FunctionDir(m.WebRoot, tenant, fn.Slug)
	bin, ok := resolveRuntimeBin(fn)
	if !ok {
		return "", "", false
	}
	runner := filepath.Join(dir, runnerFileName(fn.Runtime))
	if have("systemd-run") {
		return "root", shellJoin(m.systemdRunArgv(fn, tenant, dir, bin, runner, fnScheduleMaxSec, false)), true
	}
	log := filepath.Join(dir, "last-run.log")
	return tenant, fmt.Sprintf("%s %s >> %s 2>&1", bin, shellQuote(runner), shellQuote(log)), true
}

// Invoke runs a function once with the given JSON payload (the event) and
// returns its structured response, captured logs and duration — the backend of
// the manual "Test" action. The handler runs as the owning tenant's system user
// (never root): the generated invoke runner reads the event on stdin, calls the
// handler, prints the return value as JSON to stdout, and routes anything the
// handler printed to stderr. A non-zero exit (an unhandled error) is reported as
// a failed run, not a transport error.
func (m *FunctionManager) Invoke(fn models.Function, sysUser, payload string, timeout time.Duration) (models.FunctionInvokeResult, error) {
	dir := FunctionDir(m.WebRoot, sysUser, fn.Slug)
	if err := m.ensureInvokeRunner(fn, sysUser, dir); err != nil {
		return models.FunctionInvokeResult{}, err
	}
	bin, ok := resolveRuntimeBin(fn)
	if !ok {
		return models.FunctionInvokeResult{}, fmt.Errorf("%s %s is not installed on this server", fn.Runtime, fn.Version)
	}
	script := filepath.Join(dir, invokeFileName(fn.Runtime))
	maxSec := int(timeout.Seconds())

	var name string
	var args []string
	switch {
	case Linux() && validSysName.MatchString(sysUser) && have("systemd-run"):
		// Preferred: fully sandboxed transient unit (clean env, fs/network/cpu/mem
		// confinement, hard timeout). --pipe streams the payload in and the result
		// out (captured in memory below — no tenant-writable redirect).
		argv := m.systemdRunArgv(fn, sysUser, dir, bin, script, maxSec, true)
		name, args = argv[0], argv[1:]
	case Linux() && validSysName.MatchString(sysUser) && have("sudo"):
		// Fallback: drop privileges with a clean environment (strips SUDO_*/panel
		// vars). No resource/network/fs isolation — systemd-run is recommended.
		home := filepath.Join(m.WebRoot, sysUser)
		name = "sudo"
		args = []string{"-n", "-u", sysUser, "--", "env", "-i",
			"HOME=" + home, "USER=" + sysUser, "LOGNAME=" + sysUser,
			"PATH=/usr/local/bin:/usr/bin:/bin", bin, script}
	case Linux() && validSysName.MatchString(sysUser):
		return models.FunctionInvokeResult{}, fmt.Errorf("systemd-run or sudo is required to run functions safely")
	default:
		name, args = bin, []string{script} // non-Linux dev host
	}

	// systemd enforces the hard cap via RuntimeMaxSec; give runCapture a little
	// extra so systemd's own timeout fires first and we still collect output.
	start := time.Now()
	out, errOut, runErr := runCapture(timeout+10*time.Second, dir, payload, name, args...)
	return models.FunctionInvokeResult{
		Response:   strings.TrimRight(out, "\n"),
		Logs:       strings.TrimRight(errOut, "\n"),
		OK:         runErr == nil,
		DurationMS: time.Since(start).Milliseconds(),
	}, nil
}

// ensureInvokeRunner makes sure the invoke runner exists and is owned by the
// tenant (covers functions created before the Test feature shipped).
func (m *FunctionManager) ensureInvokeRunner(fn models.Function, sysUser, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := writeFunctionFile(filepath.Join(dir, invokeFileName(fn.Runtime)), invokeSource(fn.Runtime)); err != nil {
		return err
	}
	chownFunctionTree(dir, sysUser)
	return nil
}

// deployBackend starts the per-runtime process: an FPM pool for PHP, a systemd
// unit for node/python.
func (m *FunctionManager) deployBackend(fn models.Function, sysUser, dir string) error {
	socket := functionSocket(fn)
	if fn.Runtime == "php" {
		poolDir := fmt.Sprintf("/etc/php/%s/fpm/pool.d", fn.Version)
		if st, err := os.Stat(poolDir); err != nil || !st.IsDir() {
			return nil // PHP-FPM not installed here; nothing to start
		}
		var sb strings.Builder
		if err := fnPoolTemplate.Execute(&sb, fnPoolData{Slug: fn.Slug, SysUser: sysUser, Socket: socket, Dir: dir}); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(poolDir, "repanel-fn-"+fn.Slug+".conf"), []byte(sb.String()), 0o644); err != nil {
			return err
		}
		ReloadService("php" + fn.Version + "-fpm")
		return nil
	}

	bin, ok := runtimeBinary(fn.Runtime, fn.Version)
	if !ok {
		if !Linux() {
			return nil // dev host without the runtime; skip
		}
		return fmt.Errorf("%s %s is not installed on this server", fn.Runtime, fn.Version)
	}
	var sb strings.Builder
	if err := fnUnitTemplate.Execute(&sb, fnUnitData{
		Slug: fn.Slug, Runtime: fn.Runtime, Version: fn.Version, SysUser: sysUser,
		Dir: dir, Socket: socket, RuntimeDir: functionRuntimeDir(fn.Slug),
		Bin: bin, Bootstrap: filepath.Join(dir, bootstrapFileName(fn.Runtime)),
		Props: sandboxProperties(dir, fn.AllowNetwork, 0, functionsHideProc()),
	}); err != nil {
		return err
	}
	if !have("systemctl") {
		return nil // not a systemd host; skip
	}
	if err := os.WriteFile(functionUnitPath(fn.Slug), []byte(sb.String()), 0o644); err != nil {
		return err
	}
	if _, err := run("systemctl", "daemon-reload"); err != nil {
		return err
	}
	if _, err := run("systemctl", "enable", "--now", functionUnit(fn.Slug)); err != nil {
		return err
	}
	return nil
}

// stopBackend stops (without deleting) the function's backend process.
func (m *FunctionManager) stopBackend(fn models.Function) {
	if fn.Runtime == "php" {
		return // shared php-fpm master keeps running; pool removal handles it
	}
	if have("systemctl") {
		run("systemctl", "disable", "--now", functionUnit(fn.Slug))
	}
}

// writeVhost renders the nginx server blocks for the function hostname.
func (m *FunctionManager) writeVhost(fn models.Function, dir, certPath, keyPath string) error {
	confDir := nginxConfDir(m.NginxDir)
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		return err
	}
	var sb strings.Builder
	if err := fnVhostTemplate.Execute(&sb, fnVhostData{
		Slug: fn.Slug, Hostname: fn.Hostname, Dir: dir, Socket: functionSocket(fn),
		CertPath: certPath, KeyPath: keyPath, IsPHP: fn.Runtime == "php",
	}); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(confDir, "fn-"+fn.Slug+".conf"), []byte(sb.String()), 0o644)
}

func writeFunctionFile(path string, content []byte) error {
	if len(content) > maxFunctionCodeSize {
		return fmt.Errorf("function code exceeds %d bytes", maxFunctionCodeSize)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|oNoFollow, 0o640)
	if err != nil {
		return err
	}
	if _, err := f.Write(content); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// ---- templates ----

type fnVhostData struct {
	Slug, Hostname, Dir, Socket, CertPath, KeyPath string
	IsPHP                                          bool
}

var fnVhostTemplate = template.Must(template.New("fnvhost").Parse(`# Managed by RePanel — function {{.Slug}}. Do not edit, changes will be overwritten.
server {
    listen 80;
    listen [::]:80;
    server_name {{.Hostname}};
    location /.well-known/acme-challenge/ { root {{.Dir}}; }
    location / { return 301 https://$host$request_uri; }
}

server {
    listen 443 ssl;
    listen [::]:443 ssl;
    http2 on;
    server_name {{.Hostname}};

    ssl_certificate     {{.CertPath}};
    ssl_certificate_key {{.KeyPath}};

    access_log /var/log/nginx/fn-{{.Slug}}.access.log;
    error_log  /var/log/nginx/fn-{{.Slug}}.error.log;

    client_max_body_size 16m;
    location /.well-known/acme-challenge/ { root {{.Dir}}; }
{{if .IsPHP}}
    root {{.Dir}};
    index index.php;
    location / { try_files $uri /index.php$is_args$args; }
    location ~ \.php$ {
        include fastcgi_params;
        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
        fastcgi_pass unix:{{.Socket}};
    }
{{else}}
    location / {
        proxy_pass http://unix:{{.Socket}};
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
{{end}}
}
`))

type fnPoolData struct{ Slug, SysUser, Socket, Dir string }

var fnPoolTemplate = template.Must(template.New("fnpool").Parse(`; Managed by RePanel — function {{.Slug}}
[repanel-fn-{{.Slug}}]
user = {{.SysUser}}
group = {{.SysUser}}
listen = {{.Socket}}
listen.owner = www-data
listen.group = www-data
pm = ondemand
pm.max_children = 5
pm.process_idle_timeout = 30s
chdir = {{.Dir}}
; Confinement: jail to the function dir, cap memory and run time, and disable the
; shell-exec family (the usual RCE vector). NOTE: PHP-FPM pools cannot enforce a
; private network or CPU/cgroup limits like the systemd-run sandbox used for the
; node/python and test/scheduled paths — treat PHP URL functions as the least
; isolated runtime.
php_admin_value[open_basedir] = {{.Dir}}:/tmp
php_admin_value[memory_limit] = 128M
php_admin_value[disable_functions] = exec,passthru,shell_exec,system,proc_open,popen,pcntl_exec
request_terminate_timeout = 30s
rlimit_files = 256
`))

type fnUnitData struct {
	Slug, Runtime, Version, SysUser, Dir, Socket, RuntimeDir, Bin, Bootstrap string
	Props                                                                    []string
}

var fnUnitTemplate = template.Must(template.New("fnunit").Parse(`# Managed by RePanel — function {{.Slug}} ({{.Runtime}} {{.Version}}). Do not edit.
[Unit]
Description=RePanel function {{.Slug}} ({{.Runtime}} {{.Version}})
After=network.target

[Service]
User={{.SysUser}}
Group={{.SysUser}}
WorkingDirectory={{.Dir}}
RuntimeDirectory={{.RuntimeDir}}
RuntimeDirectoryMode=0755
Environment=REPANEL_FN_SOCKET={{.Socket}}
ExecStart={{.Bin}} {{.Bootstrap}}
Restart=on-failure
RestartSec=2
{{- range .Props}}
{{.}}
{{- end}}

[Install]
WantedBy=multi-user.target
`))

// bootstrapSource returns the generated adapter that bridges HTTP <-> the
// function's handler for an exec'd runtime, or the PHP front controller.
func bootstrapSource(runtime string) []byte {
	switch runtime {
	case "node":
		return []byte(nodeBootstrap)
	case "php":
		return []byte(phpFrontController)
	default:
		return []byte(pythonBootstrap)
	}
}

// runnerSource returns the generated CLI runner that invokes the handler once
// (with a synthetic scheduled event) and prints its result — used by scheduled
// functions driven from cron.
func runnerSource(runtime string) []byte {
	switch runtime {
	case "node":
		return []byte(nodeRunner)
	case "php":
		return []byte(phpRunner)
	default:
		return []byte(pythonRunner)
	}
}

// invokeSource returns the generated runner used by the manual Test action: it
// reads the event JSON on stdin, calls the handler, prints the return value as
// JSON to stdout and the handler's own output to stderr, exiting non-zero on an
// unhandled error.
func invokeSource(runtime string) []byte {
	switch runtime {
	case "node":
		return []byte(nodeInvoke)
	case "php":
		return []byte(phpInvoke)
	default:
		return []byte(pythonInvoke)
	}
}

// DefaultFunctionCode returns starter handler code for a new function.
func DefaultFunctionCode(runtime string) string {
	switch runtime {
	case "node":
		return nodeDefaultHandler
	case "php":
		return phpDefaultHandler
	default:
		return pythonDefaultHandler
	}
}

const pythonBootstrap = `# Managed by RePanel — function runtime. Do not edit.
import json, os, sys
from http.server import BaseHTTPRequestHandler
from socketserver import ThreadingUnixStreamServer

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import handler as user

SOCKET = os.environ["REPANEL_FN_SOCKET"]


class H(BaseHTTPRequestHandler):
    def _run(self):
        length = int(self.headers.get("Content-Length") or 0)
        body = self.rfile.read(length).decode("utf-8", "replace") if length else ""
        path, _, query = self.path.partition("?")
        event = {
            "method": self.command, "path": path, "query": query,
            "headers": {k.lower(): v for k, v in self.headers.items()},
            "body": body,
        }
        try:
            res = user.handler(event) or {}
        except Exception as e:
            res = {"statusCode": 500, "body": "function error: %s" % e}
        out = res.get("body", "")
        headers = dict(res.get("headers") or {})
        if not isinstance(out, (str, bytes)):
            out = json.dumps(out)
            headers.setdefault("Content-Type", "application/json")
        data = out.encode("utf-8") if isinstance(out, str) else out
        self.send_response(int(res.get("statusCode", 200)))
        if not any(k.lower() == "content-type" for k in headers):
            self.send_header("Content-Type", "text/plain; charset=utf-8")
        for k, v in headers.items():
            self.send_header(k, v)
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    do_GET = do_POST = do_PUT = do_DELETE = do_PATCH = _run

    def log_message(self, *a):
        pass


if os.path.exists(SOCKET):
    os.unlink(SOCKET)
srv = ThreadingUnixStreamServer(SOCKET, H)
os.chmod(SOCKET, 0o666)
srv.serve_forever()
`

const nodeBootstrap = `// Managed by RePanel — function runtime. Do not edit.
const http = require('http');
const fs = require('fs');
const user = require('./handler.js');
const SOCKET = process.env.REPANEL_FN_SOCKET;

function hasCT(h) { return Object.keys(h).some((k) => k.toLowerCase() === 'content-type'); }

const server = http.createServer((req, res) => {
  const chunks = [];
  req.on('data', (c) => chunks.push(c));
  req.on('end', async () => {
    const body = Buffer.concat(chunks).toString('utf8');
    const [path, query = ''] = req.url.split('?');
    const event = { method: req.method, path, query, headers: req.headers, body };
    let r;
    try { r = (await user.handler(event)) || {}; }
    catch (e) { r = { statusCode: 500, body: 'function error: ' + e }; }
    let out = r.body == null ? '' : r.body;
    const headers = Object.assign({}, r.headers);
    if (typeof out !== 'string' && !Buffer.isBuffer(out)) {
      out = JSON.stringify(out);
      if (!hasCT(headers)) headers['Content-Type'] = 'application/json';
    }
    if (!hasCT(headers)) headers['Content-Type'] = 'text/plain; charset=utf-8';
    res.writeHead(r.statusCode || 200, headers);
    res.end(out);
  });
});

try { fs.unlinkSync(SOCKET); } catch (e) { /* ignore */ }
server.listen(SOCKET, () => { try { fs.chmodSync(SOCKET, 0o666); } catch (e) { /* ignore */ } });
`

const phpFrontController = `<?php
// Managed by RePanel — function front controller. Do not edit.
require __DIR__ . '/handler.php';
$headers = function_exists('getallheaders') ? array_change_key_case(getallheaders()) : [];
$event = [
    'method'  => $_SERVER['REQUEST_METHOD'] ?? 'GET',
    'path'    => parse_url($_SERVER['REQUEST_URI'] ?? '/', PHP_URL_PATH),
    'query'   => $_SERVER['QUERY_STRING'] ?? '',
    'headers' => $headers,
    'body'    => file_get_contents('php://input'),
];
$res = handler($event);
if (!is_array($res)) {
    $res = ['statusCode' => 200, 'body' => (string) $res];
}
http_response_code($res['statusCode'] ?? 200);
$resHeaders = $res['headers'] ?? [];
$body = $res['body'] ?? '';
if (!is_string($body)) {
    $body = json_encode($body);
    if (!isset($resHeaders['Content-Type'])) {
        $resHeaders['Content-Type'] = 'application/json';
    }
}
$hasCT = false;
foreach ($resHeaders as $k => $v) {
    if (strtolower($k) === 'content-type') { $hasCT = true; }
    header("$k: $v");
}
if (!$hasCT) {
    header('Content-Type: text/plain; charset=utf-8');
}
echo $body;
`

const pythonRunner = `# Managed by RePanel — scheduled runner. Do not edit.
import datetime, json, os, sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import handler as user

event = {"source": "schedule", "time": datetime.datetime.utcnow().isoformat() + "Z"}
res = user.handler(event)
body = res.get("body", res) if isinstance(res, dict) else res
print(body if isinstance(body, str) else json.dumps(body))
`

const nodeRunner = `// Managed by RePanel — scheduled runner. Do not edit.
const user = require('./handler.js');
(async () => {
  const event = { source: 'schedule', time: new Date().toISOString() };
  const res = (await user.handler(event)) || {};
  const body = res && typeof res === 'object' && 'body' in res ? res.body : res;
  console.log(typeof body === 'string' ? body : JSON.stringify(body));
})().catch((e) => { console.error('function error: ' + e); process.exit(1); });
`

const phpRunner = `<?php
// Managed by RePanel — scheduled runner. Do not edit.
require __DIR__ . '/handler.php';
$event = ['source' => 'schedule', 'time' => gmdate('c')];
$res = handler($event);
$body = is_array($res) && array_key_exists('body', $res) ? $res['body'] : $res;
echo is_string($body) ? $body : json_encode($body);
echo "\n";
`

const pythonInvoke = `# Managed by RePanel — test invoker. Do not edit.
import io, json, os, sys, traceback

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import handler as user

raw = sys.stdin.read()
try:
    event = json.loads(raw) if raw.strip() else {}
except Exception as e:
    sys.stderr.write("invalid JSON payload: %s\n" % e)
    event = {}

cap = io.StringIO()
real = sys.stdout
sys.stdout = cap
ok = True
try:
    res = user.handler(event)
except Exception:
    ok = False
    tb = traceback.format_exc()
    res = {"errorMessage": tb.strip().splitlines()[-1], "stackTrace": tb}
finally:
    sys.stdout = real

sys.stderr.write(cap.getvalue())
try:
    real.write(json.dumps(res, default=str))
except Exception:
    real.write(json.dumps(str(res)))
sys.exit(0 if ok else 1)
`

const nodeInvoke = `// Managed by RePanel — test invoker. Do not edit.
const path = require('path');
const user = require(path.join(__dirname, 'handler.js'));

let raw = '';
process.stdin.on('data', (c) => (raw += c));
process.stdin.on('end', async () => {
  let event = {};
  try { event = raw.trim() ? JSON.parse(raw) : {}; }
  catch (e) { process.stderr.write('invalid JSON payload: ' + e + '\n'); }

  const origLog = console.log;
  console.log = (...a) => process.stderr.write(a.map(String).join(' ') + '\n');
  let res, ok = true;
  try { res = await user.handler(event); }
  catch (e) { ok = false; res = { errorMessage: String((e && e.message) || e), stackTrace: String((e && e.stack) || '') }; }
  console.log = origLog;

  try { process.stdout.write(JSON.stringify(res === undefined ? null : res)); }
  catch (e) { process.stdout.write(JSON.stringify(String(res))); }
  process.exit(ok ? 0 : 1);
});
`

const phpInvoke = `<?php
// Managed by RePanel — test invoker. Do not edit.
require __DIR__ . '/handler.php';
$raw = file_get_contents('php://stdin');
$event = [];
if (trim($raw) !== '') {
    $decoded = json_decode($raw, true);
    if (json_last_error() === JSON_ERROR_NONE) {
        $event = $decoded;
    } else {
        fwrite(STDERR, 'invalid JSON payload: ' . json_last_error_msg() . "\n");
    }
}
ob_start();
$ok = true;
try {
    $res = handler($event);
} catch (\Throwable $e) {
    $ok = false;
    $res = ['errorMessage' => $e->getMessage(), 'stackTrace' => $e->getTraceAsString()];
}
$logs = ob_get_clean();
if ($logs !== '') {
    fwrite(STDERR, $logs);
}
fwrite(STDOUT, json_encode($res));
exit($ok ? 0 : 1);
`

const pythonDefaultHandler = `def handler(event):
    """Entry point. Return a dict with statusCode, optional headers, and body.
    'body' may be a string or any JSON-serialisable value."""
    return {
        "statusCode": 200,
        "headers": {"Content-Type": "application/json"},
        "body": {"message": "Hello from your RePanel Python function!", "path": event.get("path")},
    }
`

const nodeDefaultHandler = `// Entry point. Return { statusCode, headers?, body }.
// 'body' may be a string or any JSON-serialisable value.
exports.handler = async (event) => {
  return {
    statusCode: 200,
    headers: { 'Content-Type': 'application/json' },
    body: { message: 'Hello from your RePanel Node function!', path: event.path },
  };
};
`

const phpDefaultHandler = `<?php
// Entry point. Return ['statusCode' => int, 'headers' => [...], 'body' => mixed].
// 'body' may be a string or any JSON-serialisable value.
function handler(array $event): array {
    return [
        'statusCode' => 200,
        'headers' => ['Content-Type' => 'application/json'],
        'body' => ['message' => 'Hello from your RePanel PHP function!', 'path' => $event['path']],
    ];
}
`
