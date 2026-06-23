package system

import (
	"fmt"
	"strings"
	"time"

	"github.com/reinis1996/repanel/internal/models"
)

// ManagedServices lists the units the panel knows how to supervise,
// mirroring the service list of Plesk / DirectAdmin. A php<ver>-fpm entry is
// added for each installed PHP version by managedServices().
var ManagedServices = []models.ServiceStatus{
	{Name: "nginx", DisplayName: "Web Server (nginx)", Description: "Serves customer websites"},
	{Name: "apache2", DisplayName: "Web Server (Apache)", Description: "Alternative/backend web server"},
	{Name: "mariadb", DisplayName: "Database (MariaDB)", Description: "MySQL-compatible database server"},
	{Name: "postgresql", DisplayName: "Database (PostgreSQL)", Description: "PostgreSQL database server"},
	{Name: "bind9", DisplayName: "DNS Server (BIND)", Description: "Authoritative DNS"},
	{Name: "postfix", DisplayName: "Mail (Postfix)", Description: "SMTP server"},
	{Name: "dovecot", DisplayName: "Mail (Dovecot)", Description: "IMAP/POP3 server"},
	{Name: "opendkim", DisplayName: "Mail (OpenDKIM)", Description: "DKIM message signing"},
	{Name: "rspamd", DisplayName: "Mail (Rspamd)", Description: "Spam filtering"},
	{Name: "clamav-daemon", DisplayName: "Mail (ClamAV)", Description: "Antivirus scanning"},
	{Name: "clamav-freshclam", DisplayName: "Mail (ClamAV Freshclam)", Description: "Antivirus signature updates"},
	{Name: "proftpd", DisplayName: "FTP (ProFTPD)", Description: "FTP server"},
	{Name: "ssh", DisplayName: "SSH", Description: "Secure shell access"},
	{Name: "cron", DisplayName: "Cron", Description: "Scheduled tasks"},
	{Name: "fail2ban", DisplayName: "Fail2ban", Description: "Brute-force protection"},
	{Name: "repanel", DisplayName: "RePanel", Description: "This control panel"},
}

// managedServices returns the supervised units with a php<ver>-fpm entry for
// each installed PHP version inserted right after the web servers, so the
// Services view reflects whatever PHP versions are actually present.
func managedServices() []models.ServiceStatus {
	out := make([]models.ServiceStatus, 0, len(ManagedServices)+2)
	for _, s := range ManagedServices {
		out = append(out, s)
		if s.Name == "apache2" {
			for _, v := range PHPVersions() {
				out = append(out, models.ServiceStatus{
					Name:        "php" + v + "-fpm",
					DisplayName: "PHP-FPM " + v,
					Description: "PHP application server",
				})
			}
		}
	}
	return out
}

func systemctl(args ...string) (string, error) {
	if !have("systemctl") {
		return "", fmt.Errorf("systemctl not available on this host")
	}
	return run("systemctl", args...)
}

// ServiceList returns the status of every managed unit.
func ServiceList() []models.ServiceStatus {
	out := managedServices()
	if !have("systemctl") {
		return out
	}
	for i := range out {
		unit := out[i].Name
		if state, err := run("systemctl", "show", unit, "--property=LoadState,ActiveState,UnitFileState", "--no-pager"); err == nil {
			props := map[string]string{}
			for _, line := range strings.Split(state, "\n") {
				if k, v, ok := strings.Cut(line, "="); ok {
					props[k] = v
				}
			}
			out[i].Installed = props["LoadState"] == "loaded"
			out[i].Active = props["ActiveState"] == "active"
			out[i].Enabled = props["UnitFileState"] == "enabled" || props["UnitFileState"] == "static"
		}
	}
	fillServiceVersions(out)
	return out
}

// servicePackages maps a managed systemd unit to the Debian/Ubuntu package whose
// version represents the service. php<ver>-fpm units map to their own name and
// are handled in servicePackage. repanel is not a package (its version is filled
// in by the API layer from the running build).
var servicePackages = map[string]string{
	"nginx":            "nginx",
	"apache2":          "apache2",
	"mariadb":          "mariadb-server",
	"postgresql":       "postgresql",
	"bind9":            "bind9",
	"postfix":          "postfix",
	"dovecot":          "dovecot-core",
	"opendkim":         "opendkim",
	"rspamd":           "rspamd",
	"clamav-daemon":    "clamav-daemon",
	"clamav-freshclam": "clamav-freshclam",
	"proftpd":          "proftpd-core",
	"ssh":              "openssh-server",
	"cron":             "cron",
	"fail2ban":         "fail2ban",
}

// servicePackage returns the package name to query for a unit's version, or ""
// if the panel doesn't track one (e.g. repanel itself).
func servicePackage(unit string) string {
	if p, ok := servicePackages[unit]; ok {
		return p
	}
	if strings.HasPrefix(unit, "php") && strings.HasSuffix(unit, "-fpm") {
		return unit
	}
	return ""
}

// fillServiceVersions sets Version on every installed unit from a single
// dpkg-query lookup. It is a no-op without dpkg-query (e.g. non-Debian hosts).
func fillServiceVersions(svcs []models.ServiceStatus) {
	if !have("dpkg-query") {
		return
	}
	pkgs := []string{}
	seen := map[string]bool{}
	for _, s := range svcs {
		if !s.Installed {
			continue
		}
		if p := servicePackage(s.Name); p != "" && !seen[p] {
			seen[p] = true
			pkgs = append(pkgs, p)
		}
	}
	if len(pkgs) == 0 {
		return
	}
	versions := dpkgVersions(pkgs)
	for i := range svcs {
		if v := versions[servicePackage(svcs[i].Name)]; v != "" {
			svcs[i].Version = v
		}
	}
}

// dpkgVersions returns package→clean-version for the installed members of pkgs
// in one query. Missing packages are simply absent from the result.
func dpkgVersions(pkgs []string) map[string]string {
	out := map[string]string{}
	args := append([]string{"-W", "-f=${Package} ${Version}\n"}, pkgs...)
	stdout, _, _ := runCapture(15*time.Second, "", "", "dpkg-query", args...)
	for _, line := range strings.Split(stdout, "\n") {
		if f := strings.Fields(line); len(f) == 2 {
			out[f[0]] = cleanPkgVersion(f[1])
		}
	}
	return out
}

// cleanPkgVersion trims a Debian version (epoch + revision + build metadata) down
// to the upstream version for display: "1:2.4.62-1~deb12u1" -> "2.4.62".
func cleanPkgVersion(v string) string {
	if i := strings.IndexByte(v, ':'); i >= 0 {
		v = v[i+1:]
	}
	if i := strings.IndexFunc(v, func(r rune) bool { return r == '-' || r == '+' || r == '~' || r == ' ' }); i >= 0 {
		v = v[:i]
	}
	return v
}

var allowedActions = map[string]bool{"start": true, "stop": true, "restart": true, "reload": true, "enable": true, "disable": true}

// ServiceAction runs start/stop/restart/reload/enable/disable on a managed unit.
func ServiceAction(unit, action string) error {
	if !allowedActions[action] {
		return fmt.Errorf("unsupported action %q", action)
	}
	known := false
	for _, s := range managedServices() {
		if s.Name == unit {
			known = true
			break
		}
	}
	if !known {
		return fmt.Errorf("unknown service %q", unit)
	}
	_, err := systemctl(action, unit)
	return err
}

// ReloadService reloads a unit if present, ignoring missing units so config
// writers can call it unconditionally.
func ReloadService(unit string) error {
	if !have("systemctl") {
		return nil
	}
	if _, err := run("systemctl", "is-active", "--quiet", unit); err != nil {
		return nil // not running; nothing to reload
	}
	_, err := systemctl("reload-or-restart", unit)
	return err
}
