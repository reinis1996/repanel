// Package api exposes the panel's REST interface under /api/.
package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/repanel/repanel/internal/auth"
	"github.com/repanel/repanel/internal/config"
	"github.com/repanel/repanel/internal/database"
	"github.com/repanel/repanel/internal/models"
)

type Server struct {
	Cfg     *config.Config
	DB      *database.DB
	Auth    *auth.Manager
	Version string
}

func New(cfg *config.Config, db *database.DB, version string) *Server {
	return &Server{
		Cfg:     cfg,
		DB:      db,
		Auth:    &auth.Manager{DB: db, SessionHours: cfg.SessionHours},
		Version: version,
	}
}

// Routes registers every API endpoint on a fresh mux.
func (s *Server) Routes(mux *http.ServeMux) {
	// Public
	mux.HandleFunc("POST /api/login", s.handleLogin)
	mux.HandleFunc("GET /api/setup", s.handleSetupStatus)
	mux.HandleFunc("POST /api/setup", s.handleSetup)

	// Authenticated
	mux.Handle("POST /api/logout", s.user(s.handleLogout))
	mux.Handle("GET /api/me", s.user(s.handleMe))
	mux.Handle("POST /api/me/password", s.user(s.handleChangePassword))
	mux.Handle("GET /api/dashboard", s.user(s.handleDashboard))

	mux.Handle("GET /api/domains", s.user(s.handleDomainList))
	mux.Handle("POST /api/domains", s.user(s.handleDomainCreate))
	mux.Handle("DELETE /api/domains/{id}", s.user(s.handleDomainDelete))
	mux.Handle("POST /api/domains/{id}/suspend", s.user(s.handleDomainSuspend))
	mux.Handle("POST /api/domains/{id}/php", s.user(s.handleDomainPHP))
	mux.Handle("GET /api/php-versions", s.user(s.handlePHPVersions))

	mux.Handle("GET /api/dns", s.user(s.handleZoneList))
	mux.Handle("GET /api/dns/{id}", s.user(s.handleZoneGet))
	mux.Handle("POST /api/dns/{id}/records", s.user(s.handleRecordCreate))
	mux.Handle("PUT /api/dns/records/{rid}", s.user(s.handleRecordUpdate))
	mux.Handle("DELETE /api/dns/records/{rid}", s.user(s.handleRecordDelete))

	mux.Handle("GET /api/mail", s.user(s.handleMailList))
	mux.Handle("POST /api/mail/boxes", s.user(s.handleMailboxCreate))
	mux.Handle("POST /api/mail/boxes/{id}/password", s.user(s.handleMailboxPassword))
	mux.Handle("DELETE /api/mail/boxes/{id}", s.user(s.handleMailboxDelete))
	mux.Handle("POST /api/mail/aliases", s.user(s.handleAliasCreate))
	mux.Handle("DELETE /api/mail/aliases/{id}", s.user(s.handleAliasDelete))

	mux.Handle("GET /api/databases", s.user(s.handleDBList))
	mux.Handle("POST /api/databases", s.user(s.handleDBCreate))
	mux.Handle("DELETE /api/databases/{id}", s.user(s.handleDBDelete))
	mux.Handle("POST /api/databases/{id}/password", s.user(s.handleDBPassword))

	mux.Handle("GET /api/ftp", s.user(s.handleFTPList))
	mux.Handle("POST /api/ftp", s.user(s.handleFTPCreate))
	mux.Handle("DELETE /api/ftp/{id}", s.user(s.handleFTPDelete))
	mux.Handle("POST /api/ftp/{id}/password", s.user(s.handleFTPPassword))

	mux.Handle("GET /api/ssl", s.user(s.handleCertList))
	mux.Handle("POST /api/ssl/issue", s.user(s.handleCertIssue))
	mux.Handle("DELETE /api/ssl/{id}", s.user(s.handleCertDelete))

	mux.Handle("GET /api/files", s.user(s.handleFileList))
	mux.Handle("GET /api/files/content", s.user(s.handleFileRead))
	mux.Handle("POST /api/files/content", s.user(s.handleFileWrite))
	mux.Handle("GET /api/files/download", s.user(s.handleFileDownload))
	mux.Handle("POST /api/files/upload", s.user(s.handleFileUpload))
	mux.Handle("POST /api/files/mkdir", s.user(s.handleFileMkdir))
	mux.Handle("POST /api/files/rename", s.user(s.handleFileRename))
	mux.Handle("POST /api/files/delete", s.user(s.handleFileDelete))

	mux.Handle("GET /api/cron", s.user(s.handleCronList))
	mux.Handle("POST /api/cron", s.user(s.handleCronCreate))
	mux.Handle("PUT /api/cron/{id}", s.user(s.handleCronUpdate))
	mux.Handle("DELETE /api/cron/{id}", s.user(s.handleCronDelete))

	// Admin / reseller
	mux.Handle("GET /api/users", s.admin(s.handleUserList, true))
	mux.Handle("POST /api/users", s.admin(s.handleUserCreate, true))
	mux.Handle("PUT /api/users/{id}", s.admin(s.handleUserUpdate, true))
	mux.Handle("DELETE /api/users/{id}", s.admin(s.handleUserDelete, true))

	// Admin only
	mux.Handle("GET /api/services", s.admin(s.handleServiceList, false))
	mux.Handle("POST /api/services/{name}/{action}", s.admin(s.handleServiceAction, false))
	mux.Handle("GET /api/firewall", s.admin(s.handleFirewallList, false))
	mux.Handle("POST /api/firewall", s.admin(s.handleFirewallCreate, false))
	mux.Handle("DELETE /api/firewall/{id}", s.admin(s.handleFirewallDelete, false))
	mux.Handle("POST /api/firewall/toggle", s.admin(s.handleFirewallToggle, false))
	mux.Handle("GET /api/settings", s.admin(s.handleSettingsGet, false))
	mux.Handle("POST /api/settings", s.admin(s.handleSettingsSet, false))
}

// ---- middleware ----

type ctxKey int

const userKey ctxKey = 1

type handlerWithUser func(w http.ResponseWriter, r *http.Request, u *models.User)

func (s *Server) user(h handlerWithUser) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := s.Auth.UserForRequest(r)
		if u == nil {
			s.err(w, http.StatusUnauthorized, "not authenticated")
			return
		}
		h(w, r, u)
	})
}

// admin restricts to admins; allowReseller extends access to resellers.
func (s *Server) admin(h handlerWithUser, allowReseller bool) http.Handler {
	return s.user(func(w http.ResponseWriter, r *http.Request, u *models.User) {
		if u.Role == models.RoleAdmin || (allowReseller && u.Role == models.RoleReseller) {
			h(w, r, u)
			return
		}
		s.err(w, http.StatusForbidden, "insufficient privileges")
	})
}

// ---- response helpers ----

func (s *Server) json(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func (s *Server) err(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// fail logs an internal error and returns a sanitized message to the client.
func (s *Server) fail(w http.ResponseWriter, op string, err error) {
	log.Printf("ERROR %s: %v", op, err)
	msg := err.Error()
	if len(msg) > 300 {
		msg = msg[:300]
	}
	s.err(w, http.StatusInternalServerError, msg)
}

func decode[T any](r *http.Request) (T, error) {
	var v T
	err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)).Decode(&v)
	return v, err
}

func pathID(r *http.Request, name string) int64 {
	id, _ := strconv.ParseInt(r.PathValue(name), 10, 64)
	return id
}

// isAdminish reports whether u may act on other users' resources.
func isAdminish(u *models.User) bool {
	return u.Role == models.RoleAdmin || u.Role == models.RoleReseller
}

// scopeWhere returns a SQL fragment limiting rows to resources the user may
// see, assuming the table has a user_id column. Resellers see their own and
// their customers' resources.
func scopeWhere(u *models.User, col string) (string, []any) {
	switch u.Role {
	case models.RoleAdmin:
		return "1=1", nil
	case models.RoleReseller:
		return col + " IN (SELECT id FROM users WHERE id = ? OR owner_id = ?)", []any{u.ID, u.ID}
	default:
		return col + " = ?", []any{u.ID}
	}
}

func validDomainName(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if len(name) < 3 || len(name) > 253 || strings.Contains(name, "..") {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if label == "" || len(label) > 63 {
			return false
		}
		for i, c := range label {
			ok := c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' && i > 0 && i < len(label)-1
			if !ok {
				return false
			}
		}
	}
	return strings.Contains(name, ".")
}
