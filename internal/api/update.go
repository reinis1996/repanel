package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/reinis1996/repanel/internal/models"
	"github.com/reinis1996/repanel/internal/system"
)

// Admin self-update: report the current vs latest GitHub release, configure the
// repo/token (for the private repo), and apply the update (replace binaries +
// restart). The token is stored in settings and never returned to the client.

const updateCacheTTL = 30 * time.Minute

func (s *Server) updateRepo() string {
	if r := strings.TrimSpace(s.DB.Setting("update_repo")); r != "" {
		return r
	}
	return system.DefaultUpdateRepo
}

// latestVersion returns the cached latest release tag, refreshing from GitHub
// when the cache is stale or refresh is true. On failure it returns "" and the
// reason (so the UI can explain a blank "Latest" instead of a silent dash); the
// page still degrades gracefully. A successful empty result is never cached.
func (s *Server) latestVersion(refresh bool) (tag, reason string) {
	s.updateMu.Lock()
	if !refresh && s.updateLatest != "" && time.Since(s.updateAt) < updateCacheTTL {
		v := s.updateLatest
		s.updateMu.Unlock()
		return v, ""
	}
	s.updateMu.Unlock()

	rel, err := system.LatestRelease(s.updateRepo(), s.DB.Setting("github_token"))
	if err != nil {
		return "", updateCheckHint(err)
	}
	s.updateMu.Lock()
	s.updateLatest = rel.TagName
	s.updateAt = time.Now()
	s.updateMu.Unlock()
	return rel.TagName, ""
}

// updateCheckHint turns a raw GitHub error into an actionable message. The most
// common "suddenly stopped working" case is a 404 from /releases/latest, which
// GitHub returns when the repo has no published *stable* release (it ignores
// drafts and pre-releases) — or when the token can't see the repo.
func updateCheckHint(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "404"):
		return msg + " — the repo has no published stable release (drafts and pre-releases are ignored), or the token can't access it"
	case strings.Contains(msg, "401"):
		return msg + " — the GitHub token is invalid or expired"
	case strings.Contains(msg, "403"):
		return msg + " — GitHub rate-limited or denied the request (check the token's Contents: read access)"
	default:
		return msg
	}
}

func (s *Server) handleUpdateStatus(w http.ResponseWriter, r *http.Request, _ *models.User) {
	latest, reason := s.latestVersion(r.URL.Query().Get("refresh") == "1")
	s.json(w, map[string]any{
		"current":   s.Version,
		"latest":    latest,
		"available": system.UpdateAvailable(s.Version, latest),
		"has_token": s.DB.Setting("github_token") != "",
		"repo":      s.updateRepo(),
		"error":     reason,
	})
}

func (s *Server) handleUpdateConfig(w http.ResponseWriter, r *http.Request, _ *models.User) {
	req, err := decode[struct {
		Repo  string `json:"repo"`
		Token string `json:"token"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if repo := strings.TrimSpace(req.Repo); repo != "" {
		s.DB.SetSetting("update_repo", repo)
	}
	// Only overwrite the token when a new one is supplied, so saving the repo
	// alone doesn't wipe it.
	if strings.TrimSpace(req.Token) != "" {
		s.DB.SetSetting("github_token", strings.TrimSpace(req.Token))
	}
	// Invalidate the cache so the next status reflects the new repo/token.
	s.updateMu.Lock()
	s.updateLatest, s.updateAt = "", time.Time{}
	s.updateMu.Unlock()
	s.json(w, map[string]bool{"ok": true})
}

func (s *Server) handleUpdateApply(w http.ResponseWriter, r *http.Request, _ *models.User) {
	if err := system.ApplyUpdate(s.updateRepo(), s.DB.Setting("github_token"), s.Version); err != nil {
		s.err(w, http.StatusBadGateway, err.Error())
		return
	}
	s.json(w, map[string]any{"ok": true, "restarting": true})
	// Restart after the response has been flushed so the client sees success.
	go func() {
		time.Sleep(time.Second)
		system.RestartPanel()
	}()
}
