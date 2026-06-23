package system

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/reinis1996/repanel/internal/models"
)

// ModSecurity web application firewall (WAF) integration. The engine and the
// OWASP Core Rule Set are installed on demand; per-domain it is enabled/disabled
// and run in blocking or detection-only mode, regenerated from panel state like
// every other config the panel owns.
//
// On the nginx (or nginx→Apache) stack the WAF runs in the nginx connector at the
// front; on the Apache-only stack it runs in mod_security2. The per-domain rules
// file is identical for both — only the directive that loads it differs (nginx:
// `modsecurity_rules_file`; Apache: `Include` inside `<IfModule security2_module>`).

const (
	wafDir      = "/etc/repanel/modsec"
	wafMainConf = wafDir + "/main.conf"
)

// validWAFMode reports whether m is a supported engine mode.
func validWAFMode(m string) bool { return m == "on" || m == "detection" }

// wafEngineMode maps a stored mode to the ModSecurity SecRuleEngine value.
func wafEngineMode(mode string) string {
	if mode == "detection" {
		return "DetectionOnly"
	}
	return "On"
}

// WAFModuleAvailable reports whether the ModSecurity connector for the active
// front server is installed and loadable.
func WAFModuleAvailable(frontApache bool) bool {
	if frontApache {
		// a2enmod security2 links the module into mods-enabled.
		for _, p := range []string{"/etc/apache2/mods-enabled/security2.load", "/etc/apache2/mods-enabled/security2.conf"} {
			if _, err := os.Stat(p); err == nil {
				return true
			}
		}
		return false
	}
	// The Debian/Ubuntu nginx connector package drops both the module .so and a
	// modules-enabled snippet that load_modules it.
	for _, p := range []string{
		"/usr/lib/nginx/modules/ngx_http_modsecurity_module.so",
		"/usr/share/nginx/modules-available/mod-http-modsecurity.conf",
	} {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	if entries, err := os.ReadDir("/etc/nginx/modules-enabled"); err == nil {
		for _, e := range entries {
			if strings.Contains(e.Name(), "modsecurity") {
				return true
			}
		}
	}
	return false
}

// wafCRSInclude returns the ModSecurity Include directive(s) that load the OWASP
// Core Rule Set, or "" when no CRS is installed. The bundled `owasp-crs.load`
// pulls in crs-setup.conf and every rule; failing that we glob the rules dir.
func wafCRSInclude() string {
	if _, err := os.Stat("/usr/share/modsecurity-crs/owasp-crs.load"); err == nil {
		return "Include /usr/share/modsecurity-crs/owasp-crs.load"
	}
	for _, base := range []string{"/usr/share/modsecurity-crs", "/etc/modsecurity/crs", "/etc/nginx/modsecurity-crs"} {
		if _, err := os.Stat(filepath.Join(base, "crs-setup.conf")); err == nil {
			return fmt.Sprintf("Include %s/crs-setup.conf\nInclude %s/rules/*.conf", base, base)
		}
	}
	return ""
}

// WAFCRSInstalled reports whether an OWASP CRS is present.
func WAFCRSInstalled() bool { return wafCRSInclude() != "" }

// EnsureWAFBaseConfig writes the panel-managed base ModSecurity config that every
// per-domain rules file includes. It reuses the distribution's recommended config
// when present, otherwise writes a sane built-in baseline.
func EnsureWAFBaseConfig() error {
	if err := os.MkdirAll(wafDir, 0o755); err != nil {
		return err
	}
	var body string
	if _, err := os.Stat("/etc/modsecurity/modsecurity.conf"); err == nil {
		body = "# Managed by RePanel — base WAF config (distribution recommended).\n" +
			"Include /etc/modsecurity/modsecurity.conf\n"
	} else {
		body = wafBaselineConf
	}
	return os.WriteFile(wafMainConf, []byte(body), 0o644)
}

// wafBaselineConf is a self-contained recommended baseline used when the distro
// ships no modsecurity.conf. The per-domain file sets SecRuleEngine after this,
// so the DetectionOnly here is only a fallback.
const wafBaselineConf = `# Managed by RePanel — base WAF config (built-in baseline).
SecRuleEngine DetectionOnly
SecRequestBodyAccess On
SecRequestBodyLimit 13107200
SecRequestBodyNoFilesLimit 131072
SecRequestBodyLimitAction Reject
SecResponseBodyAccess Off
SecAuditEngine RelevantOnly
SecAuditLogRelevantStatus "^(?:5|4(?!04))"
SecAuditLogParts ABIJDEFHZ
SecAuditLogType Serial
SecAuditLog /var/log/modsec_audit.log
SecTmpDir /tmp
SecDataDir /tmp
SecPcreMatchLimit 100000
SecPcreMatchLimitRecursion 100000
`

// wafDomainPath returns the per-domain rules file path.
func wafDomainPath(domain string) string {
	return filepath.Join(wafDir, domain+".conf")
}

// WriteDomainWAF writes a domain's ModSecurity rules file from panel state and
// returns its path. It includes the base config, the OWASP CRS (when installed),
// the per-domain engine mode, and any admin custom rules.
func WriteDomainWAF(d models.Domain) (string, error) {
	if err := EnsureWAFBaseConfig(); err != nil {
		return "", err
	}
	mode := wafEngineMode(d.WAFMode)
	var b strings.Builder
	fmt.Fprintf(&b, "# Managed by RePanel — WAF rules for %s. Regenerated from panel state.\n", d.Name)
	fmt.Fprintf(&b, "Include %s\n", wafMainConf)
	if crs := wafCRSInclude(); crs != "" {
		b.WriteString(crs + "\n")
	}
	// Engine mode override wins over whatever the base/CRS configs set.
	fmt.Fprintf(&b, "SecRuleEngine %s\n", mode)
	if rules := strings.TrimSpace(d.WAFRules); rules != "" {
		b.WriteString("\n# --- Custom rules (RePanel) ---\n")
		b.WriteString(rules)
		b.WriteString("\n")
	}
	path := wafDomainPath(d.Name)
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// RemoveDomainWAF deletes a domain's rules file (no-op when absent).
func RemoveDomainWAF(domain string) { os.Remove(wafDomainPath(domain)) }

// InstallWAF installs ModSecurity and the OWASP CRS for the active front server
// and writes the base config. Long-running (apt); callers run it in the
// background.
func InstallWAF(frontApache bool) error {
	if !Linux() {
		return fmt.Errorf("the WAF is only supported on Linux")
	}
	if !have("apt-get") {
		return fmt.Errorf("apt-get is not available on this host")
	}
	if frontApache {
		if _, err := apt("install", "-y", "-q", "libapache2-mod-security2", "modsecurity-crs"); err != nil {
			return err
		}
		if have("a2enmod") {
			run("a2enmod", "security2")
		}
		if err := EnsureWAFBaseConfig(); err != nil {
			return err
		}
		return reloadApache()
	}
	if _, err := apt("install", "-y", "-q", "libnginx-mod-http-modsecurity", "modsecurity-crs"); err != nil {
		return err
	}
	if err := EnsureWAFBaseConfig(); err != nil {
		return err
	}
	return reloadNginx()
}
