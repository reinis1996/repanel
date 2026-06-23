package system

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/reinis1996/repanel/internal/models"
)

// This file extends the WordPress Workbench (see wpcli.go) with the operations an
// enterprise host expects: bulk/auto updates, user management, one-click admin
// login, maintenance tooling and database/configuration control. Like the rest of
// the workbench every WP-CLI call runs AS the site's owning system user, never
// root.

var (
	// WordPress core roles plus a permissive shape for custom roles registered by
	// plugins (lowercase slug). Used to reject crafted --role values.
	validWPRole  = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,40}$`)
	validWPLogin = regexp.MustCompile(`^[a-zA-Z0-9 ._@-]{1,60}$`)
	validWPEmail = regexp.MustCompile(`^[^\s@]{1,64}@[^\s@]{1,255}$`)
	// A memory limit like 256M / 1G, or empty to clear.
	validWPMemory = regexp.MustCompile(`^\d{1,5}[MG]$`)
)

// wpRunStdin is wpRun with data fed on STDIN, used to pass passwords to WP-CLI via
// --prompt=<arg> so they never appear in the process list (SECURITY_AUDIT F-14).
func wpRunStdin(sysUser, docroot, stdin string, args ...string) (string, error) {
	if !have("wp") {
		return "", fmt.Errorf("WP-CLI (wp) is not installed on this server")
	}
	name, cargs := wpCmd(sysUser, docroot, args...)
	out, errOut, err := runCapture(wpTimeout, docroot, stdin, name, cargs...)
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

// wpCLIURL is the official WP-CLI phar build.
const wpCLIURL = "https://raw.githubusercontent.com/wp-cli/builds/gh-pages/phar/wp-cli.phar"

// InstallWPCLI downloads the WP-CLI phar to /usr/local/bin/wp and makes it
// executable, so the workbench can be enabled on hosts where it is missing.
func InstallWPCLI() error {
	if !Linux() {
		return fmt.Errorf("WP-CLI can only be installed on the Linux host")
	}
	if have("wp") {
		return nil
	}
	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Get(wpCLIURL)
	if err != nil {
		return fmt.Errorf("download WP-CLI: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download WP-CLI: unexpected status %s", resp.Status)
	}
	dst, tmp := "/usr/local/bin/wp", "/usr/local/bin/wp.download"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil { //nolint:gosec // bounded by phar size
		f.Close()
		os.Remove(tmp)
		return err
	}
	f.Close()
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// ---- Bulk updates -----------------------------------------------------------

// WPUpdateAll updates core, every plugin and every theme, then the database
// schema, reporting how much changed. Individual plugin/theme failures don't abort
// the run; a hard core/db failure does.
func WPUpdateAll(sysUser, docroot string) (models.WPUpdateResult, error) {
	var res models.WPUpdateResult
	res.Plugins = wpCount(sysUser, docroot, "plugin")
	res.Themes = wpCount(sysUser, docroot, "theme")

	if upd, _ := wpRun(sysUser, docroot, "core", "check-update", "--field=version", "--format=count"); strings.TrimSpace(upd) != "" && strings.TrimSpace(upd) != "0" {
		if err := WPCoreUpdate(sysUser, docroot); err != nil {
			return res, err
		}
		res.Core = true
	}
	// --all is best-effort: a single broken plugin shouldn't strand the rest.
	wpRun(sysUser, docroot, "plugin", "update", "--all")
	wpRun(sysUser, docroot, "theme", "update", "--all")
	if _, err := wpRun(sysUser, docroot, "core", "update-db"); err != nil {
		return res, err
	}
	return res, nil
}

// ---- Users ------------------------------------------------------------------

// WPUsers lists the site's WordPress user accounts.
func WPUsers(sysUser, docroot string) ([]models.WPUser, error) {
	out, err := wpRun(sysUser, docroot, "user", "list", "--format=json",
		"--fields=ID,user_login,user_email,display_name,roles,user_registered")
	if err != nil {
		return nil, err
	}
	var raw []map[string]any
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, fmt.Errorf("could not read user list")
	}
	users := make([]models.WPUser, 0, len(raw))
	for _, u := range raw {
		id, _ := strconv.ParseInt(coerceStr(u["ID"]), 10, 64)
		users = append(users, models.WPUser{
			ID:          id,
			Login:       coerceStr(u["user_login"]),
			Email:       coerceStr(u["user_email"]),
			DisplayName: coerceStr(u["display_name"]),
			Roles:       coerceStr(u["roles"]),
			Registered:  coerceStr(u["user_registered"]),
		})
	}
	return users, nil
}

// WPUserCreate adds a user. The password is read from STDIN via --prompt so it is
// never exposed on the command line.
func WPUserCreate(sysUser, docroot, login, email, role, password string) error {
	if !validWPLogin.MatchString(login) {
		return fmt.Errorf("invalid username")
	}
	if !validWPEmail.MatchString(email) {
		return fmt.Errorf("invalid email address")
	}
	if !validWPRole.MatchString(role) {
		return fmt.Errorf("invalid role")
	}
	if len(password) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}
	_, err := wpRunStdin(sysUser, docroot, password+"\n", "user", "create", login, email,
		"--role="+role, "--prompt=user_pass")
	return err
}

// WPUserSetRole replaces a user's role.
func WPUserSetRole(sysUser, docroot string, userID int64, role string) error {
	if userID <= 0 {
		return fmt.Errorf("invalid user")
	}
	if !validWPRole.MatchString(role) {
		return fmt.Errorf("invalid role")
	}
	if _, err := wpRun(sysUser, docroot, "user", "set-role", strconv.FormatInt(userID, 10), role); err != nil {
		return err
	}
	return nil
}

// WPUserResetPassword sets a new password for a user (read from STDIN).
func WPUserResetPassword(sysUser, docroot string, userID int64, password string) error {
	if userID <= 0 {
		return fmt.Errorf("invalid user")
	}
	if len(password) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}
	_, err := wpRunStdin(sysUser, docroot, password+"\n", "user", "update",
		strconv.FormatInt(userID, 10), "--prompt=user_pass")
	return err
}

// WPUserDelete removes a user, reassigning their content to the given user ID.
func WPUserDelete(sysUser, docroot string, userID, reassignTo int64) error {
	if userID <= 0 {
		return fmt.Errorf("invalid user")
	}
	args := []string{"user", "delete", strconv.FormatInt(userID, 10), "--yes"}
	if reassignTo > 0 {
		args = append(args, "--reassign="+strconv.FormatInt(reassignTo, 10))
	}
	_, err := wpRun(sysUser, docroot, args...)
	return err
}

// ---- One-click admin login --------------------------------------------------

// repanelLoginPlugin is a self-contained must-use plugin that grants a single,
// short-lived, server-issued login. It is inert unless a matching token file
// (written by WPMagicLogin and owned by the site user) exists, and it deletes that
// file on first use, so it cannot be replayed and does nothing on its own.
const repanelLoginPlugin = `<?php
/* RePanel one-click login. Inert unless the panel has issued a one-time token. */
add_action( 'init', function () {
	if ( empty( $_GET['repanel_login'] ) ) {
		return;
	}
	$file = WP_CONTENT_DIR . '/.repanel-login';
	if ( ! is_readable( $file ) ) {
		return;
	}
	$data = json_decode( file_get_contents( $file ), true );
	@unlink( $file ); // single use, regardless of outcome
	if ( empty( $data['hash'] ) || empty( $data['exp'] ) || empty( $data['uid'] ) ) {
		return;
	}
	if ( time() > (int) $data['exp'] ) {
		return;
	}
	$token = (string) $_GET['repanel_login'];
	if ( ! hash_equals( (string) $data['hash'], hash( 'sha256', $token ) ) ) {
		return;
	}
	$uid = (int) $data['uid'];
	wp_set_auth_cookie( $uid, false );
	wp_set_current_user( $uid );
	wp_safe_redirect( admin_url() );
	exit;
} );
`

// WPMagicLogin issues a one-time login URL for the given user. It drops the helper
// mu-plugin (idempotently) and writes a token file readable only by the site user,
// valid for two minutes and consumed on first use. The returned URL logs straight
// into wp-admin.
func WPMagicLogin(sysUser, docroot, siteURL string, userID int64) (string, error) {
	if userID <= 0 {
		return "", fmt.Errorf("invalid user")
	}
	if siteURL == "" {
		return "", fmt.Errorf("site URL unknown")
	}
	muDir := filepath.Join(docroot, "wp-content", "mu-plugins")
	if err := os.MkdirAll(muDir, 0o755); err != nil {
		return "", fmt.Errorf("create mu-plugins: %w", err)
	}
	pluginPath := filepath.Join(muDir, "repanel-login.php")
	if err := os.WriteFile(pluginPath, []byte(repanelLoginPlugin), 0o644); err != nil {
		return "", err
	}

	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := hex.EncodeToString(buf)
	sum := sha256.Sum256([]byte(token))
	meta, _ := json.Marshal(map[string]any{
		"hash": hex.EncodeToString(sum[:]),
		"exp":  time.Now().Add(2 * time.Minute).Unix(),
		"uid":  userID,
	})
	tokenPath := filepath.Join(docroot, "wp-content", ".repanel-login")
	if err := os.WriteFile(tokenPath, meta, 0o600); err != nil {
		return "", err
	}
	// Hand both files to the site user so its PHP-FPM worker can read the token.
	if Linux() && validSysName.MatchString(sysUser) {
		run("chown", sysUser+":"+sysUser, pluginPath, tokenPath)
	}
	return strings.TrimRight(siteURL, "/") + "/?repanel_login=" + token, nil
}

// ---- Maintenance & operations ----------------------------------------------

// WPMaintenanceMode toggles WordPress maintenance mode.
func WPMaintenanceMode(sysUser, docroot string, enable bool) error {
	verb := "deactivate"
	if enable {
		verb = "activate"
	}
	_, err := wpRun(sysUser, docroot, "maintenance-mode", verb)
	return err
}

// WPFlushCache empties the object cache.
func WPFlushCache(sysUser, docroot string) error {
	_, err := wpRun(sysUser, docroot, "cache", "flush")
	return err
}

// WPDeleteTransients clears all transients from the options table.
func WPDeleteTransients(sysUser, docroot string) error {
	_, err := wpRun(sysUser, docroot, "transient", "delete", "--all")
	return err
}

// WPFlushRewrites regenerates the permalink/rewrite rules.
func WPFlushRewrites(sysUser, docroot string) error {
	_, err := wpRun(sysUser, docroot, "rewrite", "flush")
	return err
}

// WPVerifyChecksums verifies WordPress core files against the official checksums,
// returning a nil error when intact. On mismatch the error lists what changed.
func WPVerifyChecksums(sysUser, docroot string) error {
	_, err := wpRun(sysUser, docroot, "core", "verify-checksums")
	return err
}

// WPCronEvents lists scheduled WP-cron hooks and when they next run.
func WPCronEvents(sysUser, docroot string) ([]models.WPCronEvent, error) {
	out, err := wpRun(sysUser, docroot, "cron", "event", "list", "--format=json",
		"--fields=hook,next_run_relative,recurrence")
	if err != nil {
		return nil, err
	}
	var raw []map[string]any
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, fmt.Errorf("could not read cron events")
	}
	events := make([]models.WPCronEvent, 0, len(raw))
	for _, e := range raw {
		events = append(events, models.WPCronEvent{
			Hook:     coerceStr(e["hook"]),
			NextRun:  coerceStr(e["next_run_relative"]),
			Schedule: coerceStr(e["recurrence"]),
		})
	}
	return events, nil
}

// WPCronRunDue runs every WP-cron event that is currently due.
func WPCronRunDue(sysUser, docroot string) error {
	_, err := wpRun(sysUser, docroot, "cron", "event", "run", "--due-now")
	return err
}

// ---- Database ---------------------------------------------------------------

// validReplaceTerm rejects search/replace terms that could be mistaken for a
// WP-CLI flag or that span lines. URLs and table prefixes are the expected input.
var validReplaceTerm = regexp.MustCompile(`^[^\s\x00-\x1f-][^\s\x00-\x1f]{0,512}$`)

// WPSearchReplace performs a database-wide search-and-replace, the core of a
// domain migration. When dryRun is true nothing is written; the returned string is
// WP-CLI's human-readable report of how many rows would change.
func WPSearchReplace(sysUser, docroot, from, to string, dryRun bool) (string, error) {
	if !validReplaceTerm.MatchString(from) || !validReplaceTerm.MatchString(to) {
		return "", fmt.Errorf("search and replace terms must be single-line and may not start with '-'")
	}
	args := []string{"search-replace", from, to, "--all-tables", "--report-changes-count", "--no-color"}
	if dryRun {
		args = append(args, "--dry-run")
	}
	return wpRun(sysUser, docroot, args...)
}

// WPDBOptimize runs OPTIMIZE TABLE across the site's database.
func WPDBOptimize(sysUser, docroot string) error {
	_, err := wpRun(sysUser, docroot, "db", "optimize")
	return err
}

// WPDBExport streams a SQL dump of the site's database to w. The dump is produced
// by `wp db export -` so it never touches disk in the docroot.
func WPDBExport(sysUser, docroot string, w io.Writer) error {
	if !have("wp") {
		return fmt.Errorf("WP-CLI (wp) is not installed on this server")
	}
	ctx, cancel := context.WithTimeout(context.Background(), wpTimeout)
	defer cancel()
	name, cargs := wpCmd(sysUser, docroot, "db", "export", "-")
	cmd := exec.CommandContext(ctx, name, cargs...)
	cmd.Stdout = w
	var errb strings.Builder
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(errb.String()); msg != "" {
			return fmt.Errorf("%s", firstLine(msg))
		}
		return err
	}
	return nil
}

// ---- wp-config constants ----------------------------------------------------

// WPGetConfig reads the managed wp-config.php constants. Missing constants are
// reported as their WordPress defaults (off / empty).
func WPGetConfig(sysUser, docroot string) (models.WPConfig, error) {
	if _, err := wpRun(sysUser, docroot, "core", "version"); err != nil {
		return models.WPConfig{}, err
	}
	cfg := models.WPConfig{
		Debug:            wpConfigBool(sysUser, docroot, "WP_DEBUG"),
		DebugLog:         wpConfigBool(sysUser, docroot, "WP_DEBUG_LOG"),
		DisallowFileEdit: wpConfigBool(sysUser, docroot, "DISALLOW_FILE_EDIT"),
	}
	cfg.MemoryLimit, _ = wpRun(sysUser, docroot, "config", "get", "WP_MEMORY_LIMIT", "--type=constant")
	cfg.AutoUpdateCore, _ = wpRun(sysUser, docroot, "config", "get", "WP_AUTO_UPDATE_CORE", "--type=constant")
	return cfg, nil
}

func wpConfigBool(sysUser, docroot, name string) bool {
	v, err := wpRun(sysUser, docroot, "config", "get", name, "--type=constant")
	if err != nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "on", "yes":
		return true
	}
	return false
}

// WPSetConfig applies the managed wp-config.php constants.
func WPSetConfig(sysUser, docroot string, cfg models.WPConfig) error {
	if cfg.MemoryLimit != "" && !validWPMemory.MatchString(cfg.MemoryLimit) {
		return fmt.Errorf("memory limit must look like 256M or 1G")
	}
	setBool := func(name string, on bool) error {
		val := "false"
		if on {
			val = "true"
		}
		_, err := wpRun(sysUser, docroot, "config", "set", name, val, "--raw", "--type=constant")
		return err
	}
	if err := setBool("WP_DEBUG", cfg.Debug); err != nil {
		return err
	}
	if err := setBool("WP_DEBUG_LOG", cfg.DebugLog); err != nil {
		return err
	}
	if err := setBool("DISALLOW_FILE_EDIT", cfg.DisallowFileEdit); err != nil {
		return err
	}
	if cfg.MemoryLimit != "" {
		if _, err := wpRun(sysUser, docroot, "config", "set", "WP_MEMORY_LIMIT", cfg.MemoryLimit, "--type=constant"); err != nil {
			return err
		}
	}
	switch cfg.AutoUpdateCore {
	case "true", "false":
		if _, err := wpRun(sysUser, docroot, "config", "set", "WP_AUTO_UPDATE_CORE", cfg.AutoUpdateCore, "--raw", "--type=constant"); err != nil {
			return err
		}
	case "minor":
		if _, err := wpRun(sysUser, docroot, "config", "set", "WP_AUTO_UPDATE_CORE", "minor", "--type=constant"); err != nil {
			return err
		}
	}
	return nil
}

// WPRegenerateSalts replaces the authentication keys and salts in wp-config.php,
// invalidating every existing login session.
func WPRegenerateSalts(sysUser, docroot string) error {
	_, err := wpRun(sysUser, docroot, "config", "shuffle-salts")
	return err
}

// coerceStr converts a JSON-decoded value (string, number or bool) to a string.
func coerceStr(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", t)
	}
}
