package system

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// PHP version management. A fresh Debian/Ubuntu install ships a single PHP
// version; to offer customers a choice the panel can install additional
// versions from the distribution's multi-version PHP repository (Sury on
// Debian, ppa:ondrej/php on Ubuntu). Installed versions are detected from
// /etc/php by PHPVersions (nginx.go).

const aptTimeout = 15 * time.Minute

// knownPHPVersions are the versions the panel offers to install, oldest first.
// Actual availability depends on the configured third-party PHP repository.
var knownPHPVersions = []string{"7.4", "8.0", "8.1", "8.2", "8.3", "8.4"}

// phpExtensions are installed alongside every php<ver>-fpm so a site behaves
// the same on whichever version it runs (matches the installer's base set).
var phpExtensions = []string{"fpm", "mysql", "curl", "gd", "mbstring", "xml", "zip"}

// KnownPHPVersions returns the versions the panel can install.
func KnownPHPVersions() []string {
	out := make([]string, len(knownPHPVersions))
	copy(out, knownPHPVersions)
	return out
}

func validPHPVersion(v string) bool {
	for _, k := range knownPHPVersions {
		if k == v {
			return true
		}
	}
	return false
}

// InstallPHP installs a PHP-FPM version and the common extension set, adding
// the distribution's third-party PHP repository first when necessary. It is
// long-running (apt) and Linux-only; callers should run it in the background.
func InstallPHP(version string) error {
	if !validPHPVersion(version) {
		return fmt.Errorf("unsupported PHP version %q", version)
	}
	if !Linux() {
		return fmt.Errorf("PHP installation is only supported on Linux hosts")
	}
	if !have("apt-get") {
		return fmt.Errorf("apt-get is not available on this host")
	}
	if err := ensurePHPRepo(); err != nil {
		return err
	}
	pkgs := make([]string, 0, len(phpExtensions))
	for _, ext := range phpExtensions {
		pkgs = append(pkgs, "php"+version+"-"+ext)
	}
	if _, err := apt(append([]string{"install", "-y", "-q"}, pkgs...)...); err != nil {
		return err
	}
	if have("systemctl") {
		run("systemctl", "enable", "--now", "php"+version+"-fpm")
	}
	if _, err := os.Stat("/etc/php/" + version + "/fpm"); err != nil {
		return fmt.Errorf("php%s-fpm was not installed (the repository may not provide this version)", version)
	}
	return nil
}

// apt runs apt-get non-interactively with a long timeout.
func apt(args ...string) (string, error) {
	return runOpts(aptTimeout, []string{"DEBIAN_FRONTEND=noninteractive"}, "apt-get", args...)
}

// ensurePHPRepo makes sure a repository that ships multiple PHP versions is
// configured for this distribution, then refreshes the package lists.
func ensurePHPRepo() error {
	switch osReleaseField("ID") {
	case "ubuntu":
		if !have("add-apt-repository") {
			if _, err := apt("install", "-y", "-q", "software-properties-common"); err != nil {
				return err
			}
		}
		if _, err := runOpts(aptTimeout, []string{"DEBIAN_FRONTEND=noninteractive"},
			"add-apt-repository", "-y", "ppa:ondrej/php"); err != nil {
			return fmt.Errorf("add ondrej/php PPA: %w", err)
		}
	case "debian":
		if err := ensureSuryRepo(); err != nil {
			return err
		}
	}
	if _, err := apt("update", "-q"); err != nil {
		return err
	}
	return nil
}

// ensureSuryRepo installs the packages.sury.org PHP repository on Debian.
func ensureSuryRepo() error {
	const listPath = "/etc/apt/sources.list.d/sury-php.list"
	if _, err := os.Stat(listPath); err == nil {
		return nil // already configured
	}
	if _, err := apt("install", "-y", "-q", "ca-certificates", "curl"); err != nil {
		return err
	}
	if _, err := runOpts(aptTimeout, nil, "sh", "-c",
		"curl -fsSL https://packages.sury.org/php/apt.gpg -o /etc/apt/trusted.gpg.d/sury-php.gpg"); err != nil {
		return fmt.Errorf("fetch Sury signing key: %w", err)
	}
	codename := osReleaseField("VERSION_CODENAME")
	if codename == "" {
		return fmt.Errorf("cannot determine Debian codename from /etc/os-release")
	}
	line := fmt.Sprintf("deb https://packages.sury.org/php/ %s main\n", codename)
	return os.WriteFile(listPath, []byte(line), 0o644)
}

// osReleaseField returns a value from /etc/os-release (e.g. ID, VERSION_CODENAME).
func osReleaseField(key string) string {
	b, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(line, key+"="); ok {
			return strings.Trim(strings.TrimSpace(v), `"`)
		}
	}
	return ""
}
