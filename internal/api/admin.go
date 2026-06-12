package api

import (
	"net/http"
	"strings"

	"github.com/repanel/repanel/internal/models"
	"github.com/repanel/repanel/internal/system"
)

// ---- services ----

func (s *Server) handleServiceList(w http.ResponseWriter, r *http.Request, _ *models.User) {
	s.json(w, system.ServiceList())
}

func (s *Server) handleServiceAction(w http.ResponseWriter, r *http.Request, _ *models.User) {
	name := r.PathValue("name")
	action := r.PathValue("action")
	if err := system.ServiceAction(name, action); err != nil {
		s.fail(w, "service action", err)
		return
	}
	s.json(w, map[string]bool{"ok": true})
}

// ---- firewall ----

func (s *Server) handleFirewallList(w http.ResponseWriter, r *http.Request, _ *models.User) {
	rows, err := s.DB.Query(`SELECT id, port, proto, source, action, note FROM firewall_rules ORDER BY id`)
	if err != nil {
		s.fail(w, "list firewall rules", err)
		return
	}
	defer rows.Close()
	out := []models.FirewallRule{}
	for rows.Next() {
		var fr models.FirewallRule
		if rows.Scan(&fr.ID, &fr.Port, &fr.Proto, &fr.Source, &fr.Action, &fr.Note) == nil {
			out = append(out, fr)
		}
	}
	s.json(w, map[string]any{"status": system.UFWStatus(), "rules": out})
}

func (s *Server) handleFirewallCreate(w http.ResponseWriter, r *http.Request, _ *models.User) {
	req, err := decode[models.FirewallRule](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Proto == "" {
		req.Proto = "tcp"
	}
	if req.Action == "" {
		req.Action = "allow"
	}
	if req.Source == "" {
		req.Source = "any"
	}
	if err := system.ApplyFirewallRule(req); err != nil {
		s.err(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := s.DB.Exec(`INSERT INTO firewall_rules(port,proto,source,action,note) VALUES(?,?,?,?,?)`,
		req.Port, req.Proto, req.Source, req.Action, req.Note)
	if err != nil {
		s.fail(w, "store firewall rule", err)
		return
	}
	req.ID, _ = res.LastInsertId()
	s.json(w, req)
}

func (s *Server) handleFirewallDelete(w http.ResponseWriter, r *http.Request, _ *models.User) {
	id := pathID(r, "id")
	var fr models.FirewallRule
	if err := s.DB.QueryRow(`SELECT id, port, proto, source, action FROM firewall_rules WHERE id = ?`, id).
		Scan(&fr.ID, &fr.Port, &fr.Proto, &fr.Source, &fr.Action); err != nil {
		s.err(w, http.StatusNotFound, "rule not found")
		return
	}
	if err := system.RemoveFirewallRule(fr); err != nil {
		s.fail(w, "remove firewall rule", err)
		return
	}
	if _, err := s.DB.Exec(`DELETE FROM firewall_rules WHERE id = ?`, id); err != nil {
		s.fail(w, "delete firewall rule", err)
		return
	}
	s.json(w, map[string]bool{"ok": true})
}

func (s *Server) handleFirewallToggle(w http.ResponseWriter, r *http.Request, _ *models.User) {
	req, err := decode[struct {
		Enable bool `json:"enable"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	port := strings.TrimPrefix(s.Cfg.ListenAddr, ":")
	if _, p, found := strings.Cut(s.Cfg.ListenAddr, ":"); found && p != "" {
		port = p
	}
	if err := system.SetUFWEnabled(req.Enable, port); err != nil {
		s.fail(w, "toggle firewall", err)
		return
	}
	s.json(w, map[string]string{"status": system.UFWStatus()})
}

// ---- settings ----

var editableSettings = []string{"server_ip", "ns1", "ns2", "admin_email", "panel_hostname"}

func (s *Server) handleSettingsGet(w http.ResponseWriter, r *http.Request, _ *models.User) {
	out := map[string]string{}
	for _, k := range editableSettings {
		out[k] = s.DB.Setting(k)
	}
	s.json(w, out)
}

func (s *Server) handleSettingsSet(w http.ResponseWriter, r *http.Request, _ *models.User) {
	req, err := decode[map[string]string](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	for _, k := range editableSettings {
		if v, ok := req[k]; ok {
			if err := s.DB.SetSetting(k, strings.TrimSpace(v)); err != nil {
				s.fail(w, "save setting", err)
				return
			}
		}
	}
	s.json(w, map[string]bool{"ok": true})
}
