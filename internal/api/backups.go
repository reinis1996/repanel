package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/reinis1996/repanel/internal/auth"
	"github.com/reinis1996/repanel/internal/models"
	"github.com/reinis1996/repanel/internal/system"
)

const mailVhostsDir = "/var/mail/vhosts"

func (s *Server) backupsDir() string { return filepath.Join(s.Cfg.DataDir, "backups") }

func (s *Server) handleBackupList(w http.ResponseWriter, r *http.Request, u *models.User) {
	where, args := scopeWhere(u, "b.user_id")
	rows, err := s.DB.Query(`SELECT b.id, b.user_id, b.filename, b.size_bytes, b.status, b.error,
		b.created_at, u.username
		FROM backups b JOIN users u ON u.id = b.user_id
		WHERE `+where+` ORDER BY b.id DESC`, args...)
	if err != nil {
		s.fail(w, "list backups", err)
		return
	}
	defer rows.Close()
	out := []models.Backup{}
	for rows.Next() {
		var b models.Backup
		if rows.Scan(&b.ID, &b.UserID, &b.Filename, &b.SizeBytes, &b.Status, &b.Error,
			&b.CreatedAt, &b.Owner) == nil {
			out = append(out, b)
		}
	}
	s.json(w, out)
}

func (s *Server) handleBackupCreate(w http.ResponseWriter, r *http.Request, u *models.User) {
	req, _ := decode[struct {
		UserID int64 `json:"user_id"` // optional; adminish may back up others
	}](r)
	targetID := u.ID
	if req.UserID != 0 && req.UserID != u.ID {
		if _, ok := s.userScopedForManage(u, req.UserID); !ok {
			s.err(w, http.StatusForbidden, "cannot back up that user")
			return
		}
		targetID = req.UserID
	}

	var running int
	s.DB.QueryRow(`SELECT COUNT(*) FROM backups WHERE user_id = ? AND status = 'running'`, targetID).Scan(&running)
	if running > 0 {
		s.err(w, http.StatusConflict, "a backup for this account is already running")
		return
	}

	id, err := s.startBackup(targetID)
	if err != nil {
		s.fail(w, "start backup", err)
		return
	}
	s.json(w, map[string]any{"ok": true, "id": id})
}

// startBackup inserts the tracking row and launches the archive job.
func (s *Server) startBackup(userID int64) (int64, error) {
	target, err := auth.GetUserByID(s.DB, userID)
	if err != nil || target == nil {
		return 0, fmt.Errorf("user %d not found", userID)
	}
	// Stored with forward slashes regardless of OS; joined via filepath later.
	filename := target.Username + "/" +
		fmt.Sprintf("%s-%s.tar.gz", target.Username, time.Now().Format("20060102-150405"))
	res, err := s.DB.Exec(`INSERT INTO backups(user_id,filename,status) VALUES(?,?,'running')`, userID, filename)
	if err != nil {
		return 0, err
	}
	backupID, _ := res.LastInsertId()
	go s.runBackup(backupID, target, filename)
	return backupID, nil
}

// backupManifest captures enough panel state to recreate the account.
type backupManifest struct {
	Version   string             `json:"version"`
	CreatedAt time.Time          `json:"created_at"`
	Username  string             `json:"username"`
	Domains   []models.Domain    `json:"domains"`
	Zones     []models.DNSZone   `json:"zones"`
	Mailboxes []models.Mailbox   `json:"mailboxes"`
	Aliases   []models.MailAlias `json:"aliases"`
	Databases []string           `json:"databases"`
	FTP       []models.FTPAccount `json:"ftp_accounts"`
	Cron      []models.CronJob   `json:"cron_jobs"`
}

func (s *Server) runBackup(backupID int64, target *models.User, filename string) {
	finish := func(size int64, errMsg string) {
		status := "completed"
		if errMsg != "" {
			status = "failed"
			log.Printf("backup %d for %s failed: %s", backupID, target.Username, errMsg)
			if len(errMsg) > 500 {
				errMsg = errMsg[:500]
			}
		}
		s.DB.Exec(`UPDATE backups SET status = ?, size_bytes = ?, error = ? WHERE id = ?`,
			status, size, errMsg, backupID)
	}

	domains, dbNames, err := s.accountInventory(target.ID)
	if err != nil {
		finish(0, err.Error())
		return
	}

	manifest := backupManifest{
		Version:   s.Version,
		CreatedAt: time.Now().UTC(),
		Username:  target.Username,
		Domains:   domains,
		Databases: dbNames,
	}
	s.fillManifestDetails(target.ID, &manifest)
	manifestJSON, _ := json.MarshalIndent(manifest, "", "  ")

	sysUser := system.SysUserName(target.Username)
	sources := []system.BackupSource{
		{Prefix: "web", Dir: filepath.Join(s.Cfg.WebRoot, sysUser)},
	}
	for _, d := range domains {
		sources = append(sources, system.BackupSource{
			Prefix: "mail/" + d.Name,
			Dir:    filepath.Join(mailVhostsDir, d.Name),
		})
	}

	dest := filepath.Join(s.backupsDir(), filename)
	if err := system.CreateBackupArchive(dest, manifestJSON, sources, dbNames); err != nil {
		finish(0, err.Error())
		return
	}
	st, err := os.Stat(dest)
	if err != nil {
		finish(0, err.Error())
		return
	}
	finish(st.Size(), "")
	s.pruneBackups(target.ID)
}

// accountInventory lists the domains and database names owned by a user.
func (s *Server) accountInventory(userID int64) ([]models.Domain, []string, error) {
	rows, err := s.DB.Query(`SELECT id, user_id, name, document_root, php_version FROM domains WHERE user_id = ?`, userID)
	if err != nil {
		return nil, nil, err
	}
	domains := []models.Domain{}
	for rows.Next() {
		var d models.Domain
		if rows.Scan(&d.ID, &d.UserID, &d.Name, &d.DocumentRoot, &d.PHPVersion) == nil {
			domains = append(domains, d)
		}
	}
	rows.Close()

	rows, err = s.DB.Query(`SELECT name FROM db_entries WHERE user_id = ?`, userID)
	if err != nil {
		return nil, nil, err
	}
	dbNames := []string{}
	for rows.Next() {
		var n string
		if rows.Scan(&n) == nil {
			dbNames = append(dbNames, n)
		}
	}
	rows.Close()
	return domains, dbNames, nil
}

// fillManifestDetails adds zones, mail, ftp and cron data; failures here only
// degrade the manifest, never the file/database backup itself.
func (s *Server) fillManifestDetails(userID int64, m *backupManifest) {
	for _, d := range m.Domains {
		var z models.DNSZone
		if err := s.DB.QueryRow(`SELECT id, domain_id, name, serial, created_at FROM dns_zones WHERE domain_id = ?`, d.ID).
			Scan(&z.ID, &z.DomainID, &z.Name, &z.Serial, &z.CreatedAt); err == nil {
			s.loadZoneRecords(&z)
			m.Zones = append(m.Zones, z)
		}
		rows, err := s.DB.Query(`SELECT id, domain_id, address, password_hash, quota_mb, created_at FROM mailboxes WHERE domain_id = ?`, d.ID)
		if err == nil {
			for rows.Next() {
				var b models.Mailbox
				if rows.Scan(&b.ID, &b.DomainID, &b.Address, &b.PasswordHash, &b.QuotaMB, &b.CreatedAt) == nil {
					m.Mailboxes = append(m.Mailboxes, b)
				}
			}
			rows.Close()
		}
		rows, err = s.DB.Query(`SELECT id, domain_id, source, destination FROM mail_aliases WHERE domain_id = ?`, d.ID)
		if err == nil {
			for rows.Next() {
				var a models.MailAlias
				if rows.Scan(&a.ID, &a.DomainID, &a.Source, &a.Destination) == nil {
					m.Aliases = append(m.Aliases, a)
				}
			}
			rows.Close()
		}
	}
	rows, err := s.DB.Query(`SELECT id, user_id, username, directory, created_at FROM ftp_accounts WHERE user_id = ?`, userID)
	if err == nil {
		for rows.Next() {
			var f models.FTPAccount
			if rows.Scan(&f.ID, &f.UserID, &f.Username, &f.Directory, &f.CreatedAt) == nil {
				m.FTP = append(m.FTP, f)
			}
		}
		rows.Close()
	}
	rows, err = s.DB.Query(`SELECT id, user_id, schedule, command, comment, enabled FROM cron_jobs WHERE user_id = ?`, userID)
	if err == nil {
		for rows.Next() {
			var j models.CronJob
			var enabled int
			if rows.Scan(&j.ID, &j.UserID, &j.Schedule, &j.Command, &j.Comment, &enabled) == nil {
				j.Enabled = enabled != 0
				m.Cron = append(m.Cron, j)
			}
		}
		rows.Close()
	}
}

// pruneBackups keeps the newest N completed backups per user (setting
// backup_keep, default 5) and removes older archives from disk.
func (s *Server) pruneBackups(userID int64) {
	keep := 5
	if v, err := strconv.Atoi(s.DB.Setting("backup_keep")); err == nil && v > 0 {
		keep = v
	}
	rows, err := s.DB.Query(`SELECT id, filename FROM backups
		WHERE user_id = ? AND status = 'completed' ORDER BY id DESC LIMIT -1 OFFSET ?`, userID, keep)
	if err != nil {
		return
	}
	type old struct {
		id       int64
		filename string
	}
	var olds []old
	for rows.Next() {
		var o old
		if rows.Scan(&o.id, &o.filename) == nil {
			olds = append(olds, o)
		}
	}
	rows.Close()
	for _, o := range olds {
		os.Remove(filepath.Join(s.backupsDir(), o.filename))
		s.DB.Exec(`DELETE FROM backups WHERE id = ?`, o.id)
	}
}

func (s *Server) backupScoped(u *models.User, id int64) (*models.Backup, bool) {
	where, args := scopeWhere(u, "user_id")
	args = append([]any{id}, args...)
	var b models.Backup
	err := s.DB.QueryRow(`SELECT id, user_id, filename, size_bytes, status FROM backups
		WHERE id = ? AND `+where, args...).Scan(&b.ID, &b.UserID, &b.Filename, &b.SizeBytes, &b.Status)
	return &b, err == nil
}

func (s *Server) handleBackupDownload(w http.ResponseWriter, r *http.Request, u *models.User) {
	b, ok := s.backupScoped(u, pathID(r, "id"))
	if !ok || b.Status != "completed" {
		s.err(w, http.StatusNotFound, "backup not found")
		return
	}
	path := filepath.Join(s.backupsDir(), b.Filename)
	f, err := os.Open(path)
	if err != nil {
		s.err(w, http.StatusNotFound, "backup file missing on disk")
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Length", fmt.Sprint(b.SizeBytes))
	w.Header().Set("Content-Disposition", `attachment; filename="`+filepath.Base(b.Filename)+`"`)
	http.ServeContent(w, r, filepath.Base(b.Filename), time.Time{}, f)
}

func (s *Server) handleBackupRestore(w http.ResponseWriter, r *http.Request, u *models.User) {
	b, ok := s.backupScoped(u, pathID(r, "id"))
	if !ok || b.Status != "completed" {
		s.err(w, http.StatusNotFound, "backup not found")
		return
	}
	target, err := auth.GetUserByID(s.DB, b.UserID)
	if err != nil || target == nil {
		s.err(w, http.StatusNotFound, "backup owner no longer exists")
		return
	}
	domains, dbNames, err := s.accountInventory(b.UserID)
	if err != nil {
		s.fail(w, "inventory", err)
		return
	}

	sysUser := system.SysUserName(target.Username)
	dirTargets := map[string]string{
		"web": filepath.Join(s.Cfg.WebRoot, sysUser),
	}
	for _, d := range domains {
		dirTargets["mail/"+d.Name] = filepath.Join(mailVhostsDir, d.Name)
	}
	allowedDBs := map[string]bool{}
	for _, n := range dbNames {
		allowedDBs[n] = true
	}

	archive := filepath.Join(s.backupsDir(), b.Filename)
	go func() {
		log.Printf("restore of backup %d for %s started", b.ID, target.Username)
		if err := system.RestoreBackup(archive, dirTargets, allowedDBs, sysUser); err != nil {
			log.Printf("restore of backup %d failed: %v", b.ID, err)
			return
		}
		log.Printf("restore of backup %d for %s finished", b.ID, target.Username)
	}()
	s.json(w, map[string]any{"ok": true, "message": "restore started — files and databases are being written in the background"})
}

func (s *Server) handleBackupDelete(w http.ResponseWriter, r *http.Request, u *models.User) {
	b, ok := s.backupScoped(u, pathID(r, "id"))
	if !ok {
		s.err(w, http.StatusNotFound, "backup not found")
		return
	}
	if b.Status == "running" {
		s.err(w, http.StatusConflict, "backup is still running")
		return
	}
	os.Remove(filepath.Join(s.backupsDir(), b.Filename))
	if _, err := s.DB.Exec(`DELETE FROM backups WHERE id = ?`, b.ID); err != nil {
		s.fail(w, "delete backup", err)
		return
	}
	s.json(w, map[string]bool{"ok": true})
}

// MaybeRunScheduledBackups is called by the hourly housekeeping loop. With
// backup_schedule=daily it backs up every active account at ~03:00, with
// =weekly only on Sundays. A settings flag prevents double runs.
func (s *Server) MaybeRunScheduledBackups() {
	schedule := s.DB.Setting("backup_schedule")
	if schedule != "daily" && schedule != "weekly" {
		return
	}
	now := time.Now()
	if now.Hour() != 3 {
		return
	}
	if schedule == "weekly" && now.Weekday() != time.Sunday {
		return
	}
	today := now.Format("2006-01-02")
	if s.DB.Setting("backup_last_run") == today {
		return
	}
	s.DB.SetSetting("backup_last_run", today)

	rows, err := s.DB.Query(`SELECT id FROM users WHERE suspended = 0`)
	if err != nil {
		return
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	log.Printf("scheduled backups starting for %d account(s)", len(ids))
	go func() {
		for _, id := range ids {
			var running int
			s.DB.QueryRow(`SELECT COUNT(*) FROM backups WHERE user_id = ? AND status = 'running'`, id).Scan(&running)
			if running > 0 {
				continue
			}
			if _, err := s.startBackup(id); err != nil {
				log.Printf("scheduled backup for user %d: %v", id, err)
				continue
			}
			// Serialize: wait for this account to finish before the next one.
			for i := 0; i < 360; i++ {
				time.Sleep(10 * time.Second)
				var still int
				s.DB.QueryRow(`SELECT COUNT(*) FROM backups WHERE user_id = ? AND status = 'running'`, id).Scan(&still)
				if still == 0 {
					break
				}
			}
		}
		log.Printf("scheduled backups finished")
	}()
}
