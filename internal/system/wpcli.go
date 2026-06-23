package system

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/reinis1996/repanel/internal/models"
)

// WordPress Workbench: manage core, plugins, themes and settings of a
// panel-installed WordPress site through WP-CLI. Every command runs AS the site's
// owning system user (sudo -n -u <user> -- wp ...), never root — WP-CLI also
// refuses to run as root without --allow-root, which we deliberately never pass.

const wpTimeout = 3 * time.Minute

// validWPSlug constrains plugin/theme slugs so a crafted value can't be turned
// into a WP-CLI flag or path. WordPress.org slugs are lowercase alphanumerics
// with hyphens.
var validWPSlug = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)

var (
	wpPluginActions = map[string]bool{"activate": true, "deactivate": true, "delete": true, "update": true}
	wpThemeActions  = map[string]bool{"activate": true, "delete": true, "update": true}
)

// HaveWPCLI reports whether the WP-CLI binary is installed.
func HaveWPCLI() bool { return have("wp") }

// wpCmd builds the argv for running WP-CLI against the site at docroot as its
// owning system user. WP-CLI refuses to run as root without --allow-root, which
// we never pass; instead we drop to the unprivileged site user via sudo.
func wpCmd(sysUser, docroot string, args ...string) (name string, cargs []string) {
	full := append([]string{"--path=" + docroot, "--no-color"}, args...)
	if Linux() && validSysName.MatchString(sysUser) && have("sudo") {
		return "sudo", append([]string{"-n", "-u", sysUser, "--", "wp"}, full...)
	}
	return "wp", full
}

// wpRun executes a WP-CLI command for the site at docroot as its system user and
// returns trimmed stdout. On failure it returns the first line of WP-CLI's error.
func wpRun(sysUser, docroot string, args ...string) (string, error) {
	if !have("wp") {
		return "", fmt.Errorf("WP-CLI (wp) is not installed on this server")
	}
	name, cargs := wpCmd(sysUser, docroot, args...)
	out, errOut, err := runCapture(wpTimeout, docroot, "", name, cargs...)
	if err != nil {
		detail := strings.TrimSpace(errOut)
		if detail == "" {
			detail = strings.TrimSpace(out)
		}
		if detail == "" {
			detail = err.Error()
		}
		return "", fmt.Errorf("%s", firstLine(detail))
	}
	return strings.TrimSpace(out), nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

// WPGetInfo returns the core/overview state of a site. siteURL is supplied by the
// caller (the domain's URL) to avoid an extra WP-CLI round-trip.
func WPGetInfo(sysUser, docroot, siteURL string) (models.WPInfo, error) {
	info := models.WPInfo{URL: siteURL}
	v, err := wpRun(sysUser, docroot, "core", "version")
	if err != nil {
		return info, err
	}
	info.Version = v
	if out, err := wpRun(sysUser, docroot, "core", "check-update", "--format=json"); err == nil {
		var ups []map[string]any
		if json.Unmarshal([]byte(out), &ups) == nil && len(ups) > 0 {
			if s, ok := ups[0]["version"].(string); ok {
				info.UpdateVersion = s
			}
		}
	}
	info.Title, _ = wpRun(sysUser, docroot, "option", "get", "blogname")
	info.Tagline, _ = wpRun(sysUser, docroot, "option", "get", "blogdescription")
	pub, _ := wpRun(sysUser, docroot, "option", "get", "blog_public")
	info.SearchVisible = strings.TrimSpace(pub) == "1"
	info.PHPVersion, _ = wpRun(sysUser, docroot, "eval", "echo PHP_VERSION;")
	info.PluginUpdates = wpCount(sysUser, docroot, "plugin")
	info.ThemeUpdates = wpCount(sysUser, docroot, "theme")
	if st, err := wpRun(sysUser, docroot, "maintenance-mode", "status"); err == nil {
		info.MaintenanceMode = strings.Contains(strings.ToLower(st), "active")
	}
	if ms, err := wpRun(sysUser, docroot, "config", "get", "MULTISITE", "--type=constant"); err == nil {
		info.Multisite = strings.TrimSpace(ms) == "1"
	}
	return info, nil
}

// wpCount returns how many plugins/themes have an update available, or 0 when the
// query fails (e.g. no network for the update check).
func wpCount(sysUser, docroot, kind string) int {
	out, err := wpRun(sysUser, docroot, kind, "list", "--update=available", "--field=name", "--format=count")
	if err != nil {
		return 0
	}
	n := 0
	fmt.Sscanf(strings.TrimSpace(out), "%d", &n)
	return n
}

// WPPlugins lists installed plugins.
func WPPlugins(sysUser, docroot string) ([]models.WPPlugin, error) {
	out, err := wpRun(sysUser, docroot, "plugin", "list", "--format=json", "--fields=name,title,status,version,update,update_version,auto_update")
	if err != nil {
		return nil, err
	}
	var raw []map[string]string
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, fmt.Errorf("could not read plugin list")
	}
	plugins := make([]models.WPPlugin, 0, len(raw))
	for _, p := range raw {
		plugins = append(plugins, models.WPPlugin{
			Name: p["name"], Title: p["title"], Status: p["status"], Version: p["version"],
			Update: p["update"] == "available", UpdateVersion: p["update_version"],
			AutoUpdate: p["auto_update"] == "on",
		})
	}
	return plugins, nil
}

// WPThemes lists installed themes.
func WPThemes(sysUser, docroot string) ([]models.WPTheme, error) {
	out, err := wpRun(sysUser, docroot, "theme", "list", "--format=json", "--fields=name,title,status,version,update,update_version,auto_update")
	if err != nil {
		return nil, err
	}
	var raw []map[string]string
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, fmt.Errorf("could not read theme list")
	}
	themes := make([]models.WPTheme, 0, len(raw))
	for _, t := range raw {
		themes = append(themes, models.WPTheme{
			Name: t["name"], Title: t["title"], Status: t["status"], Version: t["version"],
			Update: t["update"] == "available", UpdateVersion: t["update_version"],
			AutoUpdate: t["auto_update"] == "on",
		})
	}
	return themes, nil
}

// WPCoreUpdate updates WordPress core and then the database schema.
func WPCoreUpdate(sysUser, docroot string) error {
	if _, err := wpRun(sysUser, docroot, "core", "update"); err != nil {
		return err
	}
	_, err := wpRun(sysUser, docroot, "core", "update-db")
	return err
}

// WPUpdateSettings sets the site title, tagline and search-engine visibility.
func WPUpdateSettings(sysUser, docroot, title, tagline string, searchVisible bool) error {
	if strings.ContainsAny(title+tagline, "\n\r") {
		return fmt.Errorf("title and tagline must be single lines")
	}
	if _, err := wpRun(sysUser, docroot, "option", "update", "blogname", title); err != nil {
		return err
	}
	if _, err := wpRun(sysUser, docroot, "option", "update", "blogdescription", tagline); err != nil {
		return err
	}
	pub := "0"
	if searchVisible {
		pub = "1"
	}
	_, err := wpRun(sysUser, docroot, "option", "update", "blog_public", pub)
	return err
}

// WPPluginInstall installs (optionally activating) a plugin from the WP.org
// directory.
func WPPluginInstall(sysUser, docroot, slug string, activate bool) error {
	if !validWPSlug.MatchString(slug) {
		return fmt.Errorf("invalid plugin slug")
	}
	args := []string{"plugin", "install", slug}
	if activate {
		args = append(args, "--activate")
	}
	_, err := wpRun(sysUser, docroot, args...)
	return err
}

// WPPluginAction runs activate / deactivate / update / delete on a plugin.
func WPPluginAction(sysUser, docroot, slug, action string) error {
	if !wpPluginActions[action] {
		return fmt.Errorf("unsupported plugin action %q", action)
	}
	if !validWPSlug.MatchString(slug) {
		return fmt.Errorf("invalid plugin slug")
	}
	_, err := wpRun(sysUser, docroot, "plugin", action, slug)
	return err
}

// WPThemeInstall installs (optionally activating) a theme from the WP.org
// directory.
func WPThemeInstall(sysUser, docroot, slug string, activate bool) error {
	if !validWPSlug.MatchString(slug) {
		return fmt.Errorf("invalid theme slug")
	}
	args := []string{"theme", "install", slug}
	if activate {
		args = append(args, "--activate")
	}
	_, err := wpRun(sysUser, docroot, args...)
	return err
}

// WPThemeAction runs activate / update / delete on a theme.
func WPThemeAction(sysUser, docroot, slug, action string) error {
	if !wpThemeActions[action] {
		return fmt.Errorf("unsupported theme action %q", action)
	}
	if !validWPSlug.MatchString(slug) {
		return fmt.Errorf("invalid theme slug")
	}
	_, err := wpRun(sysUser, docroot, "theme", action, slug)
	return err
}

// WPAutoUpdate enables or disables per-item background auto-updates for a plugin
// or theme. kind is "plugin" or "theme".
func WPAutoUpdate(sysUser, docroot, kind, slug string, enable bool) error {
	if kind != "plugin" && kind != "theme" {
		return fmt.Errorf("invalid kind %q", kind)
	}
	if !validWPSlug.MatchString(slug) {
		return fmt.Errorf("invalid slug")
	}
	verb := "disable"
	if enable {
		verb = "enable"
	}
	_, err := wpRun(sysUser, docroot, kind, "auto-updates", verb, slug)
	return err
}
