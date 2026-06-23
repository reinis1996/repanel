package api

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/reinis1996/repanel/internal/models"
	"github.com/reinis1996/repanel/internal/system"
)

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// handleAppCatalog lists the installable applications.
func (s *Server) handleAppCatalog(w http.ResponseWriter, r *http.Request, u *models.User) {
	s.json(w, system.AppCatalog())
}

// handleAppList returns every one-click app the caller may see, joined with its
// domain. Used by the Websites page to show install status per domain.
func (s *Server) handleAppList(w http.ResponseWriter, r *http.Request, u *models.User) {
	where, args := scopeWhere(u, "d.user_id")
	rows, err := s.DB.Query(`SELECT a.id, a.domain_id, a.app, a.status, a.error, a.url, a.db_name,
		a.db_user, a.db_pass, a.auto_setup, a.created_at, d.name
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
			&a.DBUser, &a.DBPass, &auto, &a.CreatedAt, &a.Domain) == nil {
			a.AutoSetup = auto != 0
			out = append(out, a)
		}
	}
	s.json(w, out)
}

// handleAppInstall provisions a one-click application from the catalog into a
// domain. It provisions a database when the app needs one, records the app as
// installing, and runs the download/extract in the background. WordPress is
// configured fully (WP-CLI); other apps are finished in their browser installer.
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
	app, ok := system.FindCatalogApp(req.App)
	if !ok {
		s.err(w, http.StatusBadRequest, "unknown application")
		return
	}
	if d.Runtime != "php" || d.RedirectURL != "" {
		s.err(w, http.StatusBadRequest, "applications can only be installed on a PHP website")
		return
	}
	if s.quotaExceeded(u) {
		s.err(w, http.StatusForbidden, quotaMsg)
		return
	}
	if app.NeedsDB && !system.HaveMySQL() {
		s.err(w, http.StatusBadRequest, app.Name+" requires MariaDB, which is not installed on this server")
		return
	}
	var existing int
	s.DB.QueryRow(`SELECT COUNT(*) FROM apps WHERE domain_id = ? AND status != 'failed'`, d.ID).Scan(&existing)
	if existing > 0 {
		s.err(w, http.StatusConflict, "an app is already installed (or installing) on this domain")
		return
	}

	dbName, dbPass := "", ""
	if app.NeedsDB {
		// Namespace the app's database under the domain owner's account prefix,
		// matching databases created by hand on the Databases page.
		var ownerName string
		var ownerRole models.Role
		s.DB.QueryRow(`SELECT username, role FROM users WHERE id = ?`, d.UserID).Scan(&ownerName, &ownerRole)
		dbName = applyDBPrefix(dbPrefix(&models.User{Username: ownerName, Role: ownerRole}), appDBName(app.ID, d.Name))
		if len(dbName) > 64 {
			dbName = dbName[:64]
		}
		dbPass, err = randomHex(16)
		if err != nil {
			s.fail(w, "generate password", err)
			return
		}
		// Reserve the database in panel state, then create it for real.
		if _, err := s.DB.Exec(`INSERT INTO db_entries(user_id,name,db_user,engine) VALUES(?,?,?,'mysql')`,
			d.UserID, dbName, dbName); err != nil {
			s.err(w, http.StatusConflict, "could not allocate a database for "+app.Name+" (name in use)")
			return
		}
		if err := system.CreateDatabase(dbName, dbName, dbPass); err != nil {
			s.DB.Exec(`DELETE FROM db_entries WHERE name = ?`, dbName)
			s.fail(w, "create application database", err)
			return
		}
	}

	scheme := "http"
	if d.SSL {
		scheme = "https"
	}
	siteURL := scheme + "://" + d.Name
	res, err := s.DB.Exec(`INSERT INTO apps(domain_id,app,status,url,db_name,db_user,db_pass) VALUES(?,?,'installing',?,?,?,?)`,
		d.ID, app.ID, siteURL, dbName, dbName, dbPass)
	if err != nil {
		s.fail(w, "record app", err)
		return
	}
	appID, _ := res.LastInsertId()
	sysUser := system.SysUserName(d.UserID)

	if app.ID == "wordpress" {
		go s.runWordPressInstall(appID, system.WordPressOptions{
			DocRoot: d.DocumentRoot, SysUser: sysUser, SiteURL: siteURL,
			Title:     strings.TrimSpace(req.Title),
			AdminUser: strings.TrimSpace(req.AdminUser), AdminPass: req.AdminPass,
			AdminEmail: strings.TrimSpace(req.AdminEmail),
			DBName:     dbName, DBUser: dbName, DBPass: dbPass,
		})
	} else {
		go s.runCatalogInstall(appID, app, d.DocumentRoot, sysUser)
	}
	s.json(w, map[string]any{"ok": true, "id": appID, "message": app.Name + " is installing in the background"})
}

// runCatalogInstall downloads and extracts a non-WordPress catalog app, then
// marks the record installed. Apps that need a browser installer are flagged not
// auto-setup so the UI shows the database credentials and a "finish setup" link.
func (s *Server) runCatalogInstall(appID int64, app system.CatalogApp, docRoot, sysUser string) {
	if err := system.InstallCatalogApp(app, docRoot, sysUser); err != nil {
		log.Printf("app install %d (%s) failed: %v", appID, app.ID, err)
		msg := err.Error()
		if len(msg) > 400 {
			msg = msg[:400]
		}
		s.DB.Exec(`UPDATE apps SET status = 'failed', error = ? WHERE id = ?`, msg, appID)
		return
	}
	// "none" apps (e.g. Grav) work immediately; "manual" apps need browser setup.
	s.DB.Exec(`UPDATE apps SET status = 'installed', auto_setup = ? WHERE id = ?`,
		boolInt(app.Config == "none"), appID)
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

// handleAppDelete removes an app. By default it only removes the panel record,
// leaving files and the database intact. With ?purge=1 it also drops the app's
// database (and its db_entries record) and deletes the app's files (empties the
// domain's document root) — a permanent, opt-in cleanup.
func (s *Server) handleAppDelete(w http.ResponseWriter, r *http.Request, u *models.User) {
	appID := pathID(r, "id")
	where, args := scopeWhere(u, "d.user_id")
	args = append([]any{appID}, args...)
	var domainID int64
	var dbName, dbUser string
	err := s.DB.QueryRow(`SELECT a.domain_id, a.db_name, a.db_user FROM apps a JOIN domains d ON d.id = a.domain_id
		WHERE a.id = ? AND `+where, args...).Scan(&domainID, &dbName, &dbUser)
	if err != nil {
		s.err(w, http.StatusNotFound, "app not found")
		return
	}

	if r.URL.Query().Get("purge") == "1" {
		// Drop the database (apps provision MariaDB) and forget it in panel state.
		if dbName != "" {
			if dbUser == "" {
				dbUser = dbName
			}
			if err := system.DropDatabase(dbName, dbUser); err != nil {
				log.Printf("purge app %d: drop database %s: %v", appID, dbName, err)
			}
			s.DB.Exec(`DELETE FROM db_entries WHERE name = ? AND user_id IN (SELECT user_id FROM domains WHERE id = ?)`, dbName, domainID)
		}
		// Delete the app's files (the contents of the domain's document root).
		if d, derr := s.getDomainByID(domainID); derr == nil {
			s.purgeDocRoot(d.DocumentRoot)
		}
	}

	if _, err := s.DB.Exec(`DELETE FROM apps WHERE id = ?`, appID); err != nil {
		s.fail(w, "delete app", err)
		return
	}
	s.json(w, map[string]bool{"ok": true})
}

// purgeDocRoot empties a domain's document root, but only when it is a real
// per-domain docroot (see docRootPurgeable).
func (s *Server) purgeDocRoot(docRoot string) {
	if !docRootPurgeable(s.Cfg.WebRoot, docRoot) {
		return
	}
	clean := filepath.Clean(docRoot)
	entries, err := os.ReadDir(clean)
	if err != nil {
		return
	}
	for _, e := range entries {
		os.RemoveAll(filepath.Join(clean, e.Name()))
	}
}

// docRootPurgeable guards bulk deletion: the path must sit at least two levels
// under the web root (i.e. <webroot>/<sysuser>/<domain>/...), so the web root
// itself or a user's home directory can never be wiped.
func docRootPurgeable(webRoot, docRoot string) bool {
	root := filepath.Clean(webRoot)
	clean := filepath.Clean(docRoot)
	rel, err := filepath.Rel(root, clean)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return false
	}
	return len(strings.Split(rel, string(filepath.Separator))) >= 2
}

// appDBName derives a valid MariaDB schema name from an app id and domain, e.g.
// ("wordpress", "blog.example.com") -> "wordpress_blog_example_com".
func appDBName(app, domain string) string {
	base := nonAlnum.ReplaceAllString(strings.ToLower(app+"_"+domain), "_")
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
