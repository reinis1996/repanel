package api

import (
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"github.com/reinis1996/repanel/internal/models"
	"github.com/reinis1996/repanel/internal/system"
)

const bytesPerMB = 1 << 20

// CollectTraffic reads each domain's nginx access log incrementally and folds
// the newly served bytes into the per-day traffic table. It is called from the
// hourly housekeeping loop; logrotate handles file rotation underneath us and
// CollectAccessLog recovers the rotated tail, so hourly granularity is enough.
func (s *Server) CollectTraffic() {
	rows, err := s.DB.Query(`SELECT id, name FROM domains`)
	if err != nil {
		log.Printf("traffic: list domains: %v", err)
		return
	}
	type dom struct {
		id   int64
		name string
	}
	var domains []dom
	for rows.Next() {
		var d dom
		if rows.Scan(&d.id, &d.name) == nil {
			domains = append(domains, d)
		}
	}
	rows.Close()

	for _, d := range domains {
		var oldSize int64
		s.DB.QueryRow(`SELECT log_size FROM traffic_state WHERE domain_id = ?`, d.id).Scan(&oldSize)

		logPath := filepath.Join(system.NginxLogDir, d.name+".access.log")
		res, err := system.CollectAccessLog(logPath, oldSize)
		if err != nil {
			log.Printf("traffic: parse %s: %v", logPath, err)
			continue
		}
		for day, n := range res.PerDay {
			s.DB.Exec(`INSERT INTO traffic(domain_id, day, bytes) VALUES(?,?,?)
				ON CONFLICT(domain_id, day) DO UPDATE SET bytes = bytes + excluded.bytes`,
				d.id, day, n)
		}
		s.DB.Exec(`INSERT INTO traffic_state(domain_id, log_size) VALUES(?,?)
			ON CONFLICT(domain_id) DO UPDATE SET log_size = excluded.log_size`,
			d.id, res.NewSize)
	}

	// Keep roughly a year of daily history; older rows are of no interest.
	cutoff := time.Now().AddDate(0, 0, -400).Format("2006-01-02")
	s.DB.Exec(`DELETE FROM traffic WHERE day < ?`, cutoff)
}

// handleTraffic returns per-account bandwidth for the accounts the caller may
// see, over the last ?days (default 30, capped at 365): a total, a per-domain
// breakdown and a daily series for charting.
func (s *Server) handleTraffic(w http.ResponseWriter, r *http.Request, u *models.User) {
	days := 30
	if v, err := strconv.Atoi(r.URL.Query().Get("days")); err == nil && v > 0 && v <= 365 {
		days = v
	}
	since := time.Now().AddDate(0, 0, -(days - 1)).Format("2006-01-02")

	query := `SELECT id, username FROM users`
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
		s.fail(w, "list users for traffic", err)
		return
	}
	// Collect first: the per-user aggregates below run their own queries, and
	// the SQLite pool has a single connection — nesting would deadlock.
	type acct struct {
		id   int64
		name string
	}
	var accts []acct
	for rows.Next() {
		var a acct
		if rows.Scan(&a.id, &a.name) == nil {
			accts = append(accts, a)
		}
	}
	rows.Close()

	out := []models.TrafficStat{}
	for _, a := range accts {
		out = append(out, s.trafficFor(a.id, a.name, since))
	}
	s.json(w, out)
}

// trafficFor aggregates one account's bandwidth since the given day.
func (s *Server) trafficFor(userID int64, username, since string) models.TrafficStat {
	stat := models.TrafficStat{UserID: userID, Username: username, Domains: []models.TrafficDomain{}, Series: []models.TrafficDay{}}

	// Per-domain totals (every owned domain, even those with no traffic yet).
	rows, err := s.DB.Query(`SELECT d.name, COALESCE(SUM(t.bytes), 0)
		FROM domains d
		LEFT JOIN traffic t ON t.domain_id = d.id AND t.day >= ?
		WHERE d.user_id = ?
		GROUP BY d.id ORDER BY 2 DESC, d.name`, since, userID)
	if err == nil {
		for rows.Next() {
			var name string
			var b int64
			if rows.Scan(&name, &b) == nil {
				stat.Domains = append(stat.Domains, models.TrafficDomain{Domain: name, MB: float64(b) / bytesPerMB})
				stat.TotalMB += float64(b) / bytesPerMB
			}
		}
		rows.Close()
	}

	// Daily series across all of the account's domains.
	rows, err = s.DB.Query(`SELECT t.day, SUM(t.bytes)
		FROM traffic t JOIN domains d ON d.id = t.domain_id
		WHERE d.user_id = ? AND t.day >= ?
		GROUP BY t.day ORDER BY t.day`, userID, since)
	if err == nil {
		for rows.Next() {
			var day string
			var b int64
			if rows.Scan(&day, &b) == nil {
				stat.Series = append(stat.Series, models.TrafficDay{Day: day, MB: float64(b) / bytesPerMB})
			}
		}
		rows.Close()
	}
	return stat
}
