package system

import (
	"fmt"
	"net"
	"os"
	"strings"
)

// System DNS resolver management. A mail/DNS server is sensitive to a flaky
// resolver (RBL, MX and DKIM lookups all stall), so the panel lets an operator
// pin the box's resolvers. dhcpcd regenerates /etc/resolv.conf on lease renewal,
// so we also write /etc/resolv.conf.head, which dhcpcd prepends — making the
// chosen resolvers stick across renewals and reboots.
const (
	resolvConf     = "/etc/resolv.conf"
	resolvConfHead = "/etc/resolv.conf.head"
)

// ParseResolverIPs extracts the valid IP addresses from a comma/space separated
// list, dropping anything that isn't an IP (so nothing unsafe reaches the file).
func ParseResolverIPs(list string) []string {
	var out []string
	for _, f := range strings.FieldsFunc(list, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	}) {
		if net.ParseIP(f) != nil {
			out = append(out, f)
		}
	}
	return out
}

// SetResolvers points the system at the given resolver IPs. An empty/invalid
// list is a no-op (keeps the DHCP-provided resolvers) rather than wiping DNS.
func SetResolvers(list string) error {
	ips := ParseResolverIPs(list)
	if !Linux() || len(ips) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteString("# Managed by RePanel — system DNS resolvers.\n")
	for _, ip := range ips {
		fmt.Fprintf(&b, "nameserver %s\n", ip)
	}
	// .head survives dhcpcd regeneration; resolv.conf applies it immediately.
	if err := os.WriteFile(resolvConfHead, []byte(b.String()), 0o644); err != nil {
		return err
	}
	return os.WriteFile(resolvConf, []byte(b.String()), 0o644)
}
