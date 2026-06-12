package system

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/reinis1996/repanel/internal/models"
)

// Mail integration follows the classic virtual-mailbox layout:
//   - postfix virtual_mailbox_domains / virtual_mailbox_maps / virtual_alias_maps
//     read hash maps generated below
//   - dovecot authenticates against a passwd-file with SHA512-CRYPT hashes
//   - mail is stored under /var/mail/vhosts/<domain>/<user>/ owned by vmail
//
// The installer wires postfix/dovecot to these files; the panel only needs to
// regenerate them and run postmap.

// RebuildMailMaps regenerates every postfix/dovecot map from panel state.
func RebuildMailMaps(mailDir string, domains []string, boxes []models.Mailbox, aliases []models.MailAlias) error {
	if err := os.MkdirAll(mailDir, 0o750); err != nil {
		return err
	}

	// postfix: virtual domains
	var vd strings.Builder
	for _, d := range domains {
		fmt.Fprintf(&vd, "%s OK\n", d)
	}
	if err := writeAndPostmap(filepath.Join(mailDir, "virtual_domains"), vd.String()); err != nil {
		return err
	}

	// postfix: mailbox map (address -> maildir path relative to virtual_mailbox_base)
	var vm strings.Builder
	// dovecot: passwd-file (user:{SHA512-CRYPT}hash). Extra userdb fields are
	// intentionally omitted: the 2.3 syntax for them breaks Dovecot 2.4+, and
	// quota is not enforced until the quota plugin is wired up.
	var pw strings.Builder
	for _, m := range boxes {
		user, domain, ok := strings.Cut(m.Address, "@")
		if !ok {
			continue
		}
		fmt.Fprintf(&vm, "%s %s/%s/\n", m.Address, domain, user)
		fmt.Fprintf(&pw, "%s:{SHA512-CRYPT}%s\n", m.Address, m.PasswordHash)
	}
	if err := writeAndPostmap(filepath.Join(mailDir, "virtual_mailboxes"), vm.String()); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(mailDir, "passwd"), []byte(pw.String()), 0o640); err != nil {
		return err
	}

	// postfix: alias map
	var va strings.Builder
	for _, a := range aliases {
		fmt.Fprintf(&va, "%s %s\n", a.Source, a.Destination)
	}
	if err := writeAndPostmap(filepath.Join(mailDir, "virtual_aliases"), va.String()); err != nil {
		return err
	}

	ReloadService("postfix")
	ReloadService("dovecot")
	return nil
}

func writeAndPostmap(path, content string) error {
	if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
		return err
	}
	if have("postmap") {
		if _, err := run("postmap", path); err != nil {
			return err
		}
	}
	return nil
}

// HashMailPassword produces a SHA512-CRYPT hash via `doveadm pw` when
// available (target host), falling back to `openssl passwd -6`.
func HashMailPassword(password string) (string, error) {
	if have("doveadm") {
		out, err := run("doveadm", "pw", "-s", "SHA512-CRYPT", "-p", password)
		if err != nil {
			return "", err
		}
		return strings.TrimPrefix(strings.TrimSpace(out), "{SHA512-CRYPT}"), nil
	}
	if have("openssl") {
		out, err := run("openssl", "passwd", "-6", password)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(out), nil
	}
	return "", fmt.Errorf("no password hashing tool available (need doveadm or openssl)")
}

// EnsureMaildir pre-creates the maildir owned by the vmail user.
func EnsureMaildir(address string) error {
	if !Linux() {
		return nil
	}
	user, domain, ok := strings.Cut(address, "@")
	if !ok {
		return fmt.Errorf("invalid address %q", address)
	}
	dir := filepath.Join("/var/mail/vhosts", domain, user)
	if err := os.MkdirAll(dir, 0o770); err != nil {
		return err
	}
	if _, err := run("id", "-u", "vmail"); err == nil {
		run("chown", "-R", "vmail:vmail", filepath.Join("/var/mail/vhosts", domain))
	}
	return nil
}
