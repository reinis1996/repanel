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

// RebuildMailMaps regenerates every postfix/dovecot map from panel state:
// virtual domains, the mailbox map, the dovecot passwd-file (with per-user quota),
// and the alias map (which also carries forwarders, catch-alls and distribution
// lists).
func RebuildMailMaps(mailDir string, domains []string, boxes []models.Mailbox, aliases []models.MailAlias, lists []models.MailList) error {
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
	// dovecot: passwd-file (user:{SHA512-CRYPT}hash:::::: extra). The per-user
	// quota is published as a userdb_quota_rule extra field; it is honoured once
	// delivery goes through Dovecot LMTP with the quota plugin (EnsureMailDelivery)
	// and ignored otherwise, so writing it is always safe.
	var pw strings.Builder
	for _, m := range boxes {
		user, domain, ok := strings.Cut(m.Address, "@")
		if !ok {
			continue
		}
		fmt.Fprintf(&vm, "%s %s/%s/\n", m.Address, domain, user)
		if m.QuotaMB > 0 {
			fmt.Fprintf(&pw, "%s:{SHA512-CRYPT}%s::::::userdb_quota_rule=*:storage=%dM\n", m.Address, m.PasswordHash, m.QuotaMB)
		} else {
			fmt.Fprintf(&pw, "%s:{SHA512-CRYPT}%s\n", m.Address, m.PasswordHash)
		}
	}
	if err := writeAndPostmap(filepath.Join(mailDir, "virtual_mailboxes"), vm.String()); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(mailDir, "passwd"), []byte(pw.String()), 0o640); err != nil {
		return err
	}

	// postfix: alias map — aliases/forwarders/catch-alls and distribution lists.
	var va strings.Builder
	for _, a := range aliases {
		dest := a.Destination
		// Keep-a-copy: also deliver to the local mailbox named by the source.
		// Postfix resolves the self-reference to the mailbox (no loop). It only
		// makes sense for a concrete address, not a catch-all "@domain".
		if a.KeepCopy && !strings.HasPrefix(a.Source, "@") && !aliasHasTarget(dest, a.Source) {
			dest = dest + ", " + a.Source
		}
		fmt.Fprintf(&va, "%s %s\n", a.Source, dest)
	}
	for _, l := range lists {
		members := strings.Join(l.Members, ", ")
		if strings.TrimSpace(members) == "" {
			continue
		}
		fmt.Fprintf(&va, "%s %s\n", l.Address, members)
	}
	if err := writeAndPostmap(filepath.Join(mailDir, "virtual_aliases"), va.String()); err != nil {
		return err
	}

	ReloadService("postfix")
	ReloadService("dovecot")
	return nil
}

// aliasHasTarget reports whether dest already lists addr (so keep-copy doesn't
// duplicate it).
func aliasHasTarget(dest, addr string) bool {
	for _, t := range strings.Split(dest, ",") {
		if strings.EqualFold(strings.TrimSpace(t), addr) {
			return true
		}
	}
	return false
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
// available (target host), falling back to `openssl passwd -6`. The password is
// supplied on STDIN rather than as a command-line argument so it never appears
// in the process list, where other local users — including tenants' PHP-FPM
// workers reading /proc — could otherwise capture it (see SECURITY_AUDIT F-14).
func HashMailPassword(password string) (string, error) {
	if strings.ContainsAny(password, "\n\r") {
		return "", fmt.Errorf("password contains invalid characters")
	}
	if have("doveadm") {
		// doveadm pw reads the password from stdin when stdin is not a TTY (the
		// panel runs without a controlling terminal). We feed it twice so it
		// works whether or not the build asks for a confirmation line; a single
		// read simply ignores the extra line.
		out, err := runStdin(password+"\n"+password+"\n", "doveadm", "pw", "-s", "SHA512-CRYPT")
		if err != nil {
			return "", err
		}
		return strings.TrimPrefix(out, "{SHA512-CRYPT}"), nil
	}
	if have("openssl") {
		// -stdin reads the password from standard input instead of argv.
		out, err := runStdin(password+"\n", "openssl", "passwd", "-6", "-stdin")
		if err != nil {
			return "", err
		}
		return out, nil
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
