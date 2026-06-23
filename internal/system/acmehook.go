package system

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// ACME DNS-01 support for wildcard certificates. certbot runs in manual mode and
// invokes the panel binary as its auth/cleanup hook (see IssueLetsEncryptDNS); the
// hook adds or removes the _acme-challenge TXT record in the domain's
// RePanel-managed BIND zone file, then reloads BIND. Because certbot persists the
// hook commands in the renewal config, `certbot renew` reuses them automatically.
//
// Automated DNS-01 only works for domains whose DNS zone RePanel hosts (a zone
// file exists); the issuance handler enforces that before calling certbot.

// validACMEDomain guards the domain taken from the CERTBOT_DOMAIN environment so
// it cannot escape the zones directory.
var validACMEDomain = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]{0,251}[a-z0-9])?$`)

// acmeZoneFile returns the managed zone file path for a domain, or "" if the
// domain is malformed.
func acmeZoneFile(bindDir, domain string) string {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if !validACMEDomain.MatchString(domain) || strings.Contains(domain, "..") {
		return ""
	}
	return filepath.Join(bindDir, "repanel-zones", "db."+domain)
}

// ACMEHookAuth adds an _acme-challenge TXT record carrying validation to the
// domain's zone and reloads BIND. Multiple values (wildcard + apex) accumulate as
// separate TXT records, which is exactly what the ACME server expects.
func ACMEHookAuth(bindDir, domain, validation string) error {
	zoneFile := acmeZoneFile(bindDir, domain)
	if zoneFile == "" {
		return fmt.Errorf("invalid ACME domain %q", domain)
	}
	data, err := os.ReadFile(zoneFile)
	if err != nil {
		return fmt.Errorf("zone for %s is not managed here: %w", domain, err)
	}
	// Defensive: the validation token is base64url, but never let a stray quote
	// reach the zone file.
	val := strings.NewReplacer(`"`, "", "\n", "", "\r", "").Replace(validation)
	content := bumpZoneSerial(string(data)) +
		fmt.Sprintf("_acme-challenge 60 IN TXT \"%s\"\n", val)
	if err := os.WriteFile(zoneFile, []byte(content), 0o644); err != nil {
		return err
	}
	return reloadBind()
}

// ACMEHookCleanup removes the _acme-challenge TXT record(s) carrying validation
// (or all of them when validation is empty) and reloads BIND.
func ACMEHookCleanup(bindDir, domain, validation string) error {
	zoneFile := acmeZoneFile(bindDir, domain)
	if zoneFile == "" {
		return fmt.Errorf("invalid ACME domain %q", domain)
	}
	data, err := os.ReadFile(zoneFile)
	if err != nil {
		return nil // nothing to clean up
	}
	val := strings.NewReplacer(`"`, "", "\n", "", "\r", "").Replace(validation)
	var kept []string
	for _, line := range strings.Split(string(data), "\n") {
		isChallenge := strings.HasPrefix(strings.TrimSpace(line), "_acme-challenge")
		if isChallenge && (val == "" || strings.Contains(line, val)) {
			continue
		}
		kept = append(kept, line)
	}
	content := bumpZoneSerial(strings.Join(kept, "\n"))
	if err := os.WriteFile(zoneFile, []byte(content), 0o644); err != nil {
		return err
	}
	return reloadBind()
}

// soaSerialRe matches the serial — the first number inside the SOA parentheses.
var soaSerialRe = regexp.MustCompile(`(\(\s*)(\d+)`)

// bumpZoneSerial increments the SOA serial so secondaries pick up the change. If
// no serial is found the content is returned unchanged (a primary reloads from
// file regardless, so local ACME validation still works).
func bumpZoneSerial(content string) string {
	return soaSerialRe.ReplaceAllStringFunc(content, func(m string) string {
		g := soaSerialRe.FindStringSubmatch(m)
		n, err := strconv.ParseInt(g[2], 10, 64)
		if err != nil {
			return m
		}
		return g[1] + strconv.FormatInt(n+1, 10)
	})
}
