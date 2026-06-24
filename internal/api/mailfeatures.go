package api

import (
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/reinis1996/repanel/internal/models"
	"github.com/reinis1996/repanel/internal/system"
)

// API for the mail feature set built on Dovecot LMTP: distribution lists,
// autoresponders, Sieve filters, the outbound smarthost and imapsync migrations.

// SyncMailDelivery wires up Dovecot LMTP delivery at startup when the Sieve plugin
// is present, so per-mailbox features and quotas take effect. A no-op otherwise.
func (s *Server) SyncMailDelivery() {
	if err := system.EnsureMailDelivery(s.Cfg.MailDir); err != nil {
		// Non-fatal: mail keeps working on the previous delivery config.
		s.fail0("ensure mail delivery", err)
	}
	// Keep Postfix's identity aligned with the configured panel hostname (when it
	// is a FQDN), so existing installs pick this up on the next start.
	if h := strings.TrimSpace(s.DB.Setting("panel_hostname")); strings.Contains(h, ".") {
		s.fail0("set mail hostname", system.SetMailHostname(h))
	}
}

// fail0 logs a background error (no HTTP response to write).
func (s *Server) fail0(op string, err error) {
	if err != nil {
		log.Printf("ERROR %s: %v", op, err)
	}
}

// rebuildMailboxSieve regenerates one mailbox's Sieve script from its filters and
// autoresponder.
func (s *Server) rebuildMailboxSieve(mailboxID int64, address string) error {
	var ar models.MailAutoresponder
	var enabled int
	s.DB.QueryRow(`SELECT enabled, subject, message, start_date, end_date FROM mail_autoresponders WHERE mailbox_id = ?`, mailboxID).
		Scan(&enabled, &ar.Subject, &ar.Message, &ar.StartDate, &ar.EndDate)
	ar.Enabled = enabled != 0

	filters := []models.MailFilter{}
	rows, err := s.DB.Query(`SELECT field, op, value, action, arg FROM mail_filters WHERE mailbox_id = ? ORDER BY position, id`, mailboxID)
	if err == nil {
		for rows.Next() {
			var f models.MailFilter
			if rows.Scan(&f.Field, &f.Op, &f.Value, &f.Action, &f.Arg) == nil {
				filters = append(filters, f)
			}
		}
		rows.Close()
	}
	return system.WriteMailboxSieve(address, system.BuildSieveScript(filters, ar))
}

// ---- Distribution lists -----------------------------------------------------

func (s *Server) handleListCreate(w http.ResponseWriter, r *http.Request, u *models.User) {
	req, err := decode[struct {
		Address string   `json:"address"`
		Members []string `json:"members"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	addr := strings.ToLower(strings.TrimSpace(req.Address))
	local, domainName, ok := strings.Cut(addr, "@")
	if !ok || !validMailLocalPart(local) || !validDomainName(domainName) {
		s.err(w, http.StatusBadRequest, "invalid list address")
		return
	}
	members, ok := normalizeMembers(req.Members)
	if !ok {
		s.err(w, http.StatusBadRequest, "every member must be a valid email address")
		return
	}
	d, ok := s.domainScopedByName(u, domainName)
	if !ok {
		s.err(w, http.StatusNotFound, "domain not found in your account")
		return
	}
	res, err := s.DB.Exec(`INSERT INTO mail_lists(domain_id,address,members) VALUES(?,?,?)`,
		d.ID, addr, strings.Join(members, "\n"))
	if err != nil {
		s.err(w, http.StatusConflict, "a list or address already exists")
		return
	}
	if err := s.rebuildMail(); err != nil {
		s.fail(w, "rebuild mail maps", err)
		return
	}
	id, _ := res.LastInsertId()
	s.json(w, models.MailList{ID: id, DomainID: d.ID, Address: addr, Members: members})
}

func (s *Server) handleListUpdate(w http.ResponseWriter, r *http.Request, u *models.User) {
	id := pathID(r, "id")
	if !s.listScoped(u, id) {
		s.err(w, http.StatusNotFound, "list not found")
		return
	}
	req, err := decode[struct {
		Members []string `json:"members"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	members, ok := normalizeMembers(req.Members)
	if !ok {
		s.err(w, http.StatusBadRequest, "every member must be a valid email address")
		return
	}
	if _, err := s.DB.Exec(`UPDATE mail_lists SET members = ? WHERE id = ?`, strings.Join(members, "\n"), id); err != nil {
		s.fail(w, "update list", err)
		return
	}
	if err := s.rebuildMail(); err != nil {
		s.fail(w, "rebuild mail maps", err)
		return
	}
	s.json(w, map[string]bool{"ok": true})
}

func (s *Server) handleListDelete(w http.ResponseWriter, r *http.Request, u *models.User) {
	id := pathID(r, "id")
	if !s.listScoped(u, id) {
		s.err(w, http.StatusNotFound, "list not found")
		return
	}
	if _, err := s.DB.Exec(`DELETE FROM mail_lists WHERE id = ?`, id); err != nil {
		s.fail(w, "delete list", err)
		return
	}
	if err := s.rebuildMail(); err != nil {
		s.fail(w, "rebuild mail maps", err)
		return
	}
	s.json(w, map[string]bool{"ok": true})
}

func (s *Server) listScoped(u *models.User, id int64) bool {
	where, args := scopeWhere(u, "d.user_id")
	args = append([]any{id}, args...)
	var x int64
	return s.DB.QueryRow(`SELECT l.id FROM mail_lists l JOIN domains d ON d.id = l.domain_id
		WHERE l.id = ? AND `+where, args...).Scan(&x) == nil
}

func normalizeMembers(in []string) ([]string, bool) {
	var out []string
	for _, m := range in {
		m = strings.ToLower(strings.TrimSpace(m))
		if m == "" {
			continue
		}
		if !validMailTarget(m) {
			return nil, false
		}
		out = append(out, m)
	}
	return out, len(out) > 0
}

// ---- Autoresponder ----------------------------------------------------------

func (s *Server) handleAutoresponderGet(w http.ResponseWriter, r *http.Request, u *models.User) {
	id := pathID(r, "id")
	if _, ok := s.mailboxScoped(u, id); !ok {
		s.err(w, http.StatusNotFound, "mailbox not found")
		return
	}
	ar := models.MailAutoresponder{MailboxID: id}
	var enabled int
	s.DB.QueryRow(`SELECT enabled, subject, message, start_date, end_date FROM mail_autoresponders WHERE mailbox_id = ?`, id).
		Scan(&enabled, &ar.Subject, &ar.Message, &ar.StartDate, &ar.EndDate)
	ar.Enabled = enabled != 0
	s.json(w, ar)
}

func (s *Server) handleAutoresponderSet(w http.ResponseWriter, r *http.Request, u *models.User) {
	id := pathID(r, "id")
	addr, ok := s.mailboxScoped(u, id)
	if !ok {
		s.err(w, http.StatusNotFound, "mailbox not found")
		return
	}
	req, err := decode[struct {
		Enabled   bool   `json:"enabled"`
		Subject   string `json:"subject"`
		Message   string `json:"message"`
		StartDate string `json:"start_date"`
		EndDate   string `json:"end_date"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Enabled && strings.TrimSpace(req.Message) == "" {
		s.err(w, http.StatusBadRequest, "an autoresponder needs a message")
		return
	}
	if _, err := s.DB.Exec(`INSERT INTO mail_autoresponders(mailbox_id,enabled,subject,message,start_date,end_date)
		VALUES(?,?,?,?,?,?)
		ON CONFLICT(mailbox_id) DO UPDATE SET enabled=excluded.enabled, subject=excluded.subject,
			message=excluded.message, start_date=excluded.start_date, end_date=excluded.end_date`,
		id, boolInt(req.Enabled), req.Subject, req.Message, req.StartDate, req.EndDate); err != nil {
		s.fail(w, "save autoresponder", err)
		return
	}
	if err := s.rebuildMailboxSieve(id, addr); err != nil {
		s.fail(w, "write sieve script", err)
		return
	}
	s.json(w, map[string]bool{"ok": true})
}

// ---- Filters ----------------------------------------------------------------

var (
	validFilterField  = map[string]bool{"from": true, "to": true, "subject": true, "any": true}
	validFilterOp     = map[string]bool{"contains": true, "is": true}
	validFilterAction = map[string]bool{"fileinto": true, "forward": true, "discard": true, "keep": true}
)

func (s *Server) handleFiltersGet(w http.ResponseWriter, r *http.Request, u *models.User) {
	id := pathID(r, "id")
	if _, ok := s.mailboxScoped(u, id); !ok {
		s.err(w, http.StatusNotFound, "mailbox not found")
		return
	}
	out := []models.MailFilter{}
	rows, err := s.DB.Query(`SELECT id, mailbox_id, position, field, op, value, action, arg
		FROM mail_filters WHERE mailbox_id = ? ORDER BY position, id`, id)
	if err != nil {
		s.fail(w, "list filters", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var f models.MailFilter
		if rows.Scan(&f.ID, &f.MailboxID, &f.Position, &f.Field, &f.Op, &f.Value, &f.Action, &f.Arg) == nil {
			out = append(out, f)
		}
	}
	s.json(w, out)
}

func (s *Server) handleFilterCreate(w http.ResponseWriter, r *http.Request, u *models.User) {
	id := pathID(r, "id")
	addr, ok := s.mailboxScoped(u, id)
	if !ok {
		s.err(w, http.StatusNotFound, "mailbox not found")
		return
	}
	req, err := decode[struct {
		Field  string `json:"field"`
		Op     string `json:"op"`
		Value  string `json:"value"`
		Action string `json:"action"`
		Arg    string `json:"arg"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !validFilterField[req.Field] || !validFilterOp[req.Op] || !validFilterAction[req.Action] {
		s.err(w, http.StatusBadRequest, "invalid filter rule")
		return
	}
	if strings.TrimSpace(req.Value) == "" {
		s.err(w, http.StatusBadRequest, "the rule needs a value to match")
		return
	}
	if req.Action == "forward" && !validMailTarget(req.Arg) {
		s.err(w, http.StatusBadRequest, "forward needs a valid email address")
		return
	}
	if req.Action == "fileinto" && strings.TrimSpace(req.Arg) == "" {
		s.err(w, http.StatusBadRequest, "file-into needs a folder name")
		return
	}
	var pos int
	s.DB.QueryRow(`SELECT COALESCE(MAX(position),0)+1 FROM mail_filters WHERE mailbox_id = ?`, id).Scan(&pos)
	if _, err := s.DB.Exec(`INSERT INTO mail_filters(mailbox_id,position,field,op,value,action,arg)
		VALUES(?,?,?,?,?,?,?)`, id, pos, req.Field, req.Op, req.Value, req.Action, strings.TrimSpace(req.Arg)); err != nil {
		s.fail(w, "create filter", err)
		return
	}
	if err := s.rebuildMailboxSieve(id, addr); err != nil {
		s.fail(w, "write sieve script", err)
		return
	}
	s.json(w, map[string]bool{"ok": true})
}

func (s *Server) handleFilterDelete(w http.ResponseWriter, r *http.Request, u *models.User) {
	id := pathID(r, "id")        // mailbox id
	fid := pathID(r, "filterId") // filter id
	addr, ok := s.mailboxScoped(u, id)
	if !ok {
		s.err(w, http.StatusNotFound, "mailbox not found")
		return
	}
	if _, err := s.DB.Exec(`DELETE FROM mail_filters WHERE id = ? AND mailbox_id = ?`, fid, id); err != nil {
		s.fail(w, "delete filter", err)
		return
	}
	if err := s.rebuildMailboxSieve(id, addr); err != nil {
		s.fail(w, "write sieve script", err)
		return
	}
	s.json(w, map[string]bool{"ok": true})
}

// ---- Smarthost (admin) ------------------------------------------------------

func (s *Server) handleSmarthostGet(w http.ResponseWriter, r *http.Request, u *models.User) {
	sh := models.MailSmarthost{
		Enabled:  s.DB.Setting("smarthost_enabled") == "1",
		Host:     s.DB.Setting("smarthost_host"),
		Username: s.DB.Setting("smarthost_user"),
		HasPass:  s.DB.Setting("smarthost_pass") != "",
	}
	sh.Port = 587
	if n, err := strconv.Atoi(s.DB.Setting("smarthost_port")); err == nil && n > 0 {
		sh.Port = n
	}
	s.json(w, sh)
}

func (s *Server) handleSmarthostSet(w http.ResponseWriter, r *http.Request, u *models.User) {
	req, err := decode[struct {
		Enabled  bool   `json:"enabled"`
		Host     string `json:"host"`
		Port     int    `json:"port"`
		Username string `json:"username"`
		Password string `json:"password"` // blank keeps the stored one
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !req.Enabled {
		if err := system.ClearSmarthost(); err != nil {
			s.err(w, http.StatusBadGateway, err.Error())
			return
		}
		s.DB.SetSetting("smarthost_enabled", "0")
		s.json(w, map[string]bool{"ok": true})
		return
	}
	host := strings.TrimSpace(req.Host)
	if host == "" || strings.ContainsAny(host, " \t\r\n") {
		s.err(w, http.StatusBadRequest, "a valid relay host is required")
		return
	}
	port := req.Port
	if port <= 0 || port > 65535 {
		port = 587
	}
	pass := req.Password
	if pass == "" {
		pass = s.DB.Setting("smarthost_pass") // keep existing
	}
	if req.Username == "" || pass == "" {
		s.err(w, http.StatusBadRequest, "relay username and password are required")
		return
	}
	if err := system.SetSmarthost(host, port, req.Username, pass); err != nil {
		s.err(w, http.StatusBadGateway, err.Error())
		return
	}
	s.DB.SetSetting("smarthost_enabled", "1")
	s.DB.SetSetting("smarthost_host", host)
	s.DB.SetSetting("smarthost_port", strconv.Itoa(port))
	s.DB.SetSetting("smarthost_user", req.Username)
	s.DB.SetSetting("smarthost_pass", pass)
	s.json(w, map[string]bool{"ok": true})
}

// ---- Mail feature install (admin) -------------------------------------------

func (s *Server) handleMailFeaturesInstall(w http.ResponseWriter, r *http.Request, u *models.User) {
	go func() {
		if err := system.InstallMailFeatures(s.Cfg.MailDir); err != nil {
			s.fail0("install mail features", err)
		}
	}()
	s.json(w, map[string]bool{"ok": true})
}

// ---- IMAP migration ---------------------------------------------------------

func (s *Server) handleMigrationList(w http.ResponseWriter, r *http.Request, u *models.User) {
	where, args := scopeWhere(u, "d.user_id")
	out := []models.MailMigration{}
	rows, err := s.DB.Query(`SELECT mg.id, mg.mailbox_id, m.address, mg.remote_host, mg.remote_port,
		mg.remote_user, mg.status, mg.log, mg.created_at
		FROM mail_migrations mg
		JOIN mailboxes m ON m.id = mg.mailbox_id
		JOIN domains d ON d.id = m.domain_id
		WHERE `+where+` ORDER BY mg.created_at DESC`, args...)
	if err != nil {
		s.fail(w, "list migrations", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var mg models.MailMigration
		if rows.Scan(&mg.ID, &mg.MailboxID, &mg.Mailbox, &mg.RemoteHost, &mg.RemotePort,
			&mg.RemoteUser, &mg.Status, &mg.Log, &mg.CreatedAt) == nil {
			out = append(out, mg)
		}
	}
	s.json(w, map[string]any{"migrations": out, "imapsync": system.HaveIMAPSync()})
}

func (s *Server) handleMigrationCreate(w http.ResponseWriter, r *http.Request, u *models.User) {
	id := pathID(r, "id") // mailbox id
	addr, ok := s.mailboxScoped(u, id)
	if !ok {
		s.err(w, http.StatusNotFound, "mailbox not found")
		return
	}
	if !system.HaveIMAPSync() {
		s.err(w, http.StatusBadRequest, "imapsync is not installed on this server")
		return
	}
	req, err := decode[struct {
		Host          string `json:"host"`
		Port          int    `json:"port"`
		Username      string `json:"username"`
		Password      string `json:"password"`
		LocalPassword string `json:"local_password"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	host := strings.TrimSpace(req.Host)
	if host == "" || strings.ContainsAny(host, " \t\r\n") || req.Username == "" || req.Password == "" || req.LocalPassword == "" {
		s.err(w, http.StatusBadRequest, "remote host, credentials and the destination mailbox password are required")
		return
	}
	port := req.Port
	if port <= 0 || port > 65535 {
		port = 993
	}
	res, err := s.DB.Exec(`INSERT INTO mail_migrations(mailbox_id,remote_host,remote_port,remote_user,status)
		VALUES(?,?,?,?,'running')`, id, host, port, req.Username)
	if err != nil {
		s.fail(w, "create migration", err)
		return
	}
	mgID, _ := res.LastInsertId()
	go func(mgID int64, host string, port int, ruser, rpass, laddr, lpass string) {
		log, err := system.RunIMAPSync(host, port, ruser, rpass, laddr, lpass)
		status := "completed"
		if err != nil {
			status = "failed"
			if log != "" {
				log += "\n"
			}
			log += "ERROR: " + err.Error()
		}
		s.DB.Exec(`UPDATE mail_migrations SET status = ?, log = ? WHERE id = ?`, status, log, mgID)
	}(mgID, host, port, req.Username, req.Password, addr, req.LocalPassword)

	s.json(w, map[string]any{"ok": true, "id": mgID})
}

func (s *Server) handleMigrationInstall(w http.ResponseWriter, r *http.Request, u *models.User) {
	go func() {
		if err := system.InstallIMAPSync(); err != nil {
			s.fail0("install imapsync", err)
		}
	}()
	s.json(w, map[string]bool{"ok": true})
}
