package system

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/reinis1996/repanel/internal/models"
)

// Per-account PHP-FPM. By default a domain's pool runs under the distribution's
// shared php<ver>-fpm master (in system.slice). When the owning account has
// cgroup limits (an account slice exists, see cgroup.go) its pools instead run
// under a per-account FPM master service placed in that slice, so the account's
// PHP CPU/memory/processes are bounded together. The pool socket path is the
// same either way, so nginx/Apache vhosts never change when an account moves
// between the shared and per-account master.
//
// Trade-off: a per-account master is an extra ~5–10 MB resident process, so this
// is used only for accounts that actually have limits set.

const (
	perUserFPMRoot = "/etc/repanel/php-fpm" // per-account master configs + pools
	fpmRunDir      = "/run/repanel-php"     // pid files + (optionally) sockets
	fpmLogDir      = "/var/log/repanel-php" // per-account FPM error logs
)

func perUserFPMDir(userID int64, ver string) string {
	return fmt.Sprintf("%s/%d/%s", perUserFPMRoot, userID, ver)
}
func perUserPoolDir(userID int64, ver string) string { return perUserFPMDir(userID, ver) + "/pool.d" }
func perUserMasterConf(userID int64, ver string) string {
	return perUserFPMDir(userID, ver) + "/php-fpm.conf"
}
func perUserFPMUnit(userID int64, ver string) string {
	return fmt.Sprintf("repanel-php-%s-u%d.service", ver, userID)
}
func perUserFPMUnitPath(userID int64, ver string) string {
	return "/etc/systemd/system/" + perUserFPMUnit(userID, ver)
}
func sharedPoolPath(d models.Domain) string {
	return fmt.Sprintf("/etc/php/%s/fpm/pool.d/repanel-%s.conf", d.PHPVersion, poolName(d.Name))
}
func perUserPoolPath(d models.Domain) string {
	return perUserPoolDir(d.UserID, d.PHPVersion) + "/repanel-" + poolName(d.Name) + ".conf"
}

func phpFpmBin(ver string) string { return "/usr/sbin/php-fpm" + ver }

func phpFpmBinExists(ver string) bool {
	_, err := os.Stat(phpFpmBin(ver))
	return err == nil
}

// accountHasLimits reports whether an account has a cgroup slice (limits set),
// which is the trigger for hosting its pools under a per-account FPM master.
func accountHasLimits(userID int64) bool {
	_, err := os.Stat(accountSlicePath(userID))
	return err == nil
}

// applyPHPPool installs the rendered pool config under the correct master.
func applyPHPPool(d models.Domain, content string) error {
	if accountHasLimits(d.UserID) && phpFpmBinExists(d.PHPVersion) {
		return writePerUserPool(d, content)
	}
	return writeSharedPool(d, content)
}

// ---- shared (distribution) master ------------------------------------------

func writeSharedPool(d models.Domain, content string) error {
	// Vacate the per-account master if the account just lost its limits.
	removePerUserPool(d)

	path := sharedPoolPath(d)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return err
	}
	if err := phpFpmTest(d.PHPVersion); err != nil {
		os.Remove(path)
		ReloadService("php" + d.PHPVersion + "-fpm")
		return err
	}
	ReloadService("php" + d.PHPVersion + "-fpm")
	return nil
}

// ---- per-account master ----------------------------------------------------

func writePerUserPool(d models.Domain, content string) error {
	ver := d.PHPVersion
	if err := os.MkdirAll(perUserPoolDir(d.UserID, ver), 0o755); err != nil {
		return err
	}
	os.MkdirAll(fpmRunDir, 0o755)
	os.MkdirAll(fpmLogDir, 0o750)
	if err := ensurePerUserMaster(d.UserID, ver); err != nil {
		return err
	}
	path := perUserPoolPath(d)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return err
	}
	// Validate the whole per-account config before (re)starting the master.
	if err := phpFpmTestConfig(ver, perUserMasterConf(d.UserID, ver)); err != nil {
		os.Remove(path)
		reloadPerUserMaster(d.UserID, ver)
		return err
	}
	// Release the socket from the shared master first (it may have hosted this
	// pool before the account gained limits), then bind it on the per-account one.
	if removeFileReload(sharedPoolPath(d), "php"+ver+"-fpm") {
		time.Sleep(150 * time.Millisecond) // let the shared reload release the socket
	}
	return startOrReloadPerUserMaster(d.UserID, ver)
}

// renderPerUserMasterConf builds the FPM master config for an account/version.
func renderPerUserMasterConf(userID int64, ver string) string {
	return fmt.Sprintf(`; Managed by RePanel — per-account PHP-FPM master.
[global]
pid = %s/%d-%s.pid
error_log = %s/%d-%s.log
daemonize = no
include = %s/*.conf
`, fpmRunDir, userID, ver, fpmLogDir, userID, ver, perUserPoolDir(userID, ver))
}

// renderPerUserUnit builds the systemd unit for an account's FPM master, placed
// in the account slice so its CPU/memory/processes are bounded together.
func renderPerUserUnit(userID int64, ver string) string {
	return fmt.Sprintf(`# Managed by RePanel — PHP-FPM %s for account %d. Do not edit.
[Unit]
Description=RePanel PHP-FPM %s (account %d)
After=network.target

[Service]
Type=notify
Slice=%s
PIDFile=%s/%d-%s.pid
ExecStartPre=/bin/mkdir -p %s %s
ExecStart=%s --nodaemonize --fpm-config %s
ExecReload=/bin/kill -USR2 $MAINPID
Restart=on-failure
RestartSec=2

[Install]
WantedBy=multi-user.target
`, ver, userID, ver, userID, AccountSliceName(userID), fpmRunDir, userID, ver,
		fpmRunDir, fpmLogDir, phpFpmBin(ver), perUserMasterConf(userID, ver))
}

// ensurePerUserMaster writes the per-account master config + systemd unit.
func ensurePerUserMaster(userID int64, ver string) error {
	if err := os.WriteFile(perUserMasterConf(userID, ver), []byte(renderPerUserMasterConf(userID, ver)), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(perUserFPMUnitPath(userID, ver), []byte(renderPerUserUnit(userID, ver)), 0o644); err != nil {
		return err
	}
	if have("systemctl") {
		run("systemctl", "daemon-reload")
	}
	return nil
}

func startOrReloadPerUserMaster(userID int64, ver string) error {
	unit := perUserFPMUnit(userID, ver)
	if !have("systemctl") {
		return nil
	}
	if _, err := run("systemctl", "is-active", "--quiet", unit); err == nil {
		_, err := run("systemctl", "reload", unit)
		return err
	}
	run("systemctl", "reset-failed", unit)
	_, err := run("systemctl", "enable", "--now", unit)
	return err
}

func reloadPerUserMaster(userID int64, ver string) {
	unit := perUserFPMUnit(userID, ver)
	if have("systemctl") {
		if _, err := run("systemctl", "is-active", "--quiet", unit); err == nil {
			run("systemctl", "reload", unit)
		}
	}
}

// removePerUserPool drops a domain's pool from its account master; when that was
// the account's last pool for the version the master is stopped and removed.
func removePerUserPool(d models.Domain) {
	path := perUserPoolPath(d)
	if _, err := os.Stat(path); err != nil {
		return
	}
	os.Remove(path)
	if perUserPoolDirEmpty(d.UserID, d.PHPVersion) {
		stopRemovePerUserMaster(d.UserID, d.PHPVersion)
	} else {
		reloadPerUserMaster(d.UserID, d.PHPVersion)
	}
}

func perUserPoolDirEmpty(userID int64, ver string) bool {
	entries, err := os.ReadDir(perUserPoolDir(userID, ver))
	if err != nil {
		return true
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".conf") {
			return false
		}
	}
	return true
}

func stopRemovePerUserMaster(userID int64, ver string) {
	unit := perUserFPMUnit(userID, ver)
	if have("systemctl") {
		run("systemctl", "disable", "--now", unit)
	}
	os.Remove(perUserFPMUnitPath(userID, ver))
	os.RemoveAll(perUserFPMDir(userID, ver))
	if have("systemctl") {
		run("systemctl", "daemon-reload")
	}
}

// removePHPPoolFiles removes a domain's pool from both the shared and per-account
// masters (whichever holds it) and reloads.
func removePHPPoolFiles(d models.Domain) {
	removeFileReload(sharedPoolPath(d), "php"+d.PHPVersion+"-fpm")
	removePerUserPool(d)
}

// removeFileReload removes path and reloads service if the file existed.
func removeFileReload(path, service string) bool {
	if _, err := os.Stat(path); err != nil {
		return false
	}
	os.Remove(path)
	ReloadService(service)
	return true
}

// phpFpmTestConfig validates a specific FPM master config (master + its pools).
func phpFpmTestConfig(ver, conf string) error {
	bin := phpFpmBin(ver)
	if _, err := os.Stat(bin); err != nil {
		return nil
	}
	if _, errOut, err := runCapture(30*time.Second, "", "", bin, "--fpm-config", conf, "-t"); err != nil {
		msg := strings.TrimSpace(errOut)
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("php-fpm config test failed: %s", firstLine(msg))
	}
	return nil
}
