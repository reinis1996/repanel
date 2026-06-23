package api

import (
	"log"
	"net/http"
	"strings"

	"github.com/reinis1996/repanel/internal/models"
	"github.com/reinis1996/repanel/internal/system"
)

// Cloudflare DNS sync: bind a RePanel zone to a Cloudflare zone (API token + CF
// zone id), import/export records on demand, and keep them in sync automatically
// — "push" pushes RePanel's records to Cloudflare on every change and on the
// hourly reconcile; "pull" replaces RePanel's records from Cloudflare hourly.
// Only the common record types are synced (A, AAAA, CNAME, MX, TXT); NS/SOA are
// nameserver-managed and SRV/CAA need Cloudflare's structured form, so they are
// left untouched on both sides.

var cfManagedTypes = map[string]bool{"A": true, "AAAA": true, "CNAME": true, "MX": true, "TXT": true}

const cfManagedTypesSQL = `'A','AAAA','CNAME','MX','TXT'`

// ---- record mapping ---------------------------------------------------------

func cfFQDN(name, zone string) string {
	name = strings.TrimSpace(strings.TrimSuffix(name, "."))
	if name == "" || name == "@" {
		return zone
	}
	if name == zone || strings.HasSuffix(name, "."+zone) {
		return name
	}
	return name + "." + zone
}

func cfLabel(fqdn, zone string) string {
	fqdn = strings.TrimSpace(strings.TrimSuffix(fqdn, "."))
	if strings.EqualFold(fqdn, zone) {
		return "@"
	}
	if strings.HasSuffix(fqdn, "."+zone) {
		return strings.TrimSuffix(fqdn, "."+zone)
	}
	return fqdn
}

func ensureDot(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || strings.HasSuffix(s, ".") {
		return s
	}
	return s + "."
}

// repToCF maps a RePanel record to a Cloudflare record.
func repToCF(rec models.DNSRecord, zone string) system.CFRecord {
	cf := system.CFRecord{Type: rec.Type, Name: cfFQDN(rec.Name, zone), Content: rec.Value, TTL: cfTTL(rec.TTL)}
	switch rec.Type {
	case "MX":
		p := rec.Priority
		cf.Priority = &p
		cf.Content = strings.TrimSuffix(rec.Value, ".")
	case "CNAME":
		cf.Content = strings.TrimSuffix(rec.Value, ".")
	}
	if rec.Type == "A" || rec.Type == "AAAA" || rec.Type == "CNAME" {
		cf.Proxied = rec.Proxied
	}
	if cf.Proxied {
		cf.TTL = 1 // Cloudflare requires automatic TTL for proxied records
	}
	return cf
}

// cfToRep maps a Cloudflare record to a RePanel record, reporting whether it is
// a type the panel syncs.
func cfToRep(c system.CFRecord, zone string) (models.DNSRecord, bool) {
	if !cfManagedTypes[c.Type] {
		return models.DNSRecord{}, false
	}
	rec := models.DNSRecord{Name: cfLabel(c.Name, zone), Type: c.Type, Value: c.Content, TTL: c.TTL, Proxied: c.Proxied}
	if rec.TTL <= 1 {
		rec.TTL = 3600 // Cloudflare "automatic" → a concrete TTL for our zone file
	}
	switch c.Type {
	case "MX":
		if c.Priority != nil {
			rec.Priority = *c.Priority
		}
		rec.Value = ensureDot(c.Content)
	case "CNAME":
		rec.Value = ensureDot(c.Content)
	}
	return rec, true
}

func cfTTL(ttl int) int {
	switch {
	case ttl <= 0:
		return 1 // automatic
	case ttl < 60:
		return 60
	default:
		return ttl
	}
}

func cfKey(typ, name, content string) string {
	return strings.ToLower(typ + "|" + strings.TrimSuffix(name, ".") + "|" + strings.TrimSuffix(content, "."))
}

func samePrio(a *int, b *int) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// ---- binding helpers --------------------------------------------------------

func (s *Server) cfBinding(zoneID int64) (cfZoneID, token, sync string) {
	s.DB.QueryRow(`SELECT cf_zone_id, cf_token, cf_sync FROM dns_zones WHERE id = ?`, zoneID).
		Scan(&cfZoneID, &token, &sync)
	return
}

// ---- reconcile --------------------------------------------------------------

type cfSyncResult struct {
	Created int `json:"created"`
	Updated int `json:"updated"`
	Deleted int `json:"deleted"`
}

// cfExport pushes the zone's records to Cloudflare (create/update). When prune is
// set, Cloudflare records of a managed type not present in RePanel are deleted,
// so Cloudflare mirrors RePanel exactly.
func (s *Server) cfExport(z *models.DNSZone, prune bool) (cfSyncResult, error) {
	var res cfSyncResult
	cfZoneID, token, _ := s.cfBinding(z.ID)
	if cfZoneID == "" || token == "" {
		return res, errCFNotConfigured
	}
	client := system.CFClient{Token: token}
	cfRecs, err := client.CFListRecords(cfZoneID)
	if err != nil {
		return res, err
	}
	if err := s.loadZoneRecords(z); err != nil {
		return res, err
	}
	exact := map[string]system.CFRecord{}
	for _, c := range cfRecs {
		exact[cfKey(c.Type, c.Name, c.Content)] = c
	}
	seen := map[string]bool{}
	for _, rec := range z.Records {
		if !cfManagedTypes[rec.Type] {
			continue
		}
		d := repToCF(rec, z.Name)
		if cur, ok := exact[cfKey(d.Type, d.Name, d.Content)]; ok {
			seen[cur.ID] = true
			if cur.TTL != d.TTL || !samePrio(cur.Priority, d.Priority) || cur.Proxied != d.Proxied {
				if err := client.CFUpdateRecord(cfZoneID, cur.ID, d); err != nil {
					return res, err
				}
				res.Updated++
			}
		} else {
			if err := client.CFCreateRecord(cfZoneID, d); err != nil {
				return res, err
			}
			res.Created++
		}
	}
	if prune {
		for _, c := range cfRecs {
			if cfManagedTypes[c.Type] && !seen[c.ID] {
				if err := client.CFDeleteRecord(cfZoneID, c.ID); err != nil {
					return res, err
				}
				res.Deleted++
			}
		}
	}
	return res, nil
}

// cfImport replaces the zone's managed records with Cloudflare's, then rewrites
// the zone file.
func (s *Server) cfImport(z *models.DNSZone) (int, error) {
	cfZoneID, token, _ := s.cfBinding(z.ID)
	if cfZoneID == "" || token == "" {
		return 0, errCFNotConfigured
	}
	client := system.CFClient{Token: token}
	cfRecs, err := client.CFListRecords(cfZoneID)
	if err != nil {
		return 0, err
	}
	mapped := []models.DNSRecord{}
	for _, c := range cfRecs {
		if rec, ok := cfToRep(c, z.Name); ok {
			mapped = append(mapped, rec)
		}
	}
	if _, err := s.DB.Exec(`DELETE FROM dns_records WHERE zone_id = ? AND type IN (`+cfManagedTypesSQL+`)`, z.ID); err != nil {
		return 0, err
	}
	for _, rec := range mapped {
		s.DB.Exec(`INSERT INTO dns_records(zone_id,name,type,value,ttl,priority,proxied) VALUES(?,?,?,?,?,?,?)`,
			z.ID, rec.Name, rec.Type, rec.Value, rec.TTL, rec.Priority, boolInt(rec.Proxied))
	}
	return len(mapped), s.writeZoneFile(z.ID)
}

var errCFNotConfigured = &cfErr{"Cloudflare is not configured for this zone"}

type cfErr struct{ msg string }

func (e *cfErr) Error() string { return e.msg }

// ---- automatic sync ---------------------------------------------------------

// maybePushCloudflare, called after a zone file is (re)written, pushes the zone
// to Cloudflare in the background when it is in "push" mode.
func (s *Server) maybePushCloudflare(zoneID int64) {
	var name, cfZoneID, token, sync string
	s.DB.QueryRow(`SELECT name, cf_zone_id, cf_token, cf_sync FROM dns_zones WHERE id = ?`, zoneID).
		Scan(&name, &cfZoneID, &token, &sync)
	if sync != "push" || cfZoneID == "" || token == "" {
		return
	}
	go func() {
		if _, err := s.cfExport(&models.DNSZone{ID: zoneID, Name: name}, true); err != nil {
			log.Printf("cloudflare push %s: %v", name, err)
		}
	}()
}

// SyncCloudflare reconciles every bound zone; called from the hourly loop. Push
// zones are exported (with prune); pull zones are imported.
func (s *Server) SyncCloudflare() {
	type zj struct {
		id         int64
		name, sync string
	}
	var list []zj
	rows, err := s.DB.Query(`SELECT id, name, cf_sync FROM dns_zones
		WHERE cf_sync IN ('push','pull') AND cf_zone_id != '' AND cf_token != ''`)
	if err != nil {
		return
	}
	for rows.Next() {
		var z zj
		if rows.Scan(&z.id, &z.name, &z.sync) == nil {
			list = append(list, z)
		}
	}
	rows.Close()
	for _, z := range list {
		zone := &models.DNSZone{ID: z.id, Name: z.name}
		var err error
		if z.sync == "push" {
			_, err = s.cfExport(zone, true)
		} else {
			_, err = s.cfImport(zone)
		}
		if err != nil {
			log.Printf("cloudflare sync %s (%s): %v", z.name, z.sync, err)
		}
	}
}

// ---- handlers ---------------------------------------------------------------

func (s *Server) handleCloudflareSet(w http.ResponseWriter, r *http.Request, u *models.User) {
	z, err := s.getZoneScoped(u, pathID(r, "id"))
	if err != nil {
		s.err(w, http.StatusNotFound, "zone not found")
		return
	}
	req, err := decode[struct {
		ZoneID string `json:"cf_zone_id"`
		Token  string `json:"token"` // blank keeps the stored token
		Sync   string `json:"cf_sync"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	cfZoneID := strings.TrimSpace(req.ZoneID)
	sync := req.Sync
	if sync != "off" && sync != "push" && sync != "pull" {
		sync = "off"
	}
	// Keep the existing token when none is supplied (so changing the mode alone
	// doesn't wipe credentials).
	token := strings.TrimSpace(req.Token)
	if token == "" {
		_, token, _ = s.cfBinding(z.ID)
	}
	if cfZoneID != "" && token != "" {
		if err := (system.CFClient{Token: token}).CFVerify(cfZoneID); err != nil {
			s.err(w, http.StatusBadRequest, err.Error())
			return
		}
	} else if sync != "off" {
		s.err(w, http.StatusBadRequest, "a Cloudflare API token and zone id are required to enable sync")
		return
	}
	if _, err := s.DB.Exec(`UPDATE dns_zones SET cf_zone_id = ?, cf_token = ?, cf_sync = ? WHERE id = ?`,
		cfZoneID, token, sync, z.ID); err != nil {
		s.fail(w, "save cloudflare binding", err)
		return
	}
	s.json(w, map[string]any{"ok": true, "cf_zone_id": cfZoneID, "cf_sync": sync, "has_cf_token": token != ""})
}

func (s *Server) handleCloudflareUnbind(w http.ResponseWriter, r *http.Request, u *models.User) {
	z, err := s.getZoneScoped(u, pathID(r, "id"))
	if err != nil {
		s.err(w, http.StatusNotFound, "zone not found")
		return
	}
	s.DB.Exec(`UPDATE dns_zones SET cf_zone_id = '', cf_token = '', cf_sync = 'off' WHERE id = ?`, z.ID)
	s.json(w, map[string]bool{"ok": true})
}

func (s *Server) handleCloudflareImport(w http.ResponseWriter, r *http.Request, u *models.User) {
	z, err := s.getZoneScoped(u, pathID(r, "id"))
	if err != nil {
		s.err(w, http.StatusNotFound, "zone not found")
		return
	}
	n, err := s.cfImport(z)
	if err != nil {
		s.err(w, http.StatusBadGateway, err.Error())
		return
	}
	s.json(w, map[string]any{"ok": true, "imported": n})
}

func (s *Server) handleCloudflareExport(w http.ResponseWriter, r *http.Request, u *models.User) {
	z, err := s.getZoneScoped(u, pathID(r, "id"))
	if err != nil {
		s.err(w, http.StatusNotFound, "zone not found")
		return
	}
	req, _ := decode[struct {
		Prune bool `json:"prune"`
	}](r)
	res, err := s.cfExport(z, req.Prune)
	if err != nil {
		s.err(w, http.StatusBadGateway, err.Error())
		return
	}
	s.json(w, map[string]any{"ok": true, "result": res})
}
