// Package database wraps the embedded SQLite store that holds panel state.
// All host-level resources (vhosts, zones, mailboxes...) are mirrored here so
// the panel can render quickly and rebuild system config files at any time.
package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
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
`
	if _, err := db.Exec(schema); err != nil {
		return err
	}
	// Column additions for databases created by earlier versions; the error
	// when the column already exists is expected and ignored.
	db.Exec(`ALTER TABLE users ADD COLUMN disk_quota_mb INTEGER NOT NULL DEFAULT 0`)
	db.Exec(`ALTER TABLE db_entries ADD COLUMN engine TEXT NOT NULL DEFAULT 'mysql'`)
	db.Exec(`ALTER TABLE domains ADD COLUMN webmail INTEGER NOT NULL DEFAULT 0`)
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
