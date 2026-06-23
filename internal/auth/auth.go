// Package auth implements password hashing and cookie-session authentication.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/reinis1996/repanel/internal/database"
	"github.com/reinis1996/repanel/internal/models"
)

const CookieName = "repanel_session"

// TokenPrefix labels personal API tokens so they're recognisable in logs and
// the UI (and lets the auth path reject obvious non-tokens cheaply).
const TokenPrefix = "rpat_"

var ErrInvalidCredentials = errors.New("invalid username or password")

func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(b), err
}

func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

type Manager struct {
	DB           *database.DB
	SessionHours int
}

// VerifyPassword checks credentials without creating a session, so the caller can
// interpose a second factor (TOTP). The timing burn for unknown/suspended users
// blunts enumeration.
func (m *Manager) VerifyPassword(username, password string) (*models.User, error) {
	u, err := GetUserByUsername(m.DB, username)
	if err != nil || u == nil || u.Suspended {
		bcrypt.CompareHashAndPassword([]byte("$2a$10$7EqJtq98hPqEX7fNZaFWoOhi5B0xN1p0y1Qq0F0F0F0F0F0F0F0F0"), []byte(password))
		return nil, ErrInvalidCredentials
	}
	if !CheckPassword(u.PasswordHash, password) {
		return nil, ErrInvalidCredentials
	}
	return u, nil
}

// CreateSession issues a session token for a user. impersonatorID and parentToken
// are non-zero only for impersonation sessions (see the impersonation handlers).
func (m *Manager) CreateSession(userID, impersonatorID int64, parentToken string) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := hex.EncodeToString(buf)
	expires := time.Now().Add(time.Duration(m.SessionHours) * time.Hour)
	if _, err := m.DB.Exec(`INSERT INTO sessions(token,user_id,expires_at,impersonator_id,parent_token) VALUES(?,?,?,?,?)`,
		token, userID, expires, impersonatorID, parentToken); err != nil {
		return "", err
	}
	return token, nil
}

// SessionMeta returns the impersonator id and parent (admin) token for a session.
func (m *Manager) SessionMeta(token string) (impersonatorID int64, parentToken string, ok bool) {
	err := m.DB.QueryRow(`SELECT impersonator_id, parent_token FROM sessions WHERE token = ?`, token).
		Scan(&impersonatorID, &parentToken)
	return impersonatorID, parentToken, err == nil
}

func (m *Manager) Logout(token string) {
	m.DB.Exec(`DELETE FROM sessions WHERE token = ?`, token)
}

// UserForRequest resolves a request to a user, nil when anonymous. It accepts
// either a session cookie (browser) or an `Authorization: Bearer <token>`
// personal API token (automation / CLI).
func (m *Manager) UserForRequest(r *http.Request) *models.User {
	if c, err := r.Cookie(CookieName); err == nil && c.Value != "" {
		return m.userForSession(c.Value)
	}
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return m.userForToken(strings.TrimSpace(h[len("Bearer "):]))
	}
	return nil
}

// RequestReadOnly reports whether the request is authenticated with a
// read-only personal API token. Cookie (browser) sessions are always full
// access. Used by the API to reject mutating requests from read-only tokens
// (see SECURITY_AUDIT F-18).
func (m *Manager) RequestReadOnly(r *http.Request) bool {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return false
	}
	token := strings.TrimSpace(h[len("Bearer "):])
	if !strings.HasPrefix(token, TokenPrefix) {
		return false
	}
	var scope string
	m.DB.QueryRow(`SELECT scope FROM api_tokens WHERE token_hash = ?`, HashToken(token)).Scan(&scope)
	return scope == "readonly"
}

func (m *Manager) userForSession(token string) *models.User {
	var userID int64
	var expires time.Time
	err := m.DB.QueryRow(`SELECT user_id, expires_at FROM sessions WHERE token = ?`, token).
		Scan(&userID, &expires)
	if err != nil || time.Now().After(expires) {
		return nil
	}
	u, err := GetUserByID(m.DB, userID)
	if err != nil || u == nil || u.Suspended {
		return nil
	}
	return u
}

// userForToken validates a personal API token and records its use. The stored
// value is a SHA-256 hash of the token, so the database never holds the secret.
func (m *Manager) userForToken(token string) *models.User {
	if !strings.HasPrefix(token, TokenPrefix) {
		return nil
	}
	hash := HashToken(token)
	var userID int64
	var expires sql.NullTime
	err := m.DB.QueryRow(`SELECT user_id, expires_at FROM api_tokens WHERE token_hash = ?`, hash).
		Scan(&userID, &expires)
	if err != nil {
		return nil
	}
	if expires.Valid && time.Now().After(expires.Time) {
		return nil
	}
	u, err := GetUserByID(m.DB, userID)
	if err != nil || u == nil || u.Suspended {
		return nil
	}
	m.DB.Exec(`UPDATE api_tokens SET last_used_at = ? WHERE token_hash = ?`, time.Now(), hash)
	return u
}

// NewAPIToken mints a personal API token: the secret to show the user once, the
// SHA-256 hash to persist, and a short non-secret prefix for display.
func NewAPIToken() (token, hash, prefix string, err error) {
	buf := make([]byte, 24)
	if _, err = rand.Read(buf); err != nil {
		return "", "", "", err
	}
	token = TokenPrefix + hex.EncodeToString(buf)
	return token, HashToken(token), token[:len(TokenPrefix)+8], nil
}

// HashToken returns the storage hash for an API token.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// PruneSessions removes expired sessions; called periodically.
func (m *Manager) PruneSessions() {
	m.DB.Exec(`DELETE FROM sessions WHERE expires_at < ?`, time.Now())
}

func scanUser(row interface{ Scan(...any) error }) (*models.User, error) {
	var u models.User
	var suspended, totp, ssh int
	var perms string
	err := row.Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Role, &u.OwnerID, &suspended, &u.DiskQuotaMB, &u.CreatedAt, &perms, &totp, &ssh,
		&u.MaxDomains, &u.MaxMailboxes, &u.MaxDatabases, &u.BandwidthQuotaMB, &u.PlanID,
		&u.CPUQuotaPct, &u.MemoryMaxMB, &u.ProcessesMax)
	if err != nil {
		return nil, err
	}
	u.Suspended = suspended != 0
	u.TOTPEnabled = totp != 0
	u.SSHEnabled = ssh != 0
	u.Permissions = SplitPermissions(perms)
	return &u, nil
}

const userCols = `id, username, email, password_hash, role, owner_id, suspended, disk_quota_mb, created_at, permissions, totp_enabled, ssh_enabled, max_domains, max_mailboxes, max_databases, bandwidth_quota_mb, plan_id, cpu_quota_pct, memory_max_mb, processes_max`

// SplitPermissions parses the stored comma-separated module list.
func SplitPermissions(csv string) []string {
	out := []string{}
	for _, p := range strings.Split(csv, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// JoinPermissions renders a module list for storage, keeping only known modules.
func JoinPermissions(mods []string) string {
	seen := map[string]bool{}
	var out []string
	for _, m := range mods {
		m = strings.TrimSpace(m)
		if m != "" && models.ValidModule(m) && !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	return strings.Join(out, ",")
}

func GetUserByUsername(db *database.DB, username string) (*models.User, error) {
	u, err := scanUser(db.QueryRow(`SELECT `+userCols+` FROM users WHERE username = ?`, username))
	if err != nil {
		return nil, nil //nolint:nilerr // absent user is not an error
	}
	return u, nil
}

func GetUserByID(db *database.DB, id int64) (*models.User, error) {
	u, err := scanUser(db.QueryRow(`SELECT `+userCols+` FROM users WHERE id = ?`, id))
	if err != nil {
		return nil, nil //nolint:nilerr
	}
	return u, nil
}
