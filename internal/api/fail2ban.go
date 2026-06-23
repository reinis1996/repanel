package api

import (
	"net/http"

	"github.com/reinis1996/repanel/internal/models"
	"github.com/reinis1996/repanel/internal/system"
)

// fail2ban management (admin only): view jails and their banned IPs, ban/unban
// addresses, and maintain the never-ban whitelist.

func (s *Server) handleFail2banStatus(w http.ResponseWriter, r *http.Request, u *models.User) {
	if !system.Fail2banAvailable() {
		s.json(w, map[string]any{"available": false})
		return
	}
	names, err := system.Fail2banJails()
	if err != nil {
		s.err(w, http.StatusBadGateway, err.Error())
		return
	}
	jails := []system.Fail2banJail{}
	for _, n := range names {
		if j, err := system.Fail2banJailStatus(n); err == nil {
			jails = append(jails, j)
		}
	}
	s.json(w, map[string]any{
		"available": true,
		"jails":     jails,
		"whitelist":      system.Fail2banGetWhitelist(),
		"config":         system.Fail2banReadConfig(),
		"filters":        system.Fail2banFilters(),
		"custom_filters": system.Fail2banCustomFilterNames(),
	})
}

func (s *Server) handleFail2banConfig(w http.ResponseWriter, r *http.Request, u *models.User) {
	req, err := decode[system.Fail2banConfig](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := system.Fail2banWriteConfig(req); err != nil {
		s.err(w, http.StatusBadRequest, err.Error())
		return
	}
	s.json(w, map[string]bool{"ok": true})
}

func (s *Server) handleFail2banFilterGet(w http.ResponseWriter, r *http.Request, u *models.User) {
	f, err := system.Fail2banReadFilter(r.URL.Query().Get("name"))
	if err != nil {
		s.err(w, http.StatusNotFound, err.Error())
		return
	}
	s.json(w, f)
}

func (s *Server) handleFail2banFilterSet(w http.ResponseWriter, r *http.Request, u *models.User) {
	req, err := decode[struct {
		Name        string `json:"name"`
		Failregex   string `json:"failregex"`
		Ignoreregex string `json:"ignoreregex"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := system.Fail2banWriteFilter(req.Name, req.Failregex, req.Ignoreregex); err != nil {
		s.err(w, http.StatusBadRequest, err.Error())
		return
	}
	s.json(w, map[string]bool{"ok": true})
}

func (s *Server) handleFail2banFilterDelete(w http.ResponseWriter, r *http.Request, u *models.User) {
	name := r.PathValue("name")
	// Refuse if a panel-managed jail still references this filter.
	for _, j := range system.Fail2banReadConfig().Jails {
		if j.Filter == name {
			s.err(w, http.StatusConflict, "the jail \""+j.Name+"\" still uses this filter — change or remove it first")
			return
		}
	}
	if err := system.Fail2banDeleteFilter(name); err != nil {
		s.err(w, http.StatusBadRequest, err.Error())
		return
	}
	s.json(w, map[string]bool{"ok": true})
}

func (s *Server) handleFail2banBan(w http.ResponseWriter, r *http.Request, u *models.User) {
	req, err := decode[struct {
		Jail  string `json:"jail"`
		IP    string `json:"ip"`
		Unban bool   `json:"unban"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Unban {
		err = system.Fail2banUnban(req.Jail, req.IP)
	} else {
		err = system.Fail2banBan(req.Jail, req.IP)
	}
	if err != nil {
		s.err(w, http.StatusBadGateway, err.Error())
		return
	}
	s.json(w, map[string]bool{"ok": true})
}

func (s *Server) handleFail2banWhitelist(w http.ResponseWriter, r *http.Request, u *models.User) {
	req, err := decode[struct {
		Entries []string `json:"entries"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := system.Fail2banSetWhitelist(req.Entries); err != nil {
		s.err(w, http.StatusBadRequest, err.Error())
		return
	}
	s.json(w, map[string]bool{"ok": true})
}
