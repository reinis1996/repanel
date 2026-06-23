package system

import (
	"fmt"
	"net"
	"os"
	"regexp"
	"sort"
	"strings"
)

// fail2ban management through fail2ban-client. Bans/unbans are applied at runtime;
// the whitelist (ignoreip) is persisted to a panel-owned jail.d drop-in so it
// survives restarts.

const fail2banWhitelistFile = "/etc/fail2ban/jail.d/repanel-whitelist.local"

var validJailName = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,64}$`)

// Fail2banAvailable reports whether fail2ban-client is installed.
func Fail2banAvailable() bool { return have("fail2ban-client") }

// Fail2banJail is one jail's live status.
type Fail2banJail struct {
	Name   string   `json:"name"`
	Banned []string `json:"banned"`
	Total  int      `json:"total"`  // cumulative bans
	Failed int      `json:"failed"` // currently failed
}

// Fail2banJails lists the active jail names.
func Fail2banJails() ([]string, error) {
	out, err := run("fail2ban-client", "status")
	if err != nil {
		return nil, err
	}
	// "Jail list:\t sshd, nginx-http-auth"
	for _, line := range strings.Split(out, "\n") {
		if _, after, ok := strings.Cut(line, "Jail list:"); ok {
			var jails []string
			for _, j := range strings.Split(after, ",") {
				if name := strings.TrimSpace(j); name != "" {
					jails = append(jails, name)
				}
			}
			return jails, nil
		}
	}
	return nil, nil
}

// Fail2banJailStatus returns the banned IPs and counters for a jail.
func Fail2banJailStatus(jail string) (Fail2banJail, error) {
	j := Fail2banJail{Name: jail, Banned: []string{}}
	if !validJailName.MatchString(jail) {
		return j, fmt.Errorf("invalid jail name")
	}
	out, err := run("fail2ban-client", "status", jail)
	if err != nil {
		return j, err
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if _, after, ok := strings.Cut(line, "Banned IP list:"); ok {
			for _, ip := range strings.Fields(after) {
				j.Banned = append(j.Banned, ip)
			}
		} else if _, after, ok := strings.Cut(line, "Total banned:"); ok {
			fmt.Sscanf(strings.TrimSpace(after), "%d", &j.Total)
		} else if _, after, ok := strings.Cut(line, "Currently failed:"); ok {
			fmt.Sscanf(strings.TrimSpace(after), "%d", &j.Failed)
		}
	}
	return j, nil
}

// Fail2banBan / Fail2banUnban apply a runtime ban change for a jail.
func Fail2banBan(jail, ip string) error   { return fail2banIP(jail, "banip", ip) }
func Fail2banUnban(jail, ip string) error { return fail2banIP(jail, "unbanip", ip) }

func fail2banIP(jail, action, ip string) error {
	if !validJailName.MatchString(jail) {
		return fmt.Errorf("invalid jail name")
	}
	if net.ParseIP(strings.TrimSpace(ip)) == nil {
		return fmt.Errorf("invalid IP address")
	}
	_, err := run("fail2ban-client", "set", jail, action, strings.TrimSpace(ip))
	return err
}

// ---- jail / defaults configuration -----------------------------------------
//
// Tunable settings (global defaults and per-jail thresholds) are written to a
// panel-owned jail.d drop-in, so they layer on top of the distribution's
// jail.conf without editing it. Saving validates the result with
// `fail2ban-client -t` and rolls back if it's rejected.

const fail2banConfigFile = "/etc/fail2ban/jail.d/repanel-config.local"

// curatedJails are the jails relevant to the panel's stack, always offered for
// tuning even before they are active.
var curatedJails = []string{
	"sshd", "nginx-http-auth", "nginx-limit-req", "nginx-botsearch",
	"dovecot", "postfix", "postfix-sasl", "proftpd", "recidive",
}

var (
	validF2bTime   = regexp.MustCompile(`^(-1|\d+(\.\d+)?(s|m|h|d|w|mo|y)?)$`)
	validF2bNum    = regexp.MustCompile(`^\d{1,6}$`)
	validF2bFilter = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,64}$`)
	validF2bPort   = regexp.MustCompile(`^[a-zA-Z0-9,:-]{1,128}$`)
)

// Fail2banDefaults holds the [DEFAULT] tunables the panel manages.
type Fail2banDefaults struct {
	Bantime  string `json:"bantime"`
	Findtime string `json:"findtime"`
	Maxretry string `json:"maxretry"`
}

// Fail2banJailConfig is one jail's panel-managed settings plus its live state.
// Filter/Logpath/Port are needed to define a *new* jail; for jails already
// defined in jail.conf they may be left blank to inherit the stock definition.
type Fail2banJailConfig struct {
	Name     string `json:"name"`
	Enabled  bool   `json:"enabled"`
	Running  bool   `json:"running"` // currently active (read-only)
	Maxretry string `json:"maxretry"`
	Bantime  string `json:"bantime"`
	Findtime string `json:"findtime"`
	Filter   string `json:"filter"`
	Logpath  string `json:"logpath"`
	Port     string `json:"port"`
}

// Fail2banConfig is the full editable configuration surfaced to the UI.
type Fail2banConfig struct {
	Defaults Fail2banDefaults     `json:"defaults"`
	Jails    []Fail2banJailConfig `json:"jails"`
}

// parseINISections parses an INI-ish file into section -> key -> value.
func parseINISections(content string) map[string]map[string]string {
	out := map[string]map[string]string{}
	cur := ""
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			cur = strings.TrimSpace(line[1 : len(line)-1])
			if out[cur] == nil {
				out[cur] = map[string]string{}
			}
			continue
		}
		if cur == "" {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			out[cur][strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return out
}

// Fail2banReadConfig returns the panel-managed defaults and per-jail settings,
// merged with the live running state. The jail list is the union of the curated
// stack jails, the currently-running jails, and any the drop-in already configures.
func Fail2banReadConfig() Fail2banConfig {
	var cfg Fail2banConfig
	content, _ := os.ReadFile(fail2banConfigFile)
	sections := parseINISections(string(content))
	def := sections["DEFAULT"]
	cfg.Defaults = Fail2banDefaults{Bantime: def["bantime"], Findtime: def["findtime"], Maxretry: def["maxretry"]}

	running := map[string]bool{}
	if jails, err := Fail2banJails(); err == nil {
		for _, j := range jails {
			running[j] = true
		}
	}
	seen := map[string]bool{}
	order := []string{}
	add := func(n string) {
		if n != "" && n != "DEFAULT" && n != "INCLUDES" && validJailName.MatchString(n) && !seen[n] {
			seen[n] = true
			order = append(order, n)
		}
	}
	for _, n := range curatedJails {
		add(n)
	}
	for n := range running {
		add(n)
	}
	for n := range sections {
		add(n)
	}
	cfg.Jails = []Fail2banJailConfig{}
	for _, n := range order {
		sec := sections[n]
		jc := Fail2banJailConfig{
			Name:     n,
			Running:  running[n],
			Maxretry: sec["maxretry"],
			Bantime:  sec["bantime"],
			Findtime: sec["findtime"],
			Filter:   sec["filter"],
			Logpath:  sec["logpath"],
			Port:     sec["port"],
		}
		if v, ok := sec["enabled"]; ok {
			jc.Enabled = v == "true" || v == "1"
		} else {
			jc.Enabled = running[n]
		}
		cfg.Jails = append(cfg.Jails, jc)
	}
	return cfg
}

// renderFail2banConfig validates cfg and renders the drop-in file content.
func renderFail2banConfig(cfg Fail2banConfig) (string, error) {
	if err := validTime(cfg.Defaults.Bantime); err != nil {
		return "", err
	}
	if err := validTime(cfg.Defaults.Findtime); err != nil {
		return "", err
	}
	if err := validNum(cfg.Defaults.Maxretry); err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("# Managed by RePanel — fail2ban defaults and jails. Edit via the panel.\n[DEFAULT]\n")
	writeKV(&b, "bantime", cfg.Defaults.Bantime)
	writeKV(&b, "findtime", cfg.Defaults.Findtime)
	writeKV(&b, "maxretry", cfg.Defaults.Maxretry)
	for _, j := range cfg.Jails {
		if !validJailName.MatchString(j.Name) {
			return "", fmt.Errorf("invalid jail name %q", j.Name)
		}
		if err := validNum(j.Maxretry); err != nil {
			return "", fmt.Errorf("jail %s: %w", j.Name, err)
		}
		if err := validTime(j.Bantime); err != nil {
			return "", fmt.Errorf("jail %s: %w", j.Name, err)
		}
		if err := validTime(j.Findtime); err != nil {
			return "", fmt.Errorf("jail %s: %w", j.Name, err)
		}
		if f := strings.TrimSpace(j.Filter); f != "" && !validF2bFilter.MatchString(f) {
			return "", fmt.Errorf("jail %s: invalid filter name %q", j.Name, f)
		}
		if p := strings.TrimSpace(j.Port); p != "" && !validF2bPort.MatchString(p) {
			return "", fmt.Errorf("jail %s: invalid port %q", j.Name, p)
		}
		if err := validLogpath(j.Logpath); err != nil {
			return "", fmt.Errorf("jail %s: %w", j.Name, err)
		}
		fmt.Fprintf(&b, "\n[%s]\nenabled = %t\n", j.Name, j.Enabled)
		writeKV(&b, "filter", j.Filter)
		writeKV(&b, "logpath", j.Logpath)
		writeKV(&b, "port", j.Port)
		writeKV(&b, "maxretry", j.Maxretry)
		writeKV(&b, "bantime", j.Bantime)
		writeKV(&b, "findtime", j.Findtime)
	}
	return b.String(), nil
}

// validLogpath rejects values that could break the INI structure (a log path
// can otherwise be almost anything, including globs and multiple space-separated
// paths).
func validLogpath(v string) error {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	if len(v) > 512 || strings.ContainsAny(v, "\n\r[]") {
		return fmt.Errorf("invalid log path")
	}
	return nil
}

// Fail2banFilters lists the filter names available in filter.d (without .conf),
// so the UI can offer them when defining a new jail.
func Fail2banFilters() []string {
	entries, err := os.ReadDir(fail2banFilterDir)
	if err != nil {
		return []string{}
	}
	out := []string{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if name, ok := strings.CutSuffix(e.Name(), ".conf"); ok {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// ---- custom filters (filter.d) ---------------------------------------------
//
// Admins can define custom failregex rules so a new jail can match an app's own
// log format. Panel-created filters carry a marker comment; only those may be
// edited or deleted, so the distribution's stock filters are never clobbered.

const (
	fail2banFilterDir    = "/etc/fail2ban/filter.d"
	fail2banFilterMarker = "# Managed by RePanel"
)

// Fail2banFilter is a custom filter's editable content.
type Fail2banFilter struct {
	Name        string `json:"name"`
	Failregex   string `json:"failregex"`
	Ignoreregex string `json:"ignoreregex"`
	Custom      bool   `json:"custom"` // panel-managed (editable)
}

func fail2banFilterPath(name string) string {
	return fail2banFilterDir + "/" + name + ".conf"
}

// Fail2banCustomFilterNames lists the panel-managed filter names.
func Fail2banCustomFilterNames() []string {
	out := []string{}
	entries, err := os.ReadDir(fail2banFilterDir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name, ok := strings.CutSuffix(e.Name(), ".conf")
		if !ok {
			continue
		}
		if data, err := os.ReadFile(fail2banFilterPath(name)); err == nil && strings.Contains(string(data), fail2banFilterMarker) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// Fail2banReadFilter returns a filter's failregex/ignoreregex for editing.
func Fail2banReadFilter(name string) (Fail2banFilter, error) {
	f := Fail2banFilter{Name: name}
	if !validF2bFilter.MatchString(name) {
		return f, fmt.Errorf("invalid filter name")
	}
	data, err := os.ReadFile(fail2banFilterPath(name))
	if err != nil {
		return f, fmt.Errorf("filter not found")
	}
	content := string(data)
	f.Custom = strings.Contains(content, fail2banFilterMarker)
	f.Failregex, f.Ignoreregex = parseFilterRegexes(content)
	return f, nil
}

// parseFilterRegexes extracts the failregex and ignoreregex values (joining
// fail2ban's indented continuation lines with newlines).
func parseFilterRegexes(content string) (failregex, ignoreregex string) {
	var fail, ignore []string
	var cur *[]string
	for _, raw := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") || strings.HasPrefix(trimmed, "[") {
			cur = nil
			continue
		}
		indented := strings.HasPrefix(raw, " ") || strings.HasPrefix(raw, "\t")
		if !indented {
			cur = nil
			if k, v, ok := strings.Cut(trimmed, "="); ok {
				key, val := strings.TrimSpace(k), strings.TrimSpace(v)
				switch key {
				case "failregex":
					fail = nil
					if val != "" {
						fail = append(fail, val)
					}
					cur = &fail
				case "ignoreregex":
					ignore = nil
					if val != "" {
						ignore = append(ignore, val)
					}
					cur = &ignore
				}
			}
		} else if cur != nil {
			*cur = append(*cur, trimmed)
		}
	}
	return strings.Join(fail, "\n"), strings.Join(ignore, "\n")
}

// Fail2banWriteFilter creates or updates a panel-managed filter. It refuses to
// overwrite a stock (non-panel) filter. failregex must contain the <HOST> token.
func Fail2banWriteFilter(name, failregex, ignoreregex string) error {
	if !Fail2banAvailable() {
		return fmt.Errorf("fail2ban is not installed")
	}
	if !validF2bFilter.MatchString(name) {
		return fmt.Errorf("invalid filter name (letters, digits, dot, dash, underscore)")
	}
	if strings.ContainsRune(failregex, 0) || strings.ContainsRune(ignoreregex, 0) {
		return fmt.Errorf("invalid characters in filter")
	}
	if strings.TrimSpace(failregex) == "" {
		return fmt.Errorf("failregex is required")
	}
	if !strings.Contains(failregex, "<HOST>") {
		return fmt.Errorf("failregex must contain the <HOST> token (where the offending IP appears)")
	}
	path := fail2banFilterPath(name)
	if data, err := os.ReadFile(path); err == nil && !strings.Contains(string(data), fail2banFilterMarker) {
		return fmt.Errorf("a built-in filter named %q already exists; choose another name", name)
	}
	var b strings.Builder
	b.WriteString(fail2banFilterMarker + " — custom fail2ban filter. Edit via the panel.\n[Definition]\n")
	b.WriteString(formatFilterValue("failregex", failregex))
	b.WriteString(formatFilterValue("ignoreregex", ignoreregex))
	if err := os.MkdirAll(fail2banFilterDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return err
	}
	// Apply to any running jail that already uses this filter (best effort).
	run("fail2ban-client", "reload")
	return nil
}

// formatFilterValue renders a (possibly multi-line) regex value, indenting
// continuation lines as fail2ban requires.
func formatFilterValue(key, val string) string {
	val = strings.TrimRight(val, "\r\n")
	if strings.TrimSpace(val) == "" {
		return key + " =\n"
	}
	var b strings.Builder
	for i, ln := range strings.Split(val, "\n") {
		ln = strings.TrimRight(ln, "\r")
		if i == 0 {
			fmt.Fprintf(&b, "%s = %s\n", key, ln)
		} else {
			fmt.Fprintf(&b, "            %s\n", strings.TrimLeft(ln, " \t"))
		}
	}
	return b.String()
}

// Fail2banDeleteFilter removes a panel-managed filter.
func Fail2banDeleteFilter(name string) error {
	if !validF2bFilter.MatchString(name) {
		return fmt.Errorf("invalid filter name")
	}
	path := fail2banFilterPath(name)
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("filter not found")
	}
	if !strings.Contains(string(data), fail2banFilterMarker) {
		return fmt.Errorf("only panel-created filters can be deleted")
	}
	return os.Remove(path)
}

// Fail2banWriteConfig validates and writes the panel drop-in, then reloads
// fail2ban. On a rejected configuration it reverts the file and returns the error.
func Fail2banWriteConfig(cfg Fail2banConfig) error {
	if !Fail2banAvailable() {
		return fmt.Errorf("fail2ban is not installed")
	}
	content, err := renderFail2banConfig(cfg)
	if err != nil {
		return err
	}

	old, hadOld := os.ReadFile(fail2banConfigFile)
	if err := os.MkdirAll("/etc/fail2ban/jail.d", 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(fail2banConfigFile, []byte(content), 0o644); err != nil {
		return err
	}
	restore := func() {
		if hadOld == nil {
			os.WriteFile(fail2banConfigFile, old, 0o644)
		} else {
			os.Remove(fail2banConfigFile)
		}
	}
	if out, err := run("fail2ban-client", "-t"); err != nil {
		restore()
		return fmt.Errorf("fail2ban rejected the configuration: %s", firstLine(strings.TrimSpace(out)))
	}
	// Apply. A reload failure here (with a valid config) means the service is
	// stopped; the file is kept so it applies when fail2ban next starts.
	run("fail2ban-client", "reload")
	return nil
}

// writeKV appends "key = value" only when value is non-empty.
func writeKV(b *strings.Builder, key, value string) {
	if strings.TrimSpace(value) != "" {
		fmt.Fprintf(b, "%s = %s\n", key, strings.TrimSpace(value))
	}
}

func validTime(v string) error {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	if !validF2bTime.MatchString(strings.TrimSpace(v)) {
		return fmt.Errorf("invalid time %q (use e.g. 600, 10m, 1h, 1d, -1)", v)
	}
	return nil
}

func validNum(v string) error {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	if !validF2bNum.MatchString(strings.TrimSpace(v)) {
		return fmt.Errorf("invalid number %q", v)
	}
	return nil
}

// Fail2banGetWhitelist reads the panel-managed ignoreip entries (one per line).
func Fail2banGetWhitelist() []string {
	data, err := os.ReadFile(fail2banWhitelistFile)
	if err != nil {
		return []string{}
	}
	for _, line := range strings.Split(string(data), "\n") {
		if _, after, ok := strings.Cut(line, "ignoreip ="); ok {
			out := []string{}
			for _, ip := range strings.Fields(after) {
				// Hide the always-present loopback defaults from the UI list.
				if ip != "127.0.0.1/8" && ip != "::1" {
					out = append(out, ip)
				}
			}
			return out
		}
	}
	return []string{}
}

// Fail2banSetWhitelist writes the ignoreip drop-in (loopback is always included)
// and reloads fail2ban so it takes effect.
func Fail2banSetWhitelist(entries []string) error {
	clean := []string{"127.0.0.1/8", "::1"}
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if net.ParseIP(e) == nil {
			if _, _, err := net.ParseCIDR(e); err != nil {
				return fmt.Errorf("invalid IP or CIDR: %q", e)
			}
		}
		clean = append(clean, e)
	}
	body := "# Managed by RePanel — addresses fail2ban never bans.\n[DEFAULT]\nignoreip = " +
		strings.Join(clean, " ") + "\n"
	if err := os.MkdirAll("/etc/fail2ban/jail.d", 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(fail2banWhitelistFile, []byte(body), 0o644); err != nil {
		return err
	}
	if _, err := run("fail2ban-client", "reload"); err != nil {
		return err
	}
	return nil
}
