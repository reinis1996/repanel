package api

import (
	"net/http"
	"strings"
	"sync"

	"github.com/reinis1996/repanel/internal/models"
	"github.com/reinis1996/repanel/internal/system"
)

// OS package updates (admin): list upgradable packages (flagging security ones),
// and apply the upgrade as a background job whose live apt/dpkg output the page
// streams by polling. Mirrors Virtualmin's "Software Package Updates".

// pkgUpdateJob captures a running (or finished) package-update run and its output.
type pkgUpdateJob struct {
	mu      sync.Mutex
	lines   []string
	running bool
	done    bool
	failed  bool
	errMsg  string
}

// maxPkgJobLines caps retained output so a pathological run can't grow unbounded.
const maxPkgJobLines = 5000

func (j *pkgUpdateJob) append(line string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if len(j.lines) >= maxPkgJobLines {
		j.lines = append(j.lines[:0], j.lines[len(j.lines)-maxPkgJobLines/2:]...)
		j.lines = append([]string{"… (earlier output trimmed) …"}, j.lines...)
	}
	j.lines = append(j.lines, line)
}

func (j *pkgUpdateJob) finish(err error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.running = false
	j.done = true
	if err != nil {
		j.failed = true
		j.errMsg = err.Error()
	}
}

func (s *Server) handlePackageList(w http.ResponseWriter, r *http.Request, _ *models.User) {
	if !system.AptAvailable() {
		s.json(w, map[string]any{"available": false, "updates": []models.PackageUpdate{}, "total": 0, "security": 0})
		return
	}
	if r.URL.Query().Get("refresh") == "1" {
		if err := system.RefreshPackageLists(); err != nil {
			s.fail(w, "refresh package lists", err)
			return
		}
	}
	ups, err := system.ListPackageUpdates()
	if err != nil {
		s.fail(w, "list package updates", err)
		return
	}
	security := 0
	for _, p := range ups {
		if p.Security {
			security++
		}
	}
	s.json(w, map[string]any{
		"available": true,
		"updates":   ups,
		"total":     len(ups),
		"security":  security,
	})
}

func (s *Server) handlePackageUpgrade(w http.ResponseWriter, r *http.Request, _ *models.User) {
	if !system.AptAvailable() {
		s.err(w, http.StatusBadRequest, "apt is not available on this host")
		return
	}
	s.pkgMu.Lock()
	if s.pkgJob != nil && s.pkgJob.isRunning() {
		s.pkgMu.Unlock()
		s.err(w, http.StatusConflict, "a package update is already running")
		return
	}
	job := &pkgUpdateJob{running: true}
	s.pkgJob = job
	s.pkgMu.Unlock()

	go func() {
		err := system.ApplyPackageUpdates(job.append)
		job.finish(err)
	}()
	s.json(w, map[string]bool{"ok": true})
}

func (j *pkgUpdateJob) isRunning() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.running
}

func (s *Server) handlePackageJob(w http.ResponseWriter, r *http.Request, _ *models.User) {
	s.pkgMu.Lock()
	job := s.pkgJob
	s.pkgMu.Unlock()
	if job == nil {
		s.json(w, map[string]any{"started": false, "running": false, "done": false})
		return
	}
	job.mu.Lock()
	defer job.mu.Unlock()
	s.json(w, map[string]any{
		"started": true,
		"running": job.running,
		"done":    job.done,
		"failed":  job.failed,
		"error":   job.errMsg,
		"output":  strings.Join(job.lines, "\n"),
	})
}
