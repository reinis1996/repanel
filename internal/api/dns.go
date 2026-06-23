package api

import (
	"net/http"
	"strings"

	"github.com/reinis1996/repanel/internal/models"
	"github.com/reinis1996/repanel/internal/system"
)

var allowedRecordTypes = map[string]bool{
	"A": true, "AAAA": true, "CNAME": true, "MX": true, "TXT": true,
	"NS": true, "SRV": true, "CAA": true,
}

func (s *Server) handleZoneList(w http.ResponseWriter, r *http.Request, u *models.User) {
	where, args := scopeWhere(u, "d.user_id")
	rows, err := s.DB.Query(`SELECT z.id, z.domain_id, z.name, z.serial, z.dnssec, z.created_at, z.cf_sync,
		(SELECT COUNT(*) FROM dns_records r WHERE r.zone_id = z.id) AS record_count
		FROM dns_zones z JOIN domains d ON d.id = z.domain_id WHERE `+where+` ORDER BY z.name`, args...)
	if err != nil {
		s.fail(w, "list zones", err)
		return
	}
	defer rows.Close()
	out := []models.DNSZone{}
	for rows.Next() {
		var z models.DNSZone
		var dnssec int
		if err := rows.Scan(&z.ID, &z.DomainID, &z.Name, &z.Serial, &dnssec, &z.CreatedAt, &z.CFSync, &z.RecordCount); err == nil {
			z.DNSSEC = dnssec != 0
			out = append(out, z)
		}
	}
	s.json(w, out)
}

// getZoneScoped loads a zone when the user owns the parent domain.
func (s *Server) getZoneScoped(u *models.User, zoneID int64) (*models.DNSZone, error) {
	where, args := scopeWhere(u, "d.user_id")
	args = append([]any{zoneID}, args...)
	var z models.DNSZone
	var cfToken string
	err := s.DB.QueryRow(`SELECT z.id, z.domain_id, z.name, z.serial, z.created_at, z.cf_zone_id, z.cf_sync, z.cf_token
		FROM dns_zones z JOIN domains d ON d.id = z.domain_id
		WHERE z.id = ? AND `+where, args...).
		Scan(&z.ID, &z.DomainID, &z.Name, &z.Serial, &z.CreatedAt, &z.CFZoneID, &z.CFSync, &cfToken)
	if err != nil {
		return nil, err
	}
	z.HasCFToken = cfToken != ""
	return &z, nil
}

func (s *Server) loadZoneRecords(z *models.DNSZone) error {
	rows, err := s.DB.Query(`SELECT id, zone_id, name, type, value, ttl, priority, proxied
		FROM dns_records WHERE zone_id = ? ORDER BY type, name`, z.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	z.Records = []models.DNSRecord{}
	for rows.Next() {
		var rec models.DNSRecord
		var proxied int
		if err := rows.Scan(&rec.ID, &rec.ZoneID, &rec.Name, &rec.Type, &rec.Value, &rec.TTL, &rec.Priority, &proxied); err == nil {
			rec.Proxied = proxied != 0
			z.Records = append(z.Records, rec)
		}
	}
	return nil
}

func (s *Server) handleZoneGet(w http.ResponseWriter, r *http.Request, u *models.User) {
	z, err := s.getZoneScoped(u, pathID(r, "id"))
	if err != nil {
		s.err(w, http.StatusNotFound, "zone not found")
		return
	}
	if err := s.loadZoneRecords(z); err != nil {
		s.fail(w, "load records", err)
		return
	}
	s.json(w, z)
}

// writeZoneFile re-renders the BIND zone file from db state and bumps serial.
func (s *Server) writeZoneFile(zoneID int64) error {
	var z models.DNSZone
	err := s.DB.QueryRow(`SELECT id, domain_id, name, serial, created_at FROM dns_zones WHERE id = ?`, zoneID).
		Scan(&z.ID, &z.DomainID, &z.Name, &z.Serial, &z.CreatedAt)
	if err != nil {
		return err
	}
	if err := s.loadZoneRecords(&z); err != nil {
		return err
	}
	s.DB.Exec(`UPDATE dns_zones SET serial = serial + 1 WHERE id = ?`, zoneID)
	ns1 := fqdn(s.DB.Setting("ns1"))
	ns2 := fqdn(s.DB.Setting("ns2"))
	adminMail := s.DB.Setting("admin_email")
	if adminMail != "" {
		adminMail = strings.Replace(adminMail, "@", ".", 1) + "."
	}
	slaveIPs := system.ParseSlaveIPs(s.DB.Setting("slave_dns"))
	if err := system.WriteZone(s.Cfg.BindDir, z, ns1, ns2, adminMail, slaveIPs); err != nil {
		return err
	}
	// Mirror the change to Cloudflare when this zone is in push mode (async).
	s.maybePushCloudflare(zoneID)
	return nil
}

// fqdn returns a nameserver hostname with a trailing dot, or "" if unset.
func fqdn(host string) string {
	if host == "" || strings.HasSuffix(host, ".") {
		return host
	}
	return host + "."
}

type recordRequest struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Value    string `json:"value"`
	TTL      int    `json:"ttl"`
	Priority int    `json:"priority"`
	Proxied  bool   `json:"proxied"`
}

func (req *recordRequest) validate() string {
	req.Type = strings.ToUpper(strings.TrimSpace(req.Type))
	req.Value = strings.TrimSpace(req.Value)
	req.Name = strings.TrimSpace(req.Name)
	if !allowedRecordTypes[req.Type] {
		return "unsupported record type"
	}
	if req.Value == "" {
		return "record value is required"
	}
	if strings.ContainsAny(req.Value, "\n\r") || strings.ContainsAny(req.Name, "\n\r \t") {
		return "invalid characters in record"
	}
	if req.TTL <= 0 {
		req.TTL = 3600
	}
	return ""
}

func (s *Server) handleRecordCreate(w http.ResponseWriter, r *http.Request, u *models.User) {
	z, err := s.getZoneScoped(u, pathID(r, "id"))
	if err != nil {
		s.err(w, http.StatusNotFound, "zone not found")
		return
	}
	req, err := decode[recordRequest](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if msg := req.validate(); msg != "" {
		s.err(w, http.StatusBadRequest, msg)
		return
	}
	res, err := s.DB.Exec(`INSERT INTO dns_records(zone_id,name,type,value,ttl,priority,proxied) VALUES(?,?,?,?,?,?,?)`,
		z.ID, req.Name, req.Type, req.Value, req.TTL, req.Priority, boolInt(req.Proxied))
	if err != nil {
		s.fail(w, "insert record", err)
		return
	}
	if err := s.writeZoneFile(z.ID); err != nil {
		s.fail(w, "write zone", err)
		return
	}
	id, _ := res.LastInsertId()
	s.json(w, models.DNSRecord{ID: id, ZoneID: z.ID, Name: req.Name, Type: req.Type,
		Value: req.Value, TTL: req.TTL, Priority: req.Priority, Proxied: req.Proxied})
}

// zoneIDForRecord finds the parent zone if the user may manage the record.
func (s *Server) zoneIDForRecord(u *models.User, recordID int64) (int64, bool) {
	where, args := scopeWhere(u, "d.user_id")
	args = append([]any{recordID}, args...)
	var zoneID int64
	err := s.DB.QueryRow(`SELECT z.id FROM dns_records rec
		JOIN dns_zones z ON z.id = rec.zone_id
		JOIN domains d ON d.id = z.domain_id
		WHERE rec.id = ? AND `+where, args...).Scan(&zoneID)
	return zoneID, err == nil
}

func (s *Server) handleRecordUpdate(w http.ResponseWriter, r *http.Request, u *models.User) {
	recordID := pathID(r, "rid")
	zoneID, ok := s.zoneIDForRecord(u, recordID)
	if !ok {
		s.err(w, http.StatusNotFound, "record not found")
		return
	}
	req, err := decode[recordRequest](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if msg := req.validate(); msg != "" {
		s.err(w, http.StatusBadRequest, msg)
		return
	}
	if _, err := s.DB.Exec(`UPDATE dns_records SET name=?, type=?, value=?, ttl=?, priority=?, proxied=? WHERE id=?`,
		req.Name, req.Type, req.Value, req.TTL, req.Priority, boolInt(req.Proxied), recordID); err != nil {
		s.fail(w, "update record", err)
		return
	}
	if err := s.writeZoneFile(zoneID); err != nil {
		s.fail(w, "write zone", err)
		return
	}
	s.json(w, map[string]bool{"ok": true})
}

func (s *Server) handleRecordDelete(w http.ResponseWriter, r *http.Request, u *models.User) {
	recordID := pathID(r, "rid")
	zoneID, ok := s.zoneIDForRecord(u, recordID)
	if !ok {
		s.err(w, http.StatusNotFound, "record not found")
		return
	}
	if _, err := s.DB.Exec(`DELETE FROM dns_records WHERE id = ?`, recordID); err != nil {
		s.fail(w, "delete record", err)
		return
	}
	if err := s.writeZoneFile(zoneID); err != nil {
		s.fail(w, "write zone", err)
		return
	}
	s.json(w, map[string]bool{"ok": true})
}
