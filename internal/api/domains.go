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
		d.runtime, d.node_version, d.node_port, d.ssl, d.suspended, d.web_mode, d.waf_enabled, d.created_at, u.username,
		d.kind, d.parent_id, d.redirect_url, d.redirect_code, d.aliases, COALESCE(p.name, '')
		FROM domains d JOIN users u ON u.id = d.user_id
		LEFT JOIN domains p ON p.id = d.parent_id
		WHERE `+where+` ORDER BY COALESCE(NULLIF(p.name,''), d.name), d.kind, d.name`, args...)
	if err != nil {
		s.fail(w, "list domains", err)
		return
	}
	defer rows.Close()
	ws := s.webServer()
	out := []models.Domain{}
	for rows.Next() {
		var d models.Domain
		var ssl, susp, waf int
		var aliases string
		if err := rows.Scan(&d.ID, &d.UserID, &d.Name, &d.DocumentRoot, &d.PHPVersion,
			&d.Runtime, &d.NodeVersion, &d.NodePort, &ssl, &susp, &d.WebMode, &waf, &d.CreatedAt, &d.Owner,
			&d.Kind, &d.ParentID, &d.RedirectURL, &d.RedirectCode, &aliases, &d.Parent); err == nil {
			d.SSL, d.Suspended, d.WAFEnabled = ssl != 0, susp != 0, waf != 0
			d.Aliases = strings.Fields(aliases)
			d.WebMode = ws.NormalizeMode(d.WebMode)
			out = append(out, d)
		}
	}
	s.json(w, out)
}

// getDomainScoped fetches a domain if the user is allowed to manage it.
func (s *Server) getDomainScoped(u *models.User, id int64) (*models.Domain, error) {
	where, args := scopeWhere(u, "user_id")
	return s.getDomainWhere(where, append([]any{id}, args...)...)
}

// getDomainByID loads a domain regardless of ownership, for system/background
// contexts (e.g. bandwidth enforcement) that have no requesting user.
func (s *Server) getDomainByID(id int64) (*models.Domain, error) {
	return s.getDomainWhere("1=1", id)
}

func (s *Server) getDomainWhere(where string, args ...any) (*models.Domain, error) {
	var d models.Domain
	var ssl, susp, waf int
	var aliases string
	err := s.DB.QueryRow(`SELECT id, user_id, name, document_root, php_version, runtime, node_version, node_port, ssl, suspended, web_mode, created_at, nginx_conf, apache_conf, php_conf, waf_enabled, waf_mode, waf_rules, kind, parent_id, redirect_url, redirect_code, php_settings, aliases
		FROM domains WHERE id = ? AND `+where, args...).
		Scan(&d.ID, &d.UserID, &d.Name, &d.DocumentRoot, &d.PHPVersion, &d.Runtime, &d.NodeVersion, &d.NodePort, &ssl, &susp, &d.WebMode, &d.CreatedAt, &d.NginxConf, &d.ApacheConf, &d.PHPConf, &waf, &d.WAFMode, &d.WAFRules, &d.Kind, &d.ParentID, &d.RedirectURL, &d.RedirectCode, &d.PHPSettings, &aliases)
	d.WAFEnabled = waf != 0
	if err != nil {
		return nil, fmt.Errorf("domain not found")
	}
	d.Aliases = strings.Fields(aliases)
	d.SSL, d.Suspended = ssl != 0, susp != 0
	d.WebMode = s.webServer().NormalizeMode(d.WebMode)
	s.loadDomainProtected(&d)
	return &d, nil
}

// loadDomainProtected renders the domain's password-protected-directory directive
// blocks into the domain so every vhost rebuild re-injects them. Directories
// without any users yet are skipped (so no auth file is referenced before it
// exists). Cheap (no disk writes); the .htpasswd files are written by the
// protected-dir handlers when credentials change.
func (s *Server) loadDomainProtected(d *models.Domain) {
	rows, err := s.DB.Query(`SELECT pd.id, pd.path, pd.realm, COUNT(pu.id)
		FROM protected_dirs pd LEFT JOIN protected_users pu ON pu.dir_id = pd.id
		WHERE pd.domain_id = ? GROUP BY pd.id ORDER BY pd.path`, d.ID)
	if err != nil {
		return
	}
	defer rows.Close()
	var specs []system.ProtectedSpec
	for rows.Next() {
		var sp system.ProtectedSpec
		var n int
		if rows.Scan(&sp.ID, &sp.Path, &sp.Realm, &n) == nil {
			sp.DocRoot = d.DocumentRoot
			sp.Disabled = n == 0
			specs = append(specs, sp)
		}
	}
	// Protect at the nginx layer only when nginx serves the domain directly (no
	// Apache for it). In any Apache-active mode, Apache's <Directory> block
	// enforces the auth and the nginx vhost just proxies — injecting an nginx
	// block there would either disclose PHP source (Apache-only proxy mode) or
	// fight the backend proxy. The PHP socket is passed so the renderer can
	// protect PHP without the regex location bypassing it.
	_, apacheActive, _ := s.webServer().Subsystems(*d)
	if apacheActive {
		d.ProtectedNginx = ""
	} else {
		phpSock := ""
		if d.Runtime == "php" {
			phpSock = system.PHPSocket(*d)
		}
		d.ProtectedNginx = system.RenderProtectedNginx(d.Name, phpSock, specs)
	}
	d.ProtectedApache = system.RenderProtectedApache(d.Name, specs)
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
		Runtime    string `json:"runtime"` // php (default) | node
		Version    string `json:"version"` // PHP or Node version for the chosen runtime
		PHPVersion string `json:"php_version"`
		WebMode    string `json:"web_mode"`
		CreateDNS  bool   `json:"create_dns"`
		// Subdomain / addon / alias support.
		Kind      string `json:"kind"`       // primary (default) | subdomain | alias
		ParentID  int64  `json:"parent_id"`  // owning domain for subdomain/alias
		AliasMode string `json:"alias_mode"` // mirror (default) | redirect — alias only
		// Extra hostnames pointing at the same site. nil (omitted) defaults to
		// www.<name> for a primary domain; an explicit (possibly empty) list is
		// used as-is, so a client can drop www or add its own.
		Aliases *[]string `json:"aliases"`
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
	kind := req.Kind
	if kind == "" {
		kind = "primary"
	}
	if kind != "primary" && kind != "subdomain" && kind != "alias" {
		s.err(w, http.StatusBadRequest, "invalid domain kind")
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

	// Resource limit: cap total domains an account may own (0 = unlimited). Admins
	// are never limited; resellers/users are.
	if msg := s.domainLimitReached(ownerID); msg != "" {
		s.err(w, http.StatusForbidden, msg)
		return
	}

	// Resolve and validate the parent for subdomains/aliases.
	var parent *models.Domain
	if kind == "subdomain" || kind == "alias" {
		p, perr := s.getDomainScoped(u, req.ParentID)
		if perr != nil || p.UserID != ownerID || p.Kind != "primary" {
			s.err(w, http.StatusBadRequest, "choose a primary domain you own as the parent")
			return
		}
		parent = p
		if kind == "subdomain" && !strings.HasSuffix(name, "."+parent.Name) {
			s.err(w, http.StatusBadRequest, "a subdomain name must end in ."+parent.Name)
			return
		}
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

	sysUser, err := s.sysUserForPanelUser(ownerID)
	if err != nil {
		s.fail(w, "provision system user", err)
		return
	}
	// Resolve alternative hostnames. Omitted (nil) defaults to www.<name> for a
	// primary domain only — subdomains/aliases previously got a www. prefix that
	// rarely resolves, which is exactly what broke SSL issuance.
	var aliasIn []string
	if req.Aliases != nil {
		aliasIn = *req.Aliases
	} else if kind == "primary" {
		aliasIn = []string{"www." + name}
	}
	aliases, msg := s.cleanAliases(ownerID, name, aliasIn)
	if msg != "" {
		s.err(w, http.StatusBadRequest, msg)
		return
	}

	webMode := s.webServer().NormalizeMode(req.WebMode)
	phpVersion := system.PHPVersions()[0]
	d := models.Domain{UserID: ownerID, Name: name, WebMode: webMode, Runtime: "php", PHPVersion: phpVersion, Kind: kind, ParentID: req.ParentID, Aliases: aliases}

	// An alias in redirect mode forwards to its parent; in mirror mode it serves
	// the parent's document root. A subdomain/primary gets its own document root.
	aliasRedirect := kind == "alias" && req.AliasMode == "redirect"
	switch {
	case aliasRedirect:
		d.DocumentRoot = parent.DocumentRoot
		d.RedirectURL = "http://" + parent.Name
		d.RedirectCode = 301
	case kind == "alias":
		d.DocumentRoot = parent.DocumentRoot
	default:
		d.DocumentRoot = filepath.Join(s.Cfg.WebRoot, sysUser, name, "public_html")
	}

	// Node runtime only applies to primary/subdomain sites with their own docroot.
	if req.Runtime == "node" && kind != "alias" {
		if !nodeVersionInstalled(req.Version) {
			s.err(w, http.StatusBadRequest, "that Node version is not installed on this server")
			return
		}
		port := s.allocNodePort()
		if port == 0 {
			s.err(w, http.StatusServiceUnavailable, "no free application port available")
			return
		}
		d.Runtime, d.NodeVersion, d.NodePort = "node", req.Version, port
	} else if req.Version != "" || req.PHPVersion != "" {
		v := req.Version
		if v == "" {
			v = req.PHPVersion
		}
		if phpVersionInstalled(v) {
			d.PHPVersion = v
		}
	}

	res, err := s.DB.Exec(`INSERT INTO domains(user_id,name,document_root,php_version,web_mode,runtime,node_version,node_port,node_startup,kind,parent_id,redirect_url,redirect_code,aliases)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		ownerID, name, d.DocumentRoot, d.PHPVersion, webMode, d.Runtime, d.NodeVersion, d.NodePort, "app.js", kind, req.ParentID, d.RedirectURL, d.RedirectCode, strings.Join(aliases, " "))
	if err != nil {
		s.err(w, http.StatusConflict, "domain already exists")
		return
	}
	d.ID, _ = res.LastInsertId()

	// Aliases reuse the parent's existing document root; everyone else gets one.
	if kind != "alias" {
		if err := system.EnsureDocRoot(d.DocumentRoot, sysUser, name); err != nil {
			s.fail(w, "create docroot", err)
			return
		}
	}
	if d.Runtime == "node" {
		system.WriteSampleApp(d, sysUser, "", "app.js") // best-effort starter app
	}
	if err := s.webServer().WriteVhost(d, sysUser, "", "", webMode); err != nil {
		s.fail(w, "write vhost", err)
		return
	}
	if d.Runtime == "node" {
		system.WriteNodeApp(d, sysUser, "", "app.js", nil)
	}

	// DNS: a subdomain adds an A record to its parent's zone (if hosted here);
	// primaries and aliases may get their own zone.
	if kind == "subdomain" {
		if err := s.addSubdomainRecord(parent, name); err != nil {
			s.fail(w, "add subdomain dns record", err)
			return
		}
	} else if req.CreateDNS {
		if err := s.createZoneForDomain(d); err != nil {
			s.fail(w, "create dns zone", err)
			return
		}
	}
	s.json(w, d)
}

// cleanAliases lowercases, trims, dedupes and validates alternative hostnames,
// dropping blanks and the domain's own name. It rejects an alias that overlaps a
// domain owned by another account (same tenant-isolation rule as the primary
// name; see SECURITY_AUDIT F-07). Returns a client error message on the first
// bad entry, else "".
func (s *Server) cleanAliases(ownerID int64, name string, in []string) ([]string, string) {
	seen := map[string]bool{name: true}
	out := []string{}
	for _, a := range in {
		a = strings.ToLower(strings.TrimSpace(a))
		if a == "" || seen[a] {
			continue
		}
		if !validDomainName(a) {
			return nil, "invalid alternative domain: " + a
		}
		var conflict int
		s.DB.QueryRow(`SELECT COUNT(*) FROM domains
			WHERE user_id != ? AND (name = ? OR ? LIKE '%.' || name OR name LIKE '%.' || ?)`,
			ownerID, a, a, a).Scan(&conflict)
		if conflict > 0 {
			return nil, "alternative domain overlaps a domain owned by another account: " + a
		}
		seen[a] = true
		out = append(out, a)
	}
	return out, ""
}

// domainLimitReached returns a message if the owner is at their domain cap, else
// "". Admins and unlimited (0) accounts are never capped.
func (s *Server) domainLimitReached(ownerID int64) string {
	owner, err := auth.GetUserByID(s.DB, ownerID)
	if err != nil || owner == nil || owner.Role == models.RoleAdmin || owner.MaxDomains <= 0 {
		return ""
	}
	var n int64
	s.DB.QueryRow(`SELECT COUNT(*) FROM domains WHERE user_id = ?`, ownerID).Scan(&n)
	if n >= owner.MaxDomains {
		return fmt.Sprintf("domain limit reached (%d) — ask your provider to raise it", owner.MaxDomains)
	}
	return ""
}

// addSubdomainRecord inserts an A record for a new subdomain into its parent's
// DNS zone, when that zone is hosted on this panel. No-op otherwise.
func (s *Server) addSubdomainRecord(parent *models.Domain, subname string) error {
	var zoneID int64
	if err := s.DB.QueryRow(`SELECT id FROM dns_zones WHERE domain_id = ?`, parent.ID).Scan(&zoneID); err != nil {
		return nil // parent has no zone here; nothing to add
	}
	label := strings.TrimSuffix(subname, "."+parent.Name)
	if label == "" || label == subname {
		return nil
	}
	ip := s.DB.Setting("server_ip")
	if ip == "" {
		ip = "127.0.0.1"
	}
	if _, err := s.DB.Exec(`INSERT INTO dns_records(zone_id,name,type,value,ttl,priority) VALUES(?,?,?,?,3600,0)`,
		zoneID, label, "A", ip); err != nil {
		return err
	}
	return s.writeZoneFile(zoneID)
}

// removeSubdomainRecord deletes the A record a subdomain added to its parent's
// zone. Best-effort: matches on the subdomain label within the parent zone.
func (s *Server) removeSubdomainRecord(d *models.Domain) {
	var parentName string
	var zoneID int64
	if s.DB.QueryRow(`SELECT z.id, p.name FROM domains p JOIN dns_zones z ON z.domain_id = p.id WHERE p.id = ?`, d.ParentID).
		Scan(&zoneID, &parentName) != nil {
		return
	}
	label := strings.TrimSuffix(d.Name, "."+parentName)
	if label == "" || label == d.Name {
		return
	}
	s.DB.Exec(`DELETE FROM dns_records WHERE zone_id = ? AND type = 'A' AND name = ?`, zoneID, label)
	s.writeZoneFile(zoneID)
}

// nodeVersionInstalled reports whether a Node major is installed on the host.
func nodeVersionInstalled(v string) bool {
	for _, x := range system.InstalledNodeVersions() {
		if x == v {
			return true
		}
	}
	return false
}

// phpVersionInstalled reports whether a PHP version is installed on the host.
func phpVersionInstalled(v string) bool {
	for _, x := range system.PHPVersions() {
		if x == v {
			return true
		}
	}
	return false
}

// allocNodePort returns the first free loopback port in 30000-39999 not already
// assigned to a domain, or 0 when the range is exhausted.
func (s *Server) allocNodePort() int {
	used := map[int]bool{}
	rows, err := s.DB.Query(`SELECT node_port FROM domains WHERE node_port > 0`)
	if err == nil {
		for rows.Next() {
			var p int
			if rows.Scan(&p) == nil {
				used[p] = true
			}
		}
		rows.Close()
	}
	for p := system.NodeAppPortLow; p <= system.NodeAppPortHigh; p++ {
		if !used[p] {
			return p
		}
	}
	return 0
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
	// Tear down any subdomains/aliases hanging off this domain first (their
	// parent_id has no DB-level cascade since it was added by migration).
	if d.Kind == "primary" {
		crows, _ := s.DB.Query(`SELECT id FROM domains WHERE parent_id = ?`, d.ID)
		var childIDs []int64
		if crows != nil {
			for crows.Next() {
				var cid int64
				if crows.Scan(&cid) == nil {
					childIDs = append(childIDs, cid)
				}
			}
			crows.Close()
		}
		for _, cid := range childIDs {
			if child, cerr := s.getDomainScoped(u, cid); cerr == nil {
				s.webServer().RemoveVhost(*child)
				system.RemoveDomainHtpasswd(child.Name)
				system.RemoveZone(s.Cfg.BindDir, child.Name, system.ParseSlaveIPs(s.DB.Setting("slave_dns")))
				s.DB.Exec(`DELETE FROM domains WHERE id = ?`, cid)
			}
		}
	}
	if d.Runtime == "node" {
		system.RemoveNodeApp(*d)
	}
	s.webServer().RemoveVhost(*d)
	system.RemoveDomainHtpasswd(d.Name)
	// A subdomain leaves an A record behind in its parent's zone; drop it.
	if d.Kind == "subdomain" {
		s.removeSubdomainRecord(d)
	}
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

// handleDomainDocRoot changes a site's document root so an app that serves from
// a subfolder (e.g. Laravel's public/) can be the web root. The new root must
// live inside the domain's own web space (/<webroot>/<sysuser>/<domain>/…);
// anything outside is rejected. Aliases (which share their parent's root) and
// Node apps (which use the app directory) are not eligible.
func (s *Server) handleDomainDocRoot(w http.ResponseWriter, r *http.Request, u *models.User) {
	d, err := s.getDomainScoped(u, pathID(r, "id"))
	if err != nil {
		s.err(w, http.StatusNotFound, err.Error())
		return
	}
	if d.Kind == "alias" {
		s.err(w, http.StatusBadRequest, "an alias shares its parent's document root")
		return
	}
	if d.Runtime == "node" {
		s.err(w, http.StatusBadRequest, "Node apps are served from their app directory, not a document root")
		return
	}
	req, err := decode[struct {
		DocumentRoot string `json:"document_root"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	sysUser, err := s.sysUserForPanelUser(d.UserID)
	if err != nil {
		s.fail(w, "provision system user", err)
		return
	}
	base := filepath.Join(s.Cfg.WebRoot, sysUser, d.Name)
	root, err := system.ResolveDocRoot(base, req.DocumentRoot)
	if err != nil {
		s.err(w, http.StatusBadRequest, "document root must be a folder inside "+base)
		return
	}
	if err := system.EnsureDocRoot(root, sysUser, d.Name); err != nil {
		s.fail(w, "create docroot", err)
		return
	}
	d.DocumentRoot = root
	if _, err := s.DB.Exec(`UPDATE domains SET document_root = ? WHERE id = ?`, root, d.ID); err != nil {
		s.fail(w, "update domain", err)
		return
	}
	if d.Suspended {
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
