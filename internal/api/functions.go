package api

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/reinis1996/repanel/internal/models"
	"github.com/reinis1996/repanel/internal/system"
)

// functionInvokeTimeout bounds a manual test run.
const functionInvokeTimeout = 30 * time.Second

// functionManager builds the deployer for the configured paths.
func (s *Server) functionManager() *system.FunctionManager {
	return system.NewFunctionManager(s.Cfg.WebRoot, s.Cfg.NginxDir)
}

// functionColumns is the canonical column list for scanFunction.
const functionColumns = `id, user_id, name, slug, runtime, version, trigger_type, schedule,
	allow_network, base_domain, hostname, enabled, ssl, status, error, created_at`

// functionURL is the public URL for a URL-triggered function ("" for scheduled).
func functionURL(fn models.Function) string {
	if fn.Trigger == system.TriggerSchedule {
		return ""
	}
	return "https://" + fn.Hostname
}

func (s *Server) handleFunctionList(w http.ResponseWriter, r *http.Request, u *models.User) {
	where, args := scopeWhere(u, "user_id")
	rows, err := s.DB.Query(`SELECT `+functionColumns+` FROM functions
		WHERE `+where+` ORDER BY name, id`, args...)
	if err != nil {
		s.fail(w, "list functions", err)
		return
	}
	defer rows.Close()
	out := []models.Function{}
	for rows.Next() {
		fn, err := scanFunction(rows)
		if err == nil {
			fn.URL = functionURL(fn)
			out = append(out, fn)
		}
	}
	s.json(w, out)
}

// handleFunctionMeta drives the create form: installed runtimes/versions, the
// caller's domains that can host a function URL (those with a DNS zone here),
// and the default base (the panel FQDN).
func (s *Server) handleFunctionMeta(w http.ResponseWriter, r *http.Request, u *models.User) {
	where, args := scopeWhere(u, "d.user_id")
	rows, err := s.DB.Query(`SELECT d.name FROM domains d
		JOIN dns_zones z ON z.domain_id = d.id WHERE `+where+` ORDER BY d.name`, args...)
	domains := []string{}
	if err == nil {
		for rows.Next() {
			var name string
			if rows.Scan(&name) == nil {
				domains = append(domains, name)
			}
		}
		rows.Close()
	}
	s.json(w, models.FunctionMeta{
		Runtimes:    system.AvailableFunctionRuntimes(),
		Domains:     domains,
		DefaultBase: s.DB.Setting("panel_hostname"),
	})
}

func (s *Server) handleFunctionGet(w http.ResponseWriter, r *http.Request, u *models.User) {
	fn, ok := s.functionScoped(u, pathID(r, "id"))
	if !ok {
		s.err(w, http.StatusNotFound, "function not found")
		return
	}
	sysUser := system.SysUserName(fn.UserID)
	fn.Code = s.functionManager().ReadCode(*fn, sysUser)
	fn.URL = functionURL(*fn)
	s.json(w, fn)
}

func (s *Server) handleFunctionCreate(w http.ResponseWriter, r *http.Request, u *models.User) {
	if s.quotaExceeded(u) {
		s.err(w, http.StatusForbidden, quotaMsg)
		return
	}
	req, err := decode[struct {
		Name         string `json:"name"`
		Runtime      string `json:"runtime"`
		Version      string `json:"version"`
		Trigger      string `json:"trigger"`
		Schedule     string `json:"schedule"`
		AllowNetwork bool   `json:"allow_network"`
		BaseDomain   string `json:"base_domain"`
		Code         string `json:"code"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > 64 {
		s.err(w, http.StatusBadRequest, "name is required (max 64 characters)")
		return
	}
	if !system.FunctionRuntimeValid(req.Runtime, req.Version) {
		s.err(w, http.StatusBadRequest, "that runtime/version is not installed on this server")
		return
	}
	trigger := req.Trigger
	if trigger == "" {
		trigger = system.TriggerURL
	}
	if trigger != system.TriggerURL && trigger != system.TriggerSchedule {
		s.err(w, http.StatusBadRequest, "trigger must be 'url' or 'schedule'")
		return
	}

	schedule := strings.TrimSpace(req.Schedule)
	if trigger == system.TriggerSchedule {
		if err := system.ValidateCronSchedule(schedule); err != nil {
			s.err(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	slug, err := s.uniqueFunctionSlug()
	if err != nil {
		s.fail(w, "allocate slug", err)
		return
	}

	// Resolve the URL hostname (and the zone to publish into) for URL functions.
	storedBase, hostname := "", ""
	var zoneID int64
	zoned := false
	ssl := 0
	if trigger == system.TriggerURL {
		var msg string
		hostname, storedBase, zoneID, zoned, msg = s.resolveFunctionBase(u, slug, req.BaseDomain)
		if msg != "" {
			s.err(w, http.StatusBadRequest, msg)
			return
		}
		ssl = 1
	}

	res, err := s.DB.Exec(`INSERT INTO functions(user_id,name,slug,runtime,version,trigger_type,schedule,allow_network,base_domain,hostname,enabled,ssl,status)
		VALUES(?,?,?,?,?,?,?,?,?,?,1,?,'active')`,
		u.ID, name, slug, req.Runtime, req.Version, trigger, schedule, boolInt(req.AllowNetwork), storedBase, hostname, ssl)
	if err != nil {
		s.fail(w, "record function", err)
		return
	}
	id, _ := res.LastInsertId()
	fn := models.Function{ID: id, UserID: u.ID, Name: name, Slug: slug, Runtime: req.Runtime,
		Version: req.Version, Trigger: trigger, Schedule: schedule, AllowNetwork: req.AllowNetwork,
		BaseDomain: storedBase, Hostname: hostname, Enabled: true, SSL: ssl != 0, Status: "active"}

	sysUser, err := s.sysUserForPanelUser(u.ID)
	if err != nil {
		s.DB.Exec(`DELETE FROM functions WHERE id = ?`, id)
		s.fail(w, "provision system user", err)
		return
	}
	code := req.Code
	if strings.TrimSpace(code) == "" {
		code = system.DefaultFunctionCode(req.Runtime)
	}

	certPath, keyPath := "", ""
	if trigger == system.TriggerURL {
		certPath, keyPath, err = s.ensureFunctionCert(hostname)
		if err != nil {
			s.DB.Exec(`DELETE FROM functions WHERE id = ?`, id)
			s.fail(w, "issue certificate", err)
			return
		}
	}
	if err := s.functionManager().Write(fn, sysUser, code, certPath, keyPath); err != nil {
		s.functionManager().Remove(fn, sysUser)
		s.DB.Exec(`DELETE FROM functions WHERE id = ?`, id)
		s.fail(w, "deploy function", err)
		return
	}

	if trigger == system.TriggerSchedule {
		if err := s.syncFunctionCron(); err != nil {
			s.fail(w, "schedule function", err)
			return
		}
	} else if zoned {
		// Publish wildcard A/AAAA records so every function on this domain resolves.
		if s.DB.Setting("server_ip") != "" || s.DB.Setting("server_ipv6") != "" {
			s.ensureAddrRecords(zoneID, "*."+system.FunctionSubdomain)
			if err := s.writeZoneFile(zoneID); err != nil {
				s.fail(w, "write zone", err)
				return
			}
		}
	}

	fn.Code = code
	fn.URL = functionURL(fn)
	s.json(w, fn)
}

// handleFunctionUpdate applies any subset of a function's settings — the same
// ones available at creation (name, runtime, version, trigger, schedule,
// network, base, code) plus the enabled flag — then re-provisions. Fields are
// optional pointers so the list's enable/disable toggle (which sends only
// "enabled") keeps working unchanged.
func (s *Server) handleFunctionUpdate(w http.ResponseWriter, r *http.Request, u *models.User) {
	fn, ok := s.functionScoped(u, pathID(r, "id"))
	if !ok {
		s.err(w, http.StatusNotFound, "function not found")
		return
	}
	req, err := decode[struct {
		Name         *string `json:"name"`
		Runtime      *string `json:"runtime"`
		Version      *string `json:"version"`
		Trigger      *string `json:"trigger"`
		Schedule     *string `json:"schedule"`
		AllowNetwork *bool   `json:"allow_network"`
		BaseDomain   *string `json:"base_domain"`
		Code         *string `json:"code"`
		Enabled      *bool   `json:"enabled"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	sysUser, err := s.sysUserForPanelUser(u.ID)
	if err != nil {
		s.fail(w, "resolve system user", err)
		return
	}
	mgr := s.functionManager()
	old := *fn

	// Overlay the provided fields onto the current settings.
	eff := *fn
	if req.Name != nil {
		eff.Name = strings.TrimSpace(*req.Name)
	}
	if req.Runtime != nil {
		eff.Runtime = *req.Runtime
	}
	if req.Version != nil {
		eff.Version = *req.Version
	}
	if req.Trigger != nil {
		eff.Trigger = *req.Trigger
	}
	if req.Schedule != nil {
		eff.Schedule = strings.TrimSpace(*req.Schedule)
	}
	if req.AllowNetwork != nil {
		eff.AllowNetwork = *req.AllowNetwork
	}
	if req.BaseDomain != nil {
		eff.BaseDomain = strings.ToLower(strings.TrimSpace(*req.BaseDomain))
	}
	if req.Enabled != nil {
		eff.Enabled = *req.Enabled
	}

	// Validate the effective settings.
	if eff.Name == "" || len(eff.Name) > 64 {
		s.err(w, http.StatusBadRequest, "name is required (max 64 characters)")
		return
	}
	if !system.FunctionRuntimeValid(eff.Runtime, eff.Version) {
		s.err(w, http.StatusBadRequest, "that runtime/version is not installed on this server")
		return
	}
	if eff.Trigger != system.TriggerURL && eff.Trigger != system.TriggerSchedule {
		s.err(w, http.StatusBadRequest, "trigger must be 'url' or 'schedule'")
		return
	}
	var newZoneID int64
	newZoned := false
	if eff.Trigger == system.TriggerSchedule {
		if err := system.ValidateCronSchedule(eff.Schedule); err != nil {
			s.err(w, http.StatusBadRequest, err.Error())
			return
		}
		eff.BaseDomain, eff.Hostname, eff.SSL = "", "", false
	} else {
		hostname, storedBase, zoneID, zoned, msg := s.resolveFunctionBase(u, eff.Slug, eff.BaseDomain)
		if msg != "" {
			s.err(w, http.StatusBadRequest, msg)
			return
		}
		eff.Hostname, eff.BaseDomain, eff.SSL = hostname, storedBase, true
		newZoneID, newZoned = zoneID, zoned
	}

	// Determine the handler source to deploy: provided code, else the current
	// code; when switching runtime with no code given, start from the template.
	code := mgr.ReadCode(old, sysUser)
	if req.Code != nil {
		code = *req.Code
	} else if eff.Runtime != old.Runtime || code == "" {
		code = system.DefaultFunctionCode(eff.Runtime)
	}

	// Re-provision: drop the old backend/vhost (keeping code), persist, redeploy.
	mgr.Teardown(old)
	if _, err := s.DB.Exec(`UPDATE functions SET name=?, runtime=?, version=?, trigger_type=?, schedule=?,
		allow_network=?, base_domain=?, hostname=?, ssl=?, enabled=? WHERE id=?`,
		eff.Name, eff.Runtime, eff.Version, eff.Trigger, eff.Schedule, boolInt(eff.AllowNetwork),
		eff.BaseDomain, eff.Hostname, boolInt(eff.SSL), boolInt(eff.Enabled), eff.ID); err != nil {
		s.fail(w, "update function", err)
		return
	}

	if eff.Enabled {
		certPath, keyPath := "", ""
		if eff.Trigger == system.TriggerURL {
			certPath, keyPath, err = s.ensureFunctionCert(eff.Hostname)
			if err != nil {
				s.fail(w, "issue certificate", err)
				return
			}
		}
		if err := mgr.Write(eff, sysUser, code, certPath, keyPath); err != nil {
			s.fail(w, "deploy function", err)
			return
		}
	} else if err := mgr.SaveCode(eff, sysUser, code); err != nil {
		s.err(w, http.StatusBadRequest, err.Error())
		return
	}
	// Reload nginx when a URL vhost was removed without a new one being deployed.
	if old.Trigger == system.TriggerURL && !(eff.Enabled && eff.Trigger == system.TriggerURL) {
		mgr.Reload()
	}

	if err := s.syncFunctionCron(); err != nil {
		s.fail(w, "schedule function", err)
		return
	}

	// DNS: drop the old base's wildcard if it is now unused; publish the new one.
	if old.BaseDomain != "" && (old.BaseDomain != eff.BaseDomain || eff.Trigger != system.TriggerURL) {
		s.maybeRemoveFunctionWildcard(u, old.BaseDomain)
	}
	if eff.Trigger == system.TriggerURL && newZoned {
		if s.DB.Setting("server_ip") != "" || s.DB.Setting("server_ipv6") != "" {
			s.ensureAddrRecords(newZoneID, "*."+system.FunctionSubdomain)
			if err := s.writeZoneFile(newZoneID); err != nil {
				s.fail(w, "write zone", err)
				return
			}
		}
	}

	eff.Code = code
	eff.URL = functionURL(eff)
	s.json(w, eff)
}

// handleFunctionSSL upgrades a function to a Let's Encrypt certificate, falling
// back transparently to the existing self-signed cert on failure.
func (s *Server) handleFunctionSSL(w http.ResponseWriter, r *http.Request, u *models.User) {
	fn, ok := s.functionScoped(u, pathID(r, "id"))
	if !ok {
		s.err(w, http.StatusNotFound, "function not found")
		return
	}
	if fn.Trigger != system.TriggerURL {
		s.err(w, http.StatusBadRequest, "only URL functions have a certificate")
		return
	}
	sysUser, err := s.sysUserForPanelUser(u.ID)
	if err != nil {
		s.fail(w, "resolve system user", err)
		return
	}
	docroot := system.FunctionDir(s.Cfg.WebRoot, sysUser, fn.Slug)
	certPath, keyPath, err := system.IssueLetsEncryptHosts(docroot, s.DB.Setting("admin_email"), fn.Hostname)
	if err != nil {
		s.fail(w, "issue certificate", err)
		return
	}
	if err := s.functionManager().Write(*fn, sysUser, s.functionManager().ReadCode(*fn, sysUser), certPath, keyPath); err != nil {
		s.fail(w, "redeploy function", err)
		return
	}
	s.json(w, map[string]bool{"ok": true})
}

// handleFunctionInvoke runs a function once with a caller-supplied JSON payload
// and returns its response, logs and duration — the manual "Test" action.
func (s *Server) handleFunctionInvoke(w http.ResponseWriter, r *http.Request, u *models.User) {
	fn, ok := s.functionScoped(u, pathID(r, "id"))
	if !ok {
		s.err(w, http.StatusNotFound, "function not found")
		return
	}
	req, err := decode[struct {
		Payload string `json:"payload"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	sysUser, err := s.sysUserForPanelUser(u.ID)
	if err != nil {
		s.fail(w, "resolve system user", err)
		return
	}
	res, err := s.functionManager().Invoke(*fn, sysUser, req.Payload, functionInvokeTimeout)
	if err != nil {
		s.err(w, http.StatusBadRequest, err.Error())
		return
	}
	s.json(w, res)
}

func (s *Server) handleFunctionDelete(w http.ResponseWriter, r *http.Request, u *models.User) {
	fn, ok := s.functionScoped(u, pathID(r, "id"))
	if !ok {
		s.err(w, http.StatusNotFound, "function not found")
		return
	}
	sysUser := system.SysUserName(fn.UserID)
	if err := s.functionManager().Remove(*fn, sysUser); err != nil {
		s.fail(w, "remove function", err)
		return
	}
	if _, err := s.DB.Exec(`DELETE FROM functions WHERE id = ?`, fn.ID); err != nil {
		s.fail(w, "delete function", err)
		return
	}

	if fn.Trigger == system.TriggerSchedule {
		if err := s.syncFunctionCron(); err != nil {
			s.fail(w, "rebuild function cron", err)
			return
		}
		s.json(w, map[string]bool{"ok": true})
		return
	}

	// Drop the wildcard A record once a domain has no functions left.
	s.maybeRemoveFunctionWildcard(u, fn.BaseDomain)
	s.json(w, map[string]bool{"ok": true})
}

// resolveFunctionBase turns a URL function's base choice into a hostname. An
// empty choice uses the panel FQDN (panel_hostname); a domain must be one the
// caller owns and that has a DNS zone here. Returns the full hostname, the value
// to store in base_domain (normalized choice, "" for the panel FQDN), the zone
// id and whether DNS is managed here, or a user-facing error message.
func (s *Server) resolveFunctionBase(u *models.User, slug, baseInput string) (hostname, storedBase string, zoneID int64, zoned bool, errMsg string) {
	storedBase = strings.ToLower(strings.TrimSpace(baseInput))
	base := storedBase
	if base == "" {
		base = strings.TrimSpace(s.DB.Setting("panel_hostname"))
		if base == "" {
			return "", "", 0, false, "no panel hostname is configured; set one in Settings or pick a domain"
		}
	} else {
		d, ok := s.domainScopedByName(u, base)
		if !ok {
			return "", "", 0, false, "domain not found in your account"
		}
		zoneID, zoned = s.zoneIDForDomain(d.ID)
		if !zoned {
			return "", "", 0, false, "that domain has no DNS zone here; create one on the DNS page first"
		}
	}
	return slug + "." + system.FunctionSubdomain + "." + base, storedBase, zoneID, zoned, ""
}

// maybeRemoveFunctionWildcard removes the "*.function-url" A record from base's
// zone when no functions are left on that base.
func (s *Server) maybeRemoveFunctionWildcard(u *models.User, base string) {
	if base == "" {
		return
	}
	var remaining int
	s.DB.QueryRow(`SELECT COUNT(*) FROM functions WHERE base_domain = ?`, base).Scan(&remaining)
	if remaining > 0 {
		return
	}
	d, ok := s.domainScopedByName(u, base)
	if !ok {
		return
	}
	zoneID, ok := s.zoneIDForDomain(d.ID)
	if !ok {
		return
	}
	s.DB.Exec(`DELETE FROM dns_records WHERE zone_id = ? AND name = ? AND type IN ('A','AAAA')`,
		zoneID, "*."+system.FunctionSubdomain)
	s.writeZoneFile(zoneID)
}

// ---- helpers ----

func (s *Server) functionScoped(u *models.User, id int64) (*models.Function, bool) {
	where, args := scopeWhere(u, "user_id")
	args = append([]any{id}, args...)
	row := s.DB.QueryRow(`SELECT `+functionColumns+` FROM functions
		WHERE id = ? AND `+where, args...)
	fn, err := scanFunction(row)
	return &fn, err == nil
}

func scanFunction(row interface{ Scan(...any) error }) (models.Function, error) {
	var fn models.Function
	var enabled, ssl, allowNet int
	err := row.Scan(&fn.ID, &fn.UserID, &fn.Name, &fn.Slug, &fn.Runtime, &fn.Version,
		&fn.Trigger, &fn.Schedule, &allowNet, &fn.BaseDomain, &fn.Hostname, &enabled, &ssl,
		&fn.Status, &fn.Error, &fn.CreatedAt)
	fn.Enabled, fn.SSL, fn.AllowNetwork = enabled != 0, ssl != 0, allowNet != 0
	return fn, err
}

// syncFunctionCron rebuilds the function cron.d file from every scheduled
// function in the database (mirrors syncCrontab for user cron jobs).
func (s *Server) syncFunctionCron() error {
	rows, err := s.DB.Query(`SELECT ` + functionColumns + ` FROM functions WHERE trigger_type = 'schedule'`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var funcs []models.Function
	for rows.Next() {
		if fn, err := scanFunction(rows); err == nil {
			funcs = append(funcs, fn)
		}
	}
	cache := map[int64]string{}
	return s.functionManager().RebuildCron(funcs, func(userID int64) string {
		if name, ok := cache[userID]; ok {
			return name
		}
		name, err := s.sysUserForPanelUser(userID)
		if err != nil {
			name = ""
		}
		cache[userID] = name
		return name
	})
}

// uniqueFunctionSlug returns a fresh DNS-label-safe slug not already in use.
func (s *Server) uniqueFunctionSlug() (string, error) {
	for i := 0; i < 8; i++ {
		b := make([]byte, 5)
		if _, err := rand.Read(b); err != nil {
			return "", err
		}
		slug := hex.EncodeToString(b) // 10 lowercase hex chars
		var n int
		s.DB.QueryRow(`SELECT COUNT(*) FROM functions WHERE slug = ?`, slug).Scan(&n)
		if n == 0 {
			return slug, nil
		}
	}
	return "", fmt.Errorf("could not allocate a unique slug")
}

// ensureFunctionCert returns the cert/key paths for a function hostname,
// preferring an existing Let's Encrypt certificate and otherwise generating (or
// reusing) a self-signed one so HTTPS works immediately.
func (s *Server) ensureFunctionCert(hostname string) (certPath, keyPath string, err error) {
	certPath, keyPath = s.functionCertPaths(hostname)
	if _, e := os.Stat(certPath); e == nil {
		return certPath, keyPath, nil
	}
	c, k, _, e := system.IssueSelfSigned(s.Cfg.DataDir, hostname)
	if e != nil {
		return "", "", e
	}
	return c, k, nil
}

// functionCertPaths returns the LE live paths when present, else the self-signed
// paths (which may not exist yet).
func (s *Server) functionCertPaths(hostname string) (certPath, keyPath string) {
	live := filepath.Join("/etc/letsencrypt/live", hostname)
	if _, err := os.Stat(filepath.Join(live, "fullchain.pem")); err == nil {
		return filepath.Join(live, "fullchain.pem"), filepath.Join(live, "privkey.pem")
	}
	dir := system.CertDir(s.Cfg.DataDir, hostname)
	return filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
}
