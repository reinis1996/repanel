package api

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/repanel/repanel/internal/models"
	"github.com/repanel/repanel/internal/system"
)

var validDBInput = regexp.MustCompile(`^[A-Za-z0-9_]{1,48}$`)

func (s *Server) handleDBList(w http.ResponseWriter, r *http.Request, u *models.User) {
	where, args := scopeWhere(u, "user_id")
	rows, err := s.DB.Query(`SELECT id, user_id, name, db_user, created_at FROM db_entries
		WHERE `+where+` ORDER BY name`, args...)
	if err != nil {
		s.fail(w, "list databases", err)
		return
	}
	defer rows.Close()
	sizes := system.DatabaseSizes()
	out := []models.DatabaseEntry{}
	for rows.Next() {
		var d models.DatabaseEntry
		if rows.Scan(&d.ID, &d.UserID, &d.Name, &d.DBUser, &d.CreatedAt) == nil {
			d.SizeMB = sizes[d.Name]
			out = append(out, d)
		}
	}
	s.json(w, out)
}

func (s *Server) handleDBCreate(w http.ResponseWriter, r *http.Request, u *models.User) {
	req, err := decode[struct {
		Name     string `json:"name"`
		User     string `json:"user"`
		Password string `json:"password"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name := strings.TrimSpace(req.Name)
	dbUser := strings.TrimSpace(req.User)
	if dbUser == "" {
		dbUser = name
	}
	if !validDBInput.MatchString(name) || !validDBInput.MatchString(dbUser) {
		s.err(w, http.StatusBadRequest, "names may only contain letters, digits and underscores (max 48)")
		return
	}
	if len(req.Password) < 8 {
		s.err(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	// Reserve the name in panel state first to prevent racing duplicates.
	res, err := s.DB.Exec(`INSERT INTO db_entries(user_id,name,db_user) VALUES(?,?,?)`, u.ID, name, dbUser)
	if err != nil {
		s.err(w, http.StatusConflict, "a database with this name already exists")
		return
	}
	if err := system.CreateDatabase(name, dbUser, req.Password); err != nil {
		s.DB.Exec(`DELETE FROM db_entries WHERE name = ?`, name)
		s.fail(w, "create database", err)
		return
	}
	id, _ := res.LastInsertId()
	s.json(w, models.DatabaseEntry{ID: id, UserID: u.ID, Name: name, DBUser: dbUser})
}

func (s *Server) dbEntryScoped(u *models.User, id int64) (*models.DatabaseEntry, bool) {
	where, args := scopeWhere(u, "user_id")
	args = append([]any{id}, args...)
	var d models.DatabaseEntry
	err := s.DB.QueryRow(`SELECT id, user_id, name, db_user FROM db_entries WHERE id = ? AND `+where, args...).
		Scan(&d.ID, &d.UserID, &d.Name, &d.DBUser)
	return &d, err == nil
}

func (s *Server) handleDBDelete(w http.ResponseWriter, r *http.Request, u *models.User) {
	d, ok := s.dbEntryScoped(u, pathID(r, "id"))
	if !ok {
		s.err(w, http.StatusNotFound, "database not found")
		return
	}
	if err := system.DropDatabase(d.Name, d.DBUser); err != nil {
		s.fail(w, "drop database", err)
		return
	}
	if _, err := s.DB.Exec(`DELETE FROM db_entries WHERE id = ?`, d.ID); err != nil {
		s.fail(w, "delete database entry", err)
		return
	}
	s.json(w, map[string]bool{"ok": true})
}

func (s *Server) handleDBPassword(w http.ResponseWriter, r *http.Request, u *models.User) {
	d, ok := s.dbEntryScoped(u, pathID(r, "id"))
	if !ok {
		s.err(w, http.StatusNotFound, "database not found")
		return
	}
	req, err := decode[struct {
		Password string `json:"password"`
	}](r)
	if err != nil || len(req.Password) < 8 {
		s.err(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	if err := system.SetDatabasePassword(d.DBUser, req.Password); err != nil {
		s.fail(w, "set database password", err)
		return
	}
	s.json(w, map[string]bool{"ok": true})
}
