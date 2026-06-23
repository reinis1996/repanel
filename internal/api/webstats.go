package api

import (
	"database/sql"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"github.com/reinis1996/repanel/internal/models"
	"github.com/reinis1996/repanel/internal/system"
)

// webStatsRetentionDays bounds how much detailed per-day statistics we keep.
const webStatsRetentionDays = 90

// CollectWebStats parses each domain's access log incrementally and folds the new
// activity into the web statistics tables. It shares the hourly housekeeping loop
// with CollectTraffic but keeps its own byte offset, so the two never interfere.
func (s *Server) CollectWebStats() {
	rows, err := s.DB.Query(`SELECT id, name FROM domains`)
	if err != nil {
		log.Printf("webstats: list domains: %v", err)
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

	logDir := s.webServer().AccessLogDir()
	for _, d := range domains {
		var oldSize int64
		s.DB.QueryRow(`SELECT log_size FROM web_stats_state WHERE domain_id = ?`, d.id).Scan(&oldSize)

		logPath := filepath.Join(logDir, d.name+".access.log")
		res, err := system.CollectWebStatsLog(logPath, d.name, oldSize)
		if err != nil {
			log.Printf("webstats: parse %s: %v", logPath, err)
			continue
		}
		s.applyWebStats(d.id, res)
		s.DB.Exec(`INSERT INTO web_stats_state(domain_id, log_size) VALUES(?,?)
			ON CONFLICT(domain_id) DO UPDATE SET log_size = excluded.log_size`,
			d.id, res.NewSize)
	}

	cutoff := time.Now().AddDate(0, 0, -webStatsRetentionDays).Format("2006-01-02")
	for _, t := range []string{"web_stats", "web_stats_item", "web_stats_visitor"} {
		s.DB.Exec(`DELETE FROM `+t+` WHERE day < ?`, cutoff)
	}
}

// applyWebStats writes one collection's per-day aggregates for a domain in a
// single transaction. All counters are additive so repeated collections within a
// day accumulate correctly; visitor IPs are deduplicated by the table's key.
func (s *Server) applyWebStats(domainID int64, res system.WebStatsResult) {
	if len(res.PerDay) == 0 {
		return
	}
	tx, err := s.DB.Begin()
	if err != nil {
		log.Printf("webstats: begin tx: %v", err)
		return
	}
	defer tx.Rollback() //nolint:errcheck // no-op after a successful Commit

	for day, st := range res.PerDay {
		if _, err := tx.Exec(`INSERT INTO web_stats(domain_id, day, hits, pageviews, bytes)
			VALUES(?,?,?,?,?)
			ON CONFLICT(domain_id, day) DO UPDATE SET
				hits = hits + excluded.hits,
				pageviews = pageviews + excluded.pageviews,
				bytes = bytes + excluded.bytes`,
			domainID, day, st.Hits, st.Pageviews, st.Bytes); err != nil {
			log.Printf("webstats: upsert day: %v", err)
			return
		}
		for ip := range st.Visitors {
			tx.Exec(`INSERT OR IGNORE INTO web_stats_visitor(domain_id, day, ip) VALUES(?,?,?)`, domainID, day, ip)
		}
		applyItems(tx, domainID, day, "page", st.Pages)
		applyItems(tx, domainID, day, "referrer", st.Referrers)
		applyItems(tx, domainID, day, "status", st.Statuses)
	}
	if err := tx.Commit(); err != nil {
		log.Printf("webstats: commit: %v", err)
	}
}

// applyItems upserts a kind's label->count map for one day within a transaction.
func applyItems(tx *sql.Tx, domainID int64, day, kind string, counts map[string]int64) {
	for label, n := range counts {
		tx.Exec(`INSERT INTO web_stats_item(domain_id, day, kind, label, count)
			VALUES(?,?,?,?,?)
			ON CONFLICT(domain_id, day, kind, label) DO UPDATE SET count = count + excluded.count`,
			domainID, day, kind, label, n)
	}
}

// handleWebStats returns the AWStats-style report for one domain over ?days
// (default 30, capped at the retention window). The domain is scoped to what the
// caller may see.
func (s *Server) handleWebStats(w http.ResponseWriter, r *http.Request, u *models.User) {
	d, err := s.getDomainScoped(u, pathID(r, "id"))
	if err != nil {
		s.err(w, http.StatusNotFound, err.Error())
		return
	}
	days := 30
	if v, err := strconv.Atoi(r.URL.Query().Get("days")); err == nil && v > 0 && v <= webStatsRetentionDays {
		days = v
	}
	since := time.Now().AddDate(0, 0, -(days - 1)).Format("2006-01-02")

	stats := models.WebStats{
		Domain: d.Name, Days: days,
		Series: []models.WebStatsDay{}, TopPages: []models.WebStatItem{},
		TopReferrers: []models.WebStatItem{}, StatusCodes: []models.WebStatItem{},
	}

	// Per-day counters, with visitors joined from the distinct-IP table.
	rows, err := s.DB.Query(`SELECT s.day, s.hits, s.pageviews, s.bytes,
		(SELECT COUNT(*) FROM web_stats_visitor v WHERE v.domain_id = s.domain_id AND v.day = s.day)
		FROM web_stats s WHERE s.domain_id = ? AND s.day >= ? ORDER BY s.day`, d.ID, since)
	if err != nil {
		s.fail(w, "web stats series", err)
		return
	}
	for rows.Next() {
		var day string
		var hits, pv, bytes, visitors int64
		if rows.Scan(&day, &hits, &pv, &bytes, &visitors) == nil {
			mb := float64(bytes) / bytesPerMB
			stats.Series = append(stats.Series, models.WebStatsDay{
				Day: day, Hits: hits, Pageviews: pv, Visitors: visitors, MB: mb})
			stats.Totals.Hits += hits
			stats.Totals.Pageviews += pv
			stats.Totals.MB += mb
		}
	}
	rows.Close()

	// Unique visitors across the whole window (distinct IPs, not the per-day sum).
	s.DB.QueryRow(`SELECT COUNT(DISTINCT ip) FROM web_stats_visitor WHERE domain_id = ? AND day >= ?`,
		d.ID, since).Scan(&stats.Totals.Visitors)

	stats.TopPages = s.topItems(d.ID, "page", since, 15)
	stats.TopReferrers = s.topItems(d.ID, "referrer", since, 15)
	stats.StatusCodes = s.topItems(d.ID, "status", since, 20)
	s.json(w, stats)
}

// topItems returns the most frequent labels of a kind for a domain since a day.
func (s *Server) topItems(domainID int64, kind, since string, limit int) []models.WebStatItem {
	out := []models.WebStatItem{}
	rows, err := s.DB.Query(`SELECT label, SUM(count) c FROM web_stats_item
		WHERE domain_id = ? AND kind = ? AND day >= ?
		GROUP BY label ORDER BY c DESC, label LIMIT ?`, domainID, kind, since, limit)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var it models.WebStatItem
		if rows.Scan(&it.Label, &it.Count) == nil {
			out = append(out, it)
		}
	}
	return out
}
