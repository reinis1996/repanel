package api

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/reinis1996/repanel/internal/models"
	"github.com/reinis1996/repanel/internal/system"
)

var validDBInput = regexp.MustCompile(`^[A-Za-z0-9_]{1,48}$`)

// reservedDBUsers are administrative MySQL/PostgreSQL accounts a tenant must
// never be able to name as their database user (see SECURITY_AUDIT F-03).
var reservedDBUsers = map[string]bool{
	"root": true, "mysql": true, "admin": true, "mariadb.sys": true,
	"mysql.sys": true, "mysql.session": true, "mysql.infoschema": true,
	"debian-sys-maint": true, "postgres": true, "pg_signal_backend": true,
	"pg_read_all_data": true, "pg_write_all_data": true,
}

func (s *Server) handleDBList(w http.ResponseWriter, r *http.Request, u *models.User) {
	where, args := scopeWhere(u, "user_id")
	rows, err := s.DB.Query(`SELECT id, user_id, name, db_user, engine, created_at FROM db_entries
		WHERE `+where+` ORDER BY name`, args...)
	if err != nil {
		s.fail(w, "list databases", err)
		return
	}
	defer rows.Close()
	mysqlSizes := system.DatabaseSizes()
	pgSizes := system.PostgresDatabaseSizes()
	out := []models.DatabaseEntry{}
	for rows.Next() {
		var d models.DatabaseEntry
		if rows.Scan(&d.ID, &d.UserID, &d.Name, &d.DBUser, &d.Engine, &d.CreatedAt) == nil {
			if d.Engine == enginePostgres {
				d.SizeMB = pgSizes[d.Name]
			} else {
				d.SizeMB = mysqlSizes[d.Name]
			}
			out = append(out, d)
		}
	}
	s.json(w, out)
}

const (
	engineMySQL    = "mysql"
	enginePostgres = "postgres"
)

// normalizeEngine maps user input to a supported engine, defaulting to MySQL.
func normalizeEngine(e string) string {
	if e == enginePostgres || e == "postgresql" {
		return enginePostgres
	}
	return engineMySQL
}

// handleDBEngines reports which database engines are available on the host so
// the UI can offer only what's installed.
func (s *Server) handleDBEngines(w http.ResponseWriter, r *http.Request, u *models.User) {
	s.json(w, map[string]bool{
		"mysql":    system.HaveMySQL(),
		"postgres": system.HavePostgres(),
	})
}

func (s *Server) handleDBCreate(w http.ResponseWriter, r *http.Request, u *models.User) {
	if s.quotaExceeded(u) {
		s.err(w, http.StatusForbidden, quotaMsg)
		return
	}
	req, err := decode[struct {
		Name     string `json:"name"`
		User     string `json:"user"`
		Password string `json:"password"`
		Engine   string `json:"engine"`
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
	// Tenant isolation: a customer must not be able to name an administrative
	// DB account, nor a DB user already owned by another tenant (which would
	// let them attach to or reset that user — see SECURITY_AUDIT F-03).
	if reservedDBUsers[strings.ToLower(dbUser)] {
		s.err(w, http.StatusBadRequest, "that database user name is reserved")
		return
	}
	var taken int
	s.DB.QueryRow(`SELECT COUNT(*) FROM db_entries WHERE db_user = ? AND user_id != ?`, dbUser, u.ID).Scan(&taken)
	if taken > 0 {
		s.err(w, http.StatusConflict, "that database user name is already in use")
		return
	}
	engine := normalizeEngine(req.Engine)
	if engine == enginePostgres && !system.HavePostgres() {
		s.err(w, http.StatusBadRequest, "PostgreSQL is not installed on this server")
		return
	}
	// Reserve the name in panel state first to prevent racing duplicates.
	res, err := s.DB.Exec(`INSERT INTO db_entries(user_id,name,db_user,engine) VALUES(?,?,?,?)`, u.ID, name, dbUser, engine)
	if err != nil {
		s.err(w, http.StatusConflict, "a database with this name already exists")
		return
	}
	var createErr error
	if engine == enginePostgres {
		createErr = system.CreatePostgresDatabase(name, dbUser, req.Password)
	} else {
		createErr = system.CreateDatabase(name, dbUser, req.Password)
	}
	if createErr != nil {
		s.DB.Exec(`DELETE FROM db_entries WHERE id = ?`, mustID(res))
		s.fail(w, "create database", createErr)
		return
	}
	id, _ := res.LastInsertId()
	s.json(w, models.DatabaseEntry{ID: id, UserID: u.ID, Name: name, DBUser: dbUser, Engine: engine})
}

// mustID returns the last insert id, ignoring the (sqlite-never-set) error.
func mustID(res interface{ LastInsertId() (int64, error) }) int64 {
	id, _ := res.LastInsertId()
	return id
}

func (s *Server) dbEntryScoped(u *models.User, id int64) (*models.DatabaseEntry, bool) {
	where, args := scopeWhere(u, "user_id")
	args = append([]any{id}, args...)
	var d models.DatabaseEntry
	err := s.DB.QueryRow(`SELECT id, user_id, name, db_user, engine FROM db_entries WHERE id = ? AND `+where, args...).
		Scan(&d.ID, &d.UserID, &d.Name, &d.DBUser, &d.Engine)
	return &d, err == nil
}

func (s *Server) handleDBDelete(w http.ResponseWriter, r *http.Request, u *models.User) {
	d, ok := s.dbEntryScoped(u, pathID(r, "id"))
	if !ok {
		s.err(w, http.StatusNotFound, "database not found")
		return
	}
	var dropErr error
	if d.Engine == enginePostgres {
		dropErr = system.DropPostgresDatabase(d.Name, d.DBUser)
	} else {
		dropErr = system.DropDatabase(d.Name, d.DBUser)
	}
	if dropErr != nil {
		s.fail(w, "drop database", dropErr)
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
	var pwErr error
	if d.Engine == enginePostgres {
		pwErr = system.SetPostgresPassword(d.DBUser, req.Password)
	} else {
		pwErr = system.SetDatabasePassword(d.DBUser, req.Password)
	}
	if pwErr != nil {
		s.fail(w, "set database password", pwErr)
		return
	}
	s.json(w, map[string]bool{"ok": true})
}
