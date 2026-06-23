package api

import (
	"net/http"

	"github.com/reinis1996/repanel/internal/models"
	"github.com/reinis1996/repanel/internal/system"
)

// WordPress Workbench API. Operates on panel-installed WordPress apps; every
// route is scoped to an app the caller owns (via its domain) and gated by the
// Websites & Domains module.

// wpApp resolves a WordPress app the caller may manage, returning its docroot,
// owning system user and URL.
func (s *Server) wpApp(u *models.User, appID int64) (docroot, sysUser, url string, ok bool) {
	where, args := scopeWhere(u, "d.user_id")
	args = append([]any{appID}, args...)
	var uid int64
	err := s.DB.QueryRow(`SELECT d.document_root, d.user_id, a.url FROM apps a
		JOIN domains d ON d.id = a.domain_id
		WHERE a.id = ? AND a.app = 'wordpress' AND `+where, args...).Scan(&docroot, &uid, &url)
	if err != nil {
		return "", "", "", false
	}
	return docroot, system.SysUserName(uid), url, true
}

// handleWPSites lists the caller's WordPress installs (the workbench overview).
func (s *Server) handleWPSites(w http.ResponseWriter, r *http.Request, u *models.User) {
	where, args := scopeWhere(u, "d.user_id")
	rows, err := s.DB.Query(`SELECT a.id, a.domain_id, d.name, a.url, a.status FROM apps a
		JOIN domains d ON d.id = a.domain_id
		WHERE a.app = 'wordpress' AND `+where+` ORDER BY d.name`, args...)
	if err != nil {
		s.fail(w, "list wordpress sites", err)
		return
	}
	defer rows.Close()
	sites := []models.WPSite{}
	for rows.Next() {
		var st models.WPSite
		if rows.Scan(&st.AppID, &st.DomainID, &st.Domain, &st.URL, &st.Status) == nil {
			sites = append(sites, st)
		}
	}
	s.json(w, map[string]any{"wp_cli": system.HaveWPCLI(), "sites": sites})
}

func (s *Server) handleWPInfo(w http.ResponseWriter, r *http.Request, u *models.User) {
	docroot, sysUser, url, ok := s.wpApp(u, pathID(r, "id"))
	if !ok {
		s.err(w, http.StatusNotFound, "WordPress site not found")
		return
	}
	info, err := system.WPGetInfo(sysUser, docroot, url)
	if err != nil {
		s.err(w, http.StatusBadGateway, err.Error())
		return
	}
	s.json(w, info)
}

func (s *Server) handleWPCoreUpdate(w http.ResponseWriter, r *http.Request, u *models.User) {
	docroot, sysUser, _, ok := s.wpApp(u, pathID(r, "id"))
	if !ok {
		s.err(w, http.StatusNotFound, "WordPress site not found")
		return
	}
	if err := system.WPCoreUpdate(sysUser, docroot); err != nil {
		s.err(w, http.StatusBadGateway, err.Error())
		return
	}
	s.json(w, map[string]bool{"ok": true})
}

func (s *Server) handleWPSettings(w http.ResponseWriter, r *http.Request, u *models.User) {
	docroot, sysUser, _, ok := s.wpApp(u, pathID(r, "id"))
	if !ok {
		s.err(w, http.StatusNotFound, "WordPress site not found")
		return
	}
	req, err := decode[struct {
		Title         string `json:"title"`
		Tagline       string `json:"tagline"`
		SearchVisible bool   `json:"search_visible"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := system.WPUpdateSettings(sysUser, docroot, req.Title, req.Tagline, req.SearchVisible); err != nil {
		s.err(w, http.StatusBadGateway, err.Error())
		return
	}
	s.json(w, map[string]bool{"ok": true})
}

func (s *Server) handleWPPlugins(w http.ResponseWriter, r *http.Request, u *models.User) {
	docroot, sysUser, _, ok := s.wpApp(u, pathID(r, "id"))
	if !ok {
		s.err(w, http.StatusNotFound, "WordPress site not found")
		return
	}
	list, err := system.WPPlugins(sysUser, docroot)
	if err != nil {
		s.err(w, http.StatusBadGateway, err.Error())
		return
	}
	s.json(w, list)
}

func (s *Server) handleWPPluginInstall(w http.ResponseWriter, r *http.Request, u *models.User) {
	docroot, sysUser, _, ok := s.wpApp(u, pathID(r, "id"))
	if !ok {
		s.err(w, http.StatusNotFound, "WordPress site not found")
		return
	}
	req, err := decode[struct {
		Slug     string `json:"slug"`
		Activate bool   `json:"activate"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := system.WPPluginInstall(sysUser, docroot, req.Slug, req.Activate); err != nil {
		s.err(w, http.StatusBadGateway, err.Error())
		return
	}
	s.json(w, map[string]bool{"ok": true})
}

func (s *Server) handleWPPluginAction(w http.ResponseWriter, r *http.Request, u *models.User) {
	docroot, sysUser, _, ok := s.wpApp(u, pathID(r, "id"))
	if !ok {
		s.err(w, http.StatusNotFound, "WordPress site not found")
		return
	}
	req, err := decode[struct {
		Slug   string `json:"slug"`
		Action string `json:"action"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := system.WPPluginAction(sysUser, docroot, req.Slug, req.Action); err != nil {
		s.err(w, http.StatusBadGateway, err.Error())
		return
	}
	s.json(w, map[string]bool{"ok": true})
}

func (s *Server) handleWPThemes(w http.ResponseWriter, r *http.Request, u *models.User) {
	docroot, sysUser, _, ok := s.wpApp(u, pathID(r, "id"))
	if !ok {
		s.err(w, http.StatusNotFound, "WordPress site not found")
		return
	}
	list, err := system.WPThemes(sysUser, docroot)
	if err != nil {
		s.err(w, http.StatusBadGateway, err.Error())
		return
	}
	s.json(w, list)
}

func (s *Server) handleWPThemeInstall(w http.ResponseWriter, r *http.Request, u *models.User) {
	docroot, sysUser, _, ok := s.wpApp(u, pathID(r, "id"))
	if !ok {
		s.err(w, http.StatusNotFound, "WordPress site not found")
		return
	}
	req, err := decode[struct {
		Slug     string `json:"slug"`
		Activate bool   `json:"activate"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := system.WPThemeInstall(sysUser, docroot, req.Slug, req.Activate); err != nil {
		s.err(w, http.StatusBadGateway, err.Error())
		return
	}
	s.json(w, map[string]bool{"ok": true})
}

func (s *Server) handleWPThemeAction(w http.ResponseWriter, r *http.Request, u *models.User) {
	docroot, sysUser, _, ok := s.wpApp(u, pathID(r, "id"))
	if !ok {
		s.err(w, http.StatusNotFound, "WordPress site not found")
		return
	}
	req, err := decode[struct {
		Slug   string `json:"slug"`
		Action string `json:"action"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := system.WPThemeAction(sysUser, docroot, req.Slug, req.Action); err != nil {
		s.err(w, http.StatusBadGateway, err.Error())
		return
	}
	s.json(w, map[string]bool{"ok": true})
}

// handleWPAutoUpdate toggles background auto-updates for a plugin or theme.
func (s *Server) handleWPAutoUpdate(w http.ResponseWriter, r *http.Request, u *models.User) {
	docroot, sysUser, _, ok := s.wpApp(u, pathID(r, "id"))
	if !ok {
		s.err(w, http.StatusNotFound, "WordPress site not found")
		return
	}
	req, err := decode[struct {
		Kind   string `json:"kind"` // plugin | theme
		Slug   string `json:"slug"`
		Enable bool   `json:"enable"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := system.WPAutoUpdate(sysUser, docroot, req.Kind, req.Slug, req.Enable); err != nil {
		s.err(w, http.StatusBadGateway, err.Error())
		return
	}
	s.json(w, map[string]bool{"ok": true})
}

// handleWPUpdateAll updates core, all plugins and all themes in one pass.
func (s *Server) handleWPUpdateAll(w http.ResponseWriter, r *http.Request, u *models.User) {
	docroot, sysUser, _, ok := s.wpApp(u, pathID(r, "id"))
	if !ok {
		s.err(w, http.StatusNotFound, "WordPress site not found")
		return
	}
	res, err := system.WPUpdateAll(sysUser, docroot)
	if err != nil {
		s.err(w, http.StatusBadGateway, err.Error())
		return
	}
	s.json(w, res)
}

func (s *Server) handleWPUsers(w http.ResponseWriter, r *http.Request, u *models.User) {
	docroot, sysUser, _, ok := s.wpApp(u, pathID(r, "id"))
	if !ok {
		s.err(w, http.StatusNotFound, "WordPress site not found")
		return
	}
	list, err := system.WPUsers(sysUser, docroot)
	if err != nil {
		s.err(w, http.StatusBadGateway, err.Error())
		return
	}
	s.json(w, list)
}

func (s *Server) handleWPUserCreate(w http.ResponseWriter, r *http.Request, u *models.User) {
	docroot, sysUser, _, ok := s.wpApp(u, pathID(r, "id"))
	if !ok {
		s.err(w, http.StatusNotFound, "WordPress site not found")
		return
	}
	req, err := decode[struct {
		Login    string `json:"login"`
		Email    string `json:"email"`
		Role     string `json:"role"`
		Password string `json:"password"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := system.WPUserCreate(sysUser, docroot, req.Login, req.Email, req.Role, req.Password); err != nil {
		s.err(w, http.StatusBadGateway, err.Error())
		return
	}
	s.json(w, map[string]bool{"ok": true})
}

// handleWPUserUpdate changes a user's role and/or password.
func (s *Server) handleWPUserUpdate(w http.ResponseWriter, r *http.Request, u *models.User) {
	docroot, sysUser, _, ok := s.wpApp(u, pathID(r, "id"))
	if !ok {
		s.err(w, http.StatusNotFound, "WordPress site not found")
		return
	}
	req, err := decode[struct {
		UserID   int64  `json:"user_id"`
		Role     string `json:"role"`
		Password string `json:"password"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Role != "" {
		if err := system.WPUserSetRole(sysUser, docroot, req.UserID, req.Role); err != nil {
			s.err(w, http.StatusBadGateway, err.Error())
			return
		}
	}
	if req.Password != "" {
		if err := system.WPUserResetPassword(sysUser, docroot, req.UserID, req.Password); err != nil {
			s.err(w, http.StatusBadGateway, err.Error())
			return
		}
	}
	s.json(w, map[string]bool{"ok": true})
}

func (s *Server) handleWPUserDelete(w http.ResponseWriter, r *http.Request, u *models.User) {
	docroot, sysUser, _, ok := s.wpApp(u, pathID(r, "id"))
	if !ok {
		s.err(w, http.StatusNotFound, "WordPress site not found")
		return
	}
	req, err := decode[struct {
		UserID     int64 `json:"user_id"`
		ReassignTo int64 `json:"reassign_to"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := system.WPUserDelete(sysUser, docroot, req.UserID, req.ReassignTo); err != nil {
		s.err(w, http.StatusBadGateway, err.Error())
		return
	}
	s.json(w, map[string]bool{"ok": true})
}

// handleWPMagicLogin issues a one-time login URL for a WordPress user.
func (s *Server) handleWPMagicLogin(w http.ResponseWriter, r *http.Request, u *models.User) {
	docroot, sysUser, url, ok := s.wpApp(u, pathID(r, "id"))
	if !ok {
		s.err(w, http.StatusNotFound, "WordPress site not found")
		return
	}
	req, err := decode[struct {
		UserID int64 `json:"user_id"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	loginURL, err := system.WPMagicLogin(sysUser, docroot, url, req.UserID)
	if err != nil {
		s.err(w, http.StatusBadGateway, err.Error())
		return
	}
	s.json(w, map[string]string{"url": loginURL})
}

// handleWPMaintenance toggles maintenance mode.
func (s *Server) handleWPMaintenance(w http.ResponseWriter, r *http.Request, u *models.User) {
	docroot, sysUser, _, ok := s.wpApp(u, pathID(r, "id"))
	if !ok {
		s.err(w, http.StatusNotFound, "WordPress site not found")
		return
	}
	req, err := decode[struct {
		Enable bool `json:"enable"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := system.WPMaintenanceMode(sysUser, docroot, req.Enable); err != nil {
		s.err(w, http.StatusBadGateway, err.Error())
		return
	}
	s.json(w, map[string]bool{"ok": true})
}

// handleWPTool runs a one-shot maintenance action selected by name.
func (s *Server) handleWPTool(w http.ResponseWriter, r *http.Request, u *models.User) {
	docroot, sysUser, _, ok := s.wpApp(u, pathID(r, "id"))
	if !ok {
		s.err(w, http.StatusNotFound, "WordPress site not found")
		return
	}
	req, err := decode[struct {
		Tool string `json:"tool"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var runErr error
	switch req.Tool {
	case "flush-cache":
		runErr = system.WPFlushCache(sysUser, docroot)
	case "delete-transients":
		runErr = system.WPDeleteTransients(sysUser, docroot)
	case "flush-rewrites":
		runErr = system.WPFlushRewrites(sysUser, docroot)
	case "verify-checksums":
		runErr = system.WPVerifyChecksums(sysUser, docroot)
	case "run-cron":
		runErr = system.WPCronRunDue(sysUser, docroot)
	case "optimize-db":
		runErr = system.WPDBOptimize(sysUser, docroot)
	case "regenerate-salts":
		runErr = system.WPRegenerateSalts(sysUser, docroot)
	default:
		s.err(w, http.StatusBadRequest, "unknown tool")
		return
	}
	if runErr != nil {
		s.err(w, http.StatusBadGateway, runErr.Error())
		return
	}
	s.json(w, map[string]bool{"ok": true})
}

func (s *Server) handleWPCron(w http.ResponseWriter, r *http.Request, u *models.User) {
	docroot, sysUser, _, ok := s.wpApp(u, pathID(r, "id"))
	if !ok {
		s.err(w, http.StatusNotFound, "WordPress site not found")
		return
	}
	events, err := system.WPCronEvents(sysUser, docroot)
	if err != nil {
		s.err(w, http.StatusBadGateway, err.Error())
		return
	}
	s.json(w, events)
}

// handleWPSearchReplace runs a database search-and-replace (dry-run or applied).
func (s *Server) handleWPSearchReplace(w http.ResponseWriter, r *http.Request, u *models.User) {
	docroot, sysUser, _, ok := s.wpApp(u, pathID(r, "id"))
	if !ok {
		s.err(w, http.StatusNotFound, "WordPress site not found")
		return
	}
	req, err := decode[struct {
		From   string `json:"from"`
		To     string `json:"to"`
		DryRun bool   `json:"dry_run"`
	}](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	report, err := system.WPSearchReplace(sysUser, docroot, req.From, req.To, req.DryRun)
	if err != nil {
		s.err(w, http.StatusBadGateway, err.Error())
		return
	}
	s.json(w, map[string]string{"report": report})
}

// handleWPDBExport streams a SQL dump of the site's database as a download.
func (s *Server) handleWPDBExport(w http.ResponseWriter, r *http.Request, u *models.User) {
	docroot, sysUser, _, ok := s.wpApp(u, pathID(r, "id"))
	if !ok {
		s.err(w, http.StatusNotFound, "WordPress site not found")
		return
	}
	w.Header().Set("Content-Type", "application/sql")
	w.Header().Set("Content-Disposition", `attachment; filename="wordpress.sql"`)
	if err := system.WPDBExport(sysUser, docroot, w); err != nil {
		// Headers may already be sent; best effort to signal failure.
		s.err(w, http.StatusBadGateway, err.Error())
		return
	}
}

func (s *Server) handleWPConfig(w http.ResponseWriter, r *http.Request, u *models.User) {
	docroot, sysUser, _, ok := s.wpApp(u, pathID(r, "id"))
	if !ok {
		s.err(w, http.StatusNotFound, "WordPress site not found")
		return
	}
	cfg, err := system.WPGetConfig(sysUser, docroot)
	if err != nil {
		s.err(w, http.StatusBadGateway, err.Error())
		return
	}
	s.json(w, cfg)
}

func (s *Server) handleWPConfigUpdate(w http.ResponseWriter, r *http.Request, u *models.User) {
	docroot, sysUser, _, ok := s.wpApp(u, pathID(r, "id"))
	if !ok {
		s.err(w, http.StatusNotFound, "WordPress site not found")
		return
	}
	req, err := decode[models.WPConfig](r)
	if err != nil {
		s.err(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := system.WPSetConfig(sysUser, docroot, req); err != nil {
		s.err(w, http.StatusBadGateway, err.Error())
		return
	}
	s.json(w, map[string]bool{"ok": true})
}

// handleWPInstallCLI installs WP-CLI on the host (admin-only utility).
func (s *Server) handleWPInstallCLI(w http.ResponseWriter, r *http.Request, u *models.User) {
	if u.Role != models.RoleAdmin {
		s.err(w, http.StatusForbidden, "only an administrator can install WP-CLI")
		return
	}
	if err := system.InstallWPCLI(); err != nil {
		s.err(w, http.StatusBadGateway, err.Error())
		return
	}
	s.json(w, map[string]bool{"ok": true})
}
