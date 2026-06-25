package system

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Mail feature plumbing: switching delivery to Dovecot LMTP (so Sieve and quota
// apply), detecting/installing the pieces, the outbound smarthost relay, and
// imapsync migrations. Everything degrades gracefully when a tool is absent.

const dovecotFeatureConf = "/etc/dovecot/conf.d/99-repanel.conf"

// MailFeaturesAvailable reports whether the Sieve interpreter is installed, the
// gate for autoresponders, filters and (alongside the quota plugin) enforced
// quotas via Dovecot LMTP delivery.
func MailFeaturesAvailable() bool {
	if have("sievec") {
		return true
	}
	_, err := os.Stat("/usr/lib/dovecot/modules/lib90_sieve_plugin.so")
	return err == nil
}

// InstallMailFeatures installs the Sieve plugin and wires up LMTP delivery. Long
// running (apt); callers run it in the background.
func InstallMailFeatures(mailDir string) error {
	if !Linux() {
		return fmt.Errorf("mail features are only supported on Linux")
	}
	if !have("apt-get") {
		return fmt.Errorf("apt-get is not available on this host")
	}
	if _, err := apt("install", "-y", "-q", "dovecot-sieve", "dovecot-lmtpd"); err != nil {
		return err
	}
	return EnsureMailDelivery(mailDir)
}

// EnsureMailDelivery makes mail delivery go through Dovecot LMTP with the Sieve
// and quota plugins, so per-mailbox filters, autoresponders and quotas take
// effect. It rewrites the panel-owned Dovecot config (version-aware), validates
// it with doveconf and rolls back on error, then points Postfix's virtual
// transport at the LMTP socket. A no-op when Dovecot or the Sieve plugin is
// absent, so it is safe to call at startup.
func EnsureMailDelivery(mailDir string) error {
	if !Linux() || !have("doveconf") || !MailFeaturesAvailable() {
		return nil
	}
	// Mail is virtual-mailbox only, so drop Dovecot's stock PAM/system passdb. It
	// fails for every virtual user and pam_unix's per-failure delay (~2s) makes
	// IMAP logins — and webmail — painfully slow.
	authChanged := disableDovecotSystemAuth()
	conf := dovecotDeliveryConf(mailDir, dovecotMajor())
	prev, hadPrev := readPrevious(dovecotFeatureConf)
	if prev == conf && !authChanged {
		// Already current; still make sure Postfix points at LMTP.
		return ensurePostfixLMTP()
	}
	if err := os.WriteFile(dovecotFeatureConf, []byte(conf), 0o644); err != nil {
		return err
	}
	if _, errOut, err := runCapture(30*time.Second, "", "", "doveconf", "-n"); err != nil {
		// Bad config: restore the previous file so mail keeps working.
		if hadPrev {
			os.WriteFile(dovecotFeatureConf, []byte(prev), 0o644)
		} else {
			os.Remove(dovecotFeatureConf)
		}
		return fmt.Errorf("dovecot config rejected: %s", firstLine(strings.TrimSpace(errOut)))
	}
	if err := ReloadService("dovecot"); err != nil {
		return err
	}
	return ensurePostfixLMTP()
}

// disableDovecotSystemAuth comments out the stock "!include auth-system.conf.ext"
// line in 10-auth.conf so Dovecot authenticates only against the panel's
// passwd-file. Left enabled, the PAM/system passdb is tried first and fails for
// every virtual mailbox, and pam_unix's failure delay (~2s) is added to each
// login. Idempotent; returns true if it changed the file. Safe because the panel
// serves virtual mailboxes only — no system account needs IMAP/POP3.
func disableDovecotSystemAuth() bool {
	const path = "/etc/dovecot/conf.d/10-auth.conf"
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	lines := strings.Split(string(data), "\n")
	changed := false
	for i, ln := range lines {
		if strings.TrimSpace(ln) == "!include auth-system.conf.ext" {
			lines[i] = "#" + ln + "  # disabled by RePanel: virtual mailboxes only"
			changed = true
		}
	}
	if changed {
		os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
	}
	return changed
}

// ensurePostfixLMTP routes Postfix's virtual delivery to the Dovecot LMTP socket.
func ensurePostfixLMTP() error {
	if !have("postconf") {
		return nil
	}
	run("postconf", "-e", "virtual_transport = lmtp:unix:private/dovecot-lmtp")
	// Dovecot enforces the quota; let Postfix surface the rejection to senders.
	run("postconf", "-e", "lmtp_destination_recipient_limit = 1")
	ensureSubmissionService()
	return ReloadService("postfix")
}

// ensureSubmissionService enables Postfix's submission listener on :587 (Debian
// ships it commented out, so only :25 listens and webmail/clients can't send).
// Auth uses Dovecot SASL; permit_mynetworks lets local webmail relay without
// auth. Idempotent — postconf -M/-P just (re)assert the master.cf entry.
func ensureSubmissionService() {
	run("postconf", "-M", "submission/inet=submission inet n - y - - smtpd")
	for _, kv := range []string{
		"submission/inet/syslog_name=postfix/submission",
		"submission/inet/smtpd_tls_security_level=may",
		"submission/inet/smtpd_sasl_type=dovecot",
		"submission/inet/smtpd_sasl_path=private/auth",
		"submission/inet/smtpd_sasl_auth_enable=yes",
		"submission/inet/smtpd_recipient_restrictions=permit_mynetworks,permit_sasl_authenticated,reject_unauth_destination",
	} {
		run("postconf", "-P", kv)
	}
}

func readPrevious(path string) (string, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(b), true
}

// dovecotMajor returns 24 for Dovecot >= 2.4 (new config syntax) or 23 otherwise.
func dovecotMajor() int {
	out, err := run("dovecot", "--version")
	if err != nil {
		return 23
	}
	v := strings.Fields(out)
	if len(v) == 0 {
		return 23
	}
	switch {
	case strings.HasPrefix(v[0], "2.4"), strings.HasPrefix(v[0], "2.5"), strings.HasPrefix(v[0], "3."):
		return 24
	default:
		return 23
	}
}

// dovecotSpecialMailboxes auto-creates and subscribes the standard special-use
// folders for every mailbox, so clients (Roundcube, Thunderbird, mobile) show
// Sent/Drafts/Junk/Trash/Archive instead of only INBOX. It merges into the
// stock "inbox" namespace by name. The syntax is identical on Dovecot 2.3 and
// 2.4. (\Junk is what Roundcube/RFC 6154 treat as the spam folder.)
const dovecotSpecialMailboxes = `
namespace inbox {
  mailbox Drafts {
    special_use = \Drafts
    auto = subscribe
  }
  mailbox Sent {
    special_use = \Sent
    auto = subscribe
  }
  mailbox Junk {
    special_use = \Junk
    auto = subscribe
  }
  mailbox Trash {
    special_use = \Trash
    auto = subscribe
  }
  mailbox Archive {
    special_use = \Archive
    auto = subscribe
  }
}
`

// dovecotDeliveryConf renders the panel-owned Dovecot config for the given major
// version. It supersedes the installer's base config (same path) with LMTP, the
// Sieve plugin, and per-user quota sourced from the passwd-file userdb.
func dovecotDeliveryConf(mailDir string, major int) string {
	// Always a POSIX path: this is written into the target host's Dovecot config.
	passwd := strings.TrimRight(mailDir, "/") + "/passwd"
	if major >= 24 {
		// Note: per-user quota is intentionally not configured on Dovecot >= 2.4.
		// The 2.4 quota syntax changed substantially (the plugin {} block was
		// removed and quota roots use a new named-filter form); shipping an
		// unverified stanza here previously produced an invalid `quota_rule vsz {}`
		// section that doveconf rejected, which took down the whole mail config.
		// LMTP delivery + Sieve work without it; quota for 2.4 is a follow-up.
		return `# Managed by RePanel (Dovecot >= 2.4) — LMTP delivery + Sieve.
mail_driver = maildir
mail_path = /var/mail/vhosts/%{user | domain}/%{user | username}
# Store INBOX inside the maildir like every other folder. The stock 2.4 config
# sets mail_inbox_path = /var/mail/%{user} (the system mbox path), which vmail
# cannot write — leaving it set makes INBOX autocreation fail with EACCES.
mail_inbox_path =
mail_uid = vmail
mail_gid = vmail
first_valid_uid = 5000
last_valid_uid = 5000

passdb passwd-file {
  passwd_file_path = ` + passwd + `
  default_password_scheme = SHA512-CRYPT
}

userdb passwd-file {
  passwd_file_path = ` + passwd + `
  fields {
    uid = vmail
    gid = vmail
    home = /var/mail/vhosts/%{user | domain}/%{user | username}
  }
}

service auth {
  unix_listener /var/spool/postfix/private/auth {
    mode = 0660
    user = postfix
    group = postfix
  }
}
service lmtp {
  unix_listener /var/spool/postfix/private/dovecot-lmtp {
    mode = 0600
    user = postfix
    group = postfix
  }
}

protocol lmtp {
  # Resolve recipients by full address. The stock 2.4 protocol-lmtp default strips
  # the domain (%{user | username}), so delivery to info@example.com would look up
  # "info" and miss the passwd-file (keyed by the full address) — "User doesn't
  # exist". A global setting can't override this per-protocol one, so set it here.
  auth_username_format = %{user | lower}
  mail_plugins {
    sieve = yes
  }
}

sieve_script personal {
  driver = file
  path = ~/.dovecot.sieve
}
` + dovecotSpecialMailboxes
	}
	return `# Managed by RePanel (Dovecot 2.3) — LMTP delivery + Sieve + quota.
mail_location = maildir:/var/mail/vhosts/%d/%n
mail_uid = vmail
mail_gid = vmail
first_valid_uid = 5000
last_valid_uid = 5000

protocols = imap pop3 lmtp
mail_plugins = $mail_plugins quota

passdb {
  driver = passwd-file
  args = scheme=SHA512-CRYPT username_format=%u ` + passwd + `
}
userdb {
  driver = passwd-file
  args = username_format=%u ` + passwd + `
  default_fields = uid=vmail gid=vmail home=/var/mail/vhosts/%d/%n
}

service auth {
  unix_listener /var/spool/postfix/private/auth {
    mode = 0660
    user = postfix
    group = postfix
  }
}
service lmtp {
  unix_listener /var/spool/postfix/private/dovecot-lmtp {
    mode = 0600
    user = postfix
    group = postfix
  }
}

protocol lmtp {
  mail_plugins = $mail_plugins sieve
}
protocol imap {
  mail_plugins = $mail_plugins imap_quota
}

plugin {
  sieve = file:~/sieve;active=~/.dovecot.sieve
  quota = maildir:User quota
}
` + dovecotSpecialMailboxes
}

// ---- Outbound smarthost relay ----------------------------------------------

const saslPasswdPath = "/etc/postfix/sasl_passwd"

// SetSmarthost configures Postfix to relay all outbound mail through host:port,
// authenticating with user/pass. The credentials are written to the SASL password
// map (root-only) and never exposed afterwards.
func SetSmarthost(host string, port int, user, pass string) error {
	if !have("postconf") {
		return fmt.Errorf("postfix is not installed on this host")
	}
	relay := fmt.Sprintf("[%s]:%d", host, port)
	line := fmt.Sprintf("%s %s:%s\n", relay, user, pass)
	if err := os.WriteFile(saslPasswdPath, []byte(line), 0o600); err != nil {
		return err
	}
	if have("postmap") {
		if _, err := run("postmap", saslPasswdPath); err != nil {
			return err
		}
	}
	run("postconf", "-e", "relayhost = "+relay)
	run("postconf", "-e", "smtp_sasl_auth_enable = yes")
	run("postconf", "-e", "smtp_sasl_password_maps = hash:"+saslPasswdPath)
	run("postconf", "-e", "smtp_sasl_security_options = noanonymous")
	run("postconf", "-e", "smtp_tls_security_level = may")
	return ReloadService("postfix")
}

// ClearSmarthost reverts to direct delivery and removes the stored credentials.
func ClearSmarthost() error {
	if !have("postconf") {
		return nil
	}
	run("postconf", "-e", "relayhost =")
	run("postconf", "-e", "smtp_sasl_auth_enable = no")
	run("postconf", "-X", "smtp_sasl_password_maps")
	os.Remove(saslPasswdPath)
	os.Remove(saslPasswdPath + ".db")
	return ReloadService("postfix")
}

// ---- IMAP migration (imapsync) ---------------------------------------------

// HaveIMAPSync reports whether imapsync is installed.
func HaveIMAPSync() bool { return have("imapsync") }

// imapsyncURL is the project's single-file script. imapsync was removed from
// Debian's repositories (and is only in Ubuntu's universe), so `apt install
// imapsync` fails on a standard host. Instead we install its Perl module
// dependencies from apt and fetch the script directly — the method the imapsync
// project documents for Debian/Ubuntu.
const imapsyncURL = "https://raw.githubusercontent.com/imapsync/imapsync/master/imapsync"

// imapsyncPerlDeps are imapsync's Perl modules as Debian/Ubuntu packages (from
// the project's INSTALL.Debian).
var imapsyncPerlDeps = []string{
	"libauthen-ntlm-perl", "libcgi-pm-perl", "libcrypt-openssl-rsa-perl",
	"libdata-uniqid-perl", "libencode-imaputf7-perl", "libfile-copy-recursive-perl",
	"libfile-tail-perl", "libio-socket-inet6-perl", "libio-socket-ssl-perl",
	"libio-tee-perl", "libhtml-parser-perl", "libjson-webtoken-perl",
	"libmail-imapclient-perl", "libparse-recdescent-perl", "libpar-packer-perl",
	"libreadonly-perl", "libregexp-common-perl", "libsys-meminfo-perl",
	"libterm-readkey-perl", "libtest-mockobject-perl", "libunicode-string-perl",
	"liburi-perl", "libwww-perl", "libdigest-hmac-perl",
}

// InstallIMAPSync installs imapsync's Perl dependencies and the official script.
func InstallIMAPSync() error {
	if !Linux() || !have("apt-get") {
		return fmt.Errorf("imapsync can only be installed on a Debian/Ubuntu host")
	}
	// Install the Perl dependencies. If the batch fails (a release may be missing
	// one package, which makes apt reject the whole list), fall back to installing
	// them one at a time so the rest still go in.
	if _, err := apt(append([]string{"install", "-y", "-q"}, imapsyncPerlDeps...)...); err != nil {
		for _, p := range imapsyncPerlDeps {
			apt("install", "-y", "-q", p) // best effort; missing optionals are tolerated
		}
	}
	// Fetch the single-file script.
	resp, err := (&http.Client{Timeout: 2 * time.Minute}).Get(imapsyncURL)
	if err != nil {
		return fmt.Errorf("download imapsync: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download imapsync: unexpected status %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("download imapsync: %w", err)
	}
	// Sanity-check it's the Perl script before installing it executable.
	if len(data) < 1000 || !bytes.Contains(data[:200], []byte("perl")) {
		return fmt.Errorf("downloaded imapsync does not look like the expected script")
	}
	if err := os.WriteFile("/usr/local/bin/imapsync", data, 0o755); err != nil {
		return fmt.Errorf("install imapsync: %w", err)
	}
	return nil
}

// RunIMAPSync copies a remote IMAP mailbox into a local one with imapsync,
// returning the tail of its output. Passwords are passed via root-only temp files
// so they never appear in the process list.
func RunIMAPSync(remoteHost string, remotePort int, remoteUser, remotePass, localAddr, localPass string) (string, error) {
	if !have("imapsync") {
		return "", fmt.Errorf("imapsync is not installed on this server")
	}
	f1, err := writeSecret(remotePass)
	if err != nil {
		return "", err
	}
	defer os.Remove(f1)
	f2, err := writeSecret(localPass)
	if err != nil {
		return "", err
	}
	defer os.Remove(f2)

	args := []string{
		"--host1", remoteHost, "--port1", fmt.Sprint(remotePort), "--user1", remoteUser, "--passfile1", f1,
		"--host2", "127.0.0.1", "--user2", localAddr, "--passfile2", f2,
		"--automap", "--no-modulesversion", "--nofoldersizes",
		"--ssl2", "--sslargs2", "SSL_verify_mode=0", // local self-signed cert
	}
	if remotePort == 993 {
		args = append(args, "--ssl1")
	}
	// imapsync can run long; allow up to 30 minutes for a mailbox.
	out, errOut, err := runCapture(30*time.Minute, "", "", "imapsync", args...)
	log := strings.TrimSpace(out)
	if err != nil {
		detail := strings.TrimSpace(errOut)
		if detail == "" {
			detail = err.Error()
		}
		return lastLines(log, 40), fmt.Errorf("%s", firstLine(detail))
	}
	return lastLines(log, 40), nil
}

// writeSecret writes a secret to a root-only temp file for passing to a tool.
func writeSecret(s string) (string, error) {
	f, err := os.CreateTemp("", "repanel-secret-*")
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(s); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	f.Close()
	os.Chmod(f.Name(), 0o600)
	return f.Name(), nil
}

// lastLines returns the last n lines of s.
func lastLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}
