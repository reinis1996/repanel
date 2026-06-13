package system

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseAccessLine(t *testing.T) {
	cases := []struct {
		name      string
		line      string
		wantDay   string
		wantBytes int64
		wantOK    bool
	}{
		{
			name:      "combined format",
			line:      `203.0.113.7 - - [13/Jun/2026:01:02:03 +0000] "GET /index.html HTTP/1.1" 200 5123 "https://ref" "Mozilla/5.0"`,
			wantDay:   "2026-06-13",
			wantBytes: 5123,
			wantOK:    true,
		},
		{
			name:      "request with spaces and quotes",
			line:      `10.0.0.1 - bob [01/Dec/2025:23:59:59 +0200] "GET /a?b=c d HTTP/2.0" 404 17 "-" "curl/8"`,
			wantDay:   "2025-12-01",
			wantBytes: 17,
			wantOK:    true,
		},
		{name: "blank", line: "", wantOK: false},
		{name: "no bracket", line: "garbage line without a date", wantOK: false},
		{
			name:   "non-numeric bytes",
			line:   `1.1.1.1 - - [13/Jun/2026:01:02:03 +0000] "GET / HTTP/1.1" 200 -`,
			wantOK: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			day, n, ok := parseAccessLine(c.line)
			if ok != c.wantOK {
				t.Fatalf("ok = %v, want %v", ok, c.wantOK)
			}
			if ok && (day != c.wantDay || n != c.wantBytes) {
				t.Fatalf("got (%q, %d), want (%q, %d)", day, n, c.wantDay, c.wantBytes)
			}
		})
	}
}

func TestCollectAccessLogIncremental(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "site.access.log")
	line := `1.1.1.1 - - [13/Jun/2026:01:02:03 +0000] "GET / HTTP/1.1" 200 100 "-" "-"` + "\n"

	if err := os.WriteFile(path, []byte(line+line), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := CollectAccessLog(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.PerDay["2026-06-13"] != 200 {
		t.Fatalf("first read: got %d, want 200", res.PerDay["2026-06-13"])
	}

	// Append one more line; only the new bytes should be counted.
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(line)
	f.Close()

	res2, err := CollectAccessLog(path, res.NewSize)
	if err != nil {
		t.Fatal(err)
	}
	if res2.PerDay["2026-06-13"] != 100 {
		t.Fatalf("incremental read: got %d, want 100 (no double count)", res2.PerDay["2026-06-13"])
	}
}

func TestCollectAccessLogRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "site.access.log")
	line := `1.1.1.1 - - [13/Jun/2026:01:02:03 +0000] "GET / HTTP/1.1" 200 50 "-" "-"` + "\n"

	// Two lines logged and read.
	os.WriteFile(path, []byte(line+line), 0o644)
	res, _ := CollectAccessLog(path, 0)
	if res.PerDay["2026-06-13"] != 100 {
		t.Fatalf("got %d, want 100", res.PerDay["2026-06-13"])
	}

	// logrotate: a third line was written, then the file rotated to .1 and a
	// fresh (smaller) live log started. The rotated tail must still be counted.
	os.WriteFile(path+".1", []byte(line+line+line), 0o644)
	os.WriteFile(path, []byte(line), 0o644)

	res2, _ := CollectAccessLog(path, res.NewSize)
	// One unread line from .1 (the 3rd) + one line in the new file = 100.
	if res2.PerDay["2026-06-13"] != 100 {
		t.Fatalf("after rotation: got %d, want 100", res2.PerDay["2026-06-13"])
	}
}
