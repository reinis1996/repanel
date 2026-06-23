package api

import (
	"fmt"

	"github.com/reinis1996/repanel/internal/auth"
	"github.com/reinis1996/repanel/internal/models"
)

// Per-account resource limits (counts). These complement the disk quota: an
// operator caps how many domains, mailboxes and databases a customer may create.
// A limit of 0 means unlimited; admins are never capped. The checks run in the
// respective create paths, mirroring quotaExceeded.

// mailboxLimitReached returns a message if the account that owns domainOwnerID
// is at its mailbox cap, else "".
func (s *Server) mailboxLimitReached(domainOwnerID int64) string {
	owner, err := auth.GetUserByID(s.DB, domainOwnerID)
	if err != nil || owner == nil || owner.Role == models.RoleAdmin || owner.MaxMailboxes <= 0 {
		return ""
	}
	var n int64
	s.DB.QueryRow(`SELECT COUNT(*) FROM mailboxes m JOIN domains d ON d.id = m.domain_id
		WHERE d.user_id = ?`, domainOwnerID).Scan(&n)
	if n >= owner.MaxMailboxes {
		return fmt.Sprintf("mailbox limit reached (%d) — ask your provider to raise it", owner.MaxMailboxes)
	}
	return ""
}

// databaseLimitReached returns a message if the account is at its database cap.
func (s *Server) databaseLimitReached(ownerID int64) string {
	owner, err := auth.GetUserByID(s.DB, ownerID)
	if err != nil || owner == nil || owner.Role == models.RoleAdmin || owner.MaxDatabases <= 0 {
		return ""
	}
	var n int64
	s.DB.QueryRow(`SELECT COUNT(*) FROM db_entries WHERE user_id = ?`, ownerID).Scan(&n)
	if n >= owner.MaxDatabases {
		return fmt.Sprintf("database limit reached (%d) — ask your provider to raise it", owner.MaxDatabases)
	}
	return ""
}
