package system

import (
	"bufio"
	"os"
	"strings"
)

// SetSystemHostname sets the machine's static hostname to fqdn and maps it on the
// 127.0.1.1 line in /etc/hosts (the Debian convention), so `hostname -f` returns
// the FQDN. A no-op off Linux, when hostnamectl is absent, or for a non-FQDN
// value (a bare label would leave the FQDN incomplete, which is worse).
func SetSystemHostname(fqdn string) error {
	fqdn = strings.TrimSpace(fqdn)
	if !Linux() || !have("hostnamectl") || !strings.Contains(fqdn, ".") {
		return nil
	}
	if _, err := run("hostnamectl", "set-hostname", fqdn); err != nil {
		return err
	}
	return ensureHostsEntry(fqdn)
}

// ensureHostsEntry rewrites the 127.0.1.1 line in /etc/hosts to "127.0.1.1 <fqdn>
// <shortname>", or appends it when no such line exists. The loopback 127.0.0.1
// localhost line is left untouched.
func ensureHostsEntry(fqdn string) error {
	short := strings.SplitN(fqdn, ".", 2)[0]
	line := "127.0.1.1\t" + fqdn + " " + short
	data, _ := os.ReadFile("/etc/hosts")
	var out strings.Builder
	replaced := false
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	for sc.Scan() {
		if strings.HasPrefix(strings.TrimSpace(sc.Text()), "127.0.1.1") {
			out.WriteString(line + "\n")
			replaced = true
			continue
		}
		out.WriteString(sc.Text() + "\n")
	}
	if !replaced {
		out.WriteString(line + "\n")
	}
	return os.WriteFile("/etc/hosts", []byte(out.String()), 0o644)
}
