package system

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseAccessRecord(t *testing.T) {
	line := `203.0.113.7 - - [10/Oct/2024:13:55:36 +0000] "GET /blog/post?ref=x HTTP/1.1" 200 2326 "https://news.example/page" "Mozilla/5.0"`
	rec, ok := parseAccessRecord(line)
	if !ok {
		t.Fatal("expected line to parse")
	}
	if rec.IP != "203.0.113.7" {
		t.Errorf("IP = %q", rec.IP)
	}
	if rec.Day != "2024-10-10" {
		t.Errorf("Day = %q", rec.Day)
	}
	if rec.Path != "/blog/post" { // query stripped
		t.Errorf("Path = %q", rec.Path)
	}
	if rec.Status != "200" {
		t.Errorf("Status = %q", rec.Status)
	}
	if rec.Bytes != 2326 {
		t.Errorf("Bytes = %d", rec.Bytes)
	}
	if rec.Referer != "https://news.example/page" {
		t.Errorf("Referer = %q", rec.Referer)
	}
}

func TestParseAccessRecordRejectsGarbage(t *testing.T) {
	for _, s := range []string{"", "not a log line", `- - - [bad date] "GET / HTTP/1.1" 200 0`} {
		if _, ok := parseAccessRecord(s); ok {
			t.Errorf("%q should not parse", s)
		}
	}
}

func TestIsPageView(t *testing.T) {
	pages := []string{"/", "/about", "/blog/post", "/index.php", "/page.html"}
	for _, p := range pages {
		if !isPageView(p) {
			t.Errorf("%q should be a page view", p)
		}
	}
	assets := []string{"/style.css", "/app.js", "/logo.png", "/font.woff2", "/data.json"}
	for _, a := range assets {
		if isPageView(a) {
			t.Errorf("%q should be an asset, not a page view", a)
		}
	}
}

func TestExternalRefererHost(t *testing.T) {
	if got := externalRefererHost("https://google.com/search?q=x", "example.com"); got != "google.com" {
		t.Errorf("external referrer host = %q, want google.com", got)
	}
	// Direct, internal and malformed referrers yield "".
	for _, ref := range []string{"-", "", "https://example.com/x", "https://www.example.com/y"} {
		if got := externalRefererHost(ref, "example.com"); got != "" {
			t.Errorf("externalRefererHost(%q) = %q, want empty", ref, got)
		}
	}
}

func TestCollectWebStatsLog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "site.access.log")
	log := `203.0.113.7 - - [10/Oct/2024:13:55:36 +0000] "GET / HTTP/1.1" 200 100 "-" "UA"
203.0.113.7 - - [10/Oct/2024:13:56:00 +0000] "GET /style.css HTTP/1.1" 200 50 "https://example.com/" "UA"
198.51.100.2 - - [10/Oct/2024:14:00:00 +0000] "GET /about HTTP/1.1" 404 30 "https://ref.test/p" "UA"
`
	if err := os.WriteFile(path, []byte(log), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := CollectWebStatsLog(path, "example.com", 0)
	if err != nil {
		t.Fatal(err)
	}
	day := res.PerDay["2024-10-10"]
	if day == nil {
		t.Fatal("no stats for the day")
	}
	if day.Hits != 3 {
		t.Errorf("hits = %d, want 3", day.Hits)
	}
	if day.Pageviews != 2 { // "/" and "/about"; style.css excluded
		t.Errorf("pageviews = %d, want 2", day.Pageviews)
	}
	if len(day.Visitors) != 2 {
		t.Errorf("visitors = %d, want 2", len(day.Visitors))
	}
	if day.Bytes != 180 {
		t.Errorf("bytes = %d, want 180", day.Bytes)
	}
	if day.Referrers["ref.test"] != 1 {
		t.Errorf("external referrer ref.test = %d, want 1", day.Referrers["ref.test"])
	}
	if _, internal := day.Referrers["example.com"]; internal {
		t.Error("internal referrer should be excluded")
	}
	if day.Statuses["404"] != 1 || day.Statuses["200"] != 2 {
		t.Errorf("status counts = %v", day.Statuses)
	}
	if res.NewSize != int64(len(log)) {
		t.Errorf("NewSize = %d, want %d", res.NewSize, len(log))
	}
}
