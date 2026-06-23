package system

import (
	"fmt"
	"strconv"
	"strings"
)

// PostgreSQL administration runs the local `psql` client as the `postgres`
// superuser via `sudo -u postgres`, which authenticates through peer auth on
// stock Debian/Ubuntu installs — the same password-free model the MariaDB
// integration relies on. The panel process runs as root, so sudo needs no
// password.

// HavePostgres reports whether a PostgreSQL client is installed.
func HavePostgres() bool { return have("psql") }

// HaveMySQL reports whether a MariaDB/MySQL client is installed.
func HaveMySQL() bool { return have("mysql") }

// pgEscape escapes a value for a single-quoted PostgreSQL string literal.
// With standard_conforming_strings on (the default), only the quote needs
// doubling.
func pgEscape(v string) string { return strings.ReplaceAll(v, "'", "''") }

// psql runs one statement as the postgres superuser, returning tab-separated,
// header-less output. Each call autocommits, which is what CREATE DATABASE (not
// allowed inside a transaction block) needs.
func psql(stmt string) (string, error) {
	if !HavePostgres() {
		return "", fmt.Errorf("PostgreSQL client not installed on this host")
	}
	return run("sudo", "-n", "-u", "postgres", "psql", "-v", "ON_ERROR_STOP=1", "-A", "-t", "-F", "\t", "-c", stmt)
}

// CreatePostgresDatabase creates a database owned by a dedicated login role,
// mirroring the "database + related user" flow used for MariaDB.
func CreatePostgresDatabase(name, user, password string) error {
	if !validDBName.MatchString(name) || !validDBName.MatchString(user) {
		return fmt.Errorf("database and user names may only contain letters, digits and underscores")
	}
	// Create the role fresh. If it already exists we must NOT adopt or
	// re-password it — that role may belong to another tenant (or be a system
	// role), and resetting its password would hand over their databases (see
	// SECURITY_AUDIT F-03).
	if _, err := psql(fmt.Sprintf(`CREATE ROLE "%s" LOGIN PASSWORD '%s'`, user, pgEscape(password))); err != nil {
		if strings.Contains(err.Error(), "already exists") {
			return fmt.Errorf("database user %q already exists", user)
		}
		return err
	}
	if _, err := psql(fmt.Sprintf(`CREATE DATABASE "%s" OWNER "%s"`, name, user)); err != nil {
		return err
	}
	_, err := psql(fmt.Sprintf(`GRANT ALL PRIVILEGES ON DATABASE "%s" TO "%s"`, name, user))
	return err
}

// DropPostgresDatabase removes the database and its dedicated role.
func DropPostgresDatabase(name, user string) error {
	if !validDBName.MatchString(name) {
		return fmt.Errorf("invalid database name")
	}
	// Existing sessions block DROP DATABASE; disconnect them first.
	psql(fmt.Sprintf(`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '%s'`, pgEscape(name)))
	if _, err := psql(fmt.Sprintf(`DROP DATABASE IF EXISTS "%s"`, name)); err != nil {
		return err
	}
	// The role may own objects in other databases; failing to drop it is not
	// fatal to removing this database.
	if validDBName.MatchString(user) {
		psql(fmt.Sprintf(`DROP ROLE IF EXISTS "%s"`, user))
	}
	return nil
}

// SetPostgresPassword updates the dedicated role's password.
func SetPostgresPassword(user, password string) error {
	if !validDBName.MatchString(user) {
		return fmt.Errorf("invalid database user")
	}
	_, err := psql(fmt.Sprintf(`ALTER ROLE "%s" WITH PASSWORD '%s'`, user, pgEscape(password)))
	return err
}

// PostgresDatabaseSizes returns on-disk size per database in MB.
func PostgresDatabaseSizes() map[string]float64 {
	sizes := map[string]float64{}
	out, err := psql(`SELECT datname, round(pg_database_size(datname) / 1048576.0, 2) FROM pg_database WHERE datistemplate = false`)
	if err != nil {
		return sizes
	}
	for _, line := range strings.Split(out, "\n") {
		name, val, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		if f, err := strconv.ParseFloat(strings.TrimSpace(val), 64); err == nil {
			sizes[strings.TrimSpace(name)] = f
		}
	}
	return sizes
}
