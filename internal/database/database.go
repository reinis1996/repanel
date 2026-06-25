// Package database wraps the embedded SQLite store that holds panel state.
// All host-level resources (vhosts, zones, mailboxes...) are mirrored here so
// the panel can render quickly and rebuild system config files at any time.
package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/reinis1996/repanel/internal/models"
)

type DB struct {
	*sql.DB
}

func Open(dataDir string) (*DB, error) {
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	path := filepath.Join(dataDir, "repanel.db")
	sqlDB, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	// SQLite handles one writer at a time; serialize access.
	sqlDB.SetMaxOpenConns(1)
	db := &DB{sqlDB}
	if err := db.migrate(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}

func (db *DB) migrate() error {
	schema := `
CREATE TABLE IF NOT EXISTS settings (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS users (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	username      TEXT NOT NULL UNIQUE,
	email         TEXT NOT NULL DEFAULT '',
	password_hash TEXT NOT NULL,
	role          TEXT NOT NULL DEFAULT 'user',
	owner_id      INTEGER NOT NULL DEFAULT 0,
	suspended     INTEGER NOT NULL DEFAULT 0,
	created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS sessions (
	token      TEXT PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	expires_at TIMESTAMP NOT NULL
);
-- Historical host metrics, sampled periodically for the dashboard graphs.
CREATE TABLE IF NOT EXISTS metrics (
	ts   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	cpu  REAL NOT NULL DEFAULT 0,
	mem  REAL NOT NULL DEFAULT 0,
	disk REAL NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_metrics_ts ON metrics(ts);
-- Audit trail of authenticated mutations and security events.
CREATE TABLE IF NOT EXISTS audit_log (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id    INTEGER NOT NULL DEFAULT 0,
	username   TEXT NOT NULL DEFAULT '',
	action     TEXT NOT NULL,
	detail     TEXT NOT NULL DEFAULT '',
	ip         TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS domains (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	name          TEXT NOT NULL UNIQUE,
	document_root TEXT NOT NULL,
	php_version   TEXT NOT NULL DEFAULT '8.3',
	ssl           INTEGER NOT NULL DEFAULT 0,
	suspended     INTEGER NOT NULL DEFAULT 0,
	created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS dns_zones (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	domain_id  INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
	name       TEXT NOT NULL UNIQUE,
	serial     INTEGER NOT NULL DEFAULT 1,
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS dns_records (
	id       INTEGER PRIMARY KEY AUTOINCREMENT,
	zone_id  INTEGER NOT NULL REFERENCES dns_zones(id) ON DELETE CASCADE,
	name     TEXT NOT NULL,
	type     TEXT NOT NULL,
	value    TEXT NOT NULL,
	ttl      INTEGER NOT NULL DEFAULT 3600,
	priority INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS mailboxes (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	domain_id     INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
	address       TEXT NOT NULL UNIQUE,
	password_hash TEXT NOT NULL,
	quota_mb      INTEGER NOT NULL DEFAULT 1024,
	created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS mail_aliases (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	domain_id   INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
	source      TEXT NOT NULL UNIQUE,
	destination TEXT NOT NULL
);
-- Distribution lists: an address that fans out to its member addresses.
CREATE TABLE IF NOT EXISTS mail_lists (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	domain_id  INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
	address    TEXT NOT NULL UNIQUE,
	members    TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
-- One autoresponder (vacation message) per mailbox, applied via Sieve.
CREATE TABLE IF NOT EXISTS mail_autoresponders (
	mailbox_id INTEGER PRIMARY KEY REFERENCES mailboxes(id) ON DELETE CASCADE,
	enabled    INTEGER NOT NULL DEFAULT 0,
	subject    TEXT NOT NULL DEFAULT '',
	message    TEXT NOT NULL DEFAULT '',
	start_date TEXT NOT NULL DEFAULT '',
	end_date   TEXT NOT NULL DEFAULT ''
);
-- Per-mailbox Sieve filter rules, ordered by position.
CREATE TABLE IF NOT EXISTS mail_filters (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	mailbox_id INTEGER NOT NULL REFERENCES mailboxes(id) ON DELETE CASCADE,
	position   INTEGER NOT NULL DEFAULT 0,
	field      TEXT NOT NULL,           -- from | to | subject | any
	op         TEXT NOT NULL,           -- contains | is
	value      TEXT NOT NULL,
	action     TEXT NOT NULL,           -- fileinto | forward | discard | keep
	arg        TEXT NOT NULL DEFAULT '' -- folder name or forward address
);
-- IMAP migration jobs (imapsync), tracked per mailbox.
CREATE TABLE IF NOT EXISTS mail_migrations (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	mailbox_id  INTEGER NOT NULL REFERENCES mailboxes(id) ON DELETE CASCADE,
	remote_host TEXT NOT NULL,
	remote_port INTEGER NOT NULL DEFAULT 993,
	remote_user TEXT NOT NULL,
	status      TEXT NOT NULL DEFAULT 'running',
	log         TEXT NOT NULL DEFAULT '',
	created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS db_entries (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	name       TEXT NOT NULL UNIQUE,
	db_user    TEXT NOT NULL,
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS ftp_accounts (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	username   TEXT NOT NULL UNIQUE,
	directory  TEXT NOT NULL,
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS cron_jobs (
	id       INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id  INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	schedule TEXT NOT NULL,
	command  TEXT NOT NULL,
	comment  TEXT NOT NULL DEFAULT '',
	enabled  INTEGER NOT NULL DEFAULT 1
);
CREATE TABLE IF NOT EXISTS certificates (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	domain_id  INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
	domain     TEXT NOT NULL,
	issuer     TEXT NOT NULL,
	not_after  TIMESTAMP,
	cert_path  TEXT NOT NULL,
	key_path   TEXT NOT NULL,
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS firewall_rules (
	id     INTEGER PRIMARY KEY AUTOINCREMENT,
	port   TEXT NOT NULL,
	proto  TEXT NOT NULL DEFAULT 'tcp',
	source TEXT NOT NULL DEFAULT 'any',
	action TEXT NOT NULL DEFAULT 'allow',
	note   TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS backups (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	filename   TEXT NOT NULL,
	size_bytes INTEGER NOT NULL DEFAULT 0,
	status     TEXT NOT NULL DEFAULT 'running',
	error      TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
-- Offsite backup destinations (rclone remotes). config is a JSON map of rclone
-- parameters (secrets included, obscured where rclone requires it).
CREATE TABLE IF NOT EXISTS backup_destinations (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	name        TEXT NOT NULL,
	type        TEXT NOT NULL,           -- s3 | b2 | sftp | ftp | rclone
	config      TEXT NOT NULL DEFAULT '{}',
	remote_path TEXT NOT NULL DEFAULT '',
	enabled     INTEGER NOT NULL DEFAULT 1,
	keep        INTEGER NOT NULL DEFAULT 7,
	created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS traffic (
	domain_id INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
	day       TEXT NOT NULL,
	bytes     INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (domain_id, day)
);
CREATE TABLE IF NOT EXISTS traffic_state (
	domain_id INTEGER PRIMARY KEY REFERENCES domains(id) ON DELETE CASCADE,
	log_size  INTEGER NOT NULL DEFAULT 0
);
-- Web statistics (AWStats-style), parsed from the per-domain access logs.
CREATE TABLE IF NOT EXISTS web_stats (
	domain_id INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
	day       TEXT NOT NULL,
	hits      INTEGER NOT NULL DEFAULT 0,
	pageviews INTEGER NOT NULL DEFAULT 0,
	bytes     INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (domain_id, day)
);
-- Top-N items per day: kind is 'page' | 'referrer' | 'status'.
CREATE TABLE IF NOT EXISTS web_stats_item (
	domain_id INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
	day       TEXT NOT NULL,
	kind      TEXT NOT NULL,
	label     TEXT NOT NULL,
	count     INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (domain_id, day, kind, label)
);
-- Distinct visitor IPs per day, deduplicated by the primary key across the
-- hourly incremental collections; unique-visitor counts derive from this.
CREATE TABLE IF NOT EXISTS web_stats_visitor (
	domain_id INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
	day       TEXT NOT NULL,
	ip        TEXT NOT NULL,
	PRIMARY KEY (domain_id, day, ip)
);
CREATE TABLE IF NOT EXISTS web_stats_state (
	domain_id INTEGER PRIMARY KEY REFERENCES domains(id) ON DELETE CASCADE,
	log_size  INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS dkim_keys (
	domain_id   INTEGER PRIMARY KEY REFERENCES domains(id) ON DELETE CASCADE,
	selector    TEXT NOT NULL DEFAULT 'repanel',
	private_pem TEXT NOT NULL,
	public_txt  TEXT NOT NULL,
	created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS apps (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	domain_id  INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
	app        TEXT NOT NULL,
	status     TEXT NOT NULL DEFAULT 'installing',
	error      TEXT NOT NULL DEFAULT '',
	url        TEXT NOT NULL DEFAULT '',
	db_name    TEXT NOT NULL DEFAULT '',
	auto_setup INTEGER NOT NULL DEFAULT 0,
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS api_tokens (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	name         TEXT NOT NULL,
	token_hash   TEXT NOT NULL UNIQUE,
	prefix       TEXT NOT NULL,
	last_used_at TIMESTAMP,
	expires_at   TIMESTAMP,
	created_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS functions (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	name         TEXT NOT NULL,
	slug         TEXT NOT NULL UNIQUE,
	runtime       TEXT NOT NULL,
	version       TEXT NOT NULL,
	trigger_type  TEXT NOT NULL DEFAULT 'url',
	schedule      TEXT NOT NULL DEFAULT '',
	allow_network INTEGER NOT NULL DEFAULT 0,
	base_domain  TEXT NOT NULL DEFAULT '',
	hostname     TEXT NOT NULL DEFAULT '',
	enabled      INTEGER NOT NULL DEFAULT 1,
	ssl          INTEGER NOT NULL DEFAULT 0,
	status       TEXT NOT NULL DEFAULT 'active',
	error        TEXT NOT NULL DEFAULT '',
	created_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`
	if _, err := db.Exec(schema); err != nil {
		return err
	}
	// Column additions for databases created by earlier versions; the error
	// when the column already exists is expected and ignored.
	db.Exec(`ALTER TABLE users ADD COLUMN disk_quota_mb INTEGER NOT NULL DEFAULT 0`)
	db.Exec(`ALTER TABLE db_entries ADD COLUMN engine TEXT NOT NULL DEFAULT 'mysql'`)
	db.Exec(`ALTER TABLE domains ADD COLUMN webmail INTEGER NOT NULL DEFAULT 0`)
	// Per-domain web server mode (nginx | apache | nginx-apache); empty means
	// "the stack default", resolved at vhost-generation time.
	db.Exec(`ALTER TABLE domains ADD COLUMN web_mode TEXT NOT NULL DEFAULT ''`)
	// API token scope: 'full' (default) or 'readonly' (SECURITY_AUDIT F-18).
	db.Exec(`ALTER TABLE api_tokens ADD COLUMN scope TEXT NOT NULL DEFAULT 'full'`)
	// Functions gained a trigger mode (url | schedule) after the table shipped.
	db.Exec(`ALTER TABLE functions ADD COLUMN trigger_type TEXT NOT NULL DEFAULT 'url'`)
	db.Exec(`ALTER TABLE functions ADD COLUMN schedule TEXT NOT NULL DEFAULT ''`)
	// Functions are network-isolated by default; opt-in per function.
	db.Exec(`ALTER TABLE functions ADD COLUMN allow_network INTEGER NOT NULL DEFAULT 0`)
	// Node.js web apps: a domain runs either PHP (default) or a Node app.
	db.Exec(`ALTER TABLE domains ADD COLUMN runtime TEXT NOT NULL DEFAULT 'php'`)
	db.Exec(`ALTER TABLE domains ADD COLUMN node_version TEXT NOT NULL DEFAULT ''`)
	db.Exec(`ALTER TABLE domains ADD COLUMN node_app_root TEXT NOT NULL DEFAULT ''`)
	db.Exec(`ALTER TABLE domains ADD COLUMN node_startup TEXT NOT NULL DEFAULT 'app.js'`)
	db.Exec(`ALTER TABLE domains ADD COLUMN node_port INTEGER NOT NULL DEFAULT 0`)
	db.Exec(`ALTER TABLE domains ADD COLUMN node_env TEXT NOT NULL DEFAULT ''`)
	// Per-domain mail spam/virus filtering (rspamd + ClamAV); on by default.
	db.Exec(`ALTER TABLE domains ADD COLUMN spam_filter INTEGER NOT NULL DEFAULT 1`)
	// Admin-editable per-site config overrides, merged into the generated nginx
	// server block, Apache vhost and PHP-FPM pool on every rebuild so they survive
	// SSL/PHP/suspend regeneration (the "additional directives" model).
	db.Exec(`ALTER TABLE domains ADD COLUMN nginx_conf TEXT NOT NULL DEFAULT ''`)
	db.Exec(`ALTER TABLE domains ADD COLUMN apache_conf TEXT NOT NULL DEFAULT ''`)
	db.Exec(`ALTER TABLE domains ADD COLUMN php_conf TEXT NOT NULL DEFAULT ''`)
	// Per-domain ModSecurity WAF: off by default. Mode is 'on' (blocking) or
	// 'detection' (DetectionOnly); waf_rules holds admin custom rules/exclusions.
	db.Exec(`ALTER TABLE domains ADD COLUMN waf_enabled INTEGER NOT NULL DEFAULT 0`)
	db.Exec(`ALTER TABLE domains ADD COLUMN waf_mode TEXT NOT NULL DEFAULT 'on'`)
	db.Exec(`ALTER TABLE domains ADD COLUMN waf_rules TEXT NOT NULL DEFAULT ''`)
	// Forwarders: an alias may keep a copy in the local mailbox and fan out to
	// several comma-separated destinations.
	db.Exec(`ALTER TABLE mail_aliases ADD COLUMN keep_copy INTEGER NOT NULL DEFAULT 0`)
	// Two-factor auth (TOTP) and per-account SSH access.
	db.Exec(`ALTER TABLE users ADD COLUMN totp_secret TEXT NOT NULL DEFAULT ''`)
	db.Exec(`ALTER TABLE users ADD COLUMN totp_enabled INTEGER NOT NULL DEFAULT 0`)
	db.Exec(`ALTER TABLE users ADD COLUMN recovery_codes TEXT NOT NULL DEFAULT ''`)
	db.Exec(`ALTER TABLE users ADD COLUMN ssh_enabled INTEGER NOT NULL DEFAULT 0`)
	db.Exec(`ALTER TABLE users ADD COLUMN ssh_keys TEXT NOT NULL DEFAULT ''`)
	// Session impersonation: who is impersonating, and the admin session to
	// return to when impersonation stops.
	db.Exec(`ALTER TABLE sessions ADD COLUMN impersonator_id INTEGER NOT NULL DEFAULT 0`)
	db.Exec(`ALTER TABLE sessions ADD COLUMN parent_token TEXT NOT NULL DEFAULT ''`)
	// Per-user module permissions (RBAC). Stored as a comma-separated list.
	db.Exec(`ALTER TABLE users ADD COLUMN permissions TEXT NOT NULL DEFAULT ''`)
	// Subdomains, addon and alias/parked domains. kind is 'primary' (a normal
	// domain, the default), 'subdomain' (a host under a parent domain) or 'alias'
	// (a parked domain that mirrors or redirects to its parent). parent_id is the
	// owning primary domain for subdomains/aliases, 0 otherwise.
	db.Exec(`ALTER TABLE domains ADD COLUMN kind TEXT NOT NULL DEFAULT 'primary'`)
	db.Exec(`ALTER TABLE domains ADD COLUMN parent_id INTEGER NOT NULL DEFAULT 0`)
	// Domain forwarding / parked-domain redirect: when redirect_url is set the
	// vhost becomes a pure HTTP redirect to it (redirect_code is 301 or 302).
	db.Exec(`ALTER TABLE domains ADD COLUMN redirect_url TEXT NOT NULL DEFAULT ''`)
	db.Exec(`ALTER TABLE domains ADD COLUMN redirect_code INTEGER NOT NULL DEFAULT 301`)
	// Structured, owner-editable per-domain PHP settings (JSON), rendered into the
	// PHP-FPM pool as php_admin_value lines. Distinct from the admin raw php_conf.
	db.Exec(`ALTER TABLE domains ADD COLUMN php_settings TEXT NOT NULL DEFAULT ''`)
	// Extra hostnames pointing at the same site (space-separated), rendered as
	// additional server_name / ServerAlias entries and certificate SAN hosts.
	// Backfill www.<name> only on first add (ALTER succeeds), so we don't clobber
	// a primary whose aliases an owner has since cleared.
	if _, err := db.Exec(`ALTER TABLE domains ADD COLUMN aliases TEXT NOT NULL DEFAULT ''`); err == nil {
		db.Exec(`UPDATE domains SET aliases = 'www.' || name WHERE kind = 'primary'`)
	}
	// Per-zone DNSSEC signing flag (BIND inline-signing via dnssec-policy).
	db.Exec(`ALTER TABLE dns_zones ADD COLUMN dnssec INTEGER NOT NULL DEFAULT 0`)
	// Per-account resource limits (0 = unlimited), the counts a hosting operator
	// caps a customer at, complementing the existing disk quota.
	db.Exec(`ALTER TABLE users ADD COLUMN max_domains INTEGER NOT NULL DEFAULT 0`)
	db.Exec(`ALTER TABLE users ADD COLUMN max_mailboxes INTEGER NOT NULL DEFAULT 0`)
	db.Exec(`ALTER TABLE users ADD COLUMN max_databases INTEGER NOT NULL DEFAULT 0`)
	db.Exec(`ALTER TABLE users ADD COLUMN bandwidth_quota_mb INTEGER NOT NULL DEFAULT 0`)
	// Live per-account resource caps applied via a systemd slice (0 = unlimited):
	// CPU as a percentage of one core (100 = one core), memory hard cap in MB, and
	// a process/thread (TasksMax) cap.
	db.Exec(`ALTER TABLE users ADD COLUMN cpu_quota_pct INTEGER NOT NULL DEFAULT 0`)
	db.Exec(`ALTER TABLE users ADD COLUMN memory_max_mb INTEGER NOT NULL DEFAULT 0`)
	db.Exec(`ALTER TABLE users ADD COLUMN processes_max INTEGER NOT NULL DEFAULT 0`)
	// One-click apps that finish in a browser installer need their database
	// credentials shown to the user, so they're stored on the app record.
	db.Exec(`ALTER TABLE apps ADD COLUMN db_user TEXT NOT NULL DEFAULT ''`)
	db.Exec(`ALTER TABLE apps ADD COLUMN db_pass TEXT NOT NULL DEFAULT ''`)
	// Tracks domains auto-suspended for monthly bandwidth overage, kept distinct
	// from manual suspension so enforcement only ever lifts its own suspensions.
	db.Exec(`ALTER TABLE domains ADD COLUMN bw_suspended INTEGER NOT NULL DEFAULT 0`)
	// Hosting plans (service plans / packages): a named template of resource
	// limits and grantable modules. Assigning a plan to an account copies its
	// values into that account's own limit columns (which the create paths
	// enforce), and records plan_id for display.
	db.Exec(`CREATE TABLE IF NOT EXISTS plans (
		id                 INTEGER PRIMARY KEY AUTOINCREMENT,
		name               TEXT NOT NULL UNIQUE,
		disk_quota_mb      INTEGER NOT NULL DEFAULT 0,
		bandwidth_quota_mb INTEGER NOT NULL DEFAULT 0,
		max_domains        INTEGER NOT NULL DEFAULT 0,
		max_mailboxes      INTEGER NOT NULL DEFAULT 0,
		max_databases      INTEGER NOT NULL DEFAULT 0,
		modules            TEXT NOT NULL DEFAULT '',
		created_at         TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`)
	db.Exec(`ALTER TABLE users ADD COLUMN plan_id INTEGER NOT NULL DEFAULT 0`)
	// Cloudflare DNS sync: per-zone API token + Cloudflare zone id, and the sync
	// mode (off | push = RePanel authoritative, pull = Cloudflare authoritative).
	// proxied round-trips a record's Cloudflare proxy (orange-cloud) state.
	db.Exec(`ALTER TABLE dns_zones ADD COLUMN cf_zone_id TEXT NOT NULL DEFAULT ''`)
	db.Exec(`ALTER TABLE dns_zones ADD COLUMN cf_token TEXT NOT NULL DEFAULT ''`)
	db.Exec(`ALTER TABLE dns_zones ADD COLUMN cf_sync TEXT NOT NULL DEFAULT 'off'`)
	db.Exec(`ALTER TABLE dns_records ADD COLUMN proxied INTEGER NOT NULL DEFAULT 0`)
	// Password-protected directories (.htpasswd) per domain, with their users.
	db.Exec(`CREATE TABLE IF NOT EXISTS protected_dirs (
		id        INTEGER PRIMARY KEY AUTOINCREMENT,
		domain_id INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
		path      TEXT NOT NULL,
		realm     TEXT NOT NULL DEFAULT 'Restricted',
		UNIQUE(domain_id, path)
	)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS protected_users (
		id            INTEGER PRIMARY KEY AUTOINCREMENT,
		dir_id        INTEGER NOT NULL REFERENCES protected_dirs(id) ON DELETE CASCADE,
		username      TEXT NOT NULL,
		password_hash TEXT NOT NULL,
		UNIQUE(dir_id, username)
	)`)
	// One-time RBAC seed: grant every existing account all modules and default
	// both group templates to all modules, so turning RBAC on locks no one out.
	if db.Setting("rbac_initialized") == "" {
		all := strings.Join(models.AllModules, ",")
		db.Exec(`UPDATE users SET permissions = ? WHERE permissions = ''`, all)
		db.SetSetting("default_perms_user", all)
		db.SetSetting("default_perms_reseller", all)
		db.SetSetting("rbac_initialized", "1")
	}
	return nil
}

// Setting reads a value from the settings table, "" when absent.
func (db *DB) Setting(key string) string {
	var v string
	db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	return v
}

func (db *DB) SetSetting(key, value string) error {
	_, err := db.Exec(`INSERT INTO settings(key,value) VALUES(?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}
