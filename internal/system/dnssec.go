package system

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DNSSEC signing for BIND zones via dnssec-policy (BIND 9.16+). Enabling a zone
// drops a marker file beside its zone file; rebuildNamedConf then attaches
// "dnssec-policy default; inline-signing yes; key-directory ..." to that zone's
// named.conf block, and BIND generates keys and maintains the signed zone inline.
// The panel reads the resulting DS/DNSKEY records so the operator can publish the
// DS at the registrar.

// dnssecKeyDir is the BIND-writable directory holding generated DNSSEC keys.
func dnssecKeyDir(bindDir string) string { return filepath.Join(bindDir, "repanel-keys") }

func dnssecMarker(bindDir, zoneName string) string {
	return filepath.Join(bindDir, "repanel-zones", "db."+zoneName+".dnssec")
}

// DNSSECAvailable reports whether the BIND DNSSEC tooling needed to read keys is
// present. Signing itself is done by named; we only need dnssec-dsfromkey to show
// the DS records.
func DNSSECAvailable() bool {
	return have("named-checkconf") && have("dnssec-dsfromkey")
}

// DNSSECEnabled reports whether a zone is currently flagged for signing.
func DNSSECEnabled(bindDir, zoneName string) bool {
	_, err := os.Stat(dnssecMarker(bindDir, zoneName))
	return err == nil
}

// EnableDNSSEC flags a zone for signing: ensures the key directory exists (owned
// by the bind user so named can write keys), drops the marker, and rebuilds +
// reloads named so the dnssec-policy takes effect.
func EnableDNSSEC(bindDir, zoneName string, slaveIPs []string) error {
	keyDir := dnssecKeyDir(bindDir)
	if err := os.MkdirAll(keyDir, 0o770); err != nil {
		return err
	}
	// named runs as the "bind" user on Debian/Ubuntu; let it write keys here.
	_, _ = run("chown", "bind:bind", keyDir)
	if err := os.WriteFile(dnssecMarker(bindDir, zoneName), []byte("on\n"), 0o644); err != nil {
		return err
	}
	if err := rebuildNamedConf(bindDir, slaveIPs); err != nil {
		return err
	}
	// If named rejects the dnssec-policy (e.g. a BIND too old to support it), the
	// regenerated named.conf would break every zone on the next reload. Roll the
	// marker back and restore the working config rather than leave DNS broken.
	if err := reloadBind(); err != nil {
		os.Remove(dnssecMarker(bindDir, zoneName))
		rebuildNamedConf(bindDir, slaveIPs)
		reloadBind()
		return err
	}
	return nil
}

// DisableDNSSEC removes the signing flag and rebuilds + reloads named. Generated
// keys are left in the key directory (harmless once the zone is unsigned).
func DisableDNSSEC(bindDir, zoneName string, slaveIPs []string) error {
	os.Remove(dnssecMarker(bindDir, zoneName))
	if err := rebuildNamedConf(bindDir, slaveIPs); err != nil {
		return err
	}
	return reloadBind()
}

// DNSSECRecords returns the DS and DNSKEY records for a signed zone by reading
// the keys BIND generated in the key directory. The DS records are what the
// operator submits to the domain's registrar. Returns empty slices until named
// has generated the keys (which can take a few seconds after enabling).
func DNSSECRecords(bindDir, zoneName string) (ds, dnskey []string) {
	keyDir := dnssecKeyDir(bindDir)
	entries, err := os.ReadDir(keyDir)
	if err != nil {
		return nil, nil
	}
	prefix := "K" + zoneName + "."
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), prefix) || !strings.HasSuffix(e.Name(), ".key") {
			continue
		}
		keyFile := filepath.Join(keyDir, e.Name())
		data, err := os.ReadFile(keyFile)
		if err != nil {
			continue
		}
		// Only key-signing keys (flag 257) yield a DS record; collect DNSKEY lines
		// for all, DS only for KSKs.
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, ";") {
				continue
			}
			dnskey = append(dnskey, collapseSpace(line))
			if strings.Contains(line, " DNSKEY 257 ") {
				if out, _, err := runCapture(10*time.Second, "", "", "dnssec-dsfromkey", "-2", keyFile); err == nil {
					for _, dl := range strings.Split(strings.TrimSpace(out), "\n") {
						if dl = strings.TrimSpace(dl); dl != "" {
							ds = append(ds, collapseSpace(dl))
						}
					}
				}
			}
		}
	}
	return ds, dnskey
}

func collapseSpace(s string) string { return strings.Join(strings.Fields(s), " ") }
