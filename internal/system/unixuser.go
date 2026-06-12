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

// SysUserName maps a panel username to the unix account that owns its files.
func SysUserName(panelUser string) string {
	name := strings.ToLower(panelUser)
	name = regexp.MustCompile(`[^a-z0-9_-]`).ReplaceAllString(name, "")
	if name == "" || !validSysName.MatchString(name) {
		name = "rp-user"
	}
	return name
}

// EnsureUnixUser creates a locked system account with the given home if it
// does not exist yet. Customer sites and FTP logins run under this account.
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

// SetUnixPassword sets the password (used for FTP/SFTP logins).
func SetUnixPassword(name, password string) error {
	if !Linux() {
		return nil
	}
	if !validSysName.MatchString(name) {
		return fmt.Errorf("invalid system username %q", name)
	}
	cmd := exec.Command("chpasswd")
	cmd.Stdin = strings.NewReader(name + ":" + password + "\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("chpasswd: %w: %s", err, out)
	}
	return nil
}

// DeleteUnixUser removes the account, keeping home dirs for safety.
func DeleteUnixUser(name string) error {
	if !Linux() || !validSysName.MatchString(name) {
		return nil
	}
	if _, err := run("id", "-u", name); err != nil {
		return nil // already gone
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
