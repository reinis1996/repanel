package api

import (
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/reinis1996/repanel/internal/auth"
	"github.com/reinis1996/repanel/internal/models"
	"github.com/reinis1996/repanel/internal/system"
)

const totpIssuer = "RePanel"

// audit records a security-relevant event. Best-effort: a logging failure must
// never block the action it describes.
func (s *Server) audit(userID int64, username, action, detail, ip string) {
	s.DB.Exec(`INSERT INTO audit_log(user_id,username,action,detail,ip) VALUES(?,?,?,?,?)`,
		userID, username, action, detail, ip)
}

// PruneAudit trims the audit trail to the most recent ~180 days. Called from the
// hourly housekeeping loop.
func (s *Server) PruneAudit() {
	s.DB.Exec(`DELETE FROM audit_log WHERE created_at < datetime('now','-180 days')`)
}

// ---- Two-factor authentication (self-service) -------------------------------

func (s *Server) handle2FASetup(w http.ResponseWriter, r *http.Request, u *models.User) {
	if u.TOTPEnabled {
		s.err(w, http.StatusConflict, "two-factor authentication is already enabled")
		return
	}
	secret, err := auth.GenerateTOTPSecret()
	if err != nil {
		s.fail(w, "generate 2fa secret", err)
		return
	}
	// Stored but not active until the user confirms a code (totp_enabled stays 0).
	if _, err := s.DB.Exec(`UPDATE users SET totp_secret = ? WHERE id = ?`, secret, u.ID); err != nil {
		s.fail(w, "store 2fa secret", err)
		return
	}
	s.json(w, map[string]string{"secret": secret, "uri": auth.TOTPURI(secret, u.Username, totpIssuer)})
}

func (s *Server) handle2FAEnable(w http.ResponseWriter, r *http.Request, u *models.User) {
	req, err := decode[struct {
		Code string `json:"code"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var secret string
	s.DB.QueryRow(`SELECT totp_secret FROM users WHERE id = ?`, u.ID).Scan(&secret)
	if secret == "" {
		s.err(w, http.StatusBadRequest, "start setup first")
		return
	}
	if !auth.TOTPValidate(secret, req.Code) {
		s.err(w, http.StatusBadRequest, "that code is incorrect — check your authenticator and try again")
		return
	}
	codes, stored, err := auth.GenerateRecoveryCodes(10)
	if err != nil {
		s.fail(w, "generate recovery codes", err)
		return
	}
	if _, err := s.DB.Exec(`UPDATE users SET totp_enabled = 1, recovery_codes = ? WHERE id = ?`, stored, u.ID); err != nil {
		s.fail(w, "enable 2fa", err)
		return
	}
	s.audit(u.ID, u.Username, "2fa.enable", "", clientIP(r))
	s.json(w, map[string]any{"ok": true, "recovery_codes": codes})
}

func (s *Server) handle2FADisable(w http.ResponseWriter, r *http.Request, u *models.User) {
	req, err := decode[struct {
		Password string `json:"password"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// Re-authenticate before removing a security control.
	if !auth.CheckPassword(u.PasswordHash, req.Password) {
		s.err(w, http.StatusForbidden, "password is incorrect")
		return
	}
	if _, err := s.DB.Exec(`UPDATE users SET totp_enabled = 0, totp_secret = '', recovery_codes = '' WHERE id = ?`, u.ID); err != nil {
		s.fail(w, "disable 2fa", err)
		return
	}
	s.audit(u.ID, u.Username, "2fa.disable", "", clientIP(r))
	s.json(w, map[string]bool{"ok": true})
}

// ---- Impersonation ----------------------------------------------------------

func (s *Server) handleImpersonate(w http.ResponseWriter, r *http.Request, u *models.User) {
	if !isAdminish(u) {
		s.err(w, http.StatusForbidden, "insufficient privileges")
		return
	}
	targetID := pathID(r, "id")
	if targetID == u.ID {
		s.err(w, http.StatusBadRequest, "cannot impersonate yourself")
		return
	}
	target, ok := s.userScopedForManage(u, targetID)
	if !ok {
		s.err(w, http.StatusForbidden, "you cannot impersonate that account")
		return
	}
	parent := ""
	if c, err := r.Cookie(auth.CookieName); err == nil {
		parent = c.Value
	}
	token, err := s.Auth.CreateSession(target.ID, u.ID, parent)
	if err != nil {
		s.fail(w, "create impersonation session", err)
		return
	}
	s.setSessionCookie(w, r, token)
	s.audit(u.ID, u.Username, "impersonate.start", "as "+target.Username, clientIP(r))
	s.json(w, map[string]any{"ok": true, "username": target.Username})
}

func (s *Server) handleStopImpersonate(w http.ResponseWriter, r *http.Request, u *models.User) {
	c, err := r.Cookie(auth.CookieName)
	if err != nil {
		s.err(w, http.StatusBadRequest, "no session")
		return
	}
	impID, parent, ok := s.Auth.SessionMeta(c.Value)
	if !ok || impID == 0 || parent == "" {
		s.err(w, http.StatusBadRequest, "not impersonating")
		return
	}
	// Return to the admin session and discard the impersonation session.
	s.setSessionCookie(w, r, parent)
	s.Auth.Logout(c.Value)
	if imp, _ := auth.GetUserByID(s.DB, impID); imp != nil {
		s.audit(imp.ID, imp.Username, "impersonate.stop", "was "+u.Username, clientIP(r))
	}
	s.json(w, map[string]bool{"ok": true})
}

// ---- Per-account SSH access (admin) -----------------------------------------

func (s *Server) handleSSHGet(w http.ResponseWriter, r *http.Request, u *models.User) {
	target, ok := s.userScopedForManage(u, pathID(r, "id"))
	if !ok {
		s.err(w, http.StatusNotFound, "user not found")
		return
	}
	var enabled int
	var keys string
	s.DB.QueryRow(`SELECT ssh_enabled, ssh_keys FROM users WHERE id = ?`, target.ID).Scan(&enabled, &keys)
	s.json(w, map[string]any{"enabled": enabled != 0, "keys": splitLines(keys)})
}

func (s *Server) handleSSHSet(w http.ResponseWriter, r *http.Request, u *models.User) {
	target, ok := s.userScopedForManage(u, pathID(r, "id"))
	if !ok {
		s.err(w, http.StatusNotFound, "user not found")
		return
	}
	req, err := decode[struct {
		Enabled bool     `json:"enabled"`
		Keys    []string `json:"keys"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// Validate keys up front so a bad key fails before any system change.
	var clean []string
	for _, k := range req.Keys {
		if k = strings.TrimSpace(k); k == "" {
			continue
		}
		if !system.ValidSSHKey(k) {
			s.err(w, http.StatusBadRequest, "one of the SSH keys is not a valid public key")
			return
		}
		clean = append(clean, k)
	}
	sysUser, err := s.sysUserForPanelUser(target.ID)
	if err != nil {
		s.fail(w, "provision system user", err)
		return
	}
	home := filepath.Join(s.Cfg.WebRoot, sysUser)
	if err := system.SetSSHShell(sysUser, req.Enabled); err != nil {
		s.err(w, http.StatusBadGateway, err.Error())
		return
	}
	keysToWrite := clean
	if !req.Enabled {
		keysToWrite = nil // remove authorized_keys when access is off
	}
	if err := system.WriteAuthorizedKeys(sysUser, home, keysToWrite); err != nil {
		s.err(w, http.StatusBadGateway, err.Error())
		return
	}
	s.DB.Exec(`UPDATE users SET ssh_enabled = ?, ssh_keys = ? WHERE id = ?`,
		boolInt(req.Enabled), strings.Join(clean, "\n"), target.ID)
	s.audit(u.ID, u.Username, "ssh.update", "for "+target.Username+" enabled="+strconv.FormatBool(req.Enabled), clientIP(r))
	s.json(w, map[string]bool{"ok": true, "enabled": req.Enabled})
}

func splitLines(s string) []string {
	out := []string{}
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// ---- Audit log (admin) ------------------------------------------------------

func (s *Server) handleAuditList(w http.ResponseWriter, r *http.Request, u *models.User) {
	limit := 200
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 1000 {
		limit = v
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	query := `SELECT id, user_id, username, action, detail, ip, created_at FROM audit_log`
	var args []any
	if q != "" {
		query += ` WHERE username LIKE ? OR action LIKE ? OR detail LIKE ? OR ip LIKE ?`
		like := "%" + q + "%"
		args = []any{like, like, like, like}
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.DB.Query(query, args...)
	if err != nil {
		s.fail(w, "list audit log", err)
		return
	}
	defer rows.Close()
	out := []models.AuditEntry{}
	for rows.Next() {
		var e models.AuditEntry
		if rows.Scan(&e.ID, &e.UserID, &e.Username, &e.Action, &e.Detail, &e.IP, &e.CreatedAt) == nil {
			out = append(out, e)
		}
	}
	s.json(w, out)
}
