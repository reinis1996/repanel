package system

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/reinis1996/repanel/internal/models"
)

func TestValidFnSlug(t *testing.T) {
	for _, s := range []string{"abc123", "0123456789", "deadbeef00"} {
		if !validFnSlug.MatchString(s) {
			t.Errorf("%q should be valid", s)
		}
	}
	for _, s := range []string{"AB", "ab", "has-dash", "with.dot", "UPPER1", "a b", "", "../etc"} {
		if validFnSlug.MatchString(s) {
			t.Errorf("%q should be rejected", s)
		}
	}
}

// A version/runtime not installed on the host must never validate or resolve a
// binary — that allowlist is what keeps version strings out of paths/exec.
func TestFunctionRuntimeValidRejectsUnknown(t *testing.T) {
	if FunctionRuntimeValid("python", "0.0") {
		t.Error("nonexistent python version must be rejected")
	}
	if FunctionRuntimeValid("ruby", "3.0") {
		t.Error("unknown runtime must be rejected")
	}
	if _, ok := runtimeBinary("node", "9999"); ok {
		t.Error("unknown node version must not resolve a binary")
	}
	if _, ok := runtimeBinary("php", "8.3"); ok {
		t.Error("php is served via FPM and has no exec binary here")
	}
}

func TestFunctionSocketPaths(t *testing.T) {
	if got := functionSocket(models.Function{Slug: "abc123", Runtime: "php"}); !strings.HasPrefix(got, "/run/php/") {
		t.Errorf("php socket = %q", got)
	}
	if got := functionSocket(models.Function{Slug: "abc123", Runtime: "node"}); !strings.Contains(got, "repanel-fn-abc123") {
		t.Errorf("node socket = %q", got)
	}
}

func TestFunctionTemplatesRender(t *testing.T) {
	if err := fnVhostTemplate.Execute(io.Discard, fnVhostData{Slug: "abc123", Hostname: "abc123.function-url.example.com", Dir: "/d", Socket: "/s", CertPath: "/c", KeyPath: "/k", IsPHP: true}); err != nil {
		t.Fatalf("php vhost: %v", err)
	}
	if err := fnVhostTemplate.Execute(io.Discard, fnVhostData{Slug: "abc123", Hostname: "h", Dir: "/d", Socket: "/s"}); err != nil {
		t.Fatalf("proxy vhost: %v", err)
	}
	if err := fnPoolTemplate.Execute(io.Discard, fnPoolData{Slug: "abc123", SysUser: "rpu1", Socket: "/s", Dir: "/d"}); err != nil {
		t.Fatalf("pool: %v", err)
	}
	if err := fnUnitTemplate.Execute(io.Discard, fnUnitData{Slug: "abc123", Runtime: "node", Version: "20", SysUser: "rpu1", Dir: "/d", Socket: "/s", RuntimeDir: "repanel-fn-abc123", Bin: "/usr/bin/node", Bootstrap: "/d/bootstrap.js"}); err != nil {
		t.Fatalf("unit: %v", err)
	}
}

func TestDefaultFunctionCodeAndBootstrap(t *testing.T) {
	for _, rt := range functionRuntimes {
		if strings.TrimSpace(DefaultFunctionCode(rt)) == "" {
			t.Errorf("no default code for %s", rt)
		}
		if len(bootstrapSource(rt)) == 0 {
			t.Errorf("no bootstrap for %s", rt)
		}
		if len(runnerSource(rt)) == 0 {
			t.Errorf("no runner for %s", rt)
		}
	}
	if !strings.Contains(DefaultFunctionCode("php"), "function handler") {
		t.Error("php default should define handler()")
	}
}

// A scheduled function deploys only handler + runner (no vhost / backend), and a
// disabled or absent schedule is excluded from the generated cron.d file.
func TestFunctionScheduleDeployAndCron(t *testing.T) {
	dir := t.TempDir()
	m := NewFunctionManager(dir, dir)
	fn := models.Function{Slug: "sched01", Runtime: "php", Version: "0.0",
		Trigger: TriggerSchedule, Schedule: "0 * * * *", Enabled: true}

	if err := m.Write(fn, "rpu1", DefaultFunctionCode("php"), "", ""); err != nil {
		t.Fatalf("Write: %v", err)
	}
	codeDir := FunctionDir(dir, "rpu1", "sched01")
	for _, f := range []string{"handler.php", "runner.php"} {
		if _, err := os.Stat(filepath.Join(codeDir, f)); err != nil {
			t.Errorf("missing %s: %v", f, err)
		}
	}
	// A scheduled function must NOT get an nginx vhost.
	if _, err := os.Stat(filepath.Join(nginxConfDir(dir), "fn-sched01.conf")); !os.IsNotExist(err) {
		t.Error("scheduled function should not create an nginx vhost")
	}

	// cronEntry resolves for php (CLI present on the dev host) or is skipped.
	if _, cmd, ok := m.cronEntry(fn, "rpu1"); ok {
		if !strings.Contains(cmd, "runner.php") {
			t.Errorf("cron command should reference the runner: %q", cmd)
		}
	}

	if err := m.Remove(fn, "rpu1"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(codeDir); !os.IsNotExist(err) {
		t.Error("code dir should be removed")
	}
}

// The sandbox property set must isolate by default (no network, resource caps,
// fs protections) and relax network only when explicitly allowed.
func TestSandboxProperties(t *testing.T) {
	isolated := strings.Join(sandboxProperties("/d", false, 30, true), "\n")
	for _, want := range []string{
		"NoNewPrivileges=yes", "PrivateTmp=yes", "PrivateDevices=yes", "ProtectSystem=strict",
		"ProtectHome=yes", "ReadWritePaths=/d", "ProtectHostname=yes", "ProtectKernelLogs=yes",
		"SystemCallArchitectures=native", "SystemCallFilter=@system-service", "RemoveIPC=yes",
		"MemoryMax=128M", "CPUQuota=50%", "TasksMax=64", "PrivateNetwork=yes", "RuntimeMaxSec=30",
		"ProtectProc=invisible",
		// Private root: host filesystem replaced by a tmpfs with only /usr bound.
		"TemporaryFileSystem=/:ro", "BindReadOnlyPaths=/usr", "BindReadOnlyPaths=-/lib64",
	} {
		if !strings.Contains(isolated, want) {
			t.Errorf("isolated sandbox missing %q", want)
		}
	}
	if strings.Contains(isolated, "AF_INET") {
		t.Error("isolated function must not permit AF_INET")
	}
	// An isolated function must not get resolver/CA binds.
	if strings.Contains(isolated, "resolv.conf") {
		t.Error("isolated function must not bind /etc/resolv.conf")
	}
	// Deliberate omissions that would break runtimes.
	if strings.Contains(isolated, "MemoryDenyWriteExecute") {
		t.Error("must not set MemoryDenyWriteExecute (breaks Node/PHP JIT)")
	}
	if strings.Contains(isolated, "ProcSubset=pid") {
		t.Error("must not set ProcSubset=pid (hides /proc/meminfo from runtimes)")
	}

	// hideProc=false (older systemd) omits the 247+ property.
	if strings.Contains(strings.Join(sandboxProperties("/d", false, 30, false), "\n"), "ProtectProc") {
		t.Error("ProtectProc must be gated off when unsupported")
	}

	net := strings.Join(sandboxProperties("/d", true, 0, true), "\n")
	if strings.Contains(net, "PrivateNetwork=yes") {
		t.Error("network-enabled function must not set PrivateNetwork")
	}
	if !strings.Contains(net, "AF_INET") {
		t.Error("network-enabled function should permit AF_INET")
	}
	if !strings.Contains(net, "BindReadOnlyPaths=-/etc/resolv.conf") {
		t.Error("network-enabled function should bind the resolver config")
	}
	if strings.Contains(net, "RuntimeMaxSec") {
		t.Error("no runtime cap should be set when runtimeMaxSec is 0")
	}
}

// Invoke runs the handler with a payload and returns its response. Uses an empty
// sysUser so the run happens directly (no sudo drop), keeping the test hermetic;
// skips when no scripting runtime (python/node) is available on the host.
func TestFunctionInvoke(t *testing.T) {
	var rt, ver string
	for _, r := range AvailableFunctionRuntimes() {
		if (r.Runtime == "python" || r.Runtime == "node") && len(r.Versions) > 0 {
			rt, ver = r.Runtime, r.Versions[0]
			break
		}
	}
	if rt == "" {
		t.Skip("no python/node runtime installed")
	}
	dir := t.TempDir()
	m := NewFunctionManager(dir, dir)
	fn := models.Function{Slug: "inv00001", Runtime: rt, Version: ver, Trigger: TriggerSchedule, Enabled: true}
	if err := m.SaveCode(fn, "", DefaultFunctionCode(rt)); err != nil {
		t.Fatalf("SaveCode: %v", err)
	}
	res, err := m.Invoke(fn, "", `{"hello":"world"}`, 30*time.Second)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if !res.OK {
		t.Fatalf("expected ok; logs=%q response=%q", res.Logs, res.Response)
	}
	if !strings.Contains(res.Response, "Hello from") {
		t.Errorf("unexpected response: %q", res.Response)
	}
}

// Write lays down code + bootstrap + nginx vhost; Remove cleans them all up.
// Uses an unknown PHP version so deployBackend finds no /etc/php pool dir and
// makes no host changes — keeping the test hermetic on any platform.
func TestFunctionManagerWriteRemove(t *testing.T) {
	dir := t.TempDir()
	m := NewFunctionManager(dir, dir)
	fn := models.Function{Slug: "abc123", Runtime: "php", Version: "0.0",
		Hostname: "abc123.function-url.example.com", Enabled: true}

	if err := m.Write(fn, "rpu1", DefaultFunctionCode("php"), "/c/cert.pem", "/c/key.pem"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	codeDir := FunctionDir(dir, "rpu1", "abc123")
	for _, f := range []string{"handler.php", "index.php"} {
		if _, err := os.Stat(filepath.Join(codeDir, f)); err != nil {
			t.Errorf("missing %s: %v", f, err)
		}
	}
	vhost := filepath.Join(nginxConfDir(dir), "fn-abc123.conf")
	if _, err := os.Stat(vhost); err != nil {
		t.Errorf("missing vhost: %v", err)
	}

	if err := m.Remove(fn, "rpu1"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(codeDir); !os.IsNotExist(err) {
		t.Error("code dir should be removed")
	}
	if _, err := os.Stat(vhost); !os.IsNotExist(err) {
		t.Error("vhost should be removed")
	}
}
