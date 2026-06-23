package api

import (
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/reinis1996/repanel/internal/models"
	"github.com/reinis1996/repanel/internal/system"
)

func (s *Server) handleCertList(w http.ResponseWriter, r *http.Request, u *models.User) {
	where, args := scopeWhere(u, "d.user_id")
	rows, err := s.DB.Query(`SELECT c.id, c.domain_id, c.domain, c.issuer, c.not_after,
		c.cert_path, c.key_path, c.created_at
		FROM certificates c JOIN domains d ON d.id = c.domain_id
		WHERE `+where+` ORDER BY c.domain`, args...)
	if err != nil {
		s.fail(w, "list certificates", err)
		return
	}
	defer rows.Close()
	out := []models.Certificate{}
	for rows.Next() {
		var c models.Certificate
		if rows.Scan(&c.ID, &c.DomainID, &c.Domain, &c.Issuer, &c.NotAfter,
			&c.CertPath, &c.KeyPath, &c.CreatedAt) == nil {
			out = append(out, c)
		}
	}
	s.json(w, out)
}

func (s *Server) handleCertIssue(w http.ResponseWriter, r *http.Request, u *models.User) {
	req, err := decode[struct {
		DomainID int64  `json:"domain_id"`
		Method   string `json:"method"` // letsencrypt | self-signed
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	d, err := s.getDomainScoped(u, req.DomainID)
	if err != nil {
		s.err(w, http.StatusNotFound, "domain not found")
		return
	}

	var certPath, keyPath, issuer string
	var notAfter time.Time
	switch req.Method {
	case "letsencrypt":
		issuer = "letsencrypt"
		certPath, keyPath, err = system.IssueLetsEncrypt(s.Cfg.DataDir, d.Name, d.DocumentRoot,
			s.DB.Setting("admin_email"))
		if err == nil {
			notAfter, _ = system.CertExpiry(certPath)
		}
	case "letsencrypt-dns":
		// Wildcard via DNS-01, automated through the domain's RePanel-hosted zone.
		var zoneID int64
		if s.DB.QueryRow(`SELECT id FROM dns_zones WHERE name = ?`, d.Name).Scan(&zoneID) != nil {
			s.err(w, http.StatusBadRequest, "a wildcard certificate needs this domain's DNS hosted on RePanel (no managed zone found)")
			return
		}
		self, exErr := os.Executable()
		if exErr != nil {
			s.fail(w, "locate panel binary", exErr)
			return
		}
		issuer = "letsencrypt"
		certPath, keyPath, err = system.IssueLetsEncryptDNS(self, s.ConfigPath, s.DB.Setting("admin_email"), d.Name)
		if err == nil {
			notAfter, _ = system.CertExpiry(certPath)
		}
	case "self-signed":
		issuer = "self-signed"
		certPath, keyPath, notAfter, err = system.IssueSelfSigned(s.Cfg.DataDir, d.Name)
	default:
		s.err(w, http.StatusBadRequest, "unknown issuance method")
		return
	}
	if err != nil {
		s.fail(w, "issue certificate", err)
		return
	}

	res, err := s.DB.Exec(`INSERT INTO certificates(domain_id,domain,issuer,not_after,cert_path,key_path)
		VALUES(?,?,?,?,?,?)`, d.ID, d.Name, issuer, notAfter, certPath, keyPath)
	if err != nil {
		s.fail(w, "store certificate", err)
		return
	}
	// Enable SSL on the domain and regenerate the vhost.
	if _, err := s.DB.Exec(`UPDATE domains SET ssl = 1 WHERE id = ?`, d.ID); err != nil {
		s.fail(w, "enable ssl", err)
		return
	}
	d.SSL = true
	if err := s.rewriteVhost(*d); err != nil {
		s.fail(w, "rewrite vhost", err)
		return
	}
	id, _ := res.LastInsertId()
	s.json(w, models.Certificate{ID: id, DomainID: d.ID, Domain: d.Name, Issuer: issuer,
		NotAfter: notAfter, CertPath: certPath, KeyPath: keyPath})
}

func (s *Server) handleCertDelete(w http.ResponseWriter, r *http.Request, u *models.User) {
	id := pathID(r, "id")
	where, args := scopeWhere(u, "d.user_id")
	args = append([]any{id}, args...)
	var domainID int64
	if err := s.DB.QueryRow(`SELECT c.domain_id FROM certificates c JOIN domains d ON d.id = c.domain_id
		WHERE c.id = ? AND `+where, args...).Scan(&domainID); err != nil {
		s.err(w, http.StatusNotFound, "certificate not found")
		return
	}
	if _, err := s.DB.Exec(`DELETE FROM certificates WHERE id = ?`, id); err != nil {
		s.fail(w, "delete certificate", err)
		return
	}
	// If that was the last certificate, turn SSL off and rewrite the vhost.
	var remaining int
	s.DB.QueryRow(`SELECT COUNT(*) FROM certificates WHERE domain_id = ?`, domainID).Scan(&remaining)
	if remaining == 0 {
		s.DB.Exec(`UPDATE domains SET ssl = 0 WHERE id = ?`, domainID)
	}
	if d, err := s.getDomainScoped(u, domainID); err == nil && !d.Suspended {
		s.rewriteVhost(*d)
	}
	s.json(w, map[string]bool{"ok": true})
}

// handleCertUpload stores an admin-supplied certificate/key pair for a domain and
// enables HTTPS with it.
func (s *Server) handleCertUpload(w http.ResponseWriter, r *http.Request, u *models.User) {
	req, err := decode[struct {
		DomainID int64  `json:"domain_id"`
		Cert     string `json:"cert"`
		Key      string `json:"key"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	d, err := s.getDomainScoped(u, req.DomainID)
	if err != nil {
		s.err(w, http.StatusNotFound, "domain not found")
		return
	}
	certPath, keyPath, notAfter, err := system.SaveCustomCert(s.Cfg.DataDir, d.Name, req.Cert, req.Key)
	if err != nil {
		s.err(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := s.DB.Exec(`INSERT INTO certificates(domain_id,domain,issuer,not_after,cert_path,key_path)
		VALUES(?,?,?,?,?,?)`, d.ID, d.Name, "custom", notAfter, certPath, keyPath)
	if err != nil {
		s.fail(w, "store certificate", err)
		return
	}
	s.DB.Exec(`UPDATE domains SET ssl = 1 WHERE id = ?`, d.ID)
	d.SSL = true
	if err := s.rewriteVhost(*d); err != nil {
		s.fail(w, "rewrite vhost", err)
		return
	}
	id, _ := res.LastInsertId()
	s.json(w, models.Certificate{ID: id, DomainID: d.ID, Domain: d.Name, Issuer: "custom",
		NotAfter: notAfter, CertPath: certPath, KeyPath: keyPath})
}

// handlePanelCertStatus reports the control panel's current TLS certificate.
func (s *Server) handlePanelCertStatus(w http.ResponseWriter, r *http.Request, u *models.User) {
	out := map[string]any{
		"hostname":   strings.TrimSpace(s.DB.Setting("panel_hostname")),
		"issuer":     s.DB.Setting("panel_cert_issuer"),
		"configured": s.Cfg.TLSCert != "",
	}
	if s.Cfg.TLSCert != "" {
		if exp, err := system.CertExpiry(s.Cfg.TLSCert); err == nil {
			out["not_after"] = exp
		}
	}
	s.json(w, out)
}

// handlePanelCert obtains a certificate for the panel's own hostname and points
// the panel's HTTPS listener at it. Admin only; the panel restarts to load it.
// Methods: letsencrypt (HTTP-01) | custom (paste cert+key) | self-signed.
func (s *Server) handlePanelCert(w http.ResponseWriter, r *http.Request, u *models.User) {
	req, err := decode[struct {
		Method string `json:"method"`
		Cert   string `json:"cert"`
		Key    string `json:"key"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	host := strings.TrimSpace(s.DB.Setting("panel_hostname"))
	if host == "" {
		s.err(w, http.StatusBadRequest, "set the panel hostname in Settings first")
		return
	}

	var certPath, keyPath, issuer string
	switch req.Method {
	case "letsencrypt":
		// HTTP-01 for the panel host needs nginx on :80 to answer the challenge.
		ws := s.webServer()
		if ws.FrontIsApache() {
			s.err(w, http.StatusBadRequest, "Let's Encrypt for the panel needs the nginx web server; upload a certificate instead")
			return
		}
		var exists int
		if s.DB.QueryRow(`SELECT 1 FROM domains WHERE name = ?`, host).Scan(&exists) == nil {
			s.err(w, http.StatusBadRequest, "the panel hostname is also a website — issue its certificate on the SSL page and assign it to the panel")
			return
		}
		if err := ws.WritePanelACMEVhost(host); err != nil {
			s.fail(w, "prepare acme vhost", err)
			return
		}
		issuer = "letsencrypt"
		certPath, keyPath, err = system.IssueLetsEncryptHosts(system.PanelACMEWebroot(), s.DB.Setting("admin_email"), host)
	case "custom":
		issuer = "custom"
		certPath, keyPath, _, err = system.SaveCustomCert(s.Cfg.DataDir, host, req.Cert, req.Key)
	case "self-signed":
		issuer = "self-signed"
		certPath, keyPath, _, err = system.IssueSelfSigned(s.Cfg.DataDir, host)
	default:
		s.err(w, http.StatusBadRequest, "method must be letsencrypt, custom or self-signed")
		return
	}
	if err != nil {
		s.err(w, http.StatusBadGateway, err.Error())
		return
	}
	if err := system.ApplyPanelCert(s.ConfigPath, certPath, keyPath); err != nil {
		s.fail(w, "apply panel certificate", err)
		return
	}
	s.DB.SetSetting("panel_cert_issuer", issuer)
	s.json(w, map[string]any{"ok": true, "restarting": true})
	go func() { time.Sleep(time.Second); system.RestartPanel() }()
}

// validCertService is the set of server-wide services a certificate can secure.
var validCertService = map[string]bool{"mail": true, "ftp": true, "panel": true}

// handleCertAssignments reports which certificate (if any) secures each service.
func (s *Server) handleCertAssignments(w http.ResponseWriter, r *http.Request, u *models.User) {
	out := map[string]int64{}
	for svc := range validCertService {
		var id int64
		s.DB.QueryRow(`SELECT value FROM settings WHERE key = ?`, "cert_"+svc).Scan(&id)
		out[svc] = id
	}
	s.json(w, out)
}

// handleCertAssign points a server-wide service (mail / ftp / panel) at a
// certificate and reloads it. Admin only; the panel restarts to apply its own.
func (s *Server) handleCertAssign(w http.ResponseWriter, r *http.Request, u *models.User) {
	req, err := decode[struct {
		CertID  int64  `json:"cert_id"`
		Service string `json:"service"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !validCertService[req.Service] {
		s.err(w, http.StatusBadRequest, "service must be mail, ftp or panel")
		return
	}
	var certPath, keyPath string
	if err := s.DB.QueryRow(`SELECT cert_path, key_path FROM certificates WHERE id = ?`, req.CertID).
		Scan(&certPath, &keyPath); err != nil {
		s.err(w, http.StatusNotFound, "certificate not found")
		return
	}
	switch req.Service {
	case "mail":
		err = system.ApplyMailCert(certPath, keyPath)
	case "ftp":
		err = system.ApplyFTPCert(certPath, keyPath)
	case "panel":
		err = system.ApplyPanelCert(s.ConfigPath, certPath, keyPath)
	}
	if err != nil {
		s.err(w, http.StatusBadGateway, err.Error())
		return
	}
	s.DB.SetSetting("cert_"+req.Service, strconv.FormatInt(req.CertID, 10))
	s.json(w, map[string]any{"ok": true, "service": req.Service, "restarting": req.Service == "panel"})
	if req.Service == "panel" {
		go func() { time.Sleep(time.Second); system.RestartPanel() }()
	}
}
