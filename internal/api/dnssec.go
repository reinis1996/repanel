package api

import (
	"net/http"

	"github.com/reinis1996/repanel/internal/models"
	"github.com/reinis1996/repanel/internal/system"
)

// DNSSEC signing for a zone (BIND inline-signing via dnssec-policy). Enabling a
// zone tells named to generate keys and sign it; the panel surfaces the DS
// records the operator must publish at the domain's registrar.

func (s *Server) handleDNSSECStatus(w http.ResponseWriter, r *http.Request, u *models.User) {
	z, err := s.getZoneScoped(u, pathID(r, "id"))
	if err != nil {
		s.err(w, http.StatusNotFound, "zone not found")
		return
	}
	status := models.DNSSECStatus{
		ZoneID:    z.ID,
		Zone:      z.Name,
		Available: system.DNSSECAvailable(),
		Enabled:   system.DNSSECEnabled(s.Cfg.BindDir, z.Name),
	}
	if status.Enabled {
		status.DS, status.DNSKEY = system.DNSSECRecords(s.Cfg.BindDir, z.Name)
	}
	s.json(w, status)
}

func (s *Server) handleDNSSECEnable(w http.ResponseWriter, r *http.Request, u *models.User) {
	z, err := s.getZoneScoped(u, pathID(r, "id"))
	if err != nil {
		s.err(w, http.StatusNotFound, "zone not found")
		return
	}
	if !system.DNSSECAvailable() {
		s.err(w, http.StatusBadRequest, "DNSSEC tooling (BIND) is not available on this server")
		return
	}
	slaveIPs := system.ParseSlaveIPs(s.DB.Setting("slave_dns"))
	if err := system.EnableDNSSEC(s.Cfg.BindDir, z.Name, slaveIPs); err != nil {
		s.fail(w, "enable dnssec", err)
		return
	}
	s.DB.Exec(`UPDATE dns_zones SET dnssec = 1 WHERE id = ?`, z.ID)
	ds, dnskey := system.DNSSECRecords(s.Cfg.BindDir, z.Name)
	s.json(w, models.DNSSECStatus{ZoneID: z.ID, Zone: z.Name, Available: true, Enabled: true, DS: ds, DNSKEY: dnskey})
}

func (s *Server) handleDNSSECDisable(w http.ResponseWriter, r *http.Request, u *models.User) {
	z, err := s.getZoneScoped(u, pathID(r, "id"))
	if err != nil {
		s.err(w, http.StatusNotFound, "zone not found")
		return
	}
	slaveIPs := system.ParseSlaveIPs(s.DB.Setting("slave_dns"))
	if err := system.DisableDNSSEC(s.Cfg.BindDir, z.Name, slaveIPs); err != nil {
		s.fail(w, "disable dnssec", err)
		return
	}
	s.DB.Exec(`UPDATE dns_zones SET dnssec = 0 WHERE id = ?`, z.ID)
	s.json(w, models.DNSSECStatus{ZoneID: z.ID, Zone: z.Name, Available: true, Enabled: false})
}
