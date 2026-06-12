// Package config loads panel configuration from /etc/repanel/repanel.conf
// (simple KEY=VALUE format) with sane defaults for a fresh installation.
package config

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	ListenAddr   string // host:port the panel listens on
	DataDir      string // sqlite db, acme state, generated maps
	WebRoot      string // base dir for customer document roots
	NginxDir     string // nginx configuration directory
	BindDir      string // bind9 zone file directory
	MailDir      string // generated postfix/dovecot maps
	TLSCert      string // optional TLS cert for the panel itself
	TLSKey       string
	SessionHours int
	Debug        bool
}

func Default() *Config {
	return &Config{
		ListenAddr:   ":8443",
		DataDir:      "/var/lib/repanel",
		WebRoot:      "/var/www",
		NginxDir:     "/etc/nginx",
		BindDir:      "/etc/bind",
		MailDir:      "/etc/repanel/mail",
		SessionHours: 24,
	}
}

// Load reads the config file at path; missing file returns defaults.
func Load(path string) (*Config, error) {
	cfg := Default()
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		switch key {
		case "LISTEN":
			cfg.ListenAddr = val
		case "DATA_DIR":
			cfg.DataDir = val
		case "WEB_ROOT":
			cfg.WebRoot = val
		case "NGINX_DIR":
			cfg.NginxDir = val
		case "BIND_DIR":
			cfg.BindDir = val
		case "MAIL_DIR":
			cfg.MailDir = val
		case "TLS_CERT":
			cfg.TLSCert = val
		case "TLS_KEY":
			cfg.TLSKey = val
		case "SESSION_HOURS":
			if n, err := strconv.Atoi(val); err == nil && n > 0 {
				cfg.SessionHours = n
			}
		case "DEBUG":
			cfg.Debug = val == "1" || strings.EqualFold(val, "true")
		}
	}
	return cfg, sc.Err()
}
