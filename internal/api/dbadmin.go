package api

import (
	"net/http"

	"github.com/reinis1996/repanel/internal/models"
	"github.com/reinis1996/repanel/internal/system"
)

// Web database administration (Adminer). An admin installs the single-file
// Adminer client and enables it; the panel serves it on dbadmin.<panel-hostname>
// through a generated nginx vhost on the shared PHP pool. The Databases page
// then links to it per database.

type dbAdminStatus struct {
	Installed bool   `json:"installed"`
	Enabled   bool   `json:"enabled"`
	Host      string `json:"host"`
	URL       string `json:"url"`
}

func (s *Server) dbAdminStatus() dbAdminStatus {
	host := system.AdminerHost(s.DB.Setting("panel_hostname"))
	st := dbAdminStatus{
		Installed: system.AdminerInstalled(),
		Enabled:   s.DB.Setting("dbadmin_enabled") == "1",
		Host:      host,
	}
	if st.Enabled && host != "" {
		st.URL = "http://" + host + "/"
	}
	return st
}

// handleDBAdminStatus is readable by anyone with the databases module so the
// Databases page can show the Adminer links.
func (s *Server) handleDBAdminStatus(w http.ResponseWriter, r *http.Request, u *models.User) {
	s.json(w, s.dbAdminStatus())
}

func (s *Server) handleDBAdminInstall(w http.ResponseWriter, r *http.Request, u *models.User) {
	if err := system.InstallAdminer(); err != nil {
		s.fail(w, "install adminer", err)
		return
	}
	s.json(w, s.dbAdminStatus())
}

func (s *Server) handleDBAdminEnable(w http.ResponseWriter, r *http.Request, u *models.User) {
	req, err := decode[struct {
		Enabled bool `json:"enabled"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Enabled && !system.AdminerInstalled() {
		s.err(w, http.StatusBadRequest, "install Adminer first")
		return
	}
	host := system.AdminerHost(s.DB.Setting("panel_hostname"))
	if req.Enabled && host == "" {
		s.err(w, http.StatusBadRequest, "set the panel hostname in Settings first — Adminer is served on dbadmin.<hostname>")
		return
	}
	vhostHost := host
	if !req.Enabled {
		vhostHost = "" // removes the vhost
	}
	if err := s.webServer().WriteAdminerVhost(vhostHost, "", ""); err != nil {
		s.fail(w, "write adminer vhost", err)
		return
	}
	s.DB.SetSetting("dbadmin_enabled", boolSetting(req.Enabled))
	s.json(w, s.dbAdminStatus())
}

func boolSetting(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
