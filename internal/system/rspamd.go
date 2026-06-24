package system

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Mail anti-spam / anti-virus via rspamd (a Postfix milter) with ClamAV wired in
// through rspamd's antivirus module. Filtering is server-wide; per-domain control
// is done with rspamd's "settings" module, regenerated from panel state (the same
// regenerate-from-DB pattern as the mail maps) so a domain can opt out.

const rspamdLocalDir = "/etc/rspamd/local.d"

// HaveRspamd reports whether rspamd is installed.
func HaveRspamd() bool {
	if have("rspamd") {
		return true
	}
	_, err := os.Stat("/etc/rspamd")
	return err == nil
}

// HaveClamAV reports whether the ClamAV daemon is present.
func HaveClamAV() bool {
	if have("clamdscan") {
		return true
	}
	for _, p := range []string{"/var/run/clamav/clamd.ctl", "/run/clamav/clamd.ctl", "/etc/clamav"} {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

func rspamdKey(d string) string      { return strings.NewReplacer(".", "_", "-", "_").Replace(d) }
func rspamdDomainRe(d string) string { return strings.ReplaceAll(d, ".", `\.`) }

// RebuildSpamSettings regenerates rspamd's per-domain settings. Spam/virus
// filtering is on by default; recipients in disabledDomains are skipped
// (want_spam). Regenerated wholesale from panel state, then rspamd is reloaded.
func RebuildSpamSettings(disabledDomains []string) error {
	if !HaveRspamd() {
		return nil
	}
	if err := os.MkdirAll(rspamdLocalDir, 0o755); err != nil {
		return err
	}
	var sb strings.Builder
	sb.WriteString("# Managed by RePanel — per-domain spam/virus filtering.\nsettings {\n")
	for _, d := range disabledDomains {
		fmt.Fprintf(&sb, "  repanel_off_%s {\n    priority = high;\n    rcpt = \"/@%s$/i\";\n    want_spam = yes;\n  }\n",
			rspamdKey(d), rspamdDomainRe(d))
	}
	sb.WriteString("}\n")
	if err := os.WriteFile(filepath.Join(rspamdLocalDir, "settings.conf"), []byte(sb.String()), 0o644); err != nil {
		return err
	}
	return reloadRspamd()
}

func reloadRspamd() error {
	if !have("systemctl") {
		return nil
	}
	_, err := run("systemctl", "reload-or-restart", "rspamd")
	return err
}

// EnsureRspamdConfig wires ClamAV into rspamd's antivirus module and adds the
// rspamd milter to Postfix (alongside any existing milter, e.g. OpenDKIM).
func EnsureRspamdConfig() error {
	if err := os.MkdirAll(rspamdLocalDir, 0o755); err != nil {
		return err
	}
	av := `# Managed by RePanel — scan mail through ClamAV.
clamav {
  type = "clamav";
  servers = "/var/run/clamav/clamd.ctl";
  scan_mime_parts = true;
  symbol = "CLAM_VIRUS";
  action = "reject";
  message = "This message contains a virus and has been rejected.";
}
`
	os.WriteFile(filepath.Join(rspamdLocalDir, "antivirus.conf"), []byte(av), 0o644)

	if have("postconf") {
		// Use 127.0.0.1, not "localhost": on dual-stack hosts the latter resolves
		// to ::1 first and Postfix can fail to reach the milter ("connect to
		// Milter service ... Connection refused").
		ensurePostfixMilter("smtpd_milters", "inet:127.0.0.1:11332")
		ensurePostfixMilter("non_smtpd_milters", "inet:127.0.0.1:11332")
		run("postconf", "-e", "milter_default_action = accept")
		run("postconf", "-e", "milter_protocol = 6")
		ReloadService("postfix")
	}
	return reloadRspamd()
}

// ensurePostfixMilter appends milter to a Postfix milter list if not present.
func ensurePostfixMilter(key, milter string) {
	out, _ := run("postconf", "-h", key)
	cur := strings.TrimSpace(out)
	if strings.Contains(cur, milter) {
		return
	}
	val := milter
	if cur != "" {
		val = cur + ", " + milter
	}
	run("postconf", "-e", key+" = "+val)
}

// InstallAntiSpam installs and starts rspamd + ClamAV (+ Redis, which rspamd
// uses) and wires them into Postfix. Long-running (ClamAV's signature download is
// large); callers run it in the background.
func InstallAntiSpam() error {
	if !Linux() {
		return fmt.Errorf("anti-spam is only supported on Linux")
	}
	if !have("apt-get") {
		return fmt.Errorf("apt-get is not available on this host")
	}
	if _, err := apt("install", "-y", "-q", "rspamd", "redis-server", "clamav", "clamav-daemon", "clamav-freshclam"); err != nil {
		return err
	}
	if have("systemctl") {
		run("systemctl", "enable", "--now", "redis-server")
		run("systemctl", "enable", "--now", "clamav-freshclam")
		run("systemctl", "enable", "--now", "clamav-daemon")
		run("systemctl", "enable", "--now", "rspamd")
	}
	return EnsureRspamdConfig()
}
