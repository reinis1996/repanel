package api

import (
	"net/http"
	"strings"

	"github.com/repanel/repanel/internal/models"
	"github.com/repanel/repanel/internal/system"
)

func (s *Server) handleCronList(w http.ResponseWriter, r *http.Request, u *models.User) {
	where, args := scopeWhere(u, "user_id")
	rows, err := s.DB.Query(`SELECT id, user_id, schedule, command, comment, enabled FROM cron_jobs
		WHERE `+where+` ORDER BY id`, args...)
	if err != nil {
		s.fail(w, "list cron jobs", err)
		return
	}
	defer rows.Close()
	out := []models.CronJob{}
	for rows.Next() {
		var j models.CronJob
		var enabled int
		if rows.Scan(&j.ID, &j.UserID, &j.Schedule, &j.Command, &j.Comment, &enabled) == nil {
			j.Enabled = enabled != 0
			out = append(out, j)
		}
	}
	s.json(w, out)
}

// syncCrontab rebuilds /etc/cron.d/repanel from every enabled job in the db.
func (s *Server) syncCrontab() error {
	rows, err := s.DB.Query(`SELECT id, user_id, schedule, command, comment, enabled FROM cron_jobs`)
	if err != nil {
		return err
	}
	defer rows.Close()
	jobs := []models.CronJob{}
	for rows.Next() {
		var j models.CronJob
		var enabled int
		if rows.Scan(&j.ID, &j.UserID, &j.Schedule, &j.Command, &j.Comment, &enabled) == nil {
			j.Enabled = enabled != 0
			jobs = append(jobs, j)
		}
	}
	cache := map[int64]string{}
	return system.RebuildCrontab(jobs, func(userID int64) string {
		if name, ok := cache[userID]; ok {
			return name
		}
		name, err := s.sysUserForPanelUser(userID)
		if err != nil {
			name = ""
		}
		cache[userID] = name
		return name
	})
}

type cronRequest struct {
	Schedule string `json:"schedule"`
	Command  string `json:"command"`
	Comment  string `json:"comment"`
	Enabled  bool   `json:"enabled"`
}

func (req *cronRequest) validate() string {
	req.Schedule = strings.TrimSpace(req.Schedule)
	req.Command = strings.TrimSpace(req.Command)
	if err := system.ValidateCronSchedule(req.Schedule); err != nil {
		return err.Error()
	}
	if req.Command == "" {
		return "command is required"
	}
	return ""
}

func (s *Server) handleCronCreate(w http.ResponseWriter, r *http.Request, u *models.User) {
	req, err := decode[cronRequest](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if msg := req.validate(); msg != "" {
		s.err(w, http.StatusBadRequest, msg)
		return
	}
	res, err := s.DB.Exec(`INSERT INTO cron_jobs(user_id,schedule,command,comment,enabled) VALUES(?,?,?,?,1)`,
		u.ID, req.Schedule, req.Command, req.Comment)
	if err != nil {
		s.fail(w, "insert cron job", err)
		return
	}
	if err := s.syncCrontab(); err != nil {
		s.fail(w, "sync crontab", err)
		return
	}
	id, _ := res.LastInsertId()
	s.json(w, models.CronJob{ID: id, UserID: u.ID, Schedule: req.Schedule,
		Command: req.Command, Comment: req.Comment, Enabled: true})
}

func (s *Server) cronScoped(u *models.User, id int64) bool {
	where, args := scopeWhere(u, "user_id")
	args = append([]any{id}, args...)
	var found int64
	return s.DB.QueryRow(`SELECT id FROM cron_jobs WHERE id = ? AND `+where, args...).Scan(&found) == nil
}

func (s *Server) handleCronUpdate(w http.ResponseWriter, r *http.Request, u *models.User) {
	id := pathID(r, "id")
	if !s.cronScoped(u, id) {
		s.err(w, http.StatusNotFound, "cron job not found")
		return
	}
	req, err := decode[cronRequest](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if msg := req.validate(); msg != "" {
		s.err(w, http.StatusBadRequest, msg)
		return
	}
	if _, err := s.DB.Exec(`UPDATE cron_jobs SET schedule=?, command=?, comment=?, enabled=? WHERE id=?`,
		req.Schedule, req.Command, req.Comment, boolInt(req.Enabled), id); err != nil {
		s.fail(w, "update cron job", err)
		return
	}
	if err := s.syncCrontab(); err != nil {
		s.fail(w, "sync crontab", err)
		return
	}
	s.json(w, map[string]bool{"ok": true})
}

func (s *Server) handleCronDelete(w http.ResponseWriter, r *http.Request, u *models.User) {
	id := pathID(r, "id")
	if !s.cronScoped(u, id) {
		s.err(w, http.StatusNotFound, "cron job not found")
		return
	}
	if _, err := s.DB.Exec(`DELETE FROM cron_jobs WHERE id = ?`, id); err != nil {
		s.fail(w, "delete cron job", err)
		return
	}
	if err := s.syncCrontab(); err != nil {
		s.fail(w, "sync crontab", err)
		return
	}
	s.json(w, map[string]bool{"ok": true})
}
