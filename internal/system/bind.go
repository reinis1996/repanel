package system

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/reinis1996/repanel/internal/models"
)

// DefaultZoneRecords returns the records a freshly created domain gets,
// equivalent to the default DNS template in Plesk/DirectAdmin.
func DefaultZoneRecords(domain, serverIP string) []models.DNSRecord {
	if serverIP == "" {
		serverIP = "127.0.0.1"
	}
	return []models.DNSRecord{
		{Name: "@", Type: "A", Value: serverIP, TTL: 3600},
		{Name: "www", Type: "CNAME", Value: domain + ".", TTL: 3600},
		{Name: "mail", Type: "A", Value: serverIP, TTL: 3600},
		{Name: "@", Type: "MX", Value: "mail." + domain + ".", TTL: 3600, Priority: 10},
		{Name: "@", Type: "TXT", Value: "v=spf1 a mx ~all", TTL: 3600},
	}
}

// WriteZone renders a BIND zone file and registers it in
// named.conf.repanel (included from named.conf.local by the installer).
func WriteZone(bindDir string, zone models.DNSZone, primaryNS, adminMail string) error {
	if primaryNS == "" {
		primaryNS = "ns1." + zone.Name + "."
	}
	if adminMail == "" {
		adminMail = "hostmaster." + zone.Name + "."
	}
	serial := time.Now().Format("2006010215") // YYYYMMDDHH + counter-free serial

	var sb strings.Builder
	fmt.Fprintf(&sb, "; Managed by RePanel — zone %s\n", zone.Name)
	fmt.Fprintf(&sb, "$TTL 3600\n$ORIGIN %s.\n", zone.Name)
	fmt.Fprintf(&sb, "@ IN SOA %s %s ( %s 3600 600 1209600 3600 )\n", primaryNS, adminMail, serial)
	fmt.Fprintf(&sb, "@ IN NS %s\n", primaryNS)
	for _, r := range zone.Records {
		name := r.Name
		if name == "" {
			name = "@"
		}
		ttl := r.TTL
		if ttl <= 0 {
			ttl = 3600
		}
		value := r.Value
		switch strings.ToUpper(r.Type) {
		case "MX", "SRV":
			value = fmt.Sprintf("%d %s", r.Priority, r.Value)
		case "TXT":
			value = formatTXT(value)
		}
		fmt.Fprintf(&sb, "%s %d IN %s %s\n", name, ttl, strings.ToUpper(r.Type), value)
	}

	zonesDir := filepath.Join(bindDir, "repanel-zones")
	if err := os.MkdirAll(zonesDir, 0o755); err != nil {
		return err
	}
	zoneFile := filepath.Join(zonesDir, "db."+zone.Name)
	if err := os.WriteFile(zoneFile, []byte(sb.String()), 0o644); err != nil {
		return err
	}
	if err := rebuildNamedConf(bindDir); err != nil {
		return err
	}
	return reloadBind()
}

// formatTXT renders a TXT record value for a BIND zone file. A value already
// wrapped in quotes is passed through; otherwise it is escaped and, when longer
// than the 255-byte per-string limit, split into multiple quoted chunks (as
// DKIM public keys require).
func formatTXT(v string) string {
	if strings.HasPrefix(v, `"`) {
		return v
	}
	v = strings.ReplaceAll(v, `"`, `\"`)
	if len(v) <= 255 {
		return `"` + v + `"`
	}
	var chunks []string
	for len(v) > 255 {
		chunks = append(chunks, `"`+v[:255]+`"`)
		v = v[255:]
	}
	if len(v) > 0 {
		chunks = append(chunks, `"`+v+`"`)
	}
	return "( " + strings.Join(chunks, " ") + " )"
}

// RemoveZone deletes a zone file and refreshes the include config.
func RemoveZone(bindDir, zoneName string) error {
	os.Remove(filepath.Join(bindDir, "repanel-zones", "db."+zoneName))
	if err := rebuildNamedConf(bindDir); err != nil {
		return err
	}
	return reloadBind()
}

// rebuildNamedConf regenerates named.conf.repanel from the zone files on disk.
func rebuildNamedConf(bindDir string) error {
	zonesDir := filepath.Join(bindDir, "repanel-zones")
	entries, err := os.ReadDir(zonesDir)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var sb strings.Builder
	sb.WriteString("// Managed by RePanel — included from named.conf.local\n")
	for _, e := range entries {
		name, ok := strings.CutPrefix(e.Name(), "db.")
		if !ok || e.IsDir() {
			continue
		}
		fmt.Fprintf(&sb, "zone \"%s\" { type master; file \"%s\"; };\n",
			name, filepath.Join(zonesDir, e.Name()))
	}
	return os.WriteFile(filepath.Join(bindDir, "named.conf.repanel"), []byte(sb.String()), 0o644)
}

func reloadBind() error {
	if !have("named-checkconf") {
		return nil
	}
	if _, err := run("named-checkconf"); err != nil {
		return fmt.Errorf("named-checkconf failed: %w", err)
	}
	return ReloadService("bind9")
}
