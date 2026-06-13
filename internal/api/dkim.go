package api

import (
	"database/sql"
	"net/http"

	"github.com/reinis1996/repanel/internal/models"
	"github.com/reinis1996/repanel/internal/system"
)

const spfDefault = "v=spf1 a mx ~all"

func dmarcValue(domain string) string {
	return "v=DMARC1; p=none; rua=mailto:postmaster@" + domain
}

// handleDKIMList reports DKIM/DMARC status for every domain the caller manages.
func (s *Server) handleDKIMList(w http.ResponseWriter, r *http.Request, u *models.User) {
	where, args := scopeWhere(u, "d.user_id")
	rows, err := s.DB.Query(`SELECT d.id, d.name, k.selector, k.public_txt,
		(SELECT z.id FROM dns_zones z WHERE z.domain_id = d.id)
		FROM domains d LEFT JOIN dkim_keys k ON k.domain_id = d.id
		WHERE `+where+` ORDER BY d.name`, args...)
	if err != nil {
		s.fail(w, "list dkim", err)
		return
	}
	defer rows.Close()
	out := []models.DKIMStatus{}
	for rows.Next() {
		var id int64
		var name string
		var selector, publicTXT sql.NullString
		var zoneID sql.NullInt64
		if rows.Scan(&id, &name, &selector, &publicTXT, &zoneID) != nil {
			continue
		}
		sel := selector.String
		if sel == "" {
			sel = system.DKIMSelector
		}
		out = append(out, models.DKIMStatus{
			DomainID:   id,
			Domain:     name,
			Enabled:    publicTXT.Valid && publicTXT.String != "",
			Selector:   sel,
			DNSManaged: zoneID.Valid,
			DKIMName:   sel + "._domainkey",
			DKIMValue:  publicTXT.String,
			DMARCName:  "_dmarc",
			DMARCValue: dmarcValue(name),
			SPFSuggest: spfDefault,
		})
	}
	s.json(w, out)
}

// handleDKIMEnable generates a signing key for a domain (if absent), wires it
// into OpenDKIM, and publishes the DKIM/DMARC/SPF records when the domain's DNS
// zone is hosted here.
func (s *Server) handleDKIMEnable(w http.ResponseWriter, r *http.Request, u *models.User) {
	d, err := s.getDomainScoped(u, pathID(r, "id"))
	if err != nil {
		s.err(w, http.StatusNotFound, err.Error())
		return
	}

	var publicTXT, selector string
	err = s.DB.QueryRow(`SELECT public_txt, selector FROM dkim_keys WHERE domain_id = ?`, d.ID).
		Scan(&publicTXT, &selector)
	if err != nil { // no key yet — generate one
		key, gerr := system.GenerateDKIMKey()
		if gerr != nil {
			s.fail(w, "generate dkim key", gerr)
			return
		}
		if _, ierr := s.DB.Exec(`INSERT INTO dkim_keys(domain_id, selector, private_pem, public_txt)
			VALUES(?,?,?,?)`, d.ID, key.Selector, key.PrivatePEM, key.PublicTXT); ierr != nil {
			s.fail(w, "store dkim key", ierr)
			return
		}
		publicTXT, selector = key.PublicTXT, key.Selector
	}

	if err := s.rebuildDKIM(); err != nil {
		s.fail(w, "rebuild dkim", err)
		return
	}

	dnsManaged := false
	if zoneID, ok := s.zoneIDForDomain(d.ID); ok {
		dnsManaged = true
		s.upsertZoneRecord(zoneID, selector+"._domainkey", "TXT", publicTXT)
		s.ensureZoneRecord(zoneID, "_dmarc", "TXT", dmarcValue(d.Name))
		s.ensureSPF(zoneID)
		if err := s.writeZoneFile(zoneID); err != nil {
			s.fail(w, "write zone", err)
			return
		}
	}

	s.json(w, models.DKIMStatus{
		DomainID: d.ID, Domain: d.Name, Enabled: true, Selector: selector,
		DNSManaged: dnsManaged, DKIMName: selector + "._domainkey", DKIMValue: publicTXT,
		DMARCName: "_dmarc", DMARCValue: dmarcValue(d.Name), SPFSuggest: spfDefault,
	})
}

// handleDKIMDisable removes a domain's signing key and its published DKIM
// record. DMARC/SPF records are left in place (they are harmless and may have
// pre-existed).
func (s *Server) handleDKIMDisable(w http.ResponseWriter, r *http.Request, u *models.User) {
	d, err := s.getDomainScoped(u, pathID(r, "id"))
	if err != nil {
		s.err(w, http.StatusNotFound, err.Error())
		return
	}
	var selector string
	s.DB.QueryRow(`SELECT selector FROM dkim_keys WHERE domain_id = ?`, d.ID).Scan(&selector)
	if selector == "" {
		selector = system.DKIMSelector
	}
	if _, err := s.DB.Exec(`DELETE FROM dkim_keys WHERE domain_id = ?`, d.ID); err != nil {
		s.fail(w, "delete dkim key", err)
		return
	}
	if err := s.rebuildDKIM(); err != nil {
		s.fail(w, "rebuild dkim", err)
		return
	}
	if zoneID, ok := s.zoneIDForDomain(d.ID); ok {
		s.DB.Exec(`DELETE FROM dns_records WHERE zone_id = ? AND name = ? AND type = 'TXT'`,
			zoneID, selector+"._domainkey")
		if err := s.writeZoneFile(zoneID); err != nil {
			s.fail(w, "write zone", err)
			return
		}
	}
	s.json(w, map[string]bool{"ok": true})
}

// rebuildDKIM regenerates OpenDKIM's tables from every enabled domain.
func (s *Server) rebuildDKIM() error {
	rows, err := s.DB.Query(`SELECT d.name, k.selector, k.private_pem
		FROM dkim_keys k JOIN domains d ON d.id = k.domain_id`)
	if err != nil {
		return err
	}
	var domains []system.DKIMDomain
	for rows.Next() {
		var dk system.DKIMDomain
		if rows.Scan(&dk.Domain, &dk.Selector, &dk.PrivatePEM) == nil {
			domains = append(domains, dk)
		}
	}
	rows.Close()
	return system.RebuildDKIM(domains)
}

func (s *Server) zoneIDForDomain(domainID int64) (int64, bool) {
	var id int64
	err := s.DB.QueryRow(`SELECT id FROM dns_zones WHERE domain_id = ?`, domainID).Scan(&id)
	return id, err == nil
}

// upsertZoneRecord replaces any record with the same name+type, then inserts.
func (s *Server) upsertZoneRecord(zoneID int64, name, rtype, value string) {
	s.DB.Exec(`DELETE FROM dns_records WHERE zone_id = ? AND name = ? AND type = ?`, zoneID, name, rtype)
	s.DB.Exec(`INSERT INTO dns_records(zone_id,name,type,value,ttl,priority) VALUES(?,?,?,?,3600,0)`,
		zoneID, name, rtype, value)
}

// ensureZoneRecord inserts a record only when no record with that name+type
// already exists, so user-set values are never clobbered.
func (s *Server) ensureZoneRecord(zoneID int64, name, rtype, value string) {
	var n int
	s.DB.QueryRow(`SELECT COUNT(*) FROM dns_records WHERE zone_id = ? AND name = ? AND type = ?`,
		zoneID, name, rtype).Scan(&n)
	if n == 0 {
		s.DB.Exec(`INSERT INTO dns_records(zone_id,name,type,value,ttl,priority) VALUES(?,?,?,?,3600,0)`,
			zoneID, name, rtype, value)
	}
}

// ensureSPF adds a default SPF record at the apex when the zone has none.
func (s *Server) ensureSPF(zoneID int64) {
	var n int
	s.DB.QueryRow(`SELECT COUNT(*) FROM dns_records WHERE zone_id = ? AND type = 'TXT' AND value LIKE 'v=spf1%'`,
		zoneID).Scan(&n)
	if n == 0 {
		s.DB.Exec(`INSERT INTO dns_records(zone_id,name,type,value,ttl,priority) VALUES(?,'@','TXT',?,3600,0)`,
			zoneID, spfDefault)
	}
}
