package system

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/reinis1996/repanel/internal/models"
)

// OS package updates via apt: list what's upgradable (flagging security updates
// by their origin) and apply the upgrade while streaming the live output, so the
// panel can show package updates the way Virtualmin does. Debian/Ubuntu only.

// AptAvailable reports whether apt is usable on this host.
func AptAvailable() bool { return Linux() && have("apt-get") }

// RefreshPackageLists runs `apt-get update` so the upgradable list is current.
func RefreshPackageLists() error {
	if !AptAvailable() {
		return fmt.Errorf("apt is not available on this host")
	}
	_, err := apt("update", "-q")
	return err
}

// aptInstRe parses an `apt-get -s upgrade` "Inst" line:
//
//	Inst <name> [<oldver>] (<newver> <origin...> [<arch>])
//
// The old-version bracket is absent for packages newly pulled in as a
// dependency; the origin list after the new version names the archive(s), which
// is how we tell a security update (e.g. "Debian-Security" / "...-security").
var aptInstRe = regexp.MustCompile(`^Inst\s+(\S+)\s+(?:\[([^\]]+)\]\s+)?\(([^\s]+)\s+([^)]*)\)`)

// ListPackageUpdates returns the pending package upgrades from the *current*
// (cached) package lists — call RefreshPackageLists first for fresh data. It
// simulates `apt-get upgrade` (no changes are made).
func ListPackageUpdates() ([]models.PackageUpdate, error) {
	if !AptAvailable() {
		return nil, fmt.Errorf("apt is not available on this host")
	}
	out, _, err := runCapture(2*time.Minute, "", "", "apt-get", "-s", "upgrade")
	if err != nil && strings.TrimSpace(out) == "" {
		return nil, err
	}
	return parseAptUpgrade(out), nil
}

// parseAptUpgrade extracts the pending upgrades from `apt-get -s upgrade` output.
func parseAptUpgrade(out string) []models.PackageUpdate {
	ups := []models.PackageUpdate{}
	for _, line := range strings.Split(out, "\n") {
		m := aptInstRe.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		origin := m[4]
		ups = append(ups, models.PackageUpdate{
			Name:           m[1],
			CurrentVersion: m[2],
			NewVersion:     m[3],
			Security:       strings.Contains(origin, "Security") || strings.Contains(origin, "-security"),
		})
	}
	return ups
}

// ApplyPackageUpdates refreshes the package lists and upgrades all packages,
// invoking onLine for each line of apt/dpkg output so the caller can stream it.
// It runs non-interactively and keeps existing config files on conflict, so it
// never blocks waiting for input.
func ApplyPackageUpdates(onLine func(string)) error {
	if !AptAvailable() {
		return fmt.Errorf("apt is not available on this host")
	}
	if err := streamCommand(onLine, []string{"DEBIAN_FRONTEND=noninteractive"},
		"apt-get", "update", "-q"); err != nil {
		return fmt.Errorf("apt-get update: %w", err)
	}
	onLine("")
	if err := streamCommand(onLine, []string{"DEBIAN_FRONTEND=noninteractive"},
		"apt-get", "-y",
		"-o", "Dpkg::Options::=--force-confdef",
		"-o", "Dpkg::Options::=--force-confold",
		"upgrade"); err != nil {
		return fmt.Errorf("apt-get upgrade: %w", err)
	}
	return nil
}

// streamCommand runs a command with extra environment, delivering its combined
// stdout+stderr to onLine one line at a time, and returns the command's error.
func streamCommand(onLine func(string), extraEnv []string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		pw.Close()
		done <- err
	}()
	scanner := bufio.NewScanner(pr)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		onLine(scanner.Text())
	}
	return <-done
}
