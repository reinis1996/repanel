// Package api exposes the panel's REST interface under /api/.
package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/reinis1996/repanel/internal/auth"
	"github.com/reinis1996/repanel/internal/config"
	"github.com/reinis1996/repanel/internal/database"
	"github.com/reinis1996/repanel/internal/models"
	"github.com/reinis1996/repanel/internal/system"
)

type Server struct {
	Cfg        *config.Config
	DB         *database.DB
	Auth       *auth.Manager
	Version    string
	ConfigPath string // path to repanel.conf, for building certbot DNS-01 hooks

	// phpInstalls tracks in-flight / failed PHP version installs by version,
	// so the admin PHP manager can poll progress (apt runs in the background).
	phpMu       sync.Mutex
	phpInstalls map[string]*phpInstall

	// nodeInstalls tracks in-flight / failed Node version installs (downloads run
	// in the background), for the admin Node.js manager.
	nodeMu       sync.Mutex
	nodeInstalls map[string]*nodeInstall

	// login throttles failed logins per client IP (SECURITY_AUDIT F-08).
	login *loginLimiter

	// setupMu serializes first-run admin creation (SECURITY_AUDIT F-16).
	setupMu sync.Mutex

	// update caches the latest GitHub release tag so the dashboard/update page
	// don't hit the GitHub API on every load.
	updateMu     sync.Mutex
	updateLatest string
	updateAt     time.Time

	// antispam tracks the background rspamd+ClamAV install for the Mail page.
	antispamMu         sync.Mutex
	antispamInstalling bool
	antispamErr        string

	// waf tracks the background ModSecurity + OWASP CRS install.
	wafMu         sync.Mutex
	wafInstalling bool
	wafErr        string

	// pkgJob holds the in-progress / last OS package-update run, whose live output
	// the Package Updates page polls. Only one runs at a time.
	pkgMu  sync.Mutex
	pkgJob *pkgUpdateJob
}

func New(cfg *config.Config, db *database.DB, version string) *Server {
	return &Server{
		Cfg:          cfg,
		DB:           db,
		Auth:         &auth.Manager{DB: db, SessionHours: cfg.SessionHours},
		Version:      version,
		phpInstalls:  map[string]*phpInstall{},
		nodeInstalls: map[string]*nodeInstall{},
		login:        newLoginLimiter(10, 15*time.Minute),
	}
}

// webServer builds the web server orchestrator for the configured stack.
func (s *Server) webServer() *system.WebServer {
	return system.NewWebServer(s.Cfg.WebServer, s.Cfg.NginxDir, s.Cfg.ApacheDir, s.Cfg.ApachePort)
}

// Routes registers every API endpoint on a fresh mux.
func (s *Server) Routes(mux *http.ServeMux) {
	// Public
	mux.HandleFunc("POST /api/login", s.handleLogin)
	mux.HandleFunc("GET /api/setup", s.handleSetupStatus)
	mux.HandleFunc("POST /api/setup", s.handleSetup)
	// White-label branding is public — the login screen needs it before auth.
	mux.HandleFunc("GET /api/branding", s.handleBranding)

	// Authenticated
	mux.Handle("POST /api/logout", s.user(s.handleLogout))
	mux.Handle("GET /api/me", s.user(s.handleMe))
	mux.Handle("POST /api/me/password", s.user(s.handleChangePassword))
	// Two-factor auth (self-service for any authenticated account).
	mux.Handle("POST /api/me/2fa/setup", s.user(s.handle2FASetup))
	mux.Handle("POST /api/me/2fa/enable", s.user(s.handle2FAEnable))
	mux.Handle("POST /api/me/2fa/disable", s.user(s.handle2FADisable))
	mux.Handle("POST /api/impersonate/stop", s.user(s.handleStopImpersonate))
	mux.Handle("GET /api/dashboard", s.user(s.handleDashboard))

	// Websites & Domains module (also covers one-click apps and web stack info).
	mux.Handle("GET /api/domains", s.perm(models.ModuleDomains, s.handleDomainList))
	mux.Handle("POST /api/domains", s.perm(models.ModuleDomains, s.handleDomainCreate))
	mux.Handle("DELETE /api/domains/{id}", s.perm(models.ModuleDomains, s.handleDomainDelete))
	mux.Handle("POST /api/domains/{id}/suspend", s.perm(models.ModuleDomains, s.handleDomainSuspend))
	mux.Handle("POST /api/domains/{id}/php", s.perm(models.ModuleDomains, s.handleDomainPHP))
	mux.Handle("POST /api/domains/{id}/runtime", s.perm(models.ModuleDomains, s.handleDomainRuntime))
	mux.Handle("POST /api/domains/{id}/webserver", s.perm(models.ModuleDomains, s.handleDomainWebMode))
	// Domain forwarding/redirect and owner-facing structured PHP settings.
	mux.Handle("POST /api/domains/{id}/redirect", s.perm(models.ModuleDomains, s.handleDomainRedirect))
	mux.Handle("GET /api/domains/{id}/php-settings", s.perm(models.ModuleDomains, s.handleDomainPHPSettingsGet))
	mux.Handle("POST /api/domains/{id}/php-settings", s.perm(models.ModuleDomains, s.handleDomainPHPSettingsSet))
	// Password-protected directories (.htpasswd).
	mux.Handle("GET /api/domains/{id}/protected", s.perm(models.ModuleDomains, s.handleProtectedList))
	mux.Handle("POST /api/domains/{id}/protected", s.perm(models.ModuleDomains, s.handleProtectedCreate))
	mux.Handle("DELETE /api/domains/{id}/protected/{dirId}", s.perm(models.ModuleDomains, s.handleProtectedDelete))
	mux.Handle("POST /api/domains/{id}/protected/{dirId}/users", s.perm(models.ModuleDomains, s.handleProtectedUserSet))
	mux.Handle("DELETE /api/domains/{id}/protected/{dirId}/users/{username}", s.perm(models.ModuleDomains, s.handleProtectedUserDelete))
	// Per-site config editor (admin-only): edit the nginx/Apache/PHP override
	// blocks merged into the generated config on every rebuild.
	mux.Handle("GET /api/domains/{id}/config", s.admin(s.handleDomainConfigGet, false))
	mux.Handle("POST /api/domains/{id}/config", s.admin(s.handleDomainConfigSet, false))
	// Per-domain ModSecurity WAF: owners toggle it; custom rules and install are
	// gated inside the handlers.
	mux.Handle("GET /api/domains/{id}/waf", s.perm(models.ModuleDomains, s.handleWAFGet))
	mux.Handle("POST /api/domains/{id}/waf", s.perm(models.ModuleDomains, s.handleWAFSet))
	mux.Handle("POST /api/waf/install", s.admin(s.handleWAFInstall, false))
	mux.Handle("GET /api/webserver", s.perm(models.ModuleDomains, s.handleWebServerInfo))
	mux.Handle("GET /api/php-versions", s.perm(models.ModuleDomains, s.handlePHPVersions))
	mux.Handle("GET /api/node-versions", s.perm(models.ModuleDomains, s.handleNodeVersions))
	mux.Handle("GET /api/domains/{id}/node", s.perm(models.ModuleDomains, s.handleNodeGet))
	mux.Handle("POST /api/domains/{id}/node", s.perm(models.ModuleDomains, s.handleNodeUpdate))
	mux.Handle("POST /api/domains/{id}/node/restart", s.perm(models.ModuleDomains, s.handleNodeRestart))
	mux.Handle("POST /api/domains/{id}/node/enabled", s.perm(models.ModuleDomains, s.handleNodeEnabled))
	mux.Handle("POST /api/domains/{id}/node/npm", s.perm(models.ModuleDomains, s.handleNodeNpm))
	mux.Handle("GET /api/domains/{id}/node/logs", s.perm(models.ModuleDomains, s.handleNodeLogs))
	mux.Handle("GET /api/php", s.admin(s.handlePHPList, false))
	mux.Handle("POST /api/php/install", s.admin(s.handlePHPInstall, false))
	mux.Handle("GET /api/node", s.admin(s.handleNodeList, false))
	mux.Handle("POST /api/node/install", s.admin(s.handleNodeInstall, false))
	mux.Handle("GET /api/apps", s.perm(models.ModuleDomains, s.handleAppList))
	mux.Handle("GET /api/apps/catalog", s.perm(models.ModuleDomains, s.handleAppCatalog))
	mux.Handle("POST /api/domains/{id}/apps", s.perm(models.ModuleDomains, s.handleAppInstall))
	mux.Handle("DELETE /api/apps/{id}", s.perm(models.ModuleDomains, s.handleAppDelete))

	// WordPress Workbench (part of the Websites & Domains module).
	mux.Handle("GET /api/wordpress", s.perm(models.ModuleDomains, s.handleWPSites))
	mux.Handle("GET /api/wordpress/{id}", s.perm(models.ModuleDomains, s.handleWPInfo))
	mux.Handle("POST /api/wordpress/{id}/core/update", s.perm(models.ModuleDomains, s.handleWPCoreUpdate))
	mux.Handle("POST /api/wordpress/{id}/settings", s.perm(models.ModuleDomains, s.handleWPSettings))
	mux.Handle("GET /api/wordpress/{id}/plugins", s.perm(models.ModuleDomains, s.handleWPPlugins))
	mux.Handle("POST /api/wordpress/{id}/plugins", s.perm(models.ModuleDomains, s.handleWPPluginInstall))
	mux.Handle("POST /api/wordpress/{id}/plugins/action", s.perm(models.ModuleDomains, s.handleWPPluginAction))
	mux.Handle("GET /api/wordpress/{id}/themes", s.perm(models.ModuleDomains, s.handleWPThemes))
	mux.Handle("POST /api/wordpress/{id}/themes", s.perm(models.ModuleDomains, s.handleWPThemeInstall))
	mux.Handle("POST /api/wordpress/{id}/themes/action", s.perm(models.ModuleDomains, s.handleWPThemeAction))
	mux.Handle("POST /api/wordpress/{id}/auto-update", s.perm(models.ModuleDomains, s.handleWPAutoUpdate))
	mux.Handle("POST /api/wordpress/{id}/update-all", s.perm(models.ModuleDomains, s.handleWPUpdateAll))
	mux.Handle("GET /api/wordpress/{id}/users", s.perm(models.ModuleDomains, s.handleWPUsers))
	mux.Handle("POST /api/wordpress/{id}/users", s.perm(models.ModuleDomains, s.handleWPUserCreate))
	mux.Handle("POST /api/wordpress/{id}/users/update", s.perm(models.ModuleDomains, s.handleWPUserUpdate))
	mux.Handle("POST /api/wordpress/{id}/users/delete", s.perm(models.ModuleDomains, s.handleWPUserDelete))
	mux.Handle("POST /api/wordpress/{id}/users/login", s.perm(models.ModuleDomains, s.handleWPMagicLogin))
	mux.Handle("POST /api/wordpress/{id}/maintenance", s.perm(models.ModuleDomains, s.handleWPMaintenance))
	mux.Handle("POST /api/wordpress/{id}/tool", s.perm(models.ModuleDomains, s.handleWPTool))
	mux.Handle("GET /api/wordpress/{id}/cron", s.perm(models.ModuleDomains, s.handleWPCron))
	mux.Handle("POST /api/wordpress/{id}/search-replace", s.perm(models.ModuleDomains, s.handleWPSearchReplace))
	mux.Handle("GET /api/wordpress/{id}/db/export", s.perm(models.ModuleDomains, s.handleWPDBExport))
	mux.Handle("GET /api/wordpress/{id}/config", s.perm(models.ModuleDomains, s.handleWPConfig))
	mux.Handle("POST /api/wordpress/{id}/config", s.perm(models.ModuleDomains, s.handleWPConfigUpdate))
	mux.Handle("POST /api/wordpress/install-cli", s.perm(models.ModuleDomains, s.handleWPInstallCLI))

	mux.Handle("GET /api/dns", s.perm(models.ModuleDNS, s.handleZoneList))
	mux.Handle("GET /api/dns/{id}", s.perm(models.ModuleDNS, s.handleZoneGet))
	mux.Handle("POST /api/dns/{id}/records", s.perm(models.ModuleDNS, s.handleRecordCreate))
	mux.Handle("PUT /api/dns/records/{rid}", s.perm(models.ModuleDNS, s.handleRecordUpdate))
	mux.Handle("DELETE /api/dns/records/{rid}", s.perm(models.ModuleDNS, s.handleRecordDelete))
	// Cloudflare DNS sync (per zone). Own path so {id} doesn't collide with the
	// /api/dns/records/{rid} routes.
	mux.Handle("POST /api/cloudflare/{id}", s.perm(models.ModuleDNS, s.handleCloudflareSet))
	mux.Handle("DELETE /api/cloudflare/{id}", s.perm(models.ModuleDNS, s.handleCloudflareUnbind))
	mux.Handle("POST /api/cloudflare/{id}/import", s.perm(models.ModuleDNS, s.handleCloudflareImport))
	mux.Handle("POST /api/cloudflare/{id}/export", s.perm(models.ModuleDNS, s.handleCloudflareExport))
	// DNSSEC signing per zone (own path to avoid colliding with /api/dns/records).
	mux.Handle("GET /api/dnssec/{id}", s.perm(models.ModuleDNS, s.handleDNSSECStatus))
	mux.Handle("POST /api/dnssec/{id}", s.perm(models.ModuleDNS, s.handleDNSSECEnable))
	mux.Handle("DELETE /api/dnssec/{id}", s.perm(models.ModuleDNS, s.handleDNSSECDisable))

	// Mail module (mailboxes, aliases, DKIM and webmail).
	mux.Handle("GET /api/mail", s.perm(models.ModuleMail, s.handleMailList))
	mux.Handle("POST /api/mail/boxes", s.perm(models.ModuleMail, s.handleMailboxCreate))
	mux.Handle("POST /api/mail/boxes/{id}/password", s.perm(models.ModuleMail, s.handleMailboxPassword))
	mux.Handle("DELETE /api/mail/boxes/{id}", s.perm(models.ModuleMail, s.handleMailboxDelete))
	mux.Handle("POST /api/mail/aliases", s.perm(models.ModuleMail, s.handleAliasCreate))
	mux.Handle("DELETE /api/mail/aliases/{id}", s.perm(models.ModuleMail, s.handleAliasDelete))

	// Distribution lists.
	mux.Handle("POST /api/mail/lists", s.perm(models.ModuleMail, s.handleListCreate))
	mux.Handle("PUT /api/mail/lists/{id}", s.perm(models.ModuleMail, s.handleListUpdate))
	mux.Handle("DELETE /api/mail/lists/{id}", s.perm(models.ModuleMail, s.handleListDelete))

	// Per-mailbox autoresponder and Sieve filters.
	mux.Handle("GET /api/mail/boxes/{id}/autoresponder", s.perm(models.ModuleMail, s.handleAutoresponderGet))
	mux.Handle("POST /api/mail/boxes/{id}/autoresponder", s.perm(models.ModuleMail, s.handleAutoresponderSet))
	mux.Handle("GET /api/mail/boxes/{id}/filters", s.perm(models.ModuleMail, s.handleFiltersGet))
	mux.Handle("POST /api/mail/boxes/{id}/filters", s.perm(models.ModuleMail, s.handleFilterCreate))
	mux.Handle("DELETE /api/mail/boxes/{id}/filters/{filterId}", s.perm(models.ModuleMail, s.handleFilterDelete))

	// IMAP migration (imapsync).
	mux.Handle("GET /api/mail/migrations", s.perm(models.ModuleMail, s.handleMigrationList))
	mux.Handle("POST /api/mail/boxes/{id}/migrate", s.perm(models.ModuleMail, s.handleMigrationCreate))

	// Server-wide outbound relay + feature installs (admin only).
	mux.Handle("GET /api/mail/smarthost", s.admin(s.handleSmarthostGet, false))
	mux.Handle("POST /api/mail/smarthost", s.admin(s.handleSmarthostSet, false))
	mux.Handle("POST /api/mail/features/install", s.admin(s.handleMailFeaturesInstall, false))
	mux.Handle("POST /api/mail/imapsync/install", s.admin(s.handleMigrationInstall, false))

	mux.Handle("GET /api/dkim", s.perm(models.ModuleMail, s.handleDKIMList))
	mux.Handle("POST /api/dkim/{id}", s.perm(models.ModuleMail, s.handleDKIMEnable))
	mux.Handle("DELETE /api/dkim/{id}", s.perm(models.ModuleMail, s.handleDKIMDisable))

	mux.Handle("GET /api/webmail", s.perm(models.ModuleMail, s.handleWebmailList))
	mux.Handle("POST /api/webmail/{id}", s.perm(models.ModuleMail, s.handleWebmailEnable))
	mux.Handle("DELETE /api/webmail/{id}", s.perm(models.ModuleMail, s.handleWebmailDisable))

	// Spam & virus filtering (rspamd + ClamAV), per mail domain.
	mux.Handle("GET /api/mail/spam", s.perm(models.ModuleMail, s.handleSpamStatus))
	mux.Handle("POST /api/domains/{id}/spam", s.perm(models.ModuleMail, s.handleSpamToggle))
	mux.Handle("POST /api/mail/antispam/install", s.admin(s.handleAntiSpamInstall, false))

	mux.Handle("GET /api/databases", s.perm(models.ModuleDatabases, s.handleDBList))
	mux.Handle("GET /api/database-engines", s.perm(models.ModuleDatabases, s.handleDBEngines))
	mux.Handle("POST /api/databases", s.perm(models.ModuleDatabases, s.handleDBCreate))
	mux.Handle("DELETE /api/databases/{id}", s.perm(models.ModuleDatabases, s.handleDBDelete))
	mux.Handle("POST /api/databases/{id}/password", s.perm(models.ModuleDatabases, s.handleDBPassword))
	// Web database admin (Adminer): status is readable by the databases module;
	// install/enable are server-wide admin actions.
	mux.Handle("GET /api/dbadmin", s.perm(models.ModuleDatabases, s.handleDBAdminStatus))
	mux.Handle("POST /api/dbadmin/install", s.admin(s.handleDBAdminInstall, false))
	mux.Handle("POST /api/dbadmin/enable", s.admin(s.handleDBAdminEnable, false))

	mux.Handle("GET /api/ftp", s.perm(models.ModuleFTP, s.handleFTPList))
	mux.Handle("POST /api/ftp", s.perm(models.ModuleFTP, s.handleFTPCreate))
	mux.Handle("DELETE /api/ftp/{id}", s.perm(models.ModuleFTP, s.handleFTPDelete))
	mux.Handle("POST /api/ftp/{id}/password", s.perm(models.ModuleFTP, s.handleFTPPassword))

	mux.Handle("GET /api/ssl", s.perm(models.ModuleSSL, s.handleCertList))
	mux.Handle("POST /api/ssl/issue", s.perm(models.ModuleSSL, s.handleCertIssue))
	mux.Handle("POST /api/ssl/upload", s.perm(models.ModuleSSL, s.handleCertUpload))
	mux.Handle("DELETE /api/ssl/{id}", s.perm(models.ModuleSSL, s.handleCertDelete))
	// Assigning a certificate to mail/FTP/panel is a server-wide action (admin).
	mux.Handle("GET /api/ssl/assignments", s.admin(s.handleCertAssignments, false))
	mux.Handle("POST /api/ssl/assign", s.admin(s.handleCertAssign, false))
	// Securing the panel's own hostname (port from LISTEN) directly (admin).
	mux.Handle("GET /api/ssl/panel", s.admin(s.handlePanelCertStatus, false))
	mux.Handle("POST /api/ssl/panel", s.admin(s.handlePanelCert, false))

	mux.Handle("GET /api/files", s.perm(models.ModuleFiles, s.handleFileList))
	mux.Handle("GET /api/files/content", s.perm(models.ModuleFiles, s.handleFileRead))
	mux.Handle("POST /api/files/content", s.perm(models.ModuleFiles, s.handleFileWrite))
	mux.Handle("GET /api/files/download", s.perm(models.ModuleFiles, s.handleFileDownload))
	mux.Handle("POST /api/files/upload", s.perm(models.ModuleFiles, s.handleFileUpload))
	mux.Handle("POST /api/files/mkdir", s.perm(models.ModuleFiles, s.handleFileMkdir))
	mux.Handle("POST /api/files/rename", s.perm(models.ModuleFiles, s.handleFileRename))
	mux.Handle("POST /api/files/delete", s.perm(models.ModuleFiles, s.handleFileDelete))

	mux.Handle("GET /api/backups", s.perm(models.ModuleBackups, s.handleBackupList))
	mux.Handle("POST /api/backups", s.perm(models.ModuleBackups, s.handleBackupCreate))
	mux.Handle("GET /api/backups/{id}/download", s.perm(models.ModuleBackups, s.handleBackupDownload))
	mux.Handle("GET /api/backups/{id}/contents", s.perm(models.ModuleBackups, s.handleBackupContents))
	mux.Handle("POST /api/backups/{id}/restore", s.perm(models.ModuleBackups, s.handleBackupRestore))
	mux.Handle("DELETE /api/backups/{id}", s.perm(models.ModuleBackups, s.handleBackupDelete))
	// Offsite destinations + server/migration backup are server-wide (admin only).
	mux.Handle("GET /api/backups/destinations", s.admin(s.handleDestList, false))
	mux.Handle("POST /api/backups/destinations", s.admin(s.handleDestCreate, false))
	mux.Handle("PUT /api/backups/destinations/{id}", s.admin(s.handleDestUpdate, false))
	mux.Handle("DELETE /api/backups/destinations/{id}", s.admin(s.handleDestDelete, false))
	mux.Handle("POST /api/backups/destinations/{id}/test", s.admin(s.handleDestTest, false))
	mux.Handle("POST /api/backups/rclone/install", s.admin(s.handleRcloneInstall, false))
	mux.Handle("GET /api/backups/server", s.admin(s.handleServerBackup, false))
	mux.Handle("GET /api/usage", s.user(s.handleUsage))
	mux.Handle("GET /api/traffic", s.perm(models.ModuleTraffic, s.handleTraffic))
	mux.Handle("GET /api/webstats/{id}", s.perm(models.ModuleDomains, s.handleWebStats))

	mux.Handle("GET /api/tokens", s.user(s.handleTokenList))
	mux.Handle("POST /api/tokens", s.user(s.handleTokenCreate))
	mux.Handle("DELETE /api/tokens/{id}", s.user(s.handleTokenDelete))

	mux.Handle("GET /api/cron", s.perm(models.ModuleCron, s.handleCronList))
	mux.Handle("POST /api/cron", s.perm(models.ModuleCron, s.handleCronCreate))
	mux.Handle("PUT /api/cron/{id}", s.perm(models.ModuleCron, s.handleCronUpdate))
	mux.Handle("DELETE /api/cron/{id}", s.perm(models.ModuleCron, s.handleCronDelete))

	mux.Handle("GET /api/functions", s.perm(models.ModuleFunctions, s.handleFunctionList))
	mux.Handle("GET /api/functions/meta", s.perm(models.ModuleFunctions, s.handleFunctionMeta))
	mux.Handle("GET /api/functions/{id}", s.perm(models.ModuleFunctions, s.handleFunctionGet))
	mux.Handle("POST /api/functions", s.perm(models.ModuleFunctions, s.handleFunctionCreate))
	mux.Handle("PUT /api/functions/{id}", s.perm(models.ModuleFunctions, s.handleFunctionUpdate))
	mux.Handle("POST /api/functions/{id}/ssl", s.perm(models.ModuleFunctions, s.handleFunctionSSL))
	mux.Handle("POST /api/functions/{id}/invoke", s.perm(models.ModuleFunctions, s.handleFunctionInvoke))
	mux.Handle("DELETE /api/functions/{id}", s.perm(models.ModuleFunctions, s.handleFunctionDelete))

	// Admin / reseller
	// Hosting plans: catalog is readable by admins/resellers (to assign); only
	// admins manage the catalog.
	mux.Handle("GET /api/plans", s.admin(s.handlePlanList, true))
	mux.Handle("POST /api/plans", s.admin(s.handlePlanCreate, false))
	mux.Handle("PUT /api/plans/{id}", s.admin(s.handlePlanUpdate, false))
	mux.Handle("DELETE /api/plans/{id}", s.admin(s.handlePlanDelete, false))

	mux.Handle("GET /api/users", s.admin(s.handleUserList, true))
	mux.Handle("POST /api/users", s.admin(s.handleUserCreate, true))
	mux.Handle("PUT /api/users/{id}", s.admin(s.handleUserUpdate, true))
	mux.Handle("DELETE /api/users/{id}", s.admin(s.handleUserDelete, true))
	mux.Handle("POST /api/users/{id}/impersonate", s.admin(s.handleImpersonate, true))
	mux.Handle("GET /api/users/{id}/ssh", s.admin(s.handleSSHGet, true))
	mux.Handle("POST /api/users/{id}/ssh", s.admin(s.handleSSHSet, true))

	// Security: audit trail and fail2ban (admin only).
	mux.Handle("GET /api/audit", s.admin(s.handleAuditList, false))
	mux.Handle("GET /api/fail2ban", s.admin(s.handleFail2banStatus, false))
	mux.Handle("POST /api/fail2ban/ban", s.admin(s.handleFail2banBan, false))
	mux.Handle("POST /api/fail2ban/whitelist", s.admin(s.handleFail2banWhitelist, false))
	mux.Handle("POST /api/fail2ban/config", s.admin(s.handleFail2banConfig, false))
	mux.Handle("GET /api/fail2ban/filter", s.admin(s.handleFail2banFilterGet, false))
	mux.Handle("POST /api/fail2ban/filter", s.admin(s.handleFail2banFilterSet, false))
	mux.Handle("DELETE /api/fail2ban/filter/{name}", s.admin(s.handleFail2banFilterDelete, false))

	// Permissions: resellers may read the catalog/defaults (to grant their own
	// customers), but only admins may change the group defaults.
	mux.Handle("GET /api/permissions", s.admin(s.handlePermissionsGet, true))
	mux.Handle("POST /api/permissions", s.admin(s.handlePermissionsSet, false))
	// Web terminal (admin): a PTY-backed root shell over a WebSocket.
	mux.Handle("GET /api/terminal", s.admin(s.handleTerminal, false))
	// OS package updates (apt) — list, apply (streaming job), poll job output.
	mux.Handle("GET /api/packages", s.admin(s.handlePackageList, false))
	mux.Handle("POST /api/packages/upgrade", s.admin(s.handlePackageUpgrade, false))
	mux.Handle("GET /api/packages/job", s.admin(s.handlePackageJob, false))
	mux.Handle("GET /api/services", s.admin(s.handleServiceList, false))
	mux.Handle("GET /api/services/{name}/logs", s.admin(s.handleServiceLogs, false))
	mux.Handle("POST /api/services/{name}/{action}", s.admin(s.handleServiceAction, false))
	// Monitoring & ops (admin).
	mux.Handle("GET /api/metrics", s.admin(s.handleMetrics, false))
	mux.Handle("GET /api/logs", s.admin(s.handleLogList, false))
	mux.Handle("GET /api/logs/{key}", s.admin(s.handleLogView, false))
	mux.Handle("POST /api/alerts/test", s.admin(s.handleAlertTest, false))
	mux.Handle("GET /api/firewall", s.admin(s.handleFirewallList, false))
	mux.Handle("POST /api/firewall", s.admin(s.handleFirewallCreate, false))
	mux.Handle("DELETE /api/firewall/{id}", s.admin(s.handleFirewallDelete, false))
	mux.Handle("POST /api/firewall/toggle", s.admin(s.handleFirewallToggle, false))
	mux.Handle("GET /api/settings", s.admin(s.handleSettingsGet, false))
	mux.Handle("POST /api/settings", s.admin(s.handleSettingsSet, false))
	mux.Handle("GET /api/update", s.admin(s.handleUpdateStatus, false))
	mux.Handle("POST /api/update", s.admin(s.handleUpdateApply, false))
	mux.Handle("POST /api/update/config", s.admin(s.handleUpdateConfig, false))
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
		// Read-only API tokens may only perform safe (non-mutating) requests
		// (SECURITY_AUDIT F-18).
		if !safeMethod(r.Method) && s.Auth.RequestReadOnly(r) {
			s.err(w, http.StatusForbidden, "this API token is read-only")
			return
		}
		h(w, r, u)
		// Record every authenticated mutation in the audit trail. Read-only
		// requests are skipped; specific handlers (login, 2FA, impersonation) add
		// their own richer events.
		if !safeMethod(r.Method) {
			s.audit(u.ID, u.Username, r.Method+" "+r.URL.Path, "", clientIP(r))
		}
	})
}

// safeMethod reports whether an HTTP method is non-mutating.
func safeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

// perm restricts a route to accounts that hold the given module permission.
// Admins always pass. It builds on user(), so authentication and the read-only
// token guard still apply.
func (s *Server) perm(module string, h handlerWithUser) http.Handler {
	return s.user(func(w http.ResponseWriter, r *http.Request, u *models.User) {
		if u.HasModule(module) {
			h(w, r, u)
			return
		}
		s.err(w, http.StatusForbidden, "you don't have access to the "+module+" module")
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

// fail logs the full internal error server-side and returns only a generic
// message plus a correlation reference to the client, so internal details
// (paths, SQL/CLI stderr) are never disclosed (see SECURITY_AUDIT F-15).
func (s *Server) fail(w http.ResponseWriter, op string, err error) {
	ref := errorRef()
	log.Printf("ERROR %s [ref=%s]: %v", op, ref, err)
	s.err(w, http.StatusInternalServerError, "internal error (ref "+ref+")")
}

// errorRef returns a short random reference to correlate a client-facing error
// with the detailed server log line.
func errorRef() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(b[:])
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
