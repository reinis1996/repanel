package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/reinis1996/repanel/internal/auth"
	"github.com/reinis1996/repanel/internal/models"
	"github.com/reinis1996/repanel/internal/system"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Code     string `json:"code"` // TOTP or recovery code, when 2FA is enabled
}

// setSessionCookie writes the session cookie for a freshly issued token.
func (s *Server) setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     auth.CookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   r.TLS != nil,
		MaxAge:   s.Cfg.SessionHours * 3600,
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if ok, retry := s.login.allowed(ip); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
		s.err(w, http.StatusTooManyRequests, "too many failed attempts; try again later")
		return
	}
	req, err := decode[loginRequest](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	username := strings.TrimSpace(req.Username)
	u, err := s.Auth.VerifyPassword(username, req.Password)
	if err != nil {
		s.login.fail(ip)
		s.audit(0, username, "login.failed", "bad password", ip)
		s.err(w, http.StatusUnauthorized, "invalid username or password")
		return
	}

	// Second factor.
	if u.TOTPEnabled {
		code := strings.TrimSpace(req.Code)
		if code == "" {
			// Password was correct; ask the client for the 2FA code.
			s.json(w, map[string]bool{"totp_required": true})
			return
		}
		if !s.verifySecondFactor(u.ID, code) {
			s.login.fail(ip)
			s.audit(u.ID, u.Username, "login.failed", "bad 2FA code", ip)
			w.WriteHeader(http.StatusUnauthorized)
			s.json(w, map[string]any{"error": "invalid authentication code", "totp_required": true})
			return
		}
	}

	s.login.success(ip)
	token, err := s.Auth.CreateSession(u.ID, 0, "")
	if err != nil {
		s.fail(w, "create session", err)
		return
	}
	s.setSessionCookie(w, r, token)
	s.audit(u.ID, u.Username, "login", "", ip)
	s.json(w, withEffectivePerms(u))
}

// verifySecondFactor accepts either a valid TOTP code or an unused recovery code
// (which it then consumes).
func (s *Server) verifySecondFactor(userID int64, code string) bool {
	var secret, recovery string
	if s.DB.QueryRow(`SELECT totp_secret, recovery_codes FROM users WHERE id = ?`, userID).
		Scan(&secret, &recovery) != nil {
		return false
	}
	if auth.TOTPValidate(secret, code) {
		return true
	}
	if updated, ok := auth.CheckRecoveryCode(recovery, code); ok {
		s.DB.Exec(`UPDATE users SET recovery_codes = ? WHERE id = ?`, updated, userID)
		return true
	}
	return false
}

// withEffectivePerms returns a copy of the user whose Permissions reflect what
// the client may access — admins implicitly hold every module.
func withEffectivePerms(u *models.User) models.User {
	out := *u
	if u.Role == models.RoleAdmin {
		out.Permissions = models.AllModules
	}
	return out
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request, u *models.User) {
	if c, err := r.Cookie(auth.CookieName); err == nil {
		s.Auth.Logout(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: auth.CookieName, Value: "", Path: "/", MaxAge: -1})
	s.audit(u.ID, u.Username, "logout", "", clientIP(r))
	s.json(w, map[string]bool{"ok": true})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request, u *models.User) {
	resp := withEffectivePerms(u)
	// Surface impersonation so the UI can show a "stop impersonating" banner.
	if c, err := r.Cookie(auth.CookieName); err == nil {
		if impID, _, ok := s.Auth.SessionMeta(c.Value); ok && impID != 0 {
			if imp, _ := auth.GetUserByID(s.DB, impID); imp != nil {
				resp.Impersonator = imp.Username
			}
		}
	}
	s.json(w, resp)
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
	// Invalidate all other sessions for this user; keep the current one so the
	// caller stays logged in (SECURITY_AUDIT F-17).
	current := ""
	if c, err := r.Cookie(auth.CookieName); err == nil {
		current = c.Value
	}
	s.DB.Exec(`DELETE FROM sessions WHERE user_id = ? AND token != ?`, u.ID, current)
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
	// Serialize so two concurrent setup requests can't both create an admin.
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
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
		"domains":   count("domains"),
		"mailboxes": s.countMailboxes(u),
		"databases": count("db_entries"),
		"ftp":       count("ftp_accounts"),
		"users":     userCount,
	}
	// Host-level health and service status are only exposed to admins.
	if u.Role == models.RoleAdmin {
		resp["system"] = system.Info(s.Version)
		resp["services"] = s.serviceList()
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
