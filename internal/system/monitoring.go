package system

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Service health diagnostics, a tail-based log viewer, and notification delivery
// (email via the local MTA, plus webhooks). All degrade gracefully off-Linux.

var validUnit = regexp.MustCompile(`^[a-z0-9@._-]{1,64}$`)

// isManagedUnit reports whether name is a service the panel supervises, so the
// logs endpoint can't be pointed at arbitrary systemd units.
func isManagedUnit(name string) bool {
	for _, svc := range managedServices() {
		if svc.Name == name {
			return true
		}
	}
	return false
}

// ServiceLogs returns a managed service's `systemctl status` summary and recent
// journal lines, so an operator can see why it is down.
func ServiceLogs(name string) (status, logs string, err error) {
	if !validUnit.MatchString(name) || !isManagedUnit(name) {
		return "", "", fmt.Errorf("unknown service")
	}
	if !have("systemctl") {
		return "", "", fmt.Errorf("systemctl not available on this host")
	}
	status, _ = run("systemctl", "status", name, "--no-pager", "--lines", "0")
	if have("journalctl") {
		logs, _ = run("journalctl", "-u", name, "-n", "150", "--no-pager", "--output", "short-iso")
	}
	return status, logs, nil
}

// logFiles is the allowlist of host log files the viewer may tail. Keeping it
// fixed prevents the endpoint from reading arbitrary files.
var logFiles = map[string]string{
	"nginx-error":  "/var/log/nginx/error.log",
	"nginx-access": "/var/log/nginx/access.log",
	"apache-error": "/var/log/apache2/error.log",
	"mail":         "/var/log/mail.log",
	"fail2ban":     "/var/log/fail2ban.log",
	"auth":         "/var/log/auth.log",
	"syslog":       "/var/log/syslog",
}

// LogFileKeys lists the log files that exist on this host, for the viewer's menu.
func LogFileKeys() []string {
	keys := []string{}
	for k, path := range logFiles {
		if _, err := os.Stat(path); err == nil {
			keys = append(keys, k)
		}
	}
	return keys
}

// TailLog returns the last ~400 lines of an allowlisted log file.
func TailLog(key string) (string, error) {
	path, ok := logFiles[key]
	if !ok {
		return "", fmt.Errorf("unknown log")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	// Read at most the last 256 KiB to bound memory, then keep the last 400 lines.
	const window = 256 << 10
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	start := int64(0)
	if info.Size() > window {
		start = info.Size() - window
	}
	if _, err := f.Seek(start, 0); err != nil {
		return "", err
	}
	buf, err := readAllLimited(f, window+1<<10)
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(buf), "\n")
	if len(lines) > 400 {
		lines = lines[len(lines)-400:]
	}
	return strings.Join(lines, "\n"), nil
}

func readAllLimited(f *os.File, max int) ([]byte, error) {
	buf := make([]byte, max)
	n, err := f.Read(buf)
	if err != nil && n == 0 {
		return nil, err
	}
	return buf[:n], nil
}

// SendEmail sends a plain-text message through the local MTA (sendmail). It is a
// no-op when no sendmail binary is present.
func SendEmail(to, from, subject, body string) error {
	bin := sendmailPath()
	if bin == "" {
		return fmt.Errorf("no local mail transfer agent (sendmail) found")
	}
	if from == "" {
		host, _ := os.Hostname()
		from = "repanel@" + host
	}
	msg := fmt.Sprintf("From: RePanel <%s>\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s\r\n",
		from, to, subject, body)
	cmd := exec.Command(bin, "-t", "-i")
	cmd.Stdin = strings.NewReader(msg)
	var errb bytes.Buffer
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sendmail: %w: %s", err, strings.TrimSpace(errb.String()))
	}
	return nil
}

func sendmailPath() string {
	for _, p := range []string{"/usr/sbin/sendmail", "/usr/lib/sendmail"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath("sendmail"); err == nil {
		return p
	}
	return ""
}

// PostWebhook delivers a JSON payload to a URL with a short timeout.
func PostWebhook(url string, payload []byte) error {
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return fmt.Errorf("webhook URL must be http(s)")
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned %s", resp.Status)
	}
	return nil
}
