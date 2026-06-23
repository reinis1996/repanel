package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/reinis1996/repanel/internal/models"
	"github.com/reinis1996/repanel/internal/system"
)

// ---- historical metrics -----------------------------------------------------

// CollectMetrics samples host resource usage into the metrics table and trims
// history to 30 days. Called from a periodic ticker.
func (s *Server) CollectMetrics() {
	cpu, mem, disk := system.SampleResources()
	s.DB.Exec(`INSERT INTO metrics(cpu,mem,disk) VALUES(?,?,?)`, cpu, mem, disk)
	s.DB.Exec(`DELETE FROM metrics WHERE ts < datetime('now','-30 days')`)
}

// handleMetrics returns the resource-usage series over ?hours (default 24) plus a
// daily total-traffic series for charting. Admin only.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request, _ *models.User) {
	hours := 24
	if v, err := strconv.Atoi(r.URL.Query().Get("hours")); err == nil && v > 0 && v <= 720 {
		hours = v
	}
	samples := []models.MetricSample{}
	rows, err := s.DB.Query(`SELECT ts, cpu, mem, disk FROM metrics WHERE ts >= datetime('now', ?) ORDER BY ts`,
		fmt.Sprintf("-%d hours", hours))
	if err == nil {
		for rows.Next() {
			var m models.MetricSample
			if rows.Scan(&m.Ts, &m.CPU, &m.Mem, &m.Disk) == nil {
				samples = append(samples, m)
			}
		}
		rows.Close()
	}
	// Samples are recorded every 5 minutes, so a 30-day window holds ~8.6k points.
	// Aggregate to a fixed bar count so the chart stays readable and the payload
	// small (one bar per ~5 min at 24h, coarser for longer ranges).
	samples = downsampleMetrics(samples, maxMetricPoints)

	// Daily total traffic across all domains (last 30 days), for an "over time" view.
	type day struct {
		Day string  `json:"day"`
		MB  float64 `json:"mb"`
	}
	traffic := []day{}
	since := time.Now().AddDate(0, 0, -30).Format("2006-01-02")
	trows, err := s.DB.Query(`SELECT day, SUM(bytes) FROM traffic WHERE day >= ? GROUP BY day ORDER BY day`, since)
	if err == nil {
		for trows.Next() {
			var d day
			var b int64
			if trows.Scan(&d.Day, &b) == nil {
				d.MB = float64(b) / bytesPerMB
				traffic = append(traffic, d)
			}
		}
		trows.Close()
	}
	s.json(w, map[string]any{"samples": samples, "traffic": traffic})
}

// maxMetricPoints caps how many aggregated points the resource chart renders.
const maxMetricPoints = 96

// downsampleMetrics averages consecutive samples into at most max buckets,
// preserving each bucket's last timestamp. Returns the input unchanged when it
// already fits.
func downsampleMetrics(in []models.MetricSample, max int) []models.MetricSample {
	if max <= 0 || len(in) <= max {
		return in
	}
	size := (len(in) + max - 1) / max
	out := make([]models.MetricSample, 0, (len(in)+size-1)/size)
	for i := 0; i < len(in); i += size {
		end := i + size
		if end > len(in) {
			end = len(in)
		}
		var cpu, mem, disk float64
		for _, m := range in[i:end] {
			cpu += m.CPU
			mem += m.Mem
			disk += m.Disk
		}
		n := float64(end - i)
		out = append(out, models.MetricSample{
			Ts:   in[end-1].Ts,
			CPU:  cpu / n,
			Mem:  mem / n,
			Disk: disk / n,
		})
	}
	return out
}

// ---- service health & log viewer (admin) ------------------------------------

func (s *Server) handleServiceLogs(w http.ResponseWriter, r *http.Request, _ *models.User) {
	name := r.PathValue("name")
	status, logs, err := system.ServiceLogs(name)
	if err != nil {
		s.err(w, http.StatusBadRequest, err.Error())
		return
	}
	s.json(w, models.ServiceHealth{Name: name, Status: status, Logs: logs})
}

func (s *Server) handleLogList(w http.ResponseWriter, r *http.Request, _ *models.User) {
	s.json(w, map[string]any{"files": system.LogFileKeys()})
}

func (s *Server) handleLogView(w http.ResponseWriter, r *http.Request, _ *models.User) {
	key := r.PathValue("key")
	content, err := system.TailLog(key)
	if err != nil {
		s.err(w, http.StatusNotFound, "log not available")
		return
	}
	s.json(w, map[string]string{"key": key, "content": content})
}

// ---- alerting ---------------------------------------------------------------

// CheckAlerts evaluates the alert conditions and notifies (email/webhook) only on
// newly-active alerts, so a persistent condition isn't re-sent every run. Called
// from the hourly housekeeping loop.
func (s *Server) CheckAlerts() {
	if s.DB.Setting("alerts_enabled") != "1" {
		return
	}
	active := s.currentAlerts()

	var prev []string
	json.Unmarshal([]byte(s.DB.Setting("alert_active")), &prev)
	prevSet := map[string]bool{}
	for _, k := range prev {
		prevSet[k] = true
	}

	var fired []string
	keys := make([]string, 0, len(active))
	for k := range active {
		keys = append(keys, k)
		if !prevSet[k] {
			fired = append(fired, active[k])
		}
	}
	sort.Strings(keys)
	if len(fired) > 0 {
		s.notify(fired)
	}
	b, _ := json.Marshal(keys)
	s.DB.SetSetting("alert_active", string(b))
}

// currentAlerts returns the set of currently-active alerts as key -> message.
func (s *Server) currentAlerts() map[string]string {
	out := map[string]string{}

	// Disk full.
	threshold := 90.0
	if v, err := strconv.ParseFloat(s.DB.Setting("alert_disk_pct"), 64); err == nil && v > 0 {
		threshold = v
	}
	if _, _, disk := system.SampleResources(); disk >= threshold {
		out["disk"] = fmt.Sprintf("Disk usage is at %.0f%% (threshold %.0f%%).", disk, threshold)
	}

	// Services that should be running but aren't.
	for _, svc := range system.ServiceList() {
		if svc.Installed && svc.Enabled && !svc.Active && svc.Name != "repanel" {
			out["svc:"+svc.Name] = "Service " + svc.DisplayName + " (" + svc.Name + ") is not running."
		}
	}

	// Certificates expiring soon.
	days := 14
	if v, err := strconv.Atoi(s.DB.Setting("alert_cert_days")); err == nil && v > 0 {
		days = v
	}
	cutoff := time.Now().AddDate(0, 0, days)
	crows, err := s.DB.Query(`SELECT domain, not_after FROM certificates WHERE not_after IS NOT NULL AND not_after < ?`, cutoff)
	if err == nil {
		for crows.Next() {
			var domain string
			var notAfter time.Time
			if crows.Scan(&domain, &notAfter) == nil {
				out["cert:"+domain] = fmt.Sprintf("TLS certificate for %s expires %s.", domain, notAfter.Format("2006-01-02"))
			}
		}
		crows.Close()
	}

	// Recent backup failures (keyed by id so each failure alerts once).
	brows, err := s.DB.Query(`SELECT id, filename FROM backups WHERE status = 'failed' AND created_at > datetime('now','-2 days')`)
	if err == nil {
		for brows.Next() {
			var id int64
			var filename string
			if brows.Scan(&id, &filename) == nil {
				out[fmt.Sprintf("backup:%d", id)] = "A backup failed: " + filename + "."
			}
		}
		brows.Close()
	}
	return out
}

// notify delivers the fired alert messages by email and/or webhook (best-effort).
func (s *Server) notify(messages []string) {
	host, _ := os.Hostname()
	subject := fmt.Sprintf("[RePanel] %d alert(s) on %s", len(messages), host)
	body := "RePanel detected the following:\n\n  - " + strings.Join(messages, "\n  - ") + "\n"

	if email := s.DB.Setting("alert_email"); email != "" {
		if err := system.SendEmail(email, s.DB.Setting("admin_email"), subject, body); err != nil {
			log.Printf("alert email: %v", err)
		}
	}
	if hook := s.DB.Setting("alert_webhook"); hook != "" {
		payload, _ := json.Marshal(map[string]any{"subject": subject, "host": host, "alerts": messages})
		if err := system.PostWebhook(hook, payload); err != nil {
			log.Printf("alert webhook: %v", err)
		}
	}
}

// handleAlertTest sends a test notification with the current settings (admin).
func (s *Server) handleAlertTest(w http.ResponseWriter, r *http.Request, _ *models.User) {
	if s.DB.Setting("alert_email") == "" && s.DB.Setting("alert_webhook") == "" {
		s.err(w, http.StatusBadRequest, "set an alert email or webhook first")
		return
	}
	s.notify([]string{"This is a test notification from RePanel."})
	s.json(w, map[string]bool{"ok": true})
}
