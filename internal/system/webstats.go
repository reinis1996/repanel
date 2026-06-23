package system

import (
	"bufio"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Web statistics collection. Like traffic accounting (traffic.go) this parses the
// per-domain "combined"-format access log incrementally — resuming from a stored
// byte offset and recovering the tail across logrotate — but extracts the richer
// per-day breakdown an AWStats-style report needs: hits, pageviews, distinct
// visitor IPs, bandwidth, and the top pages / referrers / status codes.

// maxLabelLen caps stored page/referrer labels so a pathological URL can't bloat
// the stats tables.
const maxLabelLen = 300

// staticExt is the set of file extensions treated as assets rather than page
// views (mirrors the spirit of AWStats' "not viewed" list).
var staticExt = map[string]bool{
	"css": true, "js": true, "mjs": true, "map": true, "json": true, "xml": true,
	"png": true, "jpg": true, "jpeg": true, "gif": true, "svg": true, "ico": true,
	"webp": true, "avif": true, "bmp": true, "woff": true, "woff2": true, "ttf": true,
	"eot": true, "otf": true, "mp4": true, "webm": true, "mp3": true, "ogg": true,
	"wav": true, "zip": true, "gz": true, "br": true, "pdf": true, "txt": true,
}

// DayStats accumulates one calendar day of activity for a domain.
type DayStats struct {
	Hits      int64
	Pageviews int64
	Bytes     int64
	Visitors  map[string]struct{} // distinct client IPs
	Pages     map[string]int64    // path (no query) -> pageview hits
	Referrers map[string]int64    // external referrer host -> hits
	Statuses  map[string]int64    // HTTP status code -> hits
}

func newDayStats() *DayStats {
	return &DayStats{
		Visitors:  map[string]struct{}{},
		Pages:     map[string]int64{},
		Referrers: map[string]int64{},
		Statuses:  map[string]int64{},
	}
}

// WebStatsResult is the parsed access-log delta: per-day stats plus the live
// log's current size, so the next collection resumes where this one stopped.
type WebStatsResult struct {
	PerDay  map[string]*DayStats
	NewSize int64
}

// CollectWebStatsLog parses a domain's access log since byte offset oldSize,
// returning the per-day statistics for the appended lines. domain is the site's
// own hostname, used to distinguish internal from external referrers. Rotation is
// handled exactly as in CollectAccessLog.
func CollectWebStatsLog(path, domain string, oldSize int64) (WebStatsResult, error) {
	res := WebStatsResult{PerDay: map[string]*DayStats{}}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return res, nil
		}
		return res, err
	}
	size := info.Size()
	from := oldSize
	if size < oldSize {
		scanWebStats(path+".1", oldSize, domain, res.PerDay) // recover rotated tail
		from = 0
	}
	if err := scanWebStats(path, from, domain, res.PerDay); err != nil {
		return res, err
	}
	res.NewSize = size
	return res, nil
}

func scanWebStats(path string, from int64, domain string, perDay map[string]*DayStats) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if from > 0 {
		if _, err := f.Seek(from, io.SeekStart); err != nil {
			return err
		}
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		rec, ok := parseAccessRecord(sc.Text())
		if !ok {
			continue
		}
		day := perDay[rec.Day]
		if day == nil {
			day = newDayStats()
			perDay[rec.Day] = day
		}
		day.Hits++
		day.Bytes += rec.Bytes
		if rec.IP != "" {
			day.Visitors[rec.IP] = struct{}{}
		}
		if rec.Status != "" {
			day.Statuses[rec.Status]++
		}
		if isPageView(rec.Path) {
			day.Pageviews++
			day.Pages[clip(rec.Path)]++
		}
		if host := externalRefererHost(rec.Referer, domain); host != "" {
			day.Referrers[clip(host)]++
		}
	}
	return sc.Err()
}

// accessRecord is one parsed combined-format log line.
type accessRecord struct {
	IP      string
	Day     string // "2006-01-02"
	Method  string
	Path    string // request target with the query string stripped
	Status  string
	Bytes   int64
	Referer string
}

// parseAccessRecord parses a line of nginx/Apache "combined" log format:
//
//	$remote_addr - $remote_user [$time_local] "$request" $status $body_bytes_sent "$referer" "$ua"
//
// Lines that don't match are reported as not ok and skipped by the caller.
func parseAccessRecord(line string) (rec accessRecord, ok bool) {
	sp := strings.IndexByte(line, ' ')
	if sp <= 0 {
		return rec, false
	}
	rec.IP = line[:sp]

	lb := strings.IndexByte(line, '[')
	if lb < 0 {
		return rec, false
	}
	rb := strings.IndexByte(line[lb:], ']')
	if rb < 0 {
		return rec, false
	}
	t, err := time.Parse("02/Jan/2006:15:04:05 -0700", line[lb+1:lb+rb])
	if err != nil {
		return rec, false
	}
	rec.Day = t.Format("2006-01-02")

	rest := line[lb+rb+1:]
	q1 := strings.IndexByte(rest, '"')
	if q1 < 0 {
		return rec, false
	}
	q2 := strings.IndexByte(rest[q1+1:], '"')
	if q2 < 0 {
		return rec, false
	}
	request := rest[q1+1 : q1+1+q2]
	after := rest[q1+1+q2+1:]

	if parts := strings.Fields(request); len(parts) >= 2 {
		rec.Method, rec.Path = parts[0], stripQuery(parts[1])
	} else {
		rec.Path = "/"
	}

	fields := strings.Fields(after)
	if len(fields) < 2 {
		return rec, false
	}
	if len(fields[0]) == 3 {
		rec.Status = fields[0]
	}
	rec.Bytes, _ = strconv.ParseInt(fields[1], 10, 64)

	// Referer is the next quoted field after status/bytes.
	if rq1 := strings.IndexByte(after, '"'); rq1 >= 0 {
		if rq2 := strings.IndexByte(after[rq1+1:], '"'); rq2 >= 0 {
			rec.Referer = after[rq1+1 : rq1+1+rq2]
		}
	}
	return rec, true
}

// stripQuery removes the query string and fragment from a request target.
func stripQuery(p string) string {
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		return p[:i]
	}
	return p
}

// isPageView reports whether a path counts as a page view rather than an asset:
// directory paths and extensionless or non-static extensions are pages.
func isPageView(path string) bool {
	if path == "" {
		return false
	}
	if strings.HasSuffix(path, "/") {
		return true
	}
	seg := path
	if i := strings.LastIndexByte(seg, '/'); i >= 0 {
		seg = seg[i+1:]
	}
	dot := strings.LastIndexByte(seg, '.')
	if dot < 0 {
		return true // extensionless (e.g. /about)
	}
	return !staticExt[strings.ToLower(seg[dot+1:])]
}

// externalRefererHost returns the host of an external referrer, or "" for direct
// hits, malformed referrers, or referrers from the site itself.
func externalRefererHost(ref, domain string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" || ref == "-" {
		return ""
	}
	u, err := url.Parse(ref)
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if host == "" || host == domain || host == "www."+domain {
		return ""
	}
	return host
}

func clip(s string) string {
	if len(s) > maxLabelLen {
		return s[:maxLabelLen]
	}
	return s
}
