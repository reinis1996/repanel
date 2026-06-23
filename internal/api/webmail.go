package api

import (
	"net/http"

	"github.com/reinis1996/repanel/internal/models"
	"github.com/reinis1996/repanel/internal/system"
)

// handleWebmailList reports, for every domain the caller manages, whether
// webmail is enabled and whether Roundcube is installed on the server.
func (s *Server) handleWebmailList(w http.ResponseWriter, r *http.Request, u *models.User) {
	where, args := scopeWhere(u, "d.user_id")
	rows, err := s.DB.Query(`SELECT d.id, d.name, d.webmail,
		(SELECT z.id FROM dns_zones z WHERE z.domain_id = d.id)
		FROM domains d WHERE `+where+` ORDER BY d.name`, args...)
	if err != nil {
		s.fail(w, "list webmail", err)
		return
	}
	defer rows.Close()
	available := system.HaveWebmail()
	out := []models.WebmailStatus{}
	for rows.Next() {
		var id int64
		var name string
		var enabled int
		var zoneID *int64
		if rows.Scan(&id, &name, &enabled, &zoneID) != nil {
			continue
		}
		out = append(out, models.WebmailStatus{
			DomainID:   id,
			Domain:     name,
			Enabled:    enabled != 0,
			Available:  available,
			URL:        "http://webmail." + name,
			DNSManaged: zoneID != nil,
		})
	}
	s.json(w, out)
}

// handleWebmailEnable turns on webmail for a domain: it flags the domain,
// regenerates the shared Roundcube vhost, and publishes a webmail A record
// when the domain's DNS zone is hosted here.
func (s *Server) handleWebmailEnable(w http.ResponseWriter, r *http.Request, u *models.User) {
	d, err := s.getDomainScoped(u, pathID(r, "id"))
	if err != nil {
		s.err(w, http.StatusNotFound, err.Error())
		return
	}
	if !system.HaveWebmail() {
		s.err(w, http.StatusBadRequest, "webmail (Roundcube) is not installed on this server")
		return
	}
	if _, err := s.DB.Exec(`UPDATE domains SET webmail = 1 WHERE id = ?`, d.ID); err != nil {
		s.fail(w, "enable webmail", err)
		return
	}
	if err := s.rebuildWebmail(); err != nil {
		s.fail(w, "rebuild webmail vhost", err)
		return
	}

	dnsManaged := false
	if zoneID, ok := s.zoneIDForDomain(d.ID); ok {
		dnsManaged = true
		if ip := s.DB.Setting("server_ip"); ip != "" {
			s.ensureZoneRecord(zoneID, "webmail", "A", ip)
			if err := s.writeZoneFile(zoneID); err != nil {
				s.fail(w, "write zone", err)
				return
			}
		}
	}

	s.json(w, models.WebmailStatus{
		DomainID: d.ID, Domain: d.Name, Enabled: true, Available: true,
		URL: "http://webmail." + d.Name, DNSManaged: dnsManaged,
	})
}

// handleWebmailDisable turns webmail off for a domain and removes its webmail A
// record. The shared Roundcube install is left in place for other domains.
func (s *Server) handleWebmailDisable(w http.ResponseWriter, r *http.Request, u *models.User) {
	d, err := s.getDomainScoped(u, pathID(r, "id"))
	if err != nil {
		s.err(w, http.StatusNotFound, err.Error())
		return
	}
	if _, err := s.DB.Exec(`UPDATE domains SET webmail = 0 WHERE id = ?`, d.ID); err != nil {
		s.fail(w, "disable webmail", err)
		return
	}
	if err := s.rebuildWebmail(); err != nil {
		s.fail(w, "rebuild webmail vhost", err)
		return
	}
	if zoneID, ok := s.zoneIDForDomain(d.ID); ok {
		s.DB.Exec(`DELETE FROM dns_records WHERE zone_id = ? AND name = 'webmail' AND type = 'A'`, zoneID)
		if err := s.writeZoneFile(zoneID); err != nil {
			s.fail(w, "write zone", err)
			return
		}
	}
	s.json(w, map[string]bool{"ok": true})
}

// rebuildWebmail regenerates the shared webmail vhost from every enabled domain.
func (s *Server) rebuildWebmail() error {
	rows, err := s.DB.Query(`SELECT name FROM domains WHERE webmail = 1 ORDER BY name`)
	if err != nil {
		return err
	}
	var domains []string
	for rows.Next() {
		var name string
		if rows.Scan(&name) == nil {
			domains = append(domains, name)
		}
	}
	rows.Close()
	return s.webServer().RebuildWebmail(domains)
}
