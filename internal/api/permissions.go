package api

import (
	"net/http"

	"github.com/reinis1996/repanel/internal/auth"
	"github.com/reinis1996/repanel/internal/models"
)

// handlePermissionsGet returns the module catalog and the default module sets
// applied to new accounts of each group (user / reseller).
func (s *Server) handlePermissionsGet(w http.ResponseWriter, r *http.Request, _ *models.User) {
	type mod struct {
		Key   string `json:"key"`
		Label string `json:"label"`
	}
	mods := make([]mod, 0, len(models.AllModules))
	for _, m := range models.AllModules {
		mods = append(mods, mod{Key: m, Label: models.ModuleLabels[m]})
	}
	s.json(w, map[string]any{
		"modules":  mods,
		"user":     auth.SplitPermissions(s.DB.Setting("default_perms_user")),
		"reseller": auth.SplitPermissions(s.DB.Setting("default_perms_reseller")),
	})
}

// handlePermissionsSet stores the per-group default module sets.
func (s *Server) handlePermissionsSet(w http.ResponseWriter, r *http.Request, _ *models.User) {
	req, err := decode[struct {
		User     []string `json:"user"`
		Reseller []string `json:"reseller"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := s.DB.SetSetting("default_perms_user", auth.JoinPermissions(req.User)); err != nil {
		s.fail(w, "save default permissions", err)
		return
	}
	if err := s.DB.SetSetting("default_perms_reseller", auth.JoinPermissions(req.Reseller)); err != nil {
		s.fail(w, "save default permissions", err)
		return
	}
	s.json(w, map[string]bool{"ok": true})
}
