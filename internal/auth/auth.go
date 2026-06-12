// Package auth implements password hashing and cookie-session authentication.
package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/reinis1996/repanel/internal/database"
	"github.com/reinis1996/repanel/internal/models"
)

const CookieName = "repanel_session"

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

// UserForRequest resolves the session cookie to a user, nil when anonymous.
func (m *Manager) UserForRequest(r *http.Request) *models.User {
	c, err := r.Cookie(CookieName)
	if err != nil || c.Value == "" {
		return nil
	}
	var userID int64
	var expires time.Time
	err = m.DB.QueryRow(`SELECT user_id, expires_at FROM sessions WHERE token = ?`, c.Value).
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

// PruneSessions removes expired sessions; called periodically.
func (m *Manager) PruneSessions() {
	m.DB.Exec(`DELETE FROM sessions WHERE expires_at < ?`, time.Now())
}

func scanUser(row interface{ Scan(...any) error }) (*models.User, error) {
	var u models.User
	var suspended int
	err := row.Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Role, &u.OwnerID, &suspended, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	u.Suspended = suspended != 0
	return &u, nil
}

const userCols = `id, username, email, password_hash, role, owner_id, suspended, created_at`

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
