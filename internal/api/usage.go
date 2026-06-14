package api

import (
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/reinis1996/repanel/internal/models"
	"github.com/reinis1996/repanel/internal/system"
)

// Disk usage scans walk whole directory trees, so results are cached briefly.
var (
	usageMu    sync.Mutex
	usageCache = map[int64]cachedUsage{}
)

type cachedUsage struct {
	usage models.Usage
	at    time.Time
}

const usageTTL = 2 * time.Minute

// usageFor computes (or returns cached) disk usage for one panel user.
func (s *Server) usageFor(userID int64, username string, quotaMB int64) models.Usage {
	usageMu.Lock()
	if c, ok := usageCache[userID]; ok && time.Since(c.at) < usageTTL {
		usageMu.Unlock()
		u := c.usage
		u.DiskQuotaMB = quotaMB // quota may have changed since caching
		return u
	}
	usageMu.Unlock()

	sysUser := system.SysUserName(userID)
	usage := models.Usage{UserID: userID, Username: username, DiskQuotaMB: quotaMB}
	usage.WebMB = system.DirSizeMB(filepath.Join(s.Cfg.WebRoot, sysUser))

	rows, err := s.DB.Query(`SELECT name FROM domains WHERE user_id = ?`, userID)
	if err == nil {
		for rows.Next() {
			var name string
			if rows.Scan(&name) == nil {
				usage.MailMB += system.DirSizeMB(filepath.Join(mailVhostsDir, name))
			}
		}
		rows.Close()
	}

	sizes := system.DatabaseSizes()
	rows, err = s.DB.Query(`SELECT name FROM db_entries WHERE user_id = ?`, userID)
	if err == nil {
		for rows.Next() {
			var name string
			if rows.Scan(&name) == nil {
				usage.DBMB += sizes[name]
			}
		}
		rows.Close()
	}

	usage.TotalMB = usage.WebMB + usage.MailMB + usage.DBMB

	usageMu.Lock()
	usageCache[userID] = cachedUsage{usage: usage, at: time.Now()}
	usageMu.Unlock()
	return usage
}

// handleUsage returns disk usage for every account the caller may see.
func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request, u *models.User) {
	query := `SELECT id, username, disk_quota_mb FROM users`
	var args []any
	switch u.Role {
	case models.RoleAdmin:
	case models.RoleReseller:
		query += ` WHERE id = ? OR owner_id = ?`
		args = []any{u.ID, u.ID}
	default:
		query += ` WHERE id = ?`
		args = []any{u.ID}
	}
	rows, err := s.DB.Query(query+` ORDER BY username`, args...)
	if err != nil {
		s.fail(w, "list users for usage", err)
		return
	}
	// Collect first: usageFor runs its own queries, and the SQLite pool has a
	// single connection — nesting them inside this result set would deadlock.
	type entry struct {
		id, quota int64
		name      string
	}
	var entries []entry
	for rows.Next() {
		var e entry
		if rows.Scan(&e.id, &e.name, &e.quota) == nil {
			entries = append(entries, e)
		}
	}
	rows.Close()

	out := []models.Usage{}
	for _, e := range entries {
		out = append(out, s.usageFor(e.id, e.name, e.quota))
	}
	s.json(w, out)
}

// quotaExceeded reports whether the user is at or over their disk quota.
// Admins and unlimited (quota 0) accounts are never blocked.
func (s *Server) quotaExceeded(u *models.User) bool {
	if u.Role == models.RoleAdmin || u.DiskQuotaMB <= 0 {
		return false
	}
	usage := s.usageFor(u.ID, u.Username, u.DiskQuotaMB)
	return usage.TotalMB >= float64(u.DiskQuotaMB)
}

const quotaMsg = "disk quota exceeded — free up space or ask your provider to raise the quota"
