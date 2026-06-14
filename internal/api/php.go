package api

import (
	"log"
	"net/http"
	"sort"

	"github.com/reinis1996/repanel/internal/models"
	"github.com/reinis1996/repanel/internal/system"
)

// phpInstall is the in-memory state of a single PHP version install. While
// installing it is present with failed=false; on success it is removed (the
// version then shows up as installed); on failure failed=true with the error.
type phpInstall struct {
	failed bool
	err    string
}

// handlePHPList reports every PHP version the panel can manage: the installed
// ones, the installable ones, and any in-flight or failed installs.
func (s *Server) handlePHPList(w http.ResponseWriter, r *http.Request, _ *models.User) {
	installed := map[string]bool{}
	for _, v := range system.PHPVersions() {
		installed[v] = true
	}
	versions := map[string]bool{}
	for _, v := range system.KnownPHPVersions() {
		versions[v] = true
	}
	for v := range installed {
		versions[v] = true
	}

	s.phpMu.Lock()
	for v := range s.phpInstalls {
		versions[v] = true
	}
	out := []models.PHPVersionInfo{}
	for v := range versions {
		info := models.PHPVersionInfo{Version: v, Installed: installed[v]}
		if st := s.phpInstalls[v]; st != nil {
			if st.failed {
				info.Error = st.err
			} else {
				info.Installing = true
			}
		}
		out = append(out, info)
	}
	s.phpMu.Unlock()

	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	s.json(w, out)
}

// handlePHPInstall starts installing a PHP-FPM version (and the common
// extension set) in the background, adding the distro's multi-version PHP
// repository first if necessary. Progress is polled via handlePHPList.
func (s *Server) handlePHPInstall(w http.ResponseWriter, r *http.Request, _ *models.User) {
	req, err := decode[struct {
		Version string `json:"version"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	known := false
	for _, v := range system.KnownPHPVersions() {
		if v == req.Version {
			known = true
			break
		}
	}
	if !known {
		s.err(w, http.StatusBadRequest, "unsupported PHP version")
		return
	}
	for _, v := range system.PHPVersions() {
		if v == req.Version {
			s.err(w, http.StatusConflict, "PHP "+req.Version+" is already installed")
			return
		}
	}

	s.phpMu.Lock()
	if st := s.phpInstalls[req.Version]; st != nil && !st.failed {
		s.phpMu.Unlock()
		s.err(w, http.StatusConflict, "PHP "+req.Version+" is already being installed")
		return
	}
	s.phpInstalls[req.Version] = &phpInstall{} // installing
	s.phpMu.Unlock()

	go func(version string) {
		err := system.InstallPHP(version)
		s.phpMu.Lock()
		defer s.phpMu.Unlock()
		if err != nil {
			log.Printf("ERROR install php %s: %v", version, err)
			s.phpInstalls[version] = &phpInstall{failed: true, err: err.Error()}
			return
		}
		delete(s.phpInstalls, version) // now detected as installed
	}(req.Version)

	s.json(w, models.PHPVersionInfo{Version: req.Version, Installing: true})
}
