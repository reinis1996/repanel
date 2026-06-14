package system

import (
	"os"
	"path/filepath"
	"runtime"
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
