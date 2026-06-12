package system

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/repanel/repanel/internal/models"
)

// Firewall management drives ufw, the stock Debian/Ubuntu frontend.

var (
	validPort   = regexp.MustCompile(`^\d{1,5}(:\d{1,5})?$`)
	validSource = regexp.MustCompile(`^([0-9a-fA-F.:]+(/\d{1,3})?|any)$`)
)

func UFWAvailable() bool { return have("ufw") }

// UFWStatus returns "active", "inactive" or "not installed".
func UFWStatus() string {
	if !UFWAvailable() {
		return "not installed"
	}
	out, err := run("ufw", "status")
	if err != nil {
		return "unknown"
	}
	if strings.Contains(out, "Status: active") {
		return "active"
	}
	return "inactive"
}

// ApplyFirewallRule adds a ufw rule matching the stored model.
func ApplyFirewallRule(r models.FirewallRule) error {
	if !UFWAvailable() {
		return fmt.Errorf("ufw is not installed")
	}
	if !validPort.MatchString(r.Port) {
		return fmt.Errorf("invalid port %q", r.Port)
	}
	if r.Proto != "tcp" && r.Proto != "udp" {
		return fmt.Errorf("invalid protocol %q", r.Proto)
	}
	action := r.Action
	if action != "allow" && action != "deny" {
		return fmt.Errorf("invalid action %q", r.Action)
	}
	src := r.Source
	if src == "" {
		src = "any"
	}
	if !validSource.MatchString(src) {
		return fmt.Errorf("invalid source %q", r.Source)
	}
	args := []string{action, "from", src, "to", "any", "port", strings.ReplaceAll(r.Port, "-", ":"), "proto", r.Proto}
	_, err := run("ufw", args...)
	return err
}

// RemoveFirewallRule deletes the matching ufw rule.
func RemoveFirewallRule(r models.FirewallRule) error {
	if !UFWAvailable() {
		return nil
	}
	src := r.Source
	if src == "" {
		src = "any"
	}
	if !validPort.MatchString(r.Port) || !validSource.MatchString(src) {
		return fmt.Errorf("invalid stored rule")
	}
	args := []string{"delete", r.Action, "from", src, "to", "any", "port", r.Port, "proto", r.Proto}
	_, err := run("ufw", args...)
	return err
}

// SetUFWEnabled toggles the firewall, always keeping the panel port and SSH open.
func SetUFWEnabled(enable bool, panelPort string) error {
	if !UFWAvailable() {
		return fmt.Errorf("ufw is not installed")
	}
	if enable {
		// Never lock the admin out.
		run("ufw", "allow", "ssh")
		if validPort.MatchString(panelPort) {
			run("ufw", "allow", panelPort+"/tcp")
		}
		_, err := run("ufw", "--force", "enable")
		return err
	}
	_, err := run("ufw", "disable")
	return err
}
