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

// Login verifies credentials and creates a session, returning the token.
func (m *Manager) Login(username, password string) (string, *models.User, error) {
	u, err := GetUserByUsername(m.DB, username)
	if err != nil || u == nil || u.Suspended {
		// Burn time even for unknown users to blunt user enumeration.
		bcrypt.CompareHashAndPassword([]byte("$2a$10$7EqJtq98hPqEX7fNZaFWoOhi5B0xN1p0y1Qq0F0F0F0F0F0F0F0F0"), []byte(password))
		return "", nil, ErrInvalidCredentials
	}
	if !CheckPassword(u.PasswordHash, password) {
		return "", nil, ErrInvalidCredentials
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", nil, err
	}
	token := hex.EncodeToString(buf)
	expires := time.Now().Add(time.Duration(m.SessionHours) * time.Hour)
	if _, err := m.DB.Exec(`INSERT INTO sessions(token,user_id,expires_at) VALUES(?,?,?)`,
		token, u.ID, expires); err != nil {
		return "", nil, err
	}
	return token, u, nil
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
	var suspended int
	err := row.Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Role, &u.OwnerID, &suspended, &u.DiskQuotaMB, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	u.Suspended = suspended != 0
	return &u, nil
}

const userCols = `id, username, email, password_hash, role, owner_id, suspended, disk_quota_mb, created_at`

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
