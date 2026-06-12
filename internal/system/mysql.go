package system

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// MariaDB administration runs through the local `mysql` client over the unix
// socket, which authenticates as root via unix_socket auth on stock
// Debian/Ubuntu installs — no stored root password needed.

var validDBName = regexp.MustCompile(`^[A-Za-z0-9_]{1,60}$`)

func mysqlExec(query string) (string, error) {
	if !have("mysql") {
		return "", fmt.Errorf("MariaDB/MySQL client not installed on this host")
	}
	cmd := exec.Command("mysql", "--batch", "--skip-column-names", "-e", query)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("mysql: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func quoteIdent(name string) string { return "`" + name + "`" }

// escapeSQLString escapes a value for use inside single quotes.
func escapeSQLString(v string) string {
	r := strings.NewReplacer(`\`, `\\`, `'`, `\'`)
	return r.Replace(v)
}

// CreateDatabase creates a database plus a dedicated user with full rights on
// it, matching the Plesk "database + related user" flow.
func CreateDatabase(name, user, password string) error {
	if !validDBName.MatchString(name) || !validDBName.MatchString(user) {
		return fmt.Errorf("database and user names may only contain letters, digits and underscores")
	}
	stmts := []string{
		fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %s CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci", quoteIdent(name)),
		fmt.Sprintf("CREATE USER IF NOT EXISTS '%s'@'localhost' IDENTIFIED BY '%s'", escapeSQLString(user), escapeSQLString(password)),
		fmt.Sprintf("GRANT ALL PRIVILEGES ON %s.* TO '%s'@'localhost'", quoteIdent(name), escapeSQLString(user)),
		"FLUSH PRIVILEGES",
	}
	_, err := mysqlExec(strings.Join(stmts, "; "))
	return err
}

// DropDatabase removes the database and its dedicated user.
func DropDatabase(name, user string) error {
	if !validDBName.MatchString(name) {
		return fmt.Errorf("invalid database name")
	}
	stmts := []string{fmt.Sprintf("DROP DATABASE IF EXISTS %s", quoteIdent(name))}
	if validDBName.MatchString(user) {
		stmts = append(stmts, fmt.Sprintf("DROP USER IF EXISTS '%s'@'localhost'", escapeSQLString(user)))
	}
	_, err := mysqlExec(strings.Join(stmts, "; "))
	return err
}

// SetDatabasePassword updates the dedicated user's password.
func SetDatabasePassword(user, password string) error {
	if !validDBName.MatchString(user) {
		return fmt.Errorf("invalid database user")
	}
	_, err := mysqlExec(fmt.Sprintf("ALTER USER '%s'@'localhost' IDENTIFIED BY '%s'; FLUSH PRIVILEGES",
		escapeSQLString(user), escapeSQLString(password)))
	return err
}

// DatabaseSizes returns on-disk size per schema in MB.
func DatabaseSizes() map[string]float64 {
	sizes := map[string]float64{}
	out, err := mysqlExec("SELECT table_schema, ROUND(SUM(data_length+index_length)/1048576,2) FROM information_schema.tables GROUP BY table_schema")
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
