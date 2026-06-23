package system

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/reinis1996/repanel/internal/models"
)

// Per-site configuration views. The panel injects admin override blocks into the
// generated nginx/Apache/PHP config (see vhostData.NginxExtra etc.); these readers
// return the *rendered* on-disk files so the editor can show admins exactly what
// is currently active. Mail is global-map based (no per-domain file), so its view
// shows the domain's effective mailbox/alias entries instead.

// readFileString returns a file's contents, or "" when it does not exist.
func readFileString(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

// ReadNginxVhost returns the generated nginx server block for a domain.
func ReadNginxVhost(nginxDir, name string) string {
	return readFileString(filepath.Join(nginxConfDir(nginxDir), name+".conf"))
}

// ReadApacheVhost returns the generated Apache vhost for a domain.
func ReadApacheVhost(apacheDir, name string) string {
	return readFileString(filepath.Join(apacheConfDir(apacheDir), name+".conf"))
}

// ReadPHPPool returns the generated PHP-FPM pool file for a domain.
func ReadPHPPool(d models.Domain) string {
	return readFileString(fmt.Sprintf("/etc/php/%s/fpm/pool.d/repanel-%s.conf", d.PHPVersion, poolName(d.Name)))
}

// MailConfigForDomain returns the effective postfix mailbox/alias map entries for
// a domain (read-only) — the closest thing to a per-site mail config, since the
// underlying maps are server-wide. Passwords are never included.
func MailConfigForDomain(mailDir, domain string) string {
	suffix := "@" + domain
	var b strings.Builder
	if lines := grepSuffix(filepath.Join(mailDir, "virtual_mailboxes"), suffix); lines != "" {
		b.WriteString("# Mailboxes (address -> maildir)\n")
		b.WriteString(lines)
	}
	if lines := grepSuffix(filepath.Join(mailDir, "virtual_aliases"), suffix); lines != "" {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("# Aliases (source -> destination)\n")
		b.WriteString(lines)
	}
	return b.String()
}

// grepSuffix returns the lines of a map file whose first field ends with suffix.
func grepSuffix(path, suffix string) string {
	content := readFileString(path)
	if content == "" {
		return ""
	}
	var out strings.Builder
	for _, ln := range strings.Split(content, "\n") {
		field, _, _ := strings.Cut(strings.TrimSpace(ln), " ")
		if strings.HasSuffix(field, suffix) {
			out.WriteString(ln)
			out.WriteString("\n")
		}
	}
	return out.String()
}
