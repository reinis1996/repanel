package api

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/reinis1996/repanel/internal/auth"
	"github.com/reinis1996/repanel/internal/models"
	"github.com/reinis1996/repanel/internal/system"
)

func (s *Server) handleDomainList(w http.ResponseWriter, r *http.Request, u *models.User) {
	where, args := scopeWhere(u, "d.user_id")
	rows, err := s.DB.Query(`SELECT d.id, d.user_id, d.name, d.document_root, d.php_version,
		d.ssl, d.suspended, d.web_mode, d.created_at, u.username
		FROM domains d JOIN users u ON u.id = d.user_id WHERE `+where+` ORDER BY d.name`, args...)
	if err != nil {
		s.fail(w, "list domains", err)
		return
	}
	defer rows.Close()
	ws := s.webServer()
	out := []models.Domain{}
	for rows.Next() {
		var d models.Domain
		var ssl, susp int
		if err := rows.Scan(&d.ID, &d.UserID, &d.Name, &d.DocumentRoot, &d.PHPVersion,
			&ssl, &susp, &d.WebMode, &d.CreatedAt, &d.Owner); err == nil {
			d.SSL, d.Suspended = ssl != 0, susp != 0
			d.WebMode = ws.NormalizeMode(d.WebMode)
			out = append(out, d)
		}
	}
	s.json(w, out)
}

// getDomainScoped fetches a domain if the user is allowed to manage it.
func (s *Server) getDomainScoped(u *models.User, id int64) (*models.Domain, error) {
	where, args := scopeWhere(u, "user_id")
	args = append([]any{id}, args...)
	var d models.Domain
	var ssl, susp int
	err := s.DB.QueryRow(`SELECT id, user_id, name, document_root, php_version, ssl, suspended, web_mode, created_at
		FROM domains WHERE id = ? AND `+where, args...).
		Scan(&d.ID, &d.UserID, &d.Name, &d.DocumentRoot, &d.PHPVersion, &ssl, &susp, &d.WebMode, &d.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("domain not found")
	}
	d.SSL, d.Suspended = ssl != 0, susp != 0
	d.WebMode = s.webServer().NormalizeMode(d.WebMode)
	return &d, nil
}

// sysUserForPanelUser returns (and lazily provisions) the unix account that
// owns a panel user's files.
func (s *Server) sysUserForPanelUser(userID int64) (string, error) {
	u, err := auth.GetUserByID(s.DB, userID)
	if err != nil || u == nil {
		return "", fmt.Errorf("panel user %d not found", userID)
	}
	name := system.SysUserName(u.ID)
	home := filepath.Join(s.Cfg.WebRoot, name)
	if err := system.EnsureUnixUser(name, home); err != nil {
		return "", err
	}
	return name, nil
}

func (s *Server) handleDomainCreate(w http.ResponseWriter, r *http.Request, u *models.User) {
	req, err := decode[struct {
		Name       string `json:"name"`
		UserID     int64  `json:"user_id"` // optional, admins may assign
		PHPVersion string `json:"php_version"`
		WebMode    string `json:"web_mode"`
		CreateDNS  bool   `json:"create_dns"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name := strings.ToLower(strings.TrimSpace(req.Name))
	if !validDomainName(name) {
		s.err(w, http.StatusBadRequest, "invalid domain name")
		return
	}
	ownerID := u.ID
	if req.UserID != 0 && req.UserID != u.ID {
		if !isAdminish(u) {
			s.err(w, http.StatusForbidden, "cannot create domains for other users")
			return
		}
		ownerID = req.UserID
	}
	// Tenant isolation: refuse a domain that overlaps another tenant's domain
	// (the same name, a subdomain of theirs, or a parent of theirs), which would
	// otherwise let one customer serve/sign hostnames inside another's zone
	// (see SECURITY_AUDIT F-07). Note: this does not prove external ownership of
	// the domain — out-of-band DNS/HTTP verification is still recommended.
	var conflict int
	s.DB.QueryRow(`SELECT COUNT(*) FROM domains
		WHERE user_id != ? AND (name = ? OR ? LIKE '%.' || name OR name LIKE '%.' || ?)`,
		ownerID, name, name, name).Scan(&conflict)
	if conflict > 0 {
		s.err(w, http.StatusConflict, "this domain overlaps a domain owned by another account")
		return
	}

	phpVersion := req.PHPVersion
	if phpVersion == "" {
		phpVersion = system.PHPVersions()[0]
	}

	sysUser, err := s.sysUserForPanelUser(ownerID)
	if err != nil {
		s.fail(w, "provision system user", err)
		return
	}
	docroot := filepath.Join(s.Cfg.WebRoot, sysUser, name, "public_html")
	webMode := s.webServer().NormalizeMode(req.WebMode)

	res, err := s.DB.Exec(`INSERT INTO domains(user_id,name,document_root,php_version,web_mode) VALUES(?,?,?,?,?)`,
		ownerID, name, docroot, phpVersion, webMode)
	if err != nil {
		s.err(w, http.StatusConflict, "domain already exists")
		return
	}
	domainID, _ := res.LastInsertId()
	d := models.Domain{ID: domainID, UserID: ownerID, Name: name, DocumentRoot: docroot, PHPVersion: phpVersion, WebMode: webMode}

	if err := system.EnsureDocRoot(docroot, sysUser, name); err != nil {
		s.fail(w, "create docroot", err)
		return
	}
	if err := s.webServer().WriteVhost(d, sysUser, "", "", webMode); err != nil {
		s.fail(w, "write vhost", err)
		return
	}

	if req.CreateDNS {
		if err := s.createZoneForDomain(d); err != nil {
			s.fail(w, "create dns zone", err)
			return
		}
	}
	s.json(w, d)
}

// createZoneForDomain inserts the default zone + records and writes the file.
func (s *Server) createZoneForDomain(d models.Domain) error {
	res, err := s.DB.Exec(`INSERT INTO dns_zones(domain_id,name) VALUES(?,?)`, d.ID, d.Name)
	if err != nil {
		return err
	}
	zoneID, _ := res.LastInsertId()
	for _, rec := range system.DefaultZoneRecords(d.Name, s.DB.Setting("server_ip")) {
		if _, err := s.DB.Exec(`INSERT INTO dns_records(zone_id,name,type,value,ttl,priority)
			VALUES(?,?,?,?,?,?)`, zoneID, rec.Name, rec.Type, rec.Value, rec.TTL, rec.Priority); err != nil {
			return err
		}
	}
	return s.writeZoneFile(zoneID)
}

func (s *Server) handleDomainDelete(w http.ResponseWriter, r *http.Request, u *models.User) {
	d, err := s.getDomainScoped(u, pathID(r, "id"))
	if err != nil {
		s.err(w, http.StatusNotFound, err.Error())
		return
	}
	s.webServer().RemoveVhost(*d)
	system.RemoveZone(s.Cfg.BindDir, d.Name, system.ParseSlaveIPs(s.DB.Setting("slave_dns")))
	if _, err := s.DB.Exec(`DELETE FROM domains WHERE id = ?`, d.ID); err != nil {
		s.fail(w, "delete domain", err)
		return
	}
	// Mail maps may reference the deleted domain; rebuild from db state.
	if err := s.rebuildMail(); err != nil {
		s.fail(w, "rebuild mail maps", err)
		return
	}
	s.json(w, map[string]bool{"ok": true})
	// Note: docroot is kept on disk so customer data is never destroyed
	// implicitly; admins can remove it via the file manager.
}

func (s *Server) handleDomainSuspend(w http.ResponseWriter, r *http.Request, u *models.User) {
	if !isAdminish(u) {
		s.err(w, http.StatusForbidden, "insufficient privileges")
		return
	}
	d, err := s.getDomainScoped(u, pathID(r, "id"))
	if err != nil {
		s.err(w, http.StatusNotFound, err.Error())
		return
	}
	newState := !d.Suspended
	if _, err := s.DB.Exec(`UPDATE domains SET suspended = ? WHERE id = ?`, boolInt(newState), d.ID); err != nil {
		s.fail(w, "update domain", err)
		return
	}
	d.Suspended = newState
	if newState {
		err = s.webServer().WriteSuspendedVhost(*d)
	} else {
		err = s.rewriteVhost(*d)
	}
	if err != nil {
		s.fail(w, "rewrite vhost", err)
		return
	}
	s.json(w, d)
}

func (s *Server) handleDomainPHP(w http.ResponseWriter, r *http.Request, u *models.User) {
	d, err := s.getDomainScoped(u, pathID(r, "id"))
	if err != nil {
		s.err(w, http.StatusNotFound, err.Error())
		return
	}
	req, err := decode[struct {
		PHPVersion string `json:"php_version"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	valid := false
	for _, v := range system.PHPVersions() {
		if v == req.PHPVersion {
			valid = true
			break
		}
	}
	if !valid {
		s.err(w, http.StatusBadRequest, "PHP version not installed on this server")
		return
	}
	s.webServer().RemoveVhost(*d) // drop old pool file (keyed on old PHP version)
	d.PHPVersion = req.PHPVersion
	if _, err := s.DB.Exec(`UPDATE domains SET php_version = ? WHERE id = ?`, d.PHPVersion, d.ID); err != nil {
		s.fail(w, "update domain", err)
		return
	}
	if err := s.rewriteVhost(*d); err != nil {
		s.fail(w, "rewrite vhost", err)
		return
	}
	s.json(w, d)
}

func (s *Server) handlePHPVersions(w http.ResponseWriter, r *http.Request, _ *models.User) {
	s.json(w, system.PHPVersions())
}

// rewriteVhost regenerates the web server config including current SSL state
// and the domain's selected web mode.
func (s *Server) rewriteVhost(d models.Domain) error {
	sysUser, err := s.sysUserForPanelUser(d.UserID)
	if err != nil {
		return err
	}
	certPath, keyPath := "", ""
	if d.SSL {
		s.DB.QueryRow(`SELECT cert_path, key_path FROM certificates WHERE domain_id = ? ORDER BY id DESC LIMIT 1`,
			d.ID).Scan(&certPath, &keyPath)
	}
	return s.webServer().WriteVhost(d, sysUser, certPath, keyPath, d.WebMode)
}

// handleWebServerInfo reports the server-wide stack and the per-domain modes
// the operator may choose from.
func (s *Server) handleWebServerInfo(w http.ResponseWriter, r *http.Request, _ *models.User) {
	s.json(w, s.webServer().Info())
}

// handleDomainWebMode switches a domain between nginx / apache / nginx-apache.
// It is only meaningful in the combined stack; single-server stacks accept only
// their one mode.
func (s *Server) handleDomainWebMode(w http.ResponseWriter, r *http.Request, u *models.User) {
	d, err := s.getDomainScoped(u, pathID(r, "id"))
	if err != nil {
		s.err(w, http.StatusNotFound, err.Error())
		return
	}
	req, err := decode[struct {
		Mode string `json:"mode"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	ws := s.webServer()
	valid := false
	for _, m := range ws.Modes() {
		if m == req.Mode {
			valid = true
			break
		}
	}
	if !valid {
		s.err(w, http.StatusBadRequest, "web mode not available in this server stack")
		return
	}
	d.WebMode = req.Mode
	if _, err := s.DB.Exec(`UPDATE domains SET web_mode = ? WHERE id = ?`, d.WebMode, d.ID); err != nil {
		s.fail(w, "update domain", err)
		return
	}
	if d.Suspended {
		// Leave the 503 vhost in place; the mode takes effect on unsuspend.
		s.json(w, d)
		return
	}
	if err := s.rewriteVhost(*d); err != nil {
		s.fail(w, "rewrite vhost", err)
		return
	}
	s.json(w, d)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
