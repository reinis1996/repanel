package api

import (
	"net/http"
	"strings"

	"github.com/reinis1996/repanel/internal/models"
	"github.com/reinis1996/repanel/internal/system"
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

var editableSettings = []string{"server_ip", "ns1", "ns2", "admin_email", "panel_hostname", "backup_schedule", "backup_keep", "slave_dns"}

// dnsSettings change the contents of generated zone files / named.conf, so
// saving any of them re-renders every hosted zone.
var dnsSettings = map[string]bool{"ns1": true, "ns2": true, "admin_email": true, "slave_dns": true}

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
	dnsChanged := false
	for _, k := range editableSettings {
		v, ok := req[k]
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if k == "slave_dns" {
			// Normalize to the valid IPs so an invalid entry can't silently
			// disable transfers; reject if the user typed something unparseable.
			ips := system.ParseSlaveIPs(v)
			if v != "" && len(ips) != len(strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' })) {
				s.err(w, http.StatusBadRequest, "slave DNS must be a comma-separated list of IP addresses")
				return
			}
			v = strings.Join(ips, ", ")
		}
		if v != s.DB.Setting(k) && dnsSettings[k] {
			dnsChanged = true
		}
		if err := s.DB.SetSetting(k, v); err != nil {
			s.fail(w, "save setting", err)
			return
		}
	}
	if dnsChanged {
		if err := s.rewriteAllZones(); err != nil {
			s.fail(w, "rewrite zones", err)
			return
		}
	}
	s.json(w, map[string]bool{"ok": true})
}

// rewriteAllZones re-renders every hosted zone file (and named.conf) so that
// changes to nameserver / secondary-DNS settings take effect immediately.
func (s *Server) rewriteAllZones() error {
	rows, err := s.DB.Query(`SELECT id FROM dns_zones`)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		if err := s.writeZoneFile(id); err != nil {
			return err
		}
	}
	return nil
}
