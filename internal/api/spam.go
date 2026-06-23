package api

import (
	"log"
	"net/http"

	"github.com/reinis1996/repanel/internal/models"
	"github.com/reinis1996/repanel/internal/system"
)

// rebuildSpamSettings regenerates rspamd's per-domain settings from the panel
// state: every domain with spam_filter=0 is exempted from filtering.
func (s *Server) rebuildSpamSettings() error {
	rows, err := s.DB.Query(`SELECT name FROM domains WHERE spam_filter = 0`)
	if err != nil {
		return err
	}
	var disabled []string
	for rows.Next() {
		var name string
		if rows.Scan(&name) == nil {
			disabled = append(disabled, name)
		}
	}
	rows.Close()
	return system.RebuildSpamSettings(disabled)
}

// SyncAntiSpam applies the current per-domain spam settings at startup (no-op
// when rspamd isn't installed), so settings.conf reflects the database.
func (s *Server) SyncAntiSpam() {
	if err := s.rebuildSpamSettings(); err != nil {
		log.Printf("anti-spam: sync settings: %v", err)
	}
}

type spamDomainStatus struct {
	DomainID int64  `json:"domain_id"`
	Domain   string `json:"domain"`
	Enabled  bool   `json:"enabled"`
}

// handleSpamStatus reports anti-spam availability and per-domain state for every
// domain the caller manages.
func (s *Server) handleSpamStatus(w http.ResponseWriter, r *http.Request, u *models.User) {
	s.antispamMu.Lock()
	installing, installErr := s.antispamInstalling, s.antispamErr
	s.antispamMu.Unlock()

	where, args := scopeWhere(u, "user_id")
	rows, err := s.DB.Query(`SELECT id, name, spam_filter FROM domains WHERE `+where+` ORDER BY name`, args...)
	if err != nil {
		s.fail(w, "list spam status", err)
		return
	}
	defer rows.Close()
	domains := []spamDomainStatus{}
	for rows.Next() {
		var d spamDomainStatus
		var sf int
		if rows.Scan(&d.DomainID, &d.Domain, &sf) == nil {
			d.Enabled = sf != 0
			domains = append(domains, d)
		}
	}
	s.json(w, map[string]any{
		"available":  system.HaveRspamd(),
		"clamav":     system.HaveClamAV(),
		"installing": installing,
		"error":      installErr,
		"domains":    domains,
	})
}

// handleSpamToggle enables or disables spam/virus filtering for one domain.
func (s *Server) handleSpamToggle(w http.ResponseWriter, r *http.Request, u *models.User) {
	d, err := s.getDomainScoped(u, pathID(r, "id"))
	if err != nil {
		s.err(w, http.StatusNotFound, err.Error())
		return
	}
	req, err := decode[struct {
		Enabled bool `json:"enabled"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	val := 0
	if req.Enabled {
		val = 1
	}
	if _, err := s.DB.Exec(`UPDATE domains SET spam_filter = ? WHERE id = ?`, val, d.ID); err != nil {
		s.fail(w, "update spam filter", err)
		return
	}
	if err := s.rebuildSpamSettings(); err != nil {
		s.fail(w, "rebuild spam settings", err)
		return
	}
	s.json(w, map[string]bool{"ok": true, "enabled": req.Enabled})
}

// handleAntiSpamInstall installs rspamd + ClamAV in the background, then wires
// them into Postfix and applies the current per-domain settings.
func (s *Server) handleAntiSpamInstall(w http.ResponseWriter, r *http.Request, u *models.User) {
	s.antispamMu.Lock()
	if s.antispamInstalling {
		s.antispamMu.Unlock()
		s.err(w, http.StatusConflict, "an install is already in progress")
		return
	}
	if system.HaveRspamd() {
		s.antispamMu.Unlock()
		s.json(w, map[string]bool{"ok": true})
		return
	}
	s.antispamInstalling = true
	s.antispamErr = ""
	s.antispamMu.Unlock()

	go func() {
		err := system.InstallAntiSpam()
		if err == nil {
			err = s.rebuildSpamSettings()
		}
		s.antispamMu.Lock()
		s.antispamInstalling = false
		if err != nil {
			s.antispamErr = err.Error()
		}
		s.antispamMu.Unlock()
	}()
	s.json(w, map[string]bool{"ok": true})
}
