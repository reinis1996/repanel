package api

import (
	"net/http"
	"strings"

	"github.com/reinis1996/repanel/internal/models"
	"github.com/reinis1996/repanel/internal/system"
)

// rebuildMail regenerates all postfix/dovecot maps from database state.
func (s *Server) rebuildMail() error {
	domains := []string{}
	rows, err := s.DB.Query(`SELECT DISTINCT d.name FROM domains d
		WHERE EXISTS (SELECT 1 FROM mailboxes m WHERE m.domain_id = d.id)
		   OR EXISTS (SELECT 1 FROM mail_aliases a WHERE a.domain_id = d.id)`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var name string
		if rows.Scan(&name) == nil {
			domains = append(domains, name)
		}
	}
	rows.Close()

	boxes := []models.Mailbox{}
	rows, err = s.DB.Query(`SELECT id, domain_id, address, password_hash, quota_mb, created_at FROM mailboxes`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var m models.Mailbox
		if rows.Scan(&m.ID, &m.DomainID, &m.Address, &m.PasswordHash, &m.QuotaMB, &m.CreatedAt) == nil {
			boxes = append(boxes, m)
		}
	}
	rows.Close()

	aliases := []models.MailAlias{}
	rows, err = s.DB.Query(`SELECT id, domain_id, source, destination FROM mail_aliases`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var a models.MailAlias
		if rows.Scan(&a.ID, &a.DomainID, &a.Source, &a.Destination) == nil {
			aliases = append(aliases, a)
		}
	}
	rows.Close()

	return system.RebuildMailMaps(s.Cfg.MailDir, domains, boxes, aliases)
}

func (s *Server) handleMailList(w http.ResponseWriter, r *http.Request, u *models.User) {
	where, args := scopeWhere(u, "d.user_id")

	boxes := []models.Mailbox{}
	rows, err := s.DB.Query(`SELECT m.id, m.domain_id, m.address, m.quota_mb, m.created_at
		FROM mailboxes m JOIN domains d ON d.id = m.domain_id WHERE `+where+` ORDER BY m.address`, args...)
	if err != nil {
		s.fail(w, "list mailboxes", err)
		return
	}
	for rows.Next() {
		var m models.Mailbox
		if rows.Scan(&m.ID, &m.DomainID, &m.Address, &m.QuotaMB, &m.CreatedAt) == nil {
			boxes = append(boxes, m)
		}
	}
	rows.Close()

	aliases := []models.MailAlias{}
	rows, err = s.DB.Query(`SELECT a.id, a.domain_id, a.source, a.destination
		FROM mail_aliases a JOIN domains d ON d.id = a.domain_id WHERE `+where+` ORDER BY a.source`, args...)
	if err != nil {
		s.fail(w, "list aliases", err)
		return
	}
	for rows.Next() {
		var a models.MailAlias
		if rows.Scan(&a.ID, &a.DomainID, &a.Source, &a.Destination) == nil {
			aliases = append(aliases, a)
		}
	}
	rows.Close()

	s.json(w, map[string]any{"mailboxes": boxes, "aliases": aliases})
}

// domainScopedByName returns the domain when owned/manageable by the user.
func (s *Server) domainScopedByName(u *models.User, name string) (*models.Domain, bool) {
	where, args := scopeWhere(u, "user_id")
	args = append([]any{name}, args...)
	var d models.Domain
	err := s.DB.QueryRow(`SELECT id, user_id, name, document_root, php_version FROM domains
		WHERE name = ? AND `+where, args...).
		Scan(&d.ID, &d.UserID, &d.Name, &d.DocumentRoot, &d.PHPVersion)
	return &d, err == nil
}

func (s *Server) handleMailboxCreate(w http.ResponseWriter, r *http.Request, u *models.User) {
	if s.quotaExceeded(u) {
		s.err(w, http.StatusForbidden, quotaMsg)
		return
	}
	req, err := decode[struct {
		Address  string `json:"address"` // user@domain
		Password string `json:"password"`
		QuotaMB  int    `json:"quota_mb"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	addr := strings.ToLower(strings.TrimSpace(req.Address))
	local, domainName, ok := strings.Cut(addr, "@")
	if !ok || local == "" || !validDomainName(domainName) || strings.ContainsAny(local, " \t\n:") {
		s.err(w, http.StatusBadRequest, "invalid mail address")
		return
	}
	if len(req.Password) < 8 {
		s.err(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	d, ok := s.domainScopedByName(u, domainName)
	if !ok {
		s.err(w, http.StatusNotFound, "domain not found in your account")
		return
	}
	hash, err := system.HashMailPassword(req.Password)
	if err != nil {
		s.fail(w, "hash mail password", err)
		return
	}
	quota := req.QuotaMB
	if quota <= 0 {
		quota = 1024
	}
	res, err := s.DB.Exec(`INSERT INTO mailboxes(domain_id,address,password_hash,quota_mb) VALUES(?,?,?,?)`,
		d.ID, addr, hash, quota)
	if err != nil {
		s.err(w, http.StatusConflict, "mailbox already exists")
		return
	}
	if err := system.EnsureMaildir(addr); err != nil {
		s.fail(w, "create maildir", err)
		return
	}
	if err := s.rebuildMail(); err != nil {
		s.fail(w, "rebuild mail maps", err)
		return
	}
	id, _ := res.LastInsertId()
	s.json(w, models.Mailbox{ID: id, DomainID: d.ID, Address: addr, QuotaMB: quota})
}

// mailboxScoped verifies ownership through the parent domain.
func (s *Server) mailboxScoped(u *models.User, id int64) (string, bool) {
	where, args := scopeWhere(u, "d.user_id")
	args = append([]any{id}, args...)
	var addr string
	err := s.DB.QueryRow(`SELECT m.address FROM mailboxes m JOIN domains d ON d.id = m.domain_id
		WHERE m.id = ? AND `+where, args...).Scan(&addr)
	return addr, err == nil
}

func (s *Server) handleMailboxPassword(w http.ResponseWriter, r *http.Request, u *models.User) {
	id := pathID(r, "id")
	if _, ok := s.mailboxScoped(u, id); !ok {
		s.err(w, http.StatusNotFound, "mailbox not found")
		return
	}
	req, err := decode[struct {
		Password string `json:"password"`
	}](r)
	if err != nil || len(req.Password) < 8 {
		s.err(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	hash, err := system.HashMailPassword(req.Password)
	if err != nil {
		s.fail(w, "hash mail password", err)
		return
	}
	if _, err := s.DB.Exec(`UPDATE mailboxes SET password_hash = ? WHERE id = ?`, hash, id); err != nil {
		s.fail(w, "update mailbox", err)
		return
	}
	if err := s.rebuildMail(); err != nil {
		s.fail(w, "rebuild mail maps", err)
		return
	}
	s.json(w, map[string]bool{"ok": true})
}

func (s *Server) handleMailboxDelete(w http.ResponseWriter, r *http.Request, u *models.User) {
	id := pathID(r, "id")
	if _, ok := s.mailboxScoped(u, id); !ok {
		s.err(w, http.StatusNotFound, "mailbox not found")
		return
	}
	if _, err := s.DB.Exec(`DELETE FROM mailboxes WHERE id = ?`, id); err != nil {
		s.fail(w, "delete mailbox", err)
		return
	}
	if err := s.rebuildMail(); err != nil {
		s.fail(w, "rebuild mail maps", err)
		return
	}
	s.json(w, map[string]bool{"ok": true})
	// Maildir content is preserved on disk, consistent with domain deletion.
}

func (s *Server) handleAliasCreate(w http.ResponseWriter, r *http.Request, u *models.User) {
	req, err := decode[struct {
		Source      string `json:"source"`
		Destination string `json:"destination"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	source := strings.ToLower(strings.TrimSpace(req.Source))
	dest := strings.ToLower(strings.TrimSpace(req.Destination))
	local, domainName, ok := strings.Cut(source, "@")
	if !ok || local == "" || !strings.Contains(dest, "@") ||
		strings.ContainsAny(source+dest, " \t\n") {
		s.err(w, http.StatusBadRequest, "invalid alias")
		return
	}
	d, ok := s.domainScopedByName(u, domainName)
	if !ok {
		s.err(w, http.StatusNotFound, "domain not found in your account")
		return
	}
	res, err := s.DB.Exec(`INSERT INTO mail_aliases(domain_id,source,destination) VALUES(?,?,?)`,
		d.ID, source, dest)
	if err != nil {
		s.err(w, http.StatusConflict, "alias already exists")
		return
	}
	if err := s.rebuildMail(); err != nil {
		s.fail(w, "rebuild mail maps", err)
		return
	}
	id, _ := res.LastInsertId()
	s.json(w, models.MailAlias{ID: id, DomainID: d.ID, Source: source, Destination: dest})
}

func (s *Server) handleAliasDelete(w http.ResponseWriter, r *http.Request, u *models.User) {
	id := pathID(r, "id")
	where, args := scopeWhere(u, "d.user_id")
	args = append([]any{id}, args...)
	var aliasID int64
	if err := s.DB.QueryRow(`SELECT a.id FROM mail_aliases a JOIN domains d ON d.id = a.domain_id
		WHERE a.id = ? AND `+where, args...).Scan(&aliasID); err != nil {
		s.err(w, http.StatusNotFound, "alias not found")
		return
	}
	if _, err := s.DB.Exec(`DELETE FROM mail_aliases WHERE id = ?`, id); err != nil {
		s.fail(w, "delete alias", err)
		return
	}
	if err := s.rebuildMail(); err != nil {
		s.fail(w, "rebuild mail maps", err)
		return
	}
	s.json(w, map[string]bool{"ok": true})
}
