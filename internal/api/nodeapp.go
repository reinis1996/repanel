package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/reinis1996/repanel/internal/models"
	"github.com/reinis1996/repanel/internal/system"
)

// Node.js web-app endpoints: switch a domain's runtime between PHP and Node, and
// manage the Node app (settings, npm install, restart, start/stop).

// handleNodeVersions lists the Node versions installed on the host (dropdown).
func (s *Server) handleNodeVersions(w http.ResponseWriter, r *http.Request, _ *models.User) {
	s.json(w, system.InstalledNodeVersions())
}

// loadNodeConfig reads a domain's stored Node settings.
func (s *Server) loadNodeConfig(domainID int64) (appRoot, startup string, env map[string]string) {
	var envJSON string
	s.DB.QueryRow(`SELECT node_app_root, node_startup, node_env FROM domains WHERE id = ?`, domainID).
		Scan(&appRoot, &startup, &envJSON)
	if startup == "" {
		startup = "app.js"
	}
	env = map[string]string{}
	if envJSON != "" {
		json.Unmarshal([]byte(envJSON), &env)
	}
	return appRoot, startup, env
}

// handleDomainRuntime switches a domain between PHP and Node (or changes the
// version of the current runtime), tearing down the old runtime and deploying the
// new one.
func (s *Server) handleDomainRuntime(w http.ResponseWriter, r *http.Request, u *models.User) {
	d, err := s.getDomainScoped(u, pathID(r, "id"))
	if err != nil {
		s.err(w, http.StatusNotFound, err.Error())
		return
	}
	if d.Kind == "alias" || d.RedirectURL != "" {
		s.err(w, http.StatusBadRequest, "this domain mirrors or forwards another site and has no runtime of its own")
		return
	}
	req, err := decode[struct {
		Runtime string `json:"runtime"` // php | node
		Version string `json:"version"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Runtime != "php" && req.Runtime != "node" {
		s.err(w, http.StatusBadRequest, "runtime must be 'php' or 'node'")
		return
	}
	sysUser, err := s.sysUserForPanelUser(d.UserID)
	if err != nil {
		s.fail(w, "resolve system user", err)
		return
	}
	wasNode := d.Runtime == "node"

	if req.Runtime == "node" {
		if !nodeVersionInstalled(req.Version) {
			s.err(w, http.StatusBadRequest, "that Node version is not installed on this server")
			return
		}
		if d.NodePort == 0 {
			port := s.allocNodePort()
			if port == 0 {
				s.err(w, http.StatusServiceUnavailable, "no free application port available")
				return
			}
			d.NodePort = port
		}
		if !wasNode {
			s.webServer().RemoveVhost(*d) // drop PHP pool/vhost
		}
		d.Runtime, d.NodeVersion = "node", req.Version
		if _, err := s.DB.Exec(`UPDATE domains SET runtime='node', node_version=?, node_port=? WHERE id=?`,
			d.NodeVersion, d.NodePort, d.ID); err != nil {
			s.fail(w, "update domain", err)
			return
		}
		appRoot, startup, env := s.loadNodeConfig(d.ID)
		system.WriteSampleApp(*d, sysUser, appRoot, startup) // starter app if none exists
		if err := s.rewriteVhost(*d); err != nil {
			s.fail(w, "rewrite vhost", err)
			return
		}
		if err := system.WriteNodeApp(*d, sysUser, appRoot, startup, env); err != nil {
			s.err(w, http.StatusBadGateway, err.Error())
			return
		}
		s.json(w, d)
		return
	}

	// Switch to / stay on PHP.
	if !phpVersionInstalled(req.Version) {
		s.err(w, http.StatusBadRequest, "that PHP version is not installed on this server")
		return
	}
	if wasNode {
		system.RemoveNodeApp(*d)
		s.webServer().RemoveVhost(*d) // drop node vhost
	} else {
		s.webServer().RemoveVhost(*d) // drop old pool keyed on old PHP version
	}
	d.Runtime, d.PHPVersion = "php", req.Version
	if _, err := s.DB.Exec(`UPDATE domains SET runtime='php', php_version=? WHERE id=?`, d.PHPVersion, d.ID); err != nil {
		s.fail(w, "update domain", err)
		return
	}
	if err := s.rewriteVhost(*d); err != nil {
		s.fail(w, "rewrite vhost", err)
		return
	}
	s.json(w, d)
}

// handleNodeGet returns a domain's Node app settings and live status.
func (s *Server) handleNodeGet(w http.ResponseWriter, r *http.Request, u *models.User) {
	d, err := s.getDomainScoped(u, pathID(r, "id"))
	if err != nil || d.Runtime != "node" {
		s.err(w, http.StatusNotFound, "node app not found")
		return
	}
	appRoot, startup, env := s.loadNodeConfig(d.ID)
	s.json(w, models.NodeApp{
		DomainID: d.ID, Version: d.NodeVersion, AppRoot: appRoot, Startup: startup,
		Port: d.NodePort, Env: env, Running: system.NodeAppRunning(*d),
		URL: "http" + sslScheme(d.SSL) + "://" + d.Name,
	})
}

func sslScheme(ssl bool) string {
	if ssl {
		return "s"
	}
	return ""
}

// handleNodeUpdate updates a Node app's settings (version, app root, startup,
// env) and redeploys it.
func (s *Server) handleNodeUpdate(w http.ResponseWriter, r *http.Request, u *models.User) {
	d, err := s.getDomainScoped(u, pathID(r, "id"))
	if err != nil || d.Runtime != "node" {
		s.err(w, http.StatusNotFound, "node app not found")
		return
	}
	req, err := decode[struct {
		Version string            `json:"version"`
		AppRoot string            `json:"app_root"`
		Startup string            `json:"startup"`
		Env     map[string]string `json:"env"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Version != "" {
		if !nodeVersionInstalled(req.Version) {
			s.err(w, http.StatusBadRequest, "that Node version is not installed on this server")
			return
		}
		d.NodeVersion = req.Version
	}
	startup := strings.TrimSpace(req.Startup)
	if startup == "" {
		startup = "app.js"
	}
	envJSON, _ := json.Marshal(req.Env)
	sysUser, err := s.sysUserForPanelUser(d.UserID)
	if err != nil {
		s.fail(w, "resolve system user", err)
		return
	}
	if _, err := s.DB.Exec(`UPDATE domains SET node_version=?, node_app_root=?, node_startup=?, node_env=? WHERE id=?`,
		d.NodeVersion, strings.TrimSpace(req.AppRoot), startup, string(envJSON), d.ID); err != nil {
		s.fail(w, "update node app", err)
		return
	}
	if err := system.WriteNodeApp(*d, sysUser, strings.TrimSpace(req.AppRoot), startup, req.Env); err != nil {
		s.err(w, http.StatusBadGateway, err.Error())
		return
	}
	s.json(w, map[string]bool{"ok": true})
}

func (s *Server) handleNodeRestart(w http.ResponseWriter, r *http.Request, u *models.User) {
	d, err := s.getDomainScoped(u, pathID(r, "id"))
	if err != nil || d.Runtime != "node" {
		s.err(w, http.StatusNotFound, "node app not found")
		return
	}
	if err := system.RestartNodeApp(*d); err != nil {
		s.err(w, http.StatusBadGateway, err.Error())
		return
	}
	s.json(w, map[string]bool{"ok": true})
}

func (s *Server) handleNodeEnabled(w http.ResponseWriter, r *http.Request, u *models.User) {
	d, err := s.getDomainScoped(u, pathID(r, "id"))
	if err != nil || d.Runtime != "node" {
		s.err(w, http.StatusNotFound, "node app not found")
		return
	}
	req, err := decode[struct {
		Enabled bool `json:"enabled"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Enabled {
		sysUser, err := s.sysUserForPanelUser(d.UserID)
		if err != nil {
			s.fail(w, "resolve system user", err)
			return
		}
		appRoot, startup, env := s.loadNodeConfig(d.ID)
		if err := system.WriteNodeApp(*d, sysUser, appRoot, startup, env); err != nil {
			s.err(w, http.StatusBadGateway, err.Error())
			return
		}
	} else {
		system.StopNodeApp(*d)
	}
	s.json(w, map[string]bool{"ok": true})
}

func (s *Server) handleNodeNpm(w http.ResponseWriter, r *http.Request, u *models.User) {
	d, err := s.getDomainScoped(u, pathID(r, "id"))
	if err != nil || d.Runtime != "node" {
		s.err(w, http.StatusNotFound, "node app not found")
		return
	}
	sysUser, err := s.sysUserForPanelUser(d.UserID)
	if err != nil {
		s.fail(w, "resolve system user", err)
		return
	}
	appRoot, _, _ := s.loadNodeConfig(d.ID)
	out, err := system.NpmInstall(*d, sysUser, appRoot)
	if err != nil {
		s.err(w, http.StatusBadGateway, err.Error())
		return
	}
	s.json(w, map[string]string{"output": out})
}

func (s *Server) handleNodeLogs(w http.ResponseWriter, r *http.Request, u *models.User) {
	d, err := s.getDomainScoped(u, pathID(r, "id"))
	if err != nil || d.Runtime != "node" {
		s.err(w, http.StatusNotFound, "node app not found")
		return
	}
	s.json(w, map[string]string{"logs": system.NodeAppLogs(*d)})
}
