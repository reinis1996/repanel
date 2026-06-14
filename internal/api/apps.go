package api

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/http"
	"regexp"
	"strings"

	"github.com/reinis1996/repanel/internal/models"
	"github.com/reinis1996/repanel/internal/system"
)

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// handleAppList returns every one-click app the caller may see, joined with its
// domain. Used by the Websites page to show install status per domain.
func (s *Server) handleAppList(w http.ResponseWriter, r *http.Request, u *models.User) {
	where, args := scopeWhere(u, "d.user_id")
	rows, err := s.DB.Query(`SELECT a.id, a.domain_id, a.app, a.status, a.error, a.url, a.db_name,
		a.auto_setup, a.created_at, d.name
		FROM apps a JOIN domains d ON d.id = a.domain_id
		WHERE `+where+` ORDER BY a.id DESC`, args...)
	if err != nil {
		s.fail(w, "list apps", err)
		return
	}
	defer rows.Close()
	out := []models.App{}
	for rows.Next() {
		var a models.App
		var auto int
		if rows.Scan(&a.ID, &a.DomainID, &a.App, &a.Status, &a.Error, &a.URL, &a.DBName,
			&auto, &a.CreatedAt, &a.Domain) == nil {
			a.AutoSetup = auto != 0
			out = append(out, a)
		}
	}
	s.json(w, out)
}

// handleAppInstall provisions a one-click application into a domain. Currently
// only WordPress is supported. It creates a dedicated MariaDB database, records
// the app as installing, and runs the download/extract in the background.
func (s *Server) handleAppInstall(w http.ResponseWriter, r *http.Request, u *models.User) {
	d, err := s.getDomainScoped(u, pathID(r, "id"))
	if err != nil {
		s.err(w, http.StatusNotFound, err.Error())
		return
	}
	req, _ := decode[struct {
		App        string `json:"app"`
		Title      string `json:"title"`
		AdminUser  string `json:"admin_user"`
		AdminPass  string `json:"admin_password"`
		AdminEmail string `json:"admin_email"`
	}](r)
	if req.App != "wordpress" {
		s.err(w, http.StatusBadRequest, "unsupported app")
		return
	}
	if s.quotaExceeded(u) {
		s.err(w, http.StatusForbidden, quotaMsg)
		return
	}
	if !system.HaveMySQL() {
		s.err(w, http.StatusBadRequest, "WordPress requires MariaDB, which is not installed on this server")
		return
	}
	var existing int
	s.DB.QueryRow(`SELECT COUNT(*) FROM apps WHERE domain_id = ? AND status != 'failed'`, d.ID).Scan(&existing)
	if existing > 0 {
		s.err(w, http.StatusConflict, "an app is already installed (or installing) on this domain")
		return
	}

	dbName := wpDBName(d.Name)
	dbPass, err := randomHex(16)
	if err != nil {
		s.fail(w, "generate password", err)
		return
	}
	// Reserve the database in panel state, then create it for real.
	if _, err := s.DB.Exec(`INSERT INTO db_entries(user_id,name,db_user,engine) VALUES(?,?,?,'mysql')`,
		d.UserID, dbName, dbName); err != nil {
		s.err(w, http.StatusConflict, "could not allocate a database for WordPress (name in use)")
		return
	}
	if err := system.CreateDatabase(dbName, dbName, dbPass); err != nil {
		s.DB.Exec(`DELETE FROM db_entries WHERE name = ?`, dbName)
		s.fail(w, "create wordpress database", err)
		return
	}

	scheme := "http"
	if d.SSL {
		scheme = "https"
	}
	siteURL := scheme + "://" + d.Name
	res, err := s.DB.Exec(`INSERT INTO apps(domain_id,app,status,url,db_name) VALUES(?,?,'installing',?,?)`,
		d.ID, "wordpress", siteURL, dbName)
	if err != nil {
		s.fail(w, "record app", err)
		return
	}
	appID, _ := res.LastInsertId()

	sysUser := system.SysUserName(d.UserID)
	opts := system.WordPressOptions{
		DocRoot:    d.DocumentRoot,
		SysUser:    sysUser,
		SiteURL:    siteURL,
		Title:      strings.TrimSpace(req.Title),
		AdminUser:  strings.TrimSpace(req.AdminUser),
		AdminPass:  req.AdminPass,
		AdminEmail: strings.TrimSpace(req.AdminEmail),
		DBName:     dbName,
		DBUser:     dbName,
		DBPass:     dbPass,
	}
	go s.runWordPressInstall(appID, opts)

	s.json(w, map[string]any{"ok": true, "id": appID, "message": "WordPress is installing in the background"})
}

// runWordPressInstall performs the install and updates the app row's status.
func (s *Server) runWordPressInstall(appID int64, opts system.WordPressOptions) {
	auto, err := system.InstallWordPress(opts)
	if err != nil {
		log.Printf("wordpress install %d failed: %v", appID, err)
		msg := err.Error()
		if len(msg) > 400 {
			msg = msg[:400]
		}
		s.DB.Exec(`UPDATE apps SET status = 'failed', error = ? WHERE id = ?`, msg, appID)
		return
	}
	url := opts.SiteURL + "/wp-admin/"
	if !auto {
		// Setup wizard still needs to run; point the operator at it.
		url = opts.SiteURL + "/wp-admin/install.php"
	}
	s.DB.Exec(`UPDATE apps SET status = 'installed', auto_setup = ?, url = ? WHERE id = ?`,
		boolInt(auto), url, appID)
}

// handleAppDelete removes the app record (the files and database are left in
// place; the domain's database can be dropped from the Databases page).
func (s *Server) handleAppDelete(w http.ResponseWriter, r *http.Request, u *models.User) {
	where, args := scopeWhere(u, "d.user_id")
	args = append([]any{pathID(r, "id")}, args...)
	var domainID int64
	err := s.DB.QueryRow(`SELECT a.domain_id FROM apps a JOIN domains d ON d.id = a.domain_id
		WHERE a.id = ? AND `+where, args...).Scan(&domainID)
	if err != nil {
		s.err(w, http.StatusNotFound, "app not found")
		return
	}
	if _, err := s.DB.Exec(`DELETE FROM apps WHERE id = ?`, pathID(r, "id")); err != nil {
		s.fail(w, "delete app", err)
		return
	}
	s.json(w, map[string]bool{"ok": true})
}

// wpDBName derives a valid MariaDB schema name from a domain, e.g.
// "blog.example.com" -> "wp_blog_example_com".
func wpDBName(domain string) string {
	base := "wp_" + nonAlnum.ReplaceAllString(strings.ToLower(domain), "_")
	base = strings.Trim(base, "_")
	if len(base) > 48 {
		base = base[:48]
	}
	return base
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
