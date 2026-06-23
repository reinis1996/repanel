package system

import (
	"io"
	"strings"
	"testing"

	"github.com/reinis1996/repanel/internal/models"
)

func TestNodeVersions(t *testing.T) {
	if len(KnownNodeVersions()) == 0 {
		t.Fatal("no known Node versions offered")
	}
	if !validNodeMajor("20") {
		t.Error("Node 20 should be installable")
	}
	if validNodeMajor("19") || validNodeMajor("--x") {
		t.Error("unexpected Node version accepted")
	}
}

func TestNodeUnitName(t *testing.T) {
	if got := nodeUnit("ex-ample.com"); got != "repanel-app-ex_ample_com" {
		t.Errorf("nodeUnit = %q", got)
	}
}

// The Node app sandbox must drop privileges, lock down the filesystem (only the
// app dir writable) and cap resources, while keeping network enabled.
func TestNodeSandboxProps(t *testing.T) {
	p := strings.Join(nodeSandboxProps("/srv/app"), "\n")
	for _, want := range []string{
		"NoNewPrivileges=yes", "PrivateTmp=yes", "ProtectSystem=strict", "ProtectHome=yes",
		"ReadWritePaths=/srv/app", "MemoryMax=512M", "CPUQuota=80%", "RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("node sandbox missing %q", want)
		}
	}
	if strings.Contains(p, "PrivateNetwork") {
		t.Error("node apps must keep network enabled")
	}
}

// The unit template renders with env + props without error.
func TestNodeUnitTemplate(t *testing.T) {
	err := nodeUnitTemplate.Execute(io.Discard, nodeUnitData{
		Domain: "x.com", Version: "20", SysUser: "rpu1", AppDir: "/d", Bin: "/opt/repanel/node/20/bin/node",
		Startup: "app.js", Port: 30000, Env: []string{"FOO=bar"}, Props: nodeSandboxProps("/d"),
	})
	if err != nil {
		t.Fatalf("render unit: %v", err)
	}
	_ = models.Domain{}
}
