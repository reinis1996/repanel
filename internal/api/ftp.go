package api

import (
	"net/http"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/repanel/repanel/internal/models"
	"github.com/repanel/repanel/internal/system"
)

// FTP accounts are real (login-only, nologin shell) unix users homed inside
// the panel web root; ProFTPD with DefaultRoot ~ jails them automatically.

var validFTPUser = regexp.MustCompile(`^[a-z][a-z0-9_-]{2,30}$`)

func (s *Server) handleFTPList(w http.ResponseWriter, r *http.Request, u *models.User) {
	where, args := scopeWhere(u, "user_id")
	rows, err := s.DB.Query(`SELECT id, user_id, username, directory, created_at FROM ftp_accounts
		WHERE `+where+` ORDER BY username`, args...)
	if err != nil {
		s.fail(w, "list ftp accounts", err)
		return
	}
	defer rows.Close()
	out := []models.FTPAccount{}
	for rows.Next() {
		var f models.FTPAccount
		if rows.Scan(&f.ID, &f.UserID, &f.Username, &f.Directory, &f.CreatedAt) == nil {
			out = append(out, f)
		}
	}
	s.json(w, out)
}

func (s *Server) handleFTPCreate(w http.ResponseWriter, r *http.Request, u *models.User) {
	req, err := decode[struct {
		Username  string `json:"username"`
		Password  string `json:"password"`
		Directory string `json:"directory"` // relative to the user's web space
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	username := strings.ToLower(strings.TrimSpace(req.Username))
	if !validFTPUser.MatchString(username) {
		s.err(w, http.StatusBadRequest, "username must be 3-31 chars: lowercase letters, digits, - or _")
		return
	}
	if len(req.Password) < 8 {
		s.err(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	sysUser, err := s.sysUserForPanelUser(u.ID)
	if err != nil {
		s.fail(w, "resolve system user", err)
		return
	}
	base := filepath.Join(s.Cfg.WebRoot, sysUser)
	home, err := system.ResolveJailed(base, req.Directory)
	if err != nil {
		s.err(w, http.StatusBadRequest, "directory escapes your web space")
		return
	}
	res, err := s.DB.Exec(`INSERT INTO ftp_accounts(user_id,username,directory) VALUES(?,?,?)`,
		u.ID, username, home)
	if err != nil {
		s.err(w, http.StatusConflict, "ftp account already exists")
		return
	}
	if err := system.EnsureUnixUser(username, home); err != nil {
		s.DB.Exec(`DELETE FROM ftp_accounts WHERE username = ?`, username)
		s.fail(w, "create system user", err)
		return
	}
	if err := system.SetUnixPassword(username, req.Password); err != nil {
		s.fail(w, "set ftp password", err)
		return
	}
	id, _ := res.LastInsertId()
	s.json(w, models.FTPAccount{ID: id, UserID: u.ID, Username: username, Directory: home})
}

func (s *Server) ftpScoped(u *models.User, id int64) (*models.FTPAccount, bool) {
	where, args := scopeWhere(u, "user_id")
	args = append([]any{id}, args...)
	var f models.FTPAccount
	err := s.DB.QueryRow(`SELECT id, user_id, username, directory FROM ftp_accounts
		WHERE id = ? AND `+where, args...).Scan(&f.ID, &f.UserID, &f.Username, &f.Directory)
	return &f, err == nil
}

func (s *Server) handleFTPDelete(w http.ResponseWriter, r *http.Request, u *models.User) {
	f, ok := s.ftpScoped(u, pathID(r, "id"))
	if !ok {
		s.err(w, http.StatusNotFound, "ftp account not found")
		return
	}
	if err := system.DeleteUnixUser(f.Username); err != nil {
		s.fail(w, "delete system user", err)
		return
	}
	if _, err := s.DB.Exec(`DELETE FROM ftp_accounts WHERE id = ?`, f.ID); err != nil {
		s.fail(w, "delete ftp entry", err)
		return
	}
	s.json(w, map[string]bool{"ok": true})
}

func (s *Server) handleFTPPassword(w http.ResponseWriter, r *http.Request, u *models.User) {
	f, ok := s.ftpScoped(u, pathID(r, "id"))
	if !ok {
		s.err(w, http.StatusNotFound, "ftp account not found")
		return
	}
	req, err := decode[struct {
		Password string `json:"password"`
	}](r)
	if err != nil || len(req.Password) < 8 {
		s.err(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	if err := system.SetUnixPassword(f.Username, req.Password); err != nil {
		s.fail(w, "set ftp password", err)
		return
	}
	s.json(w, map[string]bool{"ok": true})
}
