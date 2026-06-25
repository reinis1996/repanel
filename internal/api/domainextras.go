package api

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/reinis1996/repanel/internal/models"
	"github.com/reinis1996/repanel/internal/system"
)

// This file holds the owner-facing per-domain extras: forwarding/redirect,
// structured PHP settings and password-protected directories.

// ---- forwarding / redirect ----

var redirectURLRe = regexp.MustCompile(`^https?://[a-zA-Z0-9.-]+(?::\d+)?(/[^\s"'<>]*)?$`)

func (s *Server) handleDomainRedirect(w http.ResponseWriter, r *http.Request, u *models.User) {
	d, err := s.getDomainScoped(u, pathID(r, "id"))
	if err != nil {
		s.err(w, http.StatusNotFound, err.Error())
		return
	}
	req, err := decode[struct {
		URL  string `json:"url"`  // empty clears the redirect
		Code int    `json:"code"` // 301 | 302
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	url := strings.TrimSpace(req.URL)
	if url != "" {
		if !redirectURLRe.MatchString(url) {
			s.err(w, http.StatusBadRequest, "enter a full http(s):// URL to forward to")
			return
		}
		// Don't let a domain redirect to itself (an infinite loop).
		if host := strings.TrimPrefix(strings.TrimPrefix(url, "https://"), "http://"); strings.HasPrefix(host, d.Name) {
			s.err(w, http.StatusBadRequest, "a domain cannot forward to itself")
			return
		}
	}
	code := req.Code
	if code != 302 {
		code = 301
	}
	if url == "" {
		code = 301
	}
	if _, err := s.DB.Exec(`UPDATE domains SET redirect_url = ?, redirect_code = ? WHERE id = ?`, url, code, d.ID); err != nil {
		s.fail(w, "update redirect", err)
		return
	}
	d.RedirectURL, d.RedirectCode = url, code
	if !d.Suspended {
		if err := s.rewriteVhost(*d); err != nil {
			s.fail(w, "rewrite vhost", err)
			return
		}
	}
	s.json(w, d)
}

// ---- alternative domains (aliases) ----

// handleDomainAliases replaces a domain's alternative hostnames (extra
// server_name / ServerAlias entries and certificate SAN hosts) and rebuilds the
// vhost. The cert is not reissued here, so new aliases are only secured after
// the owner re-runs SSL issuance.
func (s *Server) handleDomainAliases(w http.ResponseWriter, r *http.Request, u *models.User) {
	d, err := s.getDomainScoped(u, pathID(r, "id"))
	if err != nil {
		s.err(w, http.StatusNotFound, err.Error())
		return
	}
	req, err := decode[struct {
		Aliases []string `json:"aliases"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	aliases, msg := s.cleanAliases(d.UserID, d.Name, req.Aliases)
	if msg != "" {
		s.err(w, http.StatusBadRequest, msg)
		return
	}
	if _, err := s.DB.Exec(`UPDATE domains SET aliases = ? WHERE id = ?`, strings.Join(aliases, " "), d.ID); err != nil {
		s.fail(w, "update aliases", err)
		return
	}
	d.Aliases = aliases
	if !d.Suspended {
		if err := s.rewriteVhost(*d); err != nil {
			s.fail(w, "rewrite vhost", err)
			return
		}
	}
	s.json(w, d)
}

// ---- structured PHP settings ----

func (s *Server) handleDomainPHPSettingsGet(w http.ResponseWriter, r *http.Request, u *models.User) {
	d, err := s.getDomainScoped(u, pathID(r, "id"))
	if err != nil {
		s.err(w, http.StatusNotFound, err.Error())
		return
	}
	var settings models.PHPSettings
	if strings.TrimSpace(d.PHPSettings) == "" {
		// Defaults reflecting PHP's own defaults so the form starts neutral.
		settings = models.PHPSettings{UploadMaxFilesize: "128M", PostMaxSize: "128M", AllowUrlFopen: true}
	} else {
		settings = system.ParsePHPSettings(d.PHPSettings)
	}
	s.json(w, map[string]any{
		"runtime":  d.Runtime,
		"settings": settings,
	})
}

func (s *Server) handleDomainPHPSettingsSet(w http.ResponseWriter, r *http.Request, u *models.User) {
	d, err := s.getDomainScoped(u, pathID(r, "id"))
	if err != nil {
		s.err(w, http.StatusNotFound, err.Error())
		return
	}
	if d.Runtime != "php" {
		s.err(w, http.StatusBadRequest, "this domain does not run PHP")
		return
	}
	req, err := decode[models.PHPSettings](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	_, raw := system.SanitizePHPSettings(req)
	if _, err := s.DB.Exec(`UPDATE domains SET php_settings = ? WHERE id = ?`, raw, d.ID); err != nil {
		s.fail(w, "update php settings", err)
		return
	}
	d.PHPSettings = raw
	if !d.Suspended {
		if err := s.rewriteVhost(*d); err != nil {
			s.fail(w, "rewrite vhost", err)
			return
		}
	}
	s.json(w, map[string]bool{"ok": true})
}

// ---- password-protected directories ----

var protectedUserRe = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,32}$`)

func (s *Server) handleProtectedList(w http.ResponseWriter, r *http.Request, u *models.User) {
	d, err := s.getDomainScoped(u, pathID(r, "id"))
	if err != nil {
		s.err(w, http.StatusNotFound, err.Error())
		return
	}
	rows, err := s.DB.Query(`SELECT id, path, realm FROM protected_dirs WHERE domain_id = ? ORDER BY path`, d.ID)
	if err != nil {
		s.fail(w, "list protected dirs", err)
		return
	}
	// Read every directory row and close before fetching each one's users: the
	// SQLite pool has a single connection, so a nested query inside this open
	// result set would deadlock the whole panel (see usage.go).
	out := []models.ProtectedDir{}
	for rows.Next() {
		var pd models.ProtectedDir
		if rows.Scan(&pd.ID, &pd.Path, &pd.Realm) == nil {
			pd.DomainID = d.ID
			out = append(out, pd)
		}
	}
	rows.Close()
	for i := range out {
		out[i].Users = s.protectedUsernames(out[i].ID)
	}
	s.json(w, out)
}

func (s *Server) protectedUsernames(dirID int64) []string {
	out := []string{}
	rows, err := s.DB.Query(`SELECT username FROM protected_users WHERE dir_id = ? ORDER BY username`, dirID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if rows.Scan(&name) == nil {
			out = append(out, name)
		}
	}
	return out
}

func (s *Server) handleProtectedCreate(w http.ResponseWriter, r *http.Request, u *models.User) {
	d, err := s.getDomainScoped(u, pathID(r, "id"))
	if err != nil {
		s.err(w, http.StatusNotFound, err.Error())
		return
	}
	req, err := decode[struct {
		Path  string `json:"path"`
		Realm string `json:"realm"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	path := system.NormalizeProtectedPath(req.Path)
	if path == "" {
		s.err(w, http.StatusBadRequest, "enter a valid directory path, e.g. /admin")
		return
	}
	realm := strings.TrimSpace(req.Realm)
	if realm == "" {
		realm = "Restricted"
	}
	if _, err := s.DB.Exec(`INSERT INTO protected_dirs(domain_id, path, realm) VALUES(?,?,?)`, d.ID, path, realm); err != nil {
		s.err(w, http.StatusConflict, "that directory is already protected")
		return
	}
	s.json(w, map[string]bool{"ok": true})
}

// protectedDirScoped loads a protected directory the user may manage, returning
// the owning domain for vhost regeneration.
func (s *Server) protectedDirScoped(u *models.User, dirID int64) (*models.Domain, *models.ProtectedDir, bool) {
	var domainID int64
	var pd models.ProtectedDir
	if s.DB.QueryRow(`SELECT id, domain_id, path, realm FROM protected_dirs WHERE id = ?`, dirID).
		Scan(&pd.ID, &domainID, &pd.Path, &pd.Realm) != nil {
		return nil, nil, false
	}
	d, err := s.getDomainScoped(u, domainID)
	if err != nil {
		return nil, nil, false
	}
	return d, &pd, true
}

func (s *Server) handleProtectedDelete(w http.ResponseWriter, r *http.Request, u *models.User) {
	d, pd, ok := s.protectedDirScoped(u, pathID(r, "dirId"))
	if !ok {
		s.err(w, http.StatusNotFound, "protected directory not found")
		return
	}
	system.WriteHtpasswd(d.Name, pd.ID, nil) // remove the credential file
	s.DB.Exec(`DELETE FROM protected_dirs WHERE id = ?`, pd.ID)
	s.refreshDomainVhost(u, d.ID)
	s.json(w, map[string]bool{"ok": true})
}

func (s *Server) handleProtectedUserSet(w http.ResponseWriter, r *http.Request, u *models.User) {
	d, pd, ok := s.protectedDirScoped(u, pathID(r, "dirId"))
	if !ok {
		s.err(w, http.StatusNotFound, "protected directory not found")
		return
	}
	req, err := decode[struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	username := strings.TrimSpace(req.Username)
	if !protectedUserRe.MatchString(username) {
		s.err(w, http.StatusBadRequest, "username may use letters, digits, dot, dash and underscore")
		return
	}
	if len(req.Password) < 6 {
		s.err(w, http.StatusBadRequest, "password must be at least 6 characters")
		return
	}
	hash, err := system.HashHtpasswd(req.Password)
	if err != nil {
		s.fail(w, "hash htpasswd", err)
		return
	}
	if _, err := s.DB.Exec(`INSERT INTO protected_users(dir_id, username, password_hash) VALUES(?,?,?)
		ON CONFLICT(dir_id, username) DO UPDATE SET password_hash = excluded.password_hash`, pd.ID, username, hash); err != nil {
		s.fail(w, "save protected user", err)
		return
	}
	s.applyProtectedDir(u, d.ID, pd.ID)
	s.json(w, map[string]bool{"ok": true})
}

func (s *Server) handleProtectedUserDelete(w http.ResponseWriter, r *http.Request, u *models.User) {
	d, pd, ok := s.protectedDirScoped(u, pathID(r, "dirId"))
	if !ok {
		s.err(w, http.StatusNotFound, "protected directory not found")
		return
	}
	username := r.PathValue("username")
	s.DB.Exec(`DELETE FROM protected_users WHERE dir_id = ? AND username = ?`, pd.ID, username)
	s.applyProtectedDir(u, d.ID, pd.ID)
	s.json(w, map[string]bool{"ok": true})
}

// applyProtectedDir rewrites the directory's .htpasswd file from current users
// and regenerates the domain vhost (so the auth block appears/disappears with
// the user count).
func (s *Server) applyProtectedDir(u *models.User, domainID, dirID int64) {
	d, err := s.getDomainScoped(u, domainID)
	if err != nil {
		return
	}
	hashes := map[string]string{}
	rows, qerr := s.DB.Query(`SELECT username, password_hash FROM protected_users WHERE dir_id = ?`, dirID)
	if qerr == nil {
		for rows.Next() {
			var name, hash string
			if rows.Scan(&name, &hash) == nil {
				hashes[name] = hash
			}
		}
		rows.Close()
	}
	system.WriteHtpasswd(d.Name, dirID, hashes)
	if !d.Suspended {
		s.rewriteVhost(*d)
	}
}

// refreshDomainVhost reloads a domain (picking up changed protected dirs) and
// regenerates its vhost.
func (s *Server) refreshDomainVhost(u *models.User, domainID int64) {
	if d, err := s.getDomainScoped(u, domainID); err == nil && !d.Suspended {
		s.rewriteVhost(*d)
	}
}
