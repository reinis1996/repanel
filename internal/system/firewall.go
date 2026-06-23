package system

import (
	"fmt"
	"os"
	"os/user"
	"regexp"
	"strings"

	"github.com/reinis1996/repanel/internal/models"
)

// Firewall management drives ufw, the stock Debian/Ubuntu frontend.

var (
	validPort   = regexp.MustCompile(`^\d{1,5}(:\d{1,5})?$`)
	validSource = regexp.MustCompile(`^([0-9a-fA-F.:]+(/\d{1,3})?|any)$`)
)

// FTPPassivePorts is the ProFTPD passive port range the installer configures and
// the panel opens, so passive-mode FTP works through the firewall.
const FTPPassivePorts = "49152:49251"

// FirewallPort is a default opening required by the panel's stack.
type FirewallPort struct {
	Port  string
	Proto string // tcp | udp
	Note  string
}

// DefaultPanelPorts returns the firewall openings the panel's standard stack
// needs: SSH and the panel itself, plus the web, DNS, mail and FTP ports the
// installer provisions. They are opened unconditionally (the default install
// includes the whole stack); an operator can remove any they don't use from the
// Firewall page. panelPort is the panel's own listen port.
func DefaultPanelPorts(panelPort string) []FirewallPort {
	out := []FirewallPort{{"22", "tcp", "SSH"}}
	if validPort.MatchString(panelPort) {
		out = append(out, FirewallPort{panelPort, "tcp", "RePanel"})
	}
	out = append(out,
		FirewallPort{"80", "tcp", "HTTP"},
		FirewallPort{"443", "tcp", "HTTPS"},
		FirewallPort{"53", "tcp", "DNS"},
		FirewallPort{"53", "udp", "DNS"},
		FirewallPort{"25", "tcp", "SMTP"},
		FirewallPort{"587", "tcp", "Mail submission"},
		FirewallPort{"465", "tcp", "SMTPS"},
		FirewallPort{"143", "tcp", "IMAP"},
		FirewallPort{"993", "tcp", "IMAPS"},
		FirewallPort{"110", "tcp", "POP3"},
		FirewallPort{"995", "tcp", "POP3S"},
		FirewallPort{"21", "tcp", "FTP"},
		FirewallPort{FTPPassivePorts, "tcp", "FTP passive"},
	)
	return out
}

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

// ---- Node app loopback isolation -------------------------------------------
//
// Node apps listen on a loopback TCP port that any local process can reach, so
// without protection one tenant's code could connect straight to another
// tenant's app — bypassing nginx (and its TLS/WAF/access rules). We add a
// kernel firewall rule, via ufw's before.rules so it persists across reboots and
// ufw reloads, that lets only nginx (www-data) and the panel (root) open
// connections to the Node port range; every other local user is rejected. Apps
// keep using normal TCP, so this is transparent to tenant code.

const (
	beforeRules4 = "/etc/ufw/before.rules"
	beforeRules6 = "/etc/ufw/before6.rules"
	isoBegin     = "# BEGIN RePanel node-app isolation (managed — do not edit)"
	isoEnd       = "# END RePanel node-app isolation"
)

// ufwLoopbackOutRe matches ufw's "accept all loopback output" line; we insert
// our filter immediately before it (an earlier blanket loopback ACCEPT would
// otherwise short-circuit our rules). The chain name differs for IPv6.
func ufwLoopbackOutRe(chain string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^-A\s+` + regexp.QuoteMeta(chain) + `\s+-o\s+lo\s+-j\s+ACCEPT\s*$`)
}

// EnsureNodeAppIsolation installs (or refreshes) the loopback isolation rules in
// ufw's before.rules / before6.rules and reloads ufw. It is idempotent and a
// safe no-op when ufw is absent. Returns an error (without changing anything)
// when the kernel can't match on socket owner.
func EnsureNodeAppIsolation() error {
	if !Linux() {
		return nil
	}
	if !UFWAvailable() {
		return nil // no firewall to integrate with
	}
	if !ownerMatchSupported() {
		return fmt.Errorf("the iptables 'owner' match is unavailable on this kernel; cannot isolate Node app ports")
	}
	changed := false
	if c, err := applyIsolationToFile(beforeRules4, "ufw-before-output"); err != nil {
		return err
	} else if c {
		changed = true
	}
	// IPv6 (apps that bind ::1) — only when the panel/host has the file.
	if _, err := os.Stat(beforeRules6); err == nil {
		if c, err := applyIsolationToFile(beforeRules6, "ufw6-before-output"); err != nil {
			return err
		} else if c {
			changed = true
		}
	}
	if changed && UFWStatus() == "active" {
		if _, err := run("ufw", "reload"); err != nil {
			return fmt.Errorf("ufw reload after isolation rules: %w", err)
		}
	}
	return nil
}

// NodeAppIsolationActive reports whether the isolation block is present in the
// IPv4 before.rules (used to surface status in the UI).
func NodeAppIsolationActive() bool {
	data, err := os.ReadFile(beforeRules4)
	return err == nil && strings.Contains(string(data), isoBegin)
}

// applyIsolationToFile rewrites one ufw rules file to contain the current managed
// block, inserted right before the loopback-accept line. Returns whether the
// file changed. On a write/parse problem it restores the original.
func applyIsolationToFile(path, chain string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, nil // file absent — nothing to do for this family
	}
	updated, changed, err := insertIsolation(string(data), chain)
	if err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	if !changed {
		return false, nil // already current
	}
	_ = os.WriteFile(path+".repanel.bak", data, 0o640)
	if err := os.WriteFile(path, []byte(updated), 0o640); err != nil {
		return false, err
	}
	return true, nil
}

// insertIsolation returns the rules-file content with the managed block placed
// immediately before the loopback-accept line for the given chain, reporting
// whether it changed. It is idempotent.
func insertIsolation(content, chain string) (string, bool, error) {
	clean := stripManagedBlock(content)
	loc := ufwLoopbackOutRe(chain).FindStringIndex(clean)
	if loc == nil {
		return content, false, fmt.Errorf("could not find the '%s -o lo -j ACCEPT' rule; not modifying", chain)
	}
	updated := clean[:loc[0]] + isolationBlock(chain) + clean[loc[0]:]
	return updated, updated != content, nil
}

// isolationBlock renders the managed rule block for the given output chain.
func isolationBlock(chain string) string {
	www := "www-data"
	if u, err := user.Lookup("www-data"); err == nil {
		www = u.Uid
	}
	rng := fmt.Sprintf("%d:%d", NodeAppPortLow, NodeAppPortHigh)
	var b strings.Builder
	b.WriteString(isoBegin + "\n")
	fmt.Fprintf(&b, "-A %s -o lo -p tcp --dport %s -m owner --uid-owner %s -j ACCEPT\n", chain, rng, www)
	fmt.Fprintf(&b, "-A %s -o lo -p tcp --dport %s -m owner --uid-owner 0 -j ACCEPT\n", chain, rng)
	fmt.Fprintf(&b, "-A %s -o lo -p tcp --dport %s -j REJECT --reject-with tcp-reset\n", chain, rng)
	b.WriteString(isoEnd + "\n")
	return b.String()
}

// stripManagedBlock removes a previously-written managed block (idempotent).
func stripManagedBlock(content string) string {
	start := strings.Index(content, isoBegin)
	if start < 0 {
		return content
	}
	end := strings.Index(content, isoEnd)
	if end < 0 {
		return content
	}
	end += len(isoEnd)
	for end < len(content) && (content[end] == '\n' || content[end] == '\r') {
		end++
	}
	return content[:start] + content[end:]
}

// ownerMatchSupported probes whether the kernel's iptables 'owner' match works,
// by adding and removing a throwaway rule in a temporary chain.
func ownerMatchSupported() bool {
	if !have("iptables") {
		return false
	}
	const ch = "repanel-ownertest"
	run("iptables", "-N", ch) // ignore "chain exists"
	_, err := run("iptables", "-A", ch, "-m", "owner", "--uid-owner", "0", "-j", "RETURN")
	run("iptables", "-F", ch)
	run("iptables", "-X", ch)
	return err == nil
}
