package api

import (
	"log"
	"time"
)

// Bandwidth quota enforcement. Each account may be capped at a number of MB of
// web traffic per calendar month (0 = unlimited). The hourly housekeeping loop
// calls EnforceBandwidthLimits: accounts over their quota have their websites
// auto-suspended (served a 503), and are automatically restored when usage falls
// back under the cap (e.g. at the start of a new month).
//
// Auto-suspension is tracked by domains.bw_suspended, separate from the manual
// suspended flag, so enforcement only ever lifts suspensions it imposed and
// never touches a site an operator suspended by hand.

// EnforceBandwidthLimits suspends/restores websites per account bandwidth quota.
func (s *Server) EnforceBandwidthLimits() {
	type acct struct {
		id    int64
		quota int64
	}
	var accts []acct
	rows, err := s.DB.Query(`SELECT id, bandwidth_quota_mb FROM users WHERE bandwidth_quota_mb > 0 AND role != 'admin'`)
	if err != nil {
		return
	}
	for rows.Next() {
		var a acct
		if rows.Scan(&a.id, &a.quota) == nil {
			accts = append(accts, a)
		}
	}
	rows.Close()

	month := time.Now().Format("2006-01") // current calendar month, matches traffic.day prefix
	for _, a := range accts {
		var bytes int64
		s.DB.QueryRow(`SELECT COALESCE(SUM(t.bytes), 0) FROM traffic t
			JOIN domains d ON d.id = t.domain_id
			WHERE d.user_id = ? AND t.day LIKE ?`, a.id, month+"%").Scan(&bytes)
		usedMB := bytes / (1024 * 1024)
		over := usedMB >= a.quota
		s.applyBandwidthState(a.id, over)
	}
}

// applyBandwidthState suspends (over) or restores (under) an account's websites
// that are governed by bandwidth auto-suspension.
func (s *Server) applyBandwidthState(userID int64, over bool) {
	var ids []int64
	var q string
	if over {
		// Suspend currently-active sites not already auto-suspended.
		q = `SELECT id FROM domains WHERE user_id = ? AND suspended = 0 AND bw_suspended = 0`
	} else {
		// Restore only the sites this enforcer suspended.
		q = `SELECT id FROM domains WHERE user_id = ? AND bw_suspended = 1`
	}
	rows, err := s.DB.Query(q, userID)
	if err != nil {
		return
	}
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()

	for _, id := range ids {
		d, err := s.getDomainByID(id)
		if err != nil {
			continue
		}
		if over {
			if err := s.webServer().WriteSuspendedVhost(*d); err != nil {
				log.Printf("bandwidth suspend %s: %v", d.Name, err)
				continue
			}
			s.DB.Exec(`UPDATE domains SET suspended = 1, bw_suspended = 1 WHERE id = ?`, id)
		} else {
			s.DB.Exec(`UPDATE domains SET suspended = 0, bw_suspended = 0 WHERE id = ?`, id)
			d.Suspended = false
			if err := s.rewriteVhost(*d); err != nil {
				log.Printf("bandwidth restore %s: %v", d.Name, err)
			}
		}
	}
}
