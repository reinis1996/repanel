package api

import (
	"log"
	"net/http"
	"sort"

	"github.com/reinis1996/repanel/internal/models"
	"github.com/reinis1996/repanel/internal/system"
)

// Admin Node.js version manager — installs official Node binaries side-by-side
// under /opt/repanel/node, mirroring the PHP version manager (php.go).

// nodeInstall is the in-memory state of one in-flight Node version install.
type nodeInstall struct {
	failed bool
	err    string
}

// handleNodeList reports every Node version the panel can manage: installed,
// installable, and any in-flight or failed installs.
func (s *Server) handleNodeList(w http.ResponseWriter, r *http.Request, _ *models.User) {
	installed := map[string]bool{}
	for _, v := range system.InstalledNodeVersions() {
		installed[v] = true
	}
	versions := map[string]bool{}
	for _, v := range system.KnownNodeVersions() {
		versions[v] = true
	}
	for v := range installed {
		versions[v] = true
	}

	s.nodeMu.Lock()
	for v := range s.nodeInstalls {
		versions[v] = true
	}
	out := []models.NodeVersionInfo{}
	for v := range versions {
		info := models.NodeVersionInfo{Version: v, Installed: installed[v]}
		if st := s.nodeInstalls[v]; st != nil {
			if st.failed {
				info.Error = st.err
			} else {
				info.Installing = true
			}
		}
		out = append(out, info)
	}
	s.nodeMu.Unlock()

	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	s.json(w, out)
}

// handleNodeInstall starts downloading and installing a Node version in the
// background; progress is polled via handleNodeList.
func (s *Server) handleNodeInstall(w http.ResponseWriter, r *http.Request, _ *models.User) {
	req, err := decode[struct {
		Version string `json:"version"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	known := false
	for _, v := range system.KnownNodeVersions() {
		if v == req.Version {
			known = true
			break
		}
	}
	if !known {
		s.err(w, http.StatusBadRequest, "unsupported Node version")
		return
	}
	for _, v := range system.InstalledNodeVersions() {
		if v == req.Version {
			s.err(w, http.StatusConflict, "Node "+req.Version+" is already installed")
			return
		}
	}

	s.nodeMu.Lock()
	if st := s.nodeInstalls[req.Version]; st != nil && !st.failed {
		s.nodeMu.Unlock()
		s.err(w, http.StatusConflict, "Node "+req.Version+" is already being installed")
		return
	}
	s.nodeInstalls[req.Version] = &nodeInstall{}
	s.nodeMu.Unlock()

	go func(version string) {
		err := system.InstallNode(version)
		s.nodeMu.Lock()
		defer s.nodeMu.Unlock()
		if err != nil {
			log.Printf("ERROR install node %s: %v", version, err)
			s.nodeInstalls[version] = &nodeInstall{failed: true, err: err.Error()}
			return
		}
		delete(s.nodeInstalls, version)
	}(req.Version)

	s.json(w, models.NodeVersionInfo{Version: req.Version, Installing: true})
}
