package system

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var validSysName = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,30}$`)

// SysUserName maps a panel user *id* to the unix account that owns its files.
// It is intentionally derived from the immutable, unique user id rather than
// the (mutable, lossy-when-sanitised) username so that two distinct panel users
// can never collapse onto the same system account and share a home directory,
// PHP-FPM pool or file-manager jail (see SECURITY_AUDIT F-04).
func SysUserName(userID int64) string {
	return fmt.Sprintf("rpu%d", userID)
}

// minManagedUID is the lowest uid the panel will create or modify. Accounts
// below it are system accounts (root, www-data, postgres, bind, vmail, …) which
// the panel must never touch (see SECURITY_AUDIT F-01).
const minManagedUID = 1000

// managedAccount reports whether name is an existing non-system account the
// panel is allowed to modify or delete (uid >= minManagedUID).
func managedAccount(name string) bool {
	if !Linux() {
		return true // dev hosts: integrations are no-ops anyway
	}
	return UIDOf(name) >= minManagedUID
}

// EnsureUnixUser idempotently creates a locked owner account for a tenant's
// files. It is only used for the panel-derived `rpu<id>` accounts; a
// pre-existing account of that exact name is accepted as-is.
func EnsureUnixUser(name, home string) error {
	if !Linux() {
		return nil
	}
	if !validSysName.MatchString(name) {
		return fmt.Errorf("invalid system username %q", name)
	}
	if _, err := run("id", "-u", name); err == nil {
		return nil // exists
	}
	if _, err := run("useradd", "--create-home", "--home-dir", home,
		"--shell", "/usr/sbin/nologin", name); err != nil {
		return fmt.Errorf("useradd: %w", err)
	}
	return nil
}

// CreateUnixUser creates a brand-new locked account and FAILS if an account of
// that name already exists. Unlike EnsureUnixUser it never adopts a
// pre-existing (possibly privileged or foreign-tenant) account, which is what
// makes FTP-account creation safe (see SECURITY_AUDIT F-01).
func CreateUnixUser(name, home string) error {
	if !Linux() {
		return nil
	}
	if !validSysName.MatchString(name) {
		return fmt.Errorf("invalid system username %q", name)
	}
	if _, err := run("id", "-u", name); err == nil {
		return fmt.Errorf("account %q already exists", name)
	}
	if _, err := run("useradd", "--home-dir", home, "--shell", "/usr/sbin/nologin", name); err != nil {
		return fmt.Errorf("useradd: %w", err)
	}
	return nil
}

// SetUnixPassword sets the password (used for FTP/SFTP logins). It refuses to
// touch system accounts (uid < minManagedUID) so a crafted account name cannot
// reset root's (or another service's) password.
func SetUnixPassword(name, password string) error {
	if !Linux() {
		return nil
	}
	if !validSysName.MatchString(name) {
		return fmt.Errorf("invalid system username %q", name)
	}
	if !managedAccount(name) {
		return fmt.Errorf("refusing to modify system account %q", name)
	}
	if strings.ContainsAny(password, "\n\r:") {
		return fmt.Errorf("password contains invalid characters")
	}
	cmd := exec.Command("chpasswd")
	cmd.Stdin = strings.NewReader(name + ":" + password + "\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("chpasswd: %w: %s", err, out)
	}
	return nil
}

// DeleteUnixUser removes the account, keeping home dirs for safety. It refuses
// to delete system accounts (uid < minManagedUID).
func DeleteUnixUser(name string) error {
	if !Linux() || !validSysName.MatchString(name) {
		return nil
	}
	if _, err := run("id", "-u", name); err != nil {
		return nil // already gone
	}
	if !managedAccount(name) {
		return fmt.Errorf("refusing to delete system account %q", name)
	}
	_, err := run("userdel", name)
	return err
}

// EnsureDocRoot creates the document root with a placeholder index page and
// hands ownership to the system user.
func EnsureDocRoot(path, sysUser, domain string) error {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return err
	}
	index := filepath.Join(path, "index.html")
	if _, err := os.Stat(index); os.IsNotExist(err) {
		page := fmt.Sprintf(`<!doctype html>
<html><head><meta charset="utf-8"><title>%s</title>
<style>body{font-family:system-ui;display:flex;align-items:center;justify-content:center;height:100vh;margin:0;background:#f4f6fa;color:#24344d}
.card{text-align:center;padding:3rem;background:#fff;border-radius:12px;box-shadow:0 4px 24px rgba(0,0,0,.06)}</style>
</head><body><div class="card"><h1>%s</h1><p>This site was just created with RePanel.<br>Upload your content to get started.</p></div></body></html>
`, domain, domain)
		if err := os.WriteFile(index, []byte(page), 0o644); err != nil {
			return err
		}
	}
	if Linux() && validSysName.MatchString(sysUser) {
		if _, err := run("chown", "-R", sysUser+":"+sysUser, path); err != nil {
			return err
		}
	}
	return nil
}

// UIDOf returns the uid for a system user, -1 when unknown.
func UIDOf(name string) int {
	out, err := run("id", "-u", name)
	if err != nil {
		return -1
	}
	uid, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return -1
	}
	return uid
}
