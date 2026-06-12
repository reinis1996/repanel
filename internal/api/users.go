package api

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/repanel/repanel/internal/auth"
	"github.com/repanel/repanel/internal/models"
)

var validUsername = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_.-]{2,31}$`)

func (s *Server) handleUserList(w http.ResponseWriter, r *http.Request, u *models.User) {
	query := `SELECT id, username, email, role, owner_id, suspended, created_at FROM users`
	var args []any
	if u.Role == models.RoleReseller {
		query += ` WHERE owner_id = ? OR id = ?`
		args = []any{u.ID, u.ID}
	}
	query += ` ORDER BY username`
	rows, err := s.DB.Query(query, args...)
	if err != nil {
		s.fail(w, "list users", err)
		return
	}
	defer rows.Close()
	out := []models.User{}
	for rows.Next() {
		var usr models.User
		var susp int
		if rows.Scan(&usr.ID, &usr.Username, &usr.Email, &usr.Role, &usr.OwnerID, &susp, &usr.CreatedAt) == nil {
			usr.Suspended = susp != 0
			out = append(out, usr)
		}
	}
	s.json(w, out)
}

func (s *Server) handleUserCreate(w http.ResponseWriter, r *http.Request, u *models.User) {
	req, err := decode[struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	username := strings.TrimSpace(req.Username)
	if !validUsername.MatchString(username) {
		s.err(w, http.StatusBadRequest, "username must be 3-32 chars, start with a letter")
		return
	}
	if len(req.Password) < 8 {
		s.err(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	role := models.Role(req.Role)
	if role == "" {
		role = models.RoleUser
	}
	// Resellers may only create plain users under their own account;
	// only admins may mint admins and resellers.
	if u.Role == models.RoleReseller && role != models.RoleUser {
		s.err(w, http.StatusForbidden, "resellers can only create user accounts")
		return
	}
	if role != models.RoleAdmin && role != models.RoleReseller && role != models.RoleUser {
		s.err(w, http.StatusBadRequest, "invalid role")
		return
	}
	ownerID := int64(0)
	if u.Role == models.RoleReseller {
		ownerID = u.ID
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		s.fail(w, "hash password", err)
		return
	}
	res, err := s.DB.Exec(`INSERT INTO users(username,email,password_hash,role,owner_id) VALUES(?,?,?,?,?)`,
		username, strings.TrimSpace(req.Email), hash, role, ownerID)
	if err != nil {
		s.err(w, http.StatusConflict, "username already taken")
		return
	}
	id, _ := res.LastInsertId()
	s.json(w, models.User{ID: id, Username: username, Email: req.Email, Role: role, OwnerID: ownerID})
}

// userScopedForManage loads a target user the caller may administer.
func (s *Server) userScopedForManage(caller *models.User, id int64) (*models.User, bool) {
	target, err := auth.GetUserByID(s.DB, id)
	if err != nil || target == nil {
		return nil, false
	}
	if caller.Role == models.RoleAdmin {
		return target, true
	}
	if caller.Role == models.RoleReseller && target.OwnerID == caller.ID {
		return target, true
	}
	return nil, false
}

func (s *Server) handleUserUpdate(w http.ResponseWriter, r *http.Request, u *models.User) {
	target, ok := s.userScopedForManage(u, pathID(r, "id"))
	if !ok {
		s.err(w, http.StatusNotFound, "user not found")
		return
	}
	req, err := decode[struct {
		Email     *string `json:"email"`
		Password  *string `json:"password"`
		Suspended *bool   `json:"suspended"`
		Role      *string `json:"role"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Email != nil {
		if _, err := s.DB.Exec(`UPDATE users SET email = ? WHERE id = ?`, strings.TrimSpace(*req.Email), target.ID); err != nil {
			s.fail(w, "update email", err)
			return
		}
	}
	if req.Password != nil {
		if len(*req.Password) < 8 {
			s.err(w, http.StatusBadRequest, "password must be at least 8 characters")
			return
		}
		hash, err := auth.HashPassword(*req.Password)
		if err != nil {
			s.fail(w, "hash password", err)
			return
		}
		if _, err := s.DB.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, hash, target.ID); err != nil {
			s.fail(w, "update password", err)
			return
		}
	}
	if req.Role != nil && u.Role == models.RoleAdmin {
		role := models.Role(*req.Role)
		if role != models.RoleAdmin && role != models.RoleReseller && role != models.RoleUser {
			s.err(w, http.StatusBadRequest, "invalid role")
			return
		}
		if target.ID == u.ID {
			s.err(w, http.StatusBadRequest, "cannot change your own role")
			return
		}
		if _, err := s.DB.Exec(`UPDATE users SET role = ? WHERE id = ?`, role, target.ID); err != nil {
			s.fail(w, "update role", err)
			return
		}
	}
	if req.Suspended != nil {
		if target.ID == u.ID {
			s.err(w, http.StatusBadRequest, "cannot suspend yourself")
			return
		}
		if _, err := s.DB.Exec(`UPDATE users SET suspended = ? WHERE id = ?`, boolInt(*req.Suspended), target.ID); err != nil {
			s.fail(w, "update suspension", err)
			return
		}
		// Drop active sessions when suspending.
		if *req.Suspended {
			s.DB.Exec(`DELETE FROM sessions WHERE user_id = ?`, target.ID)
		}
	}
	s.json(w, map[string]bool{"ok": true})
}

func (s *Server) handleUserDelete(w http.ResponseWriter, r *http.Request, u *models.User) {
	target, ok := s.userScopedForManage(u, pathID(r, "id"))
	if !ok {
		s.err(w, http.StatusNotFound, "user not found")
		return
	}
	if target.ID == u.ID {
		s.err(w, http.StatusBadRequest, "cannot delete your own account")
		return
	}
	var domains int
	s.DB.QueryRow(`SELECT COUNT(*) FROM domains WHERE user_id = ?`, target.ID).Scan(&domains)
	if domains > 0 {
		s.err(w, http.StatusConflict, "user still owns domains; delete or reassign them first")
		return
	}
	if _, err := s.DB.Exec(`DELETE FROM users WHERE id = ?`, target.ID); err != nil {
		s.fail(w, "delete user", err)
		return
	}
	s.json(w, map[string]bool{"ok": true})
}
