package system

import (
	"bufio"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

// NginxLogDir is where the generated vhosts write their access logs
// (see WriteVhost). Traffic accounting reads the per-domain files here.
const NginxLogDir = "/var/log/nginx"

// AccessLogResult is the outcome of incrementally parsing a domain's nginx
// access log: response bytes grouped by calendar day, plus the live log's
// current size so the next collection can resume where this one stopped.
type AccessLogResult struct {
	PerDay  map[string]int64 // "2006-01-02" -> response body bytes served
	NewSize int64
}

// CollectAccessLog tallies the response bytes nginx logged for a domain since
// the previous collection. oldSize is the byte offset where the last read
// stopped. Only bytes appended since then are counted, so repeated calls never
// double-count. When the live file has shrunk (logrotate ran), the tail that
// rotated away is recovered from the freshly rotated "<path>.1" before the new
// file is read from the start.
func CollectAccessLog(path string, oldSize int64) (AccessLogResult, error) {
	res := AccessLogResult{PerDay: map[string]int64{}}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return res, nil // no traffic logged for this domain (yet)
		}
		return res, err
	}
	size := info.Size()
	from := oldSize
	if size < oldSize {
		// Rotated since last run: the bytes we hadn't read are now in <path>.1.
		// Debian's logrotate uses delaycompress, so .1 is still uncompressed.
		sumAccessLog(path+".1", oldSize, res.PerDay) // best effort
		from = 0
	}
	if err := sumAccessLog(path, from, res.PerDay); err != nil {
		return res, err
	}
	res.NewSize = size
	return res, nil
}

// sumAccessLog adds the response bytes of every log line at or after byte
// offset `from` into perDay.
func sumAccessLog(path string, from int64, perDay map[string]int64) error {
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
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20) // tolerate long lines
	for sc.Scan() {
		if day, n, ok := parseAccessLine(sc.Text()); ok {
			perDay[day] += n
		}
	}
	return sc.Err()
}

// parseAccessLine extracts the date and response-body byte count from a line in
// nginx's default "combined" log format:
//
//	$remote_addr - $remote_user [$time_local] "$request" $status $body_bytes_sent ...
//
// It returns the day as "2006-01-02" and the bytes sent. Lines that don't match
// (blank, malformed) are reported as not ok and skipped by the caller.
func parseAccessLine(line string) (string, int64, bool) {
	lb := strings.IndexByte(line, '[')
	if lb < 0 {
		return "", 0, false
	}
	rb := strings.IndexByte(line[lb:], ']')
	if rb < 0 {
		return "", 0, false
	}
	t, err := time.Parse("02/Jan/2006:15:04:05 -0700", line[lb+1:lb+rb])
	if err != nil {
		return "", 0, false
	}
	// Skip past the quoted request to reach `$status $body_bytes_sent`.
	rest := line[lb+rb+1:]
	q1 := strings.IndexByte(rest, '"')
	if q1 < 0 {
		return "", 0, false
	}
	q2 := strings.IndexByte(rest[q1+1:], '"')
	if q2 < 0 {
		return "", 0, false
	}
	fields := strings.Fields(rest[q1+1+q2+1:])
	if len(fields) < 2 {
		return "", 0, false
	}
	n, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return "", 0, false
	}
	return t.Format("2006-01-02"), n, true
}
