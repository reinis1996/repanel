package api

import (
	"net/http"
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
	case "self-signed":
		issuer = "self-signed"
		certPath, keyPath, notAfter, err = system.IssueSelfSigned(s.Cfg.DataDir, d.Name)
	default:
		s.err(w, http.StatusBadRequest, "method must be letsencrypt or self-signed")
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
