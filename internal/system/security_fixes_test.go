package system

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// F-04: distinct user ids must never collapse onto the same system account.
func TestSysUserNameUnique(t *testing.T) {
	seen := map[string]int64{}
	for _, id := range []int64{1, 2, 10, 999, 123456} {
		name := SysUserName(id)
		if !validSysName.MatchString(name) {
			t.Errorf("SysUserName(%d)=%q is not a valid system name", id, name)
		}
		if other, dup := seen[name]; dup {
			t.Errorf("SysUserName collision: ids %d and %d both -> %q", other, id, name)
		}
		seen[name] = id
	}
}

// F-12: a schedule containing a newline must be rejected so it cannot inject
// extra cron.d lines.
func TestValidateCronScheduleRejectsNewline(t *testing.T) {
	bad := []string{"* * * * *\n* * * * * root id", "*\n* * * *", "* * *\t* *", "@daily\nrm -rf /"}
	for _, s := range bad {
		if err := ValidateCronSchedule(s); err == nil {
			t.Errorf("schedule %q should be rejected", s)
		}
	}
	for _, s := range []string{"* * * * *", "@daily", "*/5 * * * *"} {
		if err := ValidateCronSchedule(s); err != nil {
			t.Errorf("schedule %q should be valid: %v", s, err)
		}
	}
}

// F-02: lexical traversal is rejected, and a symlink escaping the jail is
// rejected after resolution.
func TestResolveJailedRejectsEscape(t *testing.T) {
	jail := t.TempDir()
	for _, rel := range []string{"../etc/passwd", "../../x", "a/../../b"} {
		if _, err := ResolveJailed(jail, rel); err == nil {
			t.Errorf("ResolveJailed should reject %q", rel)
		}
	}
	if _, err := ResolveJailed(jail, "ok/sub"); err != nil {
		t.Errorf("ResolveJailed should accept in-jail path: %v", err)
	}

	if runtime.GOOS == "windows" {
		return // symlink creation typically needs privilege on Windows
	}
	outside := t.TempDir()
	link := filepath.Join(jail, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	if _, err := ResolveJailed(jail, "escape/secret"); err == nil {
		t.Errorf("ResolveJailed must reject a path through a jail-escaping symlink")
	}
}

// A symlink planted at the write target must not be followed: the panel writes
// as root, so following it would let a tenant clobber a file outside their jail.
func TestWriteFileJailedRejectsSymlinkLeaf(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation typically needs privilege on Windows")
	}
	jail := t.TempDir()
	// A real file inside the jail, and a symlink (also inside the jail) pointing
	// at it. ResolveJailed accepts the symlink because it resolves in-jail, but
	// the write must still refuse to follow it (O_NOFOLLOW), so the target keeps
	// its contents.
	realTarget := filepath.Join(jail, "real")
	if err := os.WriteFile(realTarget, []byte("real"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realTarget, filepath.Join(jail, "link")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	if err := WriteFileJailed(jail, "link", []byte("pwned"), ""); err == nil {
		t.Error("WriteFileJailed must refuse to write through a symlink leaf")
	}
	if data, _ := os.ReadFile(realTarget); string(data) != "real" {
		t.Error("write should not have followed the symlink to its target")
	}
}

// HashMailPassword must never embed the cleartext password in the result and
// must produce a SHA512-CRYPT ($6$) hash when a hashing tool is available.
func TestHashMailPassword(t *testing.T) {
	if !have("openssl") && !have("doveadm") {
		t.Skip("no hashing tool available")
	}
	hash, err := HashMailPassword("s3cr3t-pass")
	if err != nil {
		t.Fatalf("HashMailPassword: %v", err)
	}
	if !strings.HasPrefix(hash, "$6$") {
		t.Errorf("expected a $6$ SHA512-CRYPT hash, got %q", hash)
	}
	if strings.Contains(hash, "s3cr3t-pass") {
		t.Error("hash must not contain the cleartext password")
	}
	if _, err := HashMailPassword("bad\npassword"); err == nil {
		t.Error("passwords with newlines must be rejected")
	}
}
