package api

import (
	"log"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/reinis1996/repanel/internal/models"
	"github.com/reinis1996/repanel/internal/system"
)

// ---- services ----

// serviceList returns the managed-service status with the panel's own version
// filled in (the panel is not a distro package, so system.ServiceList can't
// know it). Shared by the Services page and the dashboard.
func (s *Server) serviceList() []models.ServiceStatus {
	list := system.ServiceList()
	for i := range list {
		if list[i].Name == "repanel" {
			list[i].Version = s.Version
		}
	}
	return list
}

func (s *Server) handleServiceList(w http.ResponseWriter, r *http.Request, _ *models.User) {
	s.json(w, s.serviceList())
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
	s.json(w, map[string]any{
		"status":         system.UFWStatus(),
		"rules":          out,
		"node_isolation": system.NodeAppIsolationActive(),
	})
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

// panelPort returns the panel's own listen port from the configured address.
func (s *Server) panelPort() string {
	if _, p, found := strings.Cut(s.Cfg.ListenAddr, ":"); found && p != "" {
		return p
	}
	return strings.TrimPrefix(s.Cfg.ListenAddr, ":")
}

// SeedFirewall opens the panel's standard stack ports (web, mail, DNS, FTP, SSH
// and the panel) and enables ufw. It runs once — on first start after install —
// recording the rules in panel state so they appear on the Firewall page. It is
// a no-op when ufw is absent, when it has already run (firewall_initialized), or
// when a firewall has already been configured here (existing rules), so it never
// disturbs an operator's own setup. Safe to call on every startup.
func (s *Server) SeedFirewall() {
	if s.DB.Setting("firewall_initialized") != "" || !system.UFWAvailable() {
		return
	}
	var existing int
	s.DB.QueryRow(`SELECT COUNT(*) FROM firewall_rules`).Scan(&existing)
	if existing > 0 {
		s.DB.SetSetting("firewall_initialized", "1")
		return // respect a firewall the operator already set up
	}
	port := s.panelPort()
	opened := 0
	for _, p := range system.DefaultPanelPorts(port) {
		if _, err := s.DB.Exec(`INSERT INTO firewall_rules(port,proto,source,action,note) VALUES(?,?,?,?,?)`,
			p.Port, p.Proto, "any", "allow", p.Note); err != nil {
			log.Printf("seed firewall: record %s/%s: %v", p.Port, p.Proto, err)
			continue
		}
		if err := system.ApplyFirewallRule(models.FirewallRule{Port: p.Port, Proto: p.Proto, Source: "any", Action: "allow"}); err != nil {
			log.Printf("seed firewall: apply %s/%s: %v", p.Port, p.Proto, err)
		}
		opened++
	}
	// Enabling always keeps SSH and the panel port open first (see SetUFWEnabled).
	if err := system.SetUFWEnabled(true, port); err != nil {
		log.Printf("seed firewall: enable ufw: %v", err)
	}
	s.DB.SetSetting("firewall_initialized", "1")
	log.Printf("firewall: opened %d default stack ports and enabled ufw", opened)
}

// EnsureNodeIsolation applies the loopback firewall rules that prevent one tenant
// from reaching another tenant's Node app port directly. Idempotent; safe to run
// on every startup. Runs after SeedFirewall so the two don't contend on ufw.
func (s *Server) EnsureNodeIsolation() {
	if err := system.EnsureNodeAppIsolation(); err != nil {
		log.Printf("node app isolation: %v", err)
		return
	}
	if system.NodeAppIsolationActive() {
		log.Printf("node app isolation: loopback ports %d-%d restricted to nginx and the panel",
			system.NodeAppPortLow, system.NodeAppPortHigh)
	}
}

func (s *Server) handleFirewallToggle(w http.ResponseWriter, r *http.Request, _ *models.User) {
	req, err := decode[struct {
		Enable bool `json:"enable"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	port := s.panelPort()
	if err := system.SetUFWEnabled(req.Enable, port); err != nil {
		s.fail(w, "toggle firewall", err)
		return
	}
	s.json(w, map[string]string{"status": system.UFWStatus()})
}

// ---- settings ----

var editableSettings = []string{"server_ip", "ns1", "ns2", "admin_email", "panel_hostname", "backup_schedule", "backup_keep", "slave_dns",
	"alerts_enabled", "alert_email", "alert_webhook", "alert_disk_pct", "alert_cert_days",
	"brand_name", "brand_color", "brand_logo"}

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
		if msg := settingError(k, v); msg != "" {
			s.err(w, http.StatusBadRequest, k+": "+msg)
			return
		}
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

// settingError validates a single setting value, returning a human-readable
// message when it is unacceptable (empty = accepted). The critical check is
// rejecting control characters on values rendered into generated config files:
// a nameserver or admin-email setting containing a newline would otherwise let
// the operator inject extra lines into every zone file's SOA/NS section.
func settingError(key, val string) string {
	if strings.ContainsAny(val, "\n\r\t") {
		return "value may not contain control characters"
	}
	switch key {
	case "server_ip":
		if val != "" && net.ParseIP(val) == nil {
			return "must be a valid IP address"
		}
	case "ns1", "ns2", "panel_hostname":
		if val != "" && !isHostname(val) {
			return "must be a valid hostname"
		}
	case "admin_email", "alert_email":
		if val != "" && !isEmail(val) {
			return "must be a valid email address"
		}
	case "alert_webhook":
		if val != "" && !strings.HasPrefix(val, "http://") && !strings.HasPrefix(val, "https://") {
			return "must be an http(s) URL"
		}
	case "alert_disk_pct", "alert_cert_days":
		if val != "" {
			if n, err := strconv.Atoi(val); err != nil || n <= 0 {
				return "must be a positive number"
			}
		}
	case "brand_name":
		if len(val) > 40 {
			return "must be 40 characters or fewer"
		}
	case "brand_color":
		if val != "" && !brandColorRe.MatchString(val) {
			return "must be a hex color like #1a6fd4"
		}
	case "brand_logo":
		if val != "" && !strings.HasPrefix(val, "https://") && !strings.HasPrefix(val, "http://") && !strings.HasPrefix(val, "/") {
			return "must be an http(s) URL or an absolute path"
		}
	}
	return ""
}

// brandColorRe matches a 3- or 6-digit hex color.
var brandColorRe = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

// handleBranding is public so the login screen can render the white-label name,
// accent color and logo before authentication.
func (s *Server) handleBranding(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(s.DB.Setting("brand_name"))
	if name == "" {
		name = "RePanel"
	}
	s.json(w, models.Branding{
		Name:  name,
		Color: strings.TrimSpace(s.DB.Setting("brand_color")),
		Logo:  strings.TrimSpace(s.DB.Setting("brand_logo")),
	})
}

// isHostname reports whether s is a syntactically valid DNS hostname (a trailing
// dot is tolerated, since nameserver settings are often entered fully qualified).
func isHostname(s string) bool {
	s = strings.TrimSuffix(s, ".")
	if s == "" || len(s) > 253 {
		return false
	}
	for _, label := range strings.Split(s, ".") {
		if label == "" || len(label) > 63 {
			return false
		}
		for _, c := range label {
			ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-'
			if !ok {
				return false
			}
		}
	}
	return true
}

// isEmail does a deliberately conservative check: a non-empty local part, a
// single "@", and a valid hostname for the domain.
func isEmail(s string) bool {
	local, domain, ok := strings.Cut(s, "@")
	if !ok || local == "" || strings.ContainsAny(local, " @") {
		return false
	}
	return isHostname(domain)
}
