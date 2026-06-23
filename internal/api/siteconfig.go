package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/reinis1996/repanel/internal/models"
	"github.com/reinis1996/repanel/internal/system"
)

// Per-site configuration editor (admin-only). Admins edit "additional directives"
// blocks that RePanel merges into the generated nginx server block, Apache vhost
// and PHP-FPM pool on every rebuild — so the edits survive SSL/PHP/suspend
// regeneration, unlike hand-editing the generated files (which the panel
// overwrites). Saving validates the result (nginx -t / apachectl -t / php-fpm -t)
// and rolls back to the previous blocks if the new config is rejected.

const maxConfigBlock = 32 * 1024 // generous ceiling for a per-site directive block

// siteConfigResp is the payload for the per-site config editor.
type siteConfigResp struct {
	// Editable override blocks (current values).
	NginxConf  string `json:"nginx_conf"`
	ApacheConf string `json:"apache_conf"`
	PHPConf    string `json:"php_conf"`
	// Which subsystems are active for this domain (stack/mode/runtime dependent).
	NginxActive  bool   `json:"nginx_active"`
	ApacheActive bool   `json:"apache_active"`
	PHPActive    bool   `json:"php_active"`
	Runtime      string `json:"runtime"`
	// Rendered, read-only views of the current on-disk config.
	Rendered struct {
		Nginx  string `json:"nginx"`
		Apache string `json:"apache"`
		PHP    string `json:"php"`
		Mail   string `json:"mail"`
	} `json:"rendered"`
}

func (s *Server) handleDomainConfigGet(w http.ResponseWriter, r *http.Request, u *models.User) {
	d, err := s.getDomainScoped(u, pathID(r, "id"))
	if err != nil {
		s.err(w, http.StatusNotFound, err.Error())
		return
	}
	ws := s.webServer()
	var resp siteConfigResp
	resp.NginxConf, resp.ApacheConf, resp.PHPConf = d.NginxConf, d.ApacheConf, d.PHPConf
	resp.NginxActive, resp.ApacheActive, resp.PHPActive = ws.Subsystems(*d)
	resp.Runtime = d.Runtime
	resp.Rendered.Nginx = system.ReadNginxVhost(s.Cfg.NginxDir, d.Name)
	resp.Rendered.Apache = system.ReadApacheVhost(s.Cfg.ApacheDir, d.Name)
	resp.Rendered.PHP = system.ReadPHPPool(*d)
	resp.Rendered.Mail = system.MailConfigForDomain(s.Cfg.MailDir, d.Name)
	s.json(w, resp)
}

func (s *Server) handleDomainConfigSet(w http.ResponseWriter, r *http.Request, u *models.User) {
	d, err := s.getDomainScoped(u, pathID(r, "id"))
	if err != nil {
		s.err(w, http.StatusNotFound, err.Error())
		return
	}
	req, err := decode[struct {
		NginxConf  string `json:"nginx_conf"`
		ApacheConf string `json:"apache_conf"`
		PHPConf    string `json:"php_conf"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	for _, block := range []struct {
		label, val string
	}{{"nginx", req.NginxConf}, {"Apache", req.ApacheConf}, {"PHP", req.PHPConf}} {
		if len(block.val) > maxConfigBlock {
			s.err(w, http.StatusBadRequest, fmt.Sprintf("%s config exceeds %d KB", block.label, maxConfigBlock/1024))
			return
		}
		if strings.ContainsRune(block.val, 0) {
			s.err(w, http.StatusBadRequest, "config may not contain null bytes")
			return
		}
	}

	// Remember the previous blocks so we can roll back a config the server rejects.
	oldNginx, oldApache, oldPHP := d.NginxConf, d.ApacheConf, d.PHPConf
	apply := func(nginxConf, apacheConf, phpConf string) error {
		if _, err := s.DB.Exec(`UPDATE domains SET nginx_conf = ?, apache_conf = ?, php_conf = ? WHERE id = ?`,
			nginxConf, apacheConf, phpConf, d.ID); err != nil {
			return err
		}
		d.NginxConf, d.ApacheConf, d.PHPConf = nginxConf, apacheConf, phpConf
		// A suspended site serves a 503; its real vhost is regenerated on unsuspend,
		// so only persist the blocks now.
		if d.Suspended {
			return nil
		}
		return s.rewriteVhost(*d)
	}

	if err := apply(req.NginxConf, req.ApacheConf, req.PHPConf); err != nil {
		// Roll back to the last-known-good blocks and restore the working config.
		if rbErr := apply(oldNginx, oldApache, oldPHP); rbErr != nil {
			s.fail(w, "restore site config", rbErr)
			return
		}
		s.err(w, http.StatusBadGateway, "configuration rejected, reverted: "+err.Error())
		return
	}
	s.json(w, map[string]bool{"ok": true})
}
