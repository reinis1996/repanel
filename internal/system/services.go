package system

import (
	"fmt"
	"strings"

	"github.com/reinis1996/repanel/internal/models"
)

// ManagedServices lists the units the panel knows how to supervise,
// mirroring the service list of Plesk / DirectAdmin.
var ManagedServices = []models.ServiceStatus{
	{Name: "nginx", DisplayName: "Web Server (nginx)", Description: "Serves customer websites"},
	{Name: "php8.3-fpm", DisplayName: "PHP-FPM 8.3", Description: "PHP application server"},
	{Name: "mariadb", DisplayName: "Database (MariaDB)", Description: "MySQL-compatible database server"},
	{Name: "postgresql", DisplayName: "Database (PostgreSQL)", Description: "PostgreSQL database server"},
	{Name: "bind9", DisplayName: "DNS Server (BIND)", Description: "Authoritative DNS"},
	{Name: "postfix", DisplayName: "Mail (Postfix)", Description: "SMTP server"},
	{Name: "dovecot", DisplayName: "Mail (Dovecot)", Description: "IMAP/POP3 server"},
	{Name: "opendkim", DisplayName: "Mail (OpenDKIM)", Description: "DKIM message signing"},
	{Name: "proftpd", DisplayName: "FTP (ProFTPD)", Description: "FTP server"},
	{Name: "ssh", DisplayName: "SSH", Description: "Secure shell access"},
	{Name: "cron", DisplayName: "Cron", Description: "Scheduled tasks"},
	{Name: "fail2ban", DisplayName: "Fail2ban", Description: "Brute-force protection"},
	{Name: "repanel", DisplayName: "RePanel", Description: "This control panel"},
}

func systemctl(args ...string) (string, error) {
	if !have("systemctl") {
		return "", fmt.Errorf("systemctl not available on this host")
	}
	return run("systemctl", args...)
}

// ServiceList returns the status of every managed unit.
func ServiceList() []models.ServiceStatus {
	out := make([]models.ServiceStatus, len(ManagedServices))
	copy(out, ManagedServices)
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
	return out
}

var allowedActions = map[string]bool{"start": true, "stop": true, "restart": true, "reload": true, "enable": true, "disable": true}

// ServiceAction runs start/stop/restart/reload/enable/disable on a managed unit.
func ServiceAction(unit, action string) error {
	if !allowedActions[action] {
		return fmt.Errorf("unsupported action %q", action)
	}
	known := false
	for _, s := range ManagedServices {
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
