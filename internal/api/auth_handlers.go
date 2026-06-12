package api

import (
	"net/http"
	"strings"

	"github.com/reinis1996/repanel/internal/auth"
	"github.com/reinis1996/repanel/internal/models"
	"github.com/reinis1996/repanel/internal/system"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	req, err := decode[loginRequest](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	token, u, err := s.Auth.Login(strings.TrimSpace(req.Username), req.Password)
	if err != nil {
		s.err(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     auth.CookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   r.TLS != nil,
		MaxAge:   s.Cfg.SessionHours * 3600,
	})
	s.json(w, u)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request, _ *models.User) {
	if c, err := r.Cookie(auth.CookieName); err == nil {
		s.Auth.Logout(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: auth.CookieName, Value: "", Path: "/", MaxAge: -1})
	s.json(w, map[string]bool{"ok": true})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request, u *models.User) {
	s.json(w, u)
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request, u *models.User) {
	req, err := decode[struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}](r)
	if err != nil || len(req.New) < 8 {
		s.err(w, http.StatusBadRequest, "new password must be at least 8 characters")
		return
	}
	if !auth.CheckPassword(u.PasswordHash, req.Current) {
		s.err(w, http.StatusForbidden, "current password is incorrect")
		return
	}
	hash, err := auth.HashPassword(req.New)
	if err != nil {
		s.fail(w, "hash password", err)
		return
	}
	if _, err := s.DB.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, hash, u.ID); err != nil {
		s.fail(w, "update password", err)
		return
	}
	s.json(w, map[string]bool{"ok": true})
}

// ---- first-run setup ----

func (s *Server) adminExists() bool {
	var n int
	s.DB.QueryRow(`SELECT COUNT(*) FROM users WHERE role = 'admin'`).Scan(&n)
	return n > 0
}

func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	s.json(w, map[string]bool{"needs_setup": !s.adminExists()})
}

// handleSetup creates the initial admin account; only works while no admin
// exists (i.e. right after installation).
func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	if s.adminExists() {
		s.err(w, http.StatusForbidden, "setup already completed")
		return
	}
	req, err := decode[struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Password string `json:"password"`
		ServerIP string `json:"server_ip"`
	}](r)
	if err != nil || strings.TrimSpace(req.Username) == "" || len(req.Password) < 8 {
		s.err(w, http.StatusBadRequest, "username required and password must be at least 8 characters")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		s.fail(w, "hash password", err)
		return
	}
	if _, err := s.DB.Exec(
		`INSERT INTO users(username,email,password_hash,role) VALUES(?,?,?,'admin')`,
		strings.TrimSpace(req.Username), strings.TrimSpace(req.Email), hash); err != nil {
		s.fail(w, "create admin", err)
		return
	}
	if ip := strings.TrimSpace(req.ServerIP); ip != "" {
		s.DB.SetSetting("server_ip", ip)
	}
	s.DB.SetSetting("admin_email", strings.TrimSpace(req.Email))
	s.json(w, map[string]bool{"ok": true})
}

// ---- dashboard ----

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request, u *models.User) {
	where, args := scopeWhere(u, "user_id")
	count := func(table string) int {
		var n int
		s.DB.QueryRow(`SELECT COUNT(*) FROM `+table+` WHERE `+where, args...).Scan(&n)
		return n
	}
	var userCount int
	switch u.Role {
	case models.RoleAdmin:
		s.DB.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&userCount)
	case models.RoleReseller:
		s.DB.QueryRow(`SELECT COUNT(*) FROM users WHERE owner_id = ?`, u.ID).Scan(&userCount)
	}
	resp := map[string]any{
		"system":    system.Info(s.Version),
		"domains":   count("domains"),
		"mailboxes": s.countMailboxes(u),
		"databases": count("db_entries"),
		"ftp":       count("ftp_accounts"),
		"users":     userCount,
	}
	if u.Role == models.RoleAdmin {
		resp["services"] = system.ServiceList()
	}
	s.json(w, resp)
}

func (s *Server) countMailboxes(u *models.User) int {
	var n int
	if u.Role == models.RoleAdmin {
		s.DB.QueryRow(`SELECT COUNT(*) FROM mailboxes`).Scan(&n)
	} else {
		where, args := scopeWhere(u, "d.user_id")
		s.DB.QueryRow(`SELECT COUNT(*) FROM mailboxes m JOIN domains d ON d.id = m.domain_id WHERE `+where, args...).Scan(&n)
	}
	return n
}
