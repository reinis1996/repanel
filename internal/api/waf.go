package api

import (
	"net/http"
	"strings"

	"github.com/reinis1996/repanel/internal/models"
	"github.com/reinis1996/repanel/internal/system"
)

// Per-domain ModSecurity WAF. Domain owners can enable/disable it and pick the
// engine mode (blocking or detection-only); custom rule edits are admin-only,
// since a rule can use actions (e.g. exec) that reach outside the request. The
// engine + OWASP CRS are installed once, by an admin, for the active web server.

const maxWAFRules = 64 * 1024

// handleWAFGet reports the WAF state for one domain plus server-wide availability.
func (s *Server) handleWAFGet(w http.ResponseWriter, r *http.Request, u *models.User) {
	d, err := s.getDomainScoped(u, pathID(r, "id"))
	if err != nil {
		s.err(w, http.StatusNotFound, err.Error())
		return
	}
	s.wafMu.Lock()
	installing, installErr := s.wafInstalling, s.wafErr
	s.wafMu.Unlock()

	resp := map[string]any{
		"available":      system.WAFModuleAvailable(s.webServer().FrontIsApache()),
		"crs":            system.WAFCRSInstalled(),
		"installing":     installing,
		"error":          installErr,
		"enabled":        d.WAFEnabled,
		"mode":           d.WAFMode,
		"rules":          d.WAFRules,
		"can_edit_rules": u.Role == models.RoleAdmin,
	}
	s.json(w, resp)
}

// handleWAFSet updates a domain's WAF settings and rebuilds its vhost, reverting
// to the previous settings if the resulting config is rejected. Enabling requires
// the connector to be installed; editing custom rules requires admin.
func (s *Server) handleWAFSet(w http.ResponseWriter, r *http.Request, u *models.User) {
	d, err := s.getDomainScoped(u, pathID(r, "id"))
	if err != nil {
		s.err(w, http.StatusNotFound, err.Error())
		return
	}
	req, err := decode[struct {
		Enabled bool    `json:"enabled"`
		Mode    string  `json:"mode"`
		Rules   *string `json:"rules"` // pointer: omitted means "leave unchanged"
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Mode != "on" && req.Mode != "detection" {
		s.err(w, http.StatusBadRequest, "mode must be 'on' or 'detection'")
		return
	}
	if req.Enabled && !system.WAFModuleAvailable(s.webServer().FrontIsApache()) {
		s.err(w, http.StatusBadRequest, "the WAF engine is not installed on this server yet")
		return
	}

	rules := d.WAFRules
	if req.Rules != nil && *req.Rules != rules {
		if u.Role != models.RoleAdmin {
			s.err(w, http.StatusForbidden, "only an administrator can edit custom WAF rules")
			return
		}
		if len(*req.Rules) > maxWAFRules {
			s.err(w, http.StatusBadRequest, "custom rules are too large")
			return
		}
		if strings.ContainsRune(*req.Rules, 0) {
			s.err(w, http.StatusBadRequest, "rules may not contain null bytes")
			return
		}
		rules = *req.Rules
	}

	oldEnabled, oldMode, oldRules := d.WAFEnabled, d.WAFMode, d.WAFRules
	apply := func(enabled bool, mode, customRules string) error {
		if _, err := s.DB.Exec(`UPDATE domains SET waf_enabled = ?, waf_mode = ?, waf_rules = ? WHERE id = ?`,
			boolInt(enabled), mode, customRules, d.ID); err != nil {
			return err
		}
		d.WAFEnabled, d.WAFMode, d.WAFRules = enabled, mode, customRules
		if d.Suspended {
			return nil // real vhost is regenerated on unsuspend
		}
		return s.rewriteVhost(*d)
	}

	if err := apply(req.Enabled, req.Mode, rules); err != nil {
		if rbErr := apply(oldEnabled, oldMode, oldRules); rbErr != nil {
			s.fail(w, "restore waf config", rbErr)
			return
		}
		s.err(w, http.StatusBadGateway, "WAF configuration rejected, reverted: "+err.Error())
		return
	}
	s.json(w, map[string]any{"ok": true, "enabled": req.Enabled, "mode": req.Mode})
}

// handleWAFInstall installs ModSecurity + the OWASP CRS for the active front
// server in the background (admin only).
func (s *Server) handleWAFInstall(w http.ResponseWriter, r *http.Request, u *models.User) {
	s.wafMu.Lock()
	if s.wafInstalling {
		s.wafMu.Unlock()
		s.err(w, http.StatusConflict, "an install is already in progress")
		return
	}
	frontApache := s.webServer().FrontIsApache()
	if system.WAFModuleAvailable(frontApache) {
		s.wafMu.Unlock()
		s.json(w, map[string]bool{"ok": true})
		return
	}
	s.wafInstalling = true
	s.wafErr = ""
	s.wafMu.Unlock()

	go func() {
		err := system.InstallWAF(frontApache)
		s.wafMu.Lock()
		s.wafInstalling = false
		if err != nil {
			s.wafErr = err.Error()
		}
		s.wafMu.Unlock()
	}()
	s.json(w, map[string]bool{"ok": true})
}
