package api

import (
	"database/sql"
	"net/http"
	"strings"
	"time"

	"github.com/reinis1996/repanel/internal/auth"
	"github.com/reinis1996/repanel/internal/models"
)

// handleTokenList returns the caller's own API tokens (metadata only — the
// secret is shown once, at creation, and never again).
func (s *Server) handleTokenList(w http.ResponseWriter, r *http.Request, u *models.User) {
	rows, err := s.DB.Query(`SELECT id, user_id, name, prefix, last_used_at, expires_at, created_at
		FROM api_tokens WHERE user_id = ? ORDER BY id DESC`, u.ID)
	if err != nil {
		s.fail(w, "list tokens", err)
		return
	}
	defer rows.Close()
	out := []models.APIToken{}
	for rows.Next() {
		var t models.APIToken
		var lastUsed, expires sql.NullTime
		if rows.Scan(&t.ID, &t.UserID, &t.Name, &t.Prefix, &lastUsed, &expires, &t.CreatedAt) == nil {
			if lastUsed.Valid {
				t.LastUsedAt = &lastUsed.Time
			}
			if expires.Valid {
				t.ExpiresAt = &expires.Time
			}
			out = append(out, t)
		}
	}
	s.json(w, out)
}

// handleTokenCreate mints a new token for the caller and returns the secret
// exactly once. ExpiresDays of 0 means the token never expires.
func (s *Server) handleTokenCreate(w http.ResponseWriter, r *http.Request, u *models.User) {
	req, _ := decode[struct {
		Name        string `json:"name"`
		ExpiresDays int    `json:"expires_days"`
	}](r)
	name := strings.TrimSpace(req.Name)
	if name == "" {
		s.err(w, http.StatusBadRequest, "token name is required")
		return
	}
	if len(name) > 64 {
		s.err(w, http.StatusBadRequest, "token name is too long")
		return
	}
	if req.ExpiresDays < 0 || req.ExpiresDays > 3650 {
		s.err(w, http.StatusBadRequest, "expiry must be between 0 and 3650 days")
		return
	}

	token, hash, prefix, err := auth.NewAPIToken()
	if err != nil {
		s.fail(w, "generate token", err)
		return
	}
	var expires any
	if req.ExpiresDays > 0 {
		expires = time.Now().Add(time.Duration(req.ExpiresDays) * 24 * time.Hour)
	}
	res, err := s.DB.Exec(`INSERT INTO api_tokens(user_id, name, token_hash, prefix, expires_at)
		VALUES(?,?,?,?,?)`, u.ID, name, hash, prefix, expires)
	if err != nil {
		s.fail(w, "create token", err)
		return
	}
	id, _ := res.LastInsertId()
	s.json(w, models.APIToken{ID: id, UserID: u.ID, Name: name, Prefix: prefix, Token: token})
}

// handleTokenDelete revokes one of the caller's tokens.
func (s *Server) handleTokenDelete(w http.ResponseWriter, r *http.Request, u *models.User) {
	res, err := s.DB.Exec(`DELETE FROM api_tokens WHERE id = ? AND user_id = ?`, pathID(r, "id"), u.ID)
	if err != nil {
		s.fail(w, "delete token", err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		s.err(w, http.StatusNotFound, "token not found")
		return
	}
	s.json(w, map[string]bool{"ok": true})
}
