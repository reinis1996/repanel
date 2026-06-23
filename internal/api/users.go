package api

import (
	"log"
	"net/http"
	"regexp"
	"strings"

	"github.com/reinis1996/repanel/internal/auth"
	"github.com/reinis1996/repanel/internal/models"
	"github.com/reinis1996/repanel/internal/system"
)

// applyAccountResourceLimits pushes an account's stored CPU/memory/process caps
// to its systemd slice (and login slice). Best-effort and asynchronous, since
// the systemctl calls can be slow.
func (s *Server) applyAccountResourceLimits(userID int64) {
	usr, err := auth.GetUserByID(s.DB, userID)
	if err != nil || usr == nil {
		return
	}
	lim := system.AccountLimits{
		CPUQuotaPct:  int(usr.CPUQuotaPct),
		MemoryMaxMB:  int(usr.MemoryMaxMB),
		ProcessesMax: int(usr.ProcessesMax),
	}
	sysUser := system.SysUserName(userID)
	go func() {
		if err := system.ApplyAccountLimits(userID, sysUser, lim); err != nil {
			log.Printf("apply account limits for user %d: %v", userID, err)
		}
		// The account slice now exists (or was removed), so move the account's PHP
		// pools between the per-account FPM master and the shared one accordingly.
		s.rewriteAccountPHPVhosts(userID)
	}()
}

// rewriteAccountPHPVhosts regenerates the active PHP domains of an account, which
// re-homes their FPM pools onto the per-account or shared master depending on
// whether the account currently has cgroup limits.
func (s *Server) rewriteAccountPHPVhosts(userID int64) {
	rows, err := s.DB.Query(`SELECT id FROM domains WHERE user_id = ? AND runtime = 'php' AND suspended = 0`, userID)
	if err != nil {
		return
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		if d, err := s.getDomainByID(id); err == nil {
			if err := s.rewriteVhost(*d); err != nil {
				log.Printf("re-home php pool for %s: %v", d.Name, err)
			}
		}
	}
}

var validUsername = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_.-]{2,31}$`)

func (s *Server) handleUserList(w http.ResponseWriter, r *http.Request, u *models.User) {
	query := `SELECT id, username, email, role, owner_id, suspended, disk_quota_mb, created_at, permissions,
		max_domains, max_mailboxes, max_databases, bandwidth_quota_mb, plan_id, cpu_quota_pct, memory_max_mb, processes_max FROM users`
	var args []any
	if u.Role == models.RoleReseller {
		query += ` WHERE owner_id = ? OR id = ?`
		args = []any{u.ID, u.ID}
	}
	query += ` ORDER BY username`
	rows, err := s.DB.Query(query, args...)
	if err != nil {
		s.fail(w, "list users", err)
		return
	}
	defer rows.Close()
	out := []models.User{}
	for rows.Next() {
		var usr models.User
		var susp int
		var perms string
		if rows.Scan(&usr.ID, &usr.Username, &usr.Email, &usr.Role, &usr.OwnerID, &susp, &usr.DiskQuotaMB, &usr.CreatedAt, &perms,
			&usr.MaxDomains, &usr.MaxMailboxes, &usr.MaxDatabases, &usr.BandwidthQuotaMB, &usr.PlanID,
			&usr.CPUQuotaPct, &usr.MemoryMaxMB, &usr.ProcessesMax) == nil {
			usr.Suspended = susp != 0
			if usr.Role == models.RoleAdmin {
				usr.Permissions = models.AllModules
			} else {
				usr.Permissions = auth.SplitPermissions(perms)
			}
			out = append(out, usr)
		}
	}
	s.json(w, out)
}

func (s *Server) handleUserCreate(w http.ResponseWriter, r *http.Request, u *models.User) {
	req, err := decode[struct {
		Username         string   `json:"username"`
		Email            string   `json:"email"`
		Password         string   `json:"password"`
		Role             string   `json:"role"`
		Permissions      []string `json:"permissions"`
		DiskQuotaMB      int64    `json:"disk_quota_mb"`
		MaxDomains       int64    `json:"max_domains"`
		MaxMailboxes     int64    `json:"max_mailboxes"`
		MaxDatabases     int64    `json:"max_databases"`
		BandwidthQuotaMB int64    `json:"bandwidth_quota_mb"`
		PlanID           int64    `json:"plan_id"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	username := strings.TrimSpace(req.Username)
	if !validUsername.MatchString(username) {
		s.err(w, http.StatusBadRequest, "username must be 3-32 chars, start with a letter")
		return
	}
	if len(req.Password) < 8 {
		s.err(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	role := models.Role(req.Role)
	if role == "" {
		role = models.RoleUser
	}
	// Resellers may only create plain users under their own account;
	// only admins may mint admins and resellers.
	if u.Role == models.RoleReseller && role != models.RoleUser {
		s.err(w, http.StatusForbidden, "resellers can only create user accounts")
		return
	}
	if role != models.RoleAdmin && role != models.RoleReseller && role != models.RoleUser {
		s.err(w, http.StatusBadRequest, "invalid role")
		return
	}
	ownerID := int64(0)
	if u.Role == models.RoleReseller {
		ownerID = u.ID
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		s.fail(w, "hash password", err)
		return
	}
	// Resource limits come either from an assigned hosting plan or the explicit
	// fields. A plan also supplies the module set.
	disk, bw := nonneg(req.DiskQuotaMB), nonneg(req.BandwidthQuotaMB)
	maxDom, maxMbx, maxDB := nonneg(req.MaxDomains), nonneg(req.MaxMailboxes), nonneg(req.MaxDatabases)
	planID := int64(0)
	var planMods []string
	if plan := s.loadPlan(req.PlanID); plan != nil {
		planID = plan.ID
		disk, bw = plan.DiskQuotaMB, plan.BandwidthQuotaMB
		maxDom, maxMbx, maxDB = plan.MaxDomains, plan.MaxMailboxes, plan.MaxDatabases
		planMods = plan.Modules
	}

	// Module permissions: the plan's modules, else the explicit request, else the
	// group default. A reseller can never grant a module they don't hold. Admins
	// carry no stored permissions (they implicitly have everything).
	permCSV := ""
	if role != models.RoleAdmin {
		perms := planMods
		if perms == nil {
			perms = req.Permissions
		}
		if perms == nil {
			perms = s.defaultPerms(role)
		}
		if u.Role == models.RoleReseller {
			perms = capPermissions(perms, u.Permissions)
		}
		permCSV = auth.JoinPermissions(perms)
	}
	res, err := s.DB.Exec(`INSERT INTO users(username,email,password_hash,role,owner_id,permissions,disk_quota_mb,max_domains,max_mailboxes,max_databases,bandwidth_quota_mb,plan_id)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		username, strings.TrimSpace(req.Email), hash, role, ownerID, permCSV,
		disk, maxDom, maxMbx, maxDB, bw, planID)
	if err != nil {
		s.err(w, http.StatusConflict, "username already taken")
		return
	}
	id, _ := res.LastInsertId()
	s.json(w, models.User{ID: id, Username: username, Email: req.Email, Role: role, OwnerID: ownerID,
		Permissions: auth.SplitPermissions(permCSV), DiskQuotaMB: disk, PlanID: planID,
		MaxDomains: maxDom, MaxMailboxes: maxMbx, MaxDatabases: maxDB, BandwidthQuotaMB: bw})
}

// nonneg clamps a limit value to >= 0 (0 means unlimited).
func nonneg(v int64) int64 {
	if v < 0 {
		return 0
	}
	return v
}

// defaultPerms returns the configured default module set for a new account of
// the given role.
func (s *Server) defaultPerms(role models.Role) []string {
	key := "default_perms_user"
	if role == models.RoleReseller {
		key = "default_perms_reseller"
	}
	return auth.SplitPermissions(s.DB.Setting(key))
}

// capPermissions keeps only modules that appear in allowed. allowed=nil means no
// cap (admin callers). Used so a reseller can't grant beyond their own access.
func capPermissions(perms, allowed []string) []string {
	if allowed == nil {
		return perms
	}
	set := map[string]bool{}
	for _, a := range allowed {
		set[a] = true
	}
	out := []string{}
	for _, p := range perms {
		if set[p] {
			out = append(out, p)
		}
	}
	return out
}

// userScopedForManage loads a target user the caller may administer.
func (s *Server) userScopedForManage(caller *models.User, id int64) (*models.User, bool) {
	target, err := auth.GetUserByID(s.DB, id)
	if err != nil || target == nil {
		return nil, false
	}
	if caller.Role == models.RoleAdmin {
		return target, true
	}
	if caller.Role == models.RoleReseller && target.OwnerID == caller.ID {
		return target, true
	}
	return nil, false
}

func (s *Server) handleUserUpdate(w http.ResponseWriter, r *http.Request, u *models.User) {
	target, ok := s.userScopedForManage(u, pathID(r, "id"))
	if !ok {
		s.err(w, http.StatusNotFound, "user not found")
		return
	}
	req, err := decode[struct {
		Email            *string   `json:"email"`
		Password         *string   `json:"password"`
		Suspended        *bool     `json:"suspended"`
		Role             *string   `json:"role"`
		DiskQuotaMB      *int64    `json:"disk_quota_mb"`
		MaxDomains       *int64    `json:"max_domains"`
		MaxMailboxes     *int64    `json:"max_mailboxes"`
		MaxDatabases     *int64    `json:"max_databases"`
		BandwidthQuotaMB *int64    `json:"bandwidth_quota_mb"`
		CPUQuotaPct      *int64    `json:"cpu_quota_pct"`
		MemoryMaxMB      *int64    `json:"memory_max_mb"`
		ProcessesMax     *int64    `json:"processes_max"`
		PlanID           *int64    `json:"plan_id"`
		Permissions      *[]string `json:"permissions"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// Assigning a hosting plan copies its limits + modules onto the account and
	// records plan_id; clearing it (plan_id 0) just detaches, keeping the values.
	if req.PlanID != nil {
		if *req.PlanID == 0 {
			s.DB.Exec(`UPDATE users SET plan_id = 0 WHERE id = ?`, target.ID)
		} else if plan := s.loadPlan(*req.PlanID); plan != nil {
			// Admins ignore stored permissions, so leave them empty for an admin
			// target; otherwise apply the plan's module set (capped for resellers).
			perm := ""
			if target.Role != models.RoleAdmin {
				mods := plan.Modules
				if u.Role == models.RoleReseller {
					mods = capPermissions(mods, u.Permissions)
				}
				perm = auth.JoinPermissions(mods)
			}
			if _, err := s.DB.Exec(`UPDATE users SET plan_id=?, disk_quota_mb=?, bandwidth_quota_mb=?, max_domains=?, max_mailboxes=?, max_databases=?, permissions=? WHERE id=?`,
				plan.ID, plan.DiskQuotaMB, plan.BandwidthQuotaMB, plan.MaxDomains, plan.MaxMailboxes, plan.MaxDatabases, perm, target.ID); err != nil {
				s.fail(w, "apply plan", err)
				return
			}
		} else {
			s.err(w, http.StatusBadRequest, "plan not found")
			return
		}
	}
	if req.Email != nil {
		if _, err := s.DB.Exec(`UPDATE users SET email = ? WHERE id = ?`, strings.TrimSpace(*req.Email), target.ID); err != nil {
			s.fail(w, "update email", err)
			return
		}
	}
	if req.Password != nil {
		if len(*req.Password) < 8 {
			s.err(w, http.StatusBadRequest, "password must be at least 8 characters")
			return
		}
		hash, err := auth.HashPassword(*req.Password)
		if err != nil {
			s.fail(w, "hash password", err)
			return
		}
		if _, err := s.DB.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, hash, target.ID); err != nil {
			s.fail(w, "update password", err)
			return
		}
		// Force re-login everywhere after an admin/reseller password reset
		// (SECURITY_AUDIT F-17).
		s.DB.Exec(`DELETE FROM sessions WHERE user_id = ?`, target.ID)
	}
	if req.Role != nil && u.Role == models.RoleAdmin {
		role := models.Role(*req.Role)
		if role != models.RoleAdmin && role != models.RoleReseller && role != models.RoleUser {
			s.err(w, http.StatusBadRequest, "invalid role")
			return
		}
		if target.ID == u.ID {
			s.err(w, http.StatusBadRequest, "cannot change your own role")
			return
		}
		if _, err := s.DB.Exec(`UPDATE users SET role = ? WHERE id = ?`, role, target.ID); err != nil {
			s.fail(w, "update role", err)
			return
		}
	}
	if req.DiskQuotaMB != nil {
		if *req.DiskQuotaMB < 0 {
			s.err(w, http.StatusBadRequest, "quota must be 0 (unlimited) or positive")
			return
		}
		if _, err := s.DB.Exec(`UPDATE users SET disk_quota_mb = ? WHERE id = ?`, *req.DiskQuotaMB, target.ID); err != nil {
			s.fail(w, "update quota", err)
			return
		}
	}
	// Per-account resource limits (0 = unlimited). Negative values are clamped.
	for _, lim := range []struct {
		col string
		val *int64
	}{
		{"max_domains", req.MaxDomains},
		{"max_mailboxes", req.MaxMailboxes},
		{"max_databases", req.MaxDatabases},
		{"bandwidth_quota_mb", req.BandwidthQuotaMB},
		{"cpu_quota_pct", req.CPUQuotaPct},
		{"memory_max_mb", req.MemoryMaxMB},
		{"processes_max", req.ProcessesMax},
	} {
		if lim.val == nil {
			continue
		}
		if _, err := s.DB.Exec(`UPDATE users SET `+lim.col+` = ? WHERE id = ?`, nonneg(*lim.val), target.ID); err != nil {
			s.fail(w, "update "+lim.col, err)
			return
		}
	}
	// Push the live cgroup caps to systemd when any were touched (or a plan was
	// applied, which can change them).
	if req.CPUQuotaPct != nil || req.MemoryMaxMB != nil || req.ProcessesMax != nil || req.PlanID != nil {
		s.applyAccountResourceLimits(target.ID)
	}
	// Module permissions (ignored for admins, who implicitly hold everything). A
	// reseller may not grant a module they lack.
	if req.Permissions != nil && target.Role != models.RoleAdmin {
		perms := *req.Permissions
		if u.Role == models.RoleReseller {
			perms = capPermissions(perms, u.Permissions)
		}
		if _, err := s.DB.Exec(`UPDATE users SET permissions = ? WHERE id = ?`, auth.JoinPermissions(perms), target.ID); err != nil {
			s.fail(w, "update permissions", err)
			return
		}
	}
	if req.Suspended != nil {
		if target.ID == u.ID {
			s.err(w, http.StatusBadRequest, "cannot suspend yourself")
			return
		}
		if _, err := s.DB.Exec(`UPDATE users SET suspended = ? WHERE id = ?`, boolInt(*req.Suspended), target.ID); err != nil {
			s.fail(w, "update suspension", err)
			return
		}
		// Drop active sessions when suspending.
		if *req.Suspended {
			s.DB.Exec(`DELETE FROM sessions WHERE user_id = ?`, target.ID)
		}
	}
	s.json(w, map[string]bool{"ok": true})
}

func (s *Server) handleUserDelete(w http.ResponseWriter, r *http.Request, u *models.User) {
	target, ok := s.userScopedForManage(u, pathID(r, "id"))
	if !ok {
		s.err(w, http.StatusNotFound, "user not found")
		return
	}
	if target.ID == u.ID {
		s.err(w, http.StatusBadRequest, "cannot delete your own account")
		return
	}
	var domains int
	s.DB.QueryRow(`SELECT COUNT(*) FROM domains WHERE user_id = ?`, target.ID).Scan(&domains)
	if domains > 0 {
		s.err(w, http.StatusConflict, "user still owns domains; delete or reassign them first")
		return
	}
	system.RemoveAccountLimits(target.ID, system.SysUserName(target.ID))
	if _, err := s.DB.Exec(`DELETE FROM users WHERE id = ?`, target.ID); err != nil {
		s.fail(w, "delete user", err)
		return
	}
	s.json(w, map[string]bool{"ok": true})
}
