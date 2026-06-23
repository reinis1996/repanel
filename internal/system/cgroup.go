package system

import (
	"fmt"
	"os"
	"os/user"
	"strconv"
	"strings"
)

// Per-account live resource limits via systemd cgroups. Each account gets a
// slice (repanel-acct-<id>.slice) that its Node apps run in, and the same caps
// are applied to the account's login slice (user-<uid>.slice) so SSH/SFTP
// sessions are bounded too. Limits are CPU (% of one core), a memory hard cap,
// and a process/thread (TasksMax) cap; 0 means unlimited.
//
// Note: PHP-FPM pools run under the shared php-fpm master (system.slice), so
// they are not covered by these per-account slices — capping FPM per account
// would need a per-user FPM master. The slice covers Node apps and shell access.

// AccountLimits are an account's live resource caps (0 = unlimited).
type AccountLimits struct {
	CPUQuotaPct  int // percent of a single core; 100 = one core
	MemoryMaxMB  int
	ProcessesMax int
}

func (l AccountLimits) empty() bool {
	return l.CPUQuotaPct <= 0 && l.MemoryMaxMB <= 0 && l.ProcessesMax <= 0
}

// AccountSliceName is the systemd slice a panel account's workloads run in.
func AccountSliceName(userID int64) string {
	return fmt.Sprintf("repanel-acct-%d.slice", userID)
}

func accountSlicePath(userID int64) string {
	return "/etc/systemd/system/" + AccountSliceName(userID)
}

// ApplyAccountLimits writes/updates the account slice and the user login slice
// drop-in with the given caps and applies them to any running cgroups. With all
// limits unset it removes the managed files (reverting to unlimited). A no-op
// off Linux or without systemd.
func ApplyAccountLimits(userID int64, sysUser string, lim AccountLimits) error {
	if !Linux() || !have("systemctl") {
		return nil
	}
	slicePath := accountSlicePath(userID)
	userSlice, userDropin := "", ""
	if uid, ok := lookupUID(sysUser); ok {
		userSlice = "user-" + uid + ".slice"
		userDropin = "/etc/systemd/system/" + userSlice + ".d/50-repanel.conf"
	}

	if lim.empty() {
		os.Remove(slicePath)
		if userDropin != "" {
			os.Remove(userDropin)
		}
		run("systemctl", "daemon-reload")
		clearRuntimeLimits(AccountSliceName(userID))
		if userSlice != "" {
			clearRuntimeLimits(userSlice)
		}
		return nil
	}

	body := "# Managed by RePanel — per-account resource limits.\n[Slice]\n" + sliceLimitLines(lim)
	if err := os.WriteFile(slicePath, []byte(body), 0o644); err != nil {
		return err
	}
	if userDropin != "" {
		if err := os.MkdirAll(filepathDir(userDropin), 0o755); err == nil {
			os.WriteFile(userDropin, []byte(body), 0o644)
		}
	}
	run("systemctl", "daemon-reload")
	// Apply to the running cgroups immediately (best effort — the slice may be
	// inactive if the account has nothing running yet).
	applyRuntimeLimits(AccountSliceName(userID), lim)
	if userSlice != "" {
		applyRuntimeLimits(userSlice, lim)
	}
	return nil
}

// RemoveAccountLimits clears an account's slice and login-slice caps (on delete).
func RemoveAccountLimits(userID int64, sysUser string) {
	ApplyAccountLimits(userID, sysUser, AccountLimits{})
}

func sliceLimitLines(lim AccountLimits) string {
	var b strings.Builder
	if lim.CPUQuotaPct > 0 {
		fmt.Fprintf(&b, "CPUQuota=%d%%\n", lim.CPUQuotaPct)
	}
	if lim.MemoryMaxMB > 0 {
		fmt.Fprintf(&b, "MemoryMax=%dM\n", lim.MemoryMaxMB)
	}
	if lim.ProcessesMax > 0 {
		fmt.Fprintf(&b, "TasksMax=%d\n", lim.ProcessesMax)
	}
	return b.String()
}

// applyRuntimeLimits sets the live cgroup properties on an (active) slice.
func applyRuntimeLimits(slice string, lim AccountLimits) {
	props := []string{}
	if lim.CPUQuotaPct > 0 {
		props = append(props, fmt.Sprintf("CPUQuota=%d%%", lim.CPUQuotaPct))
	} else {
		props = append(props, "CPUQuota=")
	}
	if lim.MemoryMaxMB > 0 {
		props = append(props, fmt.Sprintf("MemoryMax=%dM", lim.MemoryMaxMB))
	} else {
		props = append(props, "MemoryMax=infinity")
	}
	if lim.ProcessesMax > 0 {
		props = append(props, "TasksMax="+strconv.Itoa(lim.ProcessesMax))
	} else {
		props = append(props, "TasksMax=infinity")
	}
	run("systemctl", append([]string{"set-property", "--runtime", slice}, props...)...)
}

func clearRuntimeLimits(slice string) {
	run("systemctl", "set-property", "--runtime", slice, "CPUQuota=", "MemoryMax=infinity", "TasksMax=infinity")
}

func lookupUID(sysUser string) (string, bool) {
	if sysUser == "" {
		return "", false
	}
	u, err := user.Lookup(sysUser)
	if err != nil {
		return "", false
	}
	return u.Uid, true
}

// filepathDir returns the directory of a path (avoids importing path/filepath
// just for this in a file that otherwise uses string paths).
func filepathDir(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[:i]
	}
	return "."
}
