package system

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Per-account SSH access: the panel flips the system user's login shell and
// manages its authorized_keys. Disabled accounts keep the locked /usr/sbin/nologin
// shell they are created with, so SSH access is strictly opt-in and admin-driven.

const loginShell = "/bin/bash"
const lockedShell = "/usr/sbin/nologin"

// validSSHKey matches an OpenSSH public key line (type, base64 blob, optional
// comment). Options prefixes are rejected to keep authorized_keys simple and safe.
var validSSHKey = regexp.MustCompile(`^(ssh-ed25519|ssh-rsa|ecdsa-sha2-nistp(256|384|521)|sk-ssh-ed25519@openssh\.com|sk-ecdsa-sha2-nistp256@openssh\.com) [A-Za-z0-9+/]+={0,3}( .*)?$`)

// ValidSSHKey reports whether line is a well-formed single SSH public key.
func ValidSSHKey(line string) bool {
	return validSSHKey.MatchString(strings.TrimSpace(line))
}

// SetSSHShell enables or disables shell login for a managed account.
func SetSSHShell(name string, enabled bool) error {
	if !Linux() {
		return nil
	}
	if !validSysName.MatchString(name) {
		return fmt.Errorf("invalid system username %q", name)
	}
	if !managedAccount(name) {
		return fmt.Errorf("refusing to modify system account %q", name)
	}
	shell := lockedShell
	if enabled {
		shell = loginShell
	}
	_, err := run("usermod", "-s", shell, name)
	return err
}

// WriteAuthorizedKeys replaces ~/.ssh/authorized_keys for a managed account with
// the given keys (each validated), owned by the account with strict permissions.
// An empty list removes the file.
func WriteAuthorizedKeys(name, home string, keys []string) error {
	if !Linux() {
		return nil
	}
	if !validSysName.MatchString(name) {
		return fmt.Errorf("invalid system username %q", name)
	}
	sshDir := filepath.Join(home, ".ssh")
	authFile := filepath.Join(sshDir, "authorized_keys")

	var valid []string
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if !ValidSSHKey(k) {
			return fmt.Errorf("invalid SSH public key: %.40s…", k)
		}
		valid = append(valid, k)
	}
	if len(valid) == 0 {
		os.Remove(authFile)
		return nil
	}
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(authFile, []byte(strings.Join(valid, "\n")+"\n"), 0o600); err != nil {
		return err
	}
	// Ownership must be the account itself or sshd refuses the keys.
	run("chown", "-R", name+":"+name, sshDir)
	return nil
}
