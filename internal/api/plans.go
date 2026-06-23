package api

import (
	"net/http"
	"strings"

	"github.com/reinis1996/repanel/internal/auth"
	"github.com/reinis1996/repanel/internal/models"
)

// Hosting plans (service plans / packages). A plan is a named template of
// resource limits and grantable modules. Admins manage the catalog; assigning a
// plan to an account (on create/edit) copies its limits onto that account's own
// limit columns — which the create paths already enforce — and records plan_id
// for display. Per-account overrides afterwards via the Limits modal still work.

func (s *Server) handlePlanList(w http.ResponseWriter, r *http.Request, u *models.User) {
	rows, err := s.DB.Query(`SELECT id, name, disk_quota_mb, bandwidth_quota_mb, max_domains, max_mailboxes, max_databases, modules, created_at
		FROM plans ORDER BY name`)
	if err != nil {
		s.fail(w, "list plans", err)
		return
	}
	defer rows.Close()
	out := []models.Plan{}
	for rows.Next() {
		p, err := scanPlan(rows)
		if err == nil {
			out = append(out, p)
		}
	}
	s.json(w, out)
}

type planRow interface{ Scan(...any) error }

func scanPlan(row planRow) (models.Plan, error) {
	var p models.Plan
	var modules string
	err := row.Scan(&p.ID, &p.Name, &p.DiskQuotaMB, &p.BandwidthQuotaMB, &p.MaxDomains, &p.MaxMailboxes, &p.MaxDatabases, &modules, &p.CreatedAt)
	p.Modules = auth.SplitPermissions(modules)
	return p, err
}

type planRequest struct {
	Name             string   `json:"name"`
	DiskQuotaMB      int64    `json:"disk_quota_mb"`
	BandwidthQuotaMB int64    `json:"bandwidth_quota_mb"`
	MaxDomains       int64    `json:"max_domains"`
	MaxMailboxes     int64    `json:"max_mailboxes"`
	MaxDatabases     int64    `json:"max_databases"`
	Modules          []string `json:"modules"`
}

func (s *Server) handlePlanCreate(w http.ResponseWriter, r *http.Request, u *models.User) {
	req, err := decode[planRequest](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		s.err(w, http.StatusBadRequest, "plan name is required")
		return
	}
	mods := auth.JoinPermissions(req.Modules)
	res, err := s.DB.Exec(`INSERT INTO plans(name, disk_quota_mb, bandwidth_quota_mb, max_domains, max_mailboxes, max_databases, modules)
		VALUES(?,?,?,?,?,?,?)`,
		name, nonneg(req.DiskQuotaMB), nonneg(req.BandwidthQuotaMB), nonneg(req.MaxDomains), nonneg(req.MaxMailboxes), nonneg(req.MaxDatabases), mods)
	if err != nil {
		s.err(w, http.StatusConflict, "a plan with this name already exists")
		return
	}
	id, _ := res.LastInsertId()
	s.json(w, models.Plan{ID: id, Name: name, DiskQuotaMB: nonneg(req.DiskQuotaMB), BandwidthQuotaMB: nonneg(req.BandwidthQuotaMB),
		MaxDomains: nonneg(req.MaxDomains), MaxMailboxes: nonneg(req.MaxMailboxes), MaxDatabases: nonneg(req.MaxDatabases),
		Modules: auth.SplitPermissions(mods)})
}

func (s *Server) handlePlanUpdate(w http.ResponseWriter, r *http.Request, u *models.User) {
	id := pathID(r, "id")
	req, err := decode[planRequest](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		s.err(w, http.StatusBadRequest, "plan name is required")
		return
	}
	if _, err := s.DB.Exec(`UPDATE plans SET name=?, disk_quota_mb=?, bandwidth_quota_mb=?, max_domains=?, max_mailboxes=?, max_databases=?, modules=? WHERE id=?`,
		name, nonneg(req.DiskQuotaMB), nonneg(req.BandwidthQuotaMB), nonneg(req.MaxDomains), nonneg(req.MaxMailboxes), nonneg(req.MaxDatabases), auth.JoinPermissions(req.Modules), id); err != nil {
		s.err(w, http.StatusConflict, "could not update plan (name may be taken)")
		return
	}
	s.json(w, map[string]bool{"ok": true})
}

func (s *Server) handlePlanDelete(w http.ResponseWriter, r *http.Request, u *models.User) {
	id := pathID(r, "id")
	// Detach the plan from any accounts on it (they keep their copied limits).
	s.DB.Exec(`UPDATE users SET plan_id = 0 WHERE plan_id = ?`, id)
	if _, err := s.DB.Exec(`DELETE FROM plans WHERE id = ?`, id); err != nil {
		s.fail(w, "delete plan", err)
		return
	}
	s.json(w, map[string]bool{"ok": true})
}

// loadPlan returns a plan by id, or nil if absent.
func (s *Server) loadPlan(id int64) *models.Plan {
	if id <= 0 {
		return nil
	}
	p, err := scanPlan(s.DB.QueryRow(`SELECT id, name, disk_quota_mb, bandwidth_quota_mb, max_domains, max_mailboxes, max_databases, modules, created_at
		FROM plans WHERE id = ?`, id))
	if err != nil {
		return nil
	}
	return &p
}
