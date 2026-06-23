// Package models defines the data structures shared between the database
// layer and the REST API.
package models

import "time"

type Role string

const (
	RoleAdmin    Role = "admin"
	RoleReseller Role = "reseller"
	RoleUser     Role = "user"
)

// Modules are the permission-gated feature areas. A non-admin account may only
// reach a module's pages/API when it has been granted that module. Admins always
// have every module.
const (
	ModuleDomains   = "domains"
	ModuleDNS       = "dns"
	ModuleMail      = "mail"
	ModuleDatabases = "databases"
	ModuleFiles     = "files"
	ModuleFTP       = "ftp"
	ModuleSSL       = "ssl"
	ModuleCron      = "cron"
	ModuleFunctions = "functions"
	ModuleBackups   = "backups"
	ModuleTraffic   = "traffic"
)

// AllModules is the canonical ordered list of permission-gated modules.
var AllModules = []string{
	ModuleDomains, ModuleDNS, ModuleMail, ModuleDatabases, ModuleFiles, ModuleFTP,
	ModuleSSL, ModuleCron, ModuleFunctions, ModuleBackups, ModuleTraffic,
}

// ModuleLabels are the human-readable names used in the UI.
var ModuleLabels = map[string]string{
	ModuleDomains:   "Websites & Domains",
	ModuleDNS:       "DNS",
	ModuleMail:      "Mail",
	ModuleDatabases: "Databases",
	ModuleFiles:     "File Manager",
	ModuleFTP:       "FTP",
	ModuleSSL:       "SSL/TLS",
	ModuleCron:      "Scheduled Tasks",
	ModuleFunctions: "Functions",
	ModuleBackups:   "Backups",
	ModuleTraffic:   "Traffic",
}

// ValidModule reports whether m is a known module key.
func ValidModule(m string) bool {
	_, ok := ModuleLabels[m]
	return ok
}

type User struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	Role         Role      `json:"role"`
	OwnerID      int64     `json:"owner_id"` // reseller that owns this account, 0 = admin
	Suspended    bool      `json:"suspended"`
	DiskQuotaMB  int64     `json:"disk_quota_mb"` // 0 = unlimited
	// Per-account resource limits (0 = unlimited): the counts an operator caps a
	// customer at. Enforced in the respective create paths.
	MaxDomains       int64    `json:"max_domains"`
	MaxMailboxes     int64    `json:"max_mailboxes"`
	MaxDatabases     int64    `json:"max_databases"`
	BandwidthQuotaMB int64    `json:"bandwidth_quota_mb"`
	// Live cgroup caps (0 = unlimited): CPU % of one core, memory MB, processes.
	CPUQuotaPct  int64 `json:"cpu_quota_pct"`
	MemoryMaxMB  int64 `json:"memory_max_mb"`
	ProcessesMax int64 `json:"processes_max"`
	PlanID       int64 `json:"plan_id"` // assigned hosting plan, 0 = none
	Permissions      []string `json:"permissions"` // granted module keys (admins: all)
	TOTPEnabled  bool      `json:"totp_enabled"`
	SSHEnabled   bool      `json:"ssh_enabled"`
	CreatedAt    time.Time `json:"created_at"`
	// Impersonator is set on /api/me only while an admin is impersonating this
	// account: the admin's username, so the UI can show a "stop" banner.
	Impersonator string `json:"impersonator,omitempty"`
}

// MetricSample is one historical reading of host resource usage (percentages).
type MetricSample struct {
	Ts   time.Time `json:"ts"`
	CPU  float64   `json:"cpu"`
	Mem  float64   `json:"mem"`
	Disk float64   `json:"disk"`
}

// ServiceHealth is the detailed status and recent logs of a managed service,
// for diagnosing why it is down.
type ServiceHealth struct {
	Name   string `json:"name"`
	Status string `json:"status"` // `systemctl status` summary
	Logs   string `json:"logs"`   // recent journal lines
}

// AuditEntry is one row of the security audit trail.
type AuditEntry struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id"`
	Username  string    `json:"username"`
	Action    string    `json:"action"`
	Detail    string    `json:"detail"`
	IP        string    `json:"ip"`
	CreatedAt time.Time `json:"created_at"`
}

// HasModule reports whether the user may access a module. Admins always can.
func (u *User) HasModule(m string) bool {
	if u.Role == RoleAdmin {
		return true
	}
	for _, p := range u.Permissions {
		if p == m {
			return true
		}
	}
	return false
}

// Plan is a hosting plan / service package: a named template of resource limits
// and grantable modules. Assigning it to an account copies its values onto that
// account (the limit columns the create paths already enforce).
type Plan struct {
	ID               int64     `json:"id"`
	Name             string    `json:"name"`
	DiskQuotaMB      int64     `json:"disk_quota_mb"`
	BandwidthQuotaMB int64     `json:"bandwidth_quota_mb"`
	MaxDomains       int64     `json:"max_domains"`
	MaxMailboxes     int64     `json:"max_mailboxes"`
	MaxDatabases     int64     `json:"max_databases"`
	Modules          []string  `json:"modules"`
	CreatedAt        time.Time `json:"created_at"`
}

// Branding is the white-label appearance of the panel: a custom name, accent
// color and logo. Served publicly (the login screen needs it before auth).
type Branding struct {
	Name  string `json:"name"`  // panel name (default "RePanel")
	Color string `json:"color"` // accent hex, e.g. #1a6fd4 ("" = default)
	Logo  string `json:"logo"`  // logo image URL ("" = wordmark text)
}

type Backup struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id"`
	Filename  string    `json:"filename"`
	SizeBytes int64     `json:"size_bytes"`
	Status    string    `json:"status"` // running | completed | failed
	Error     string    `json:"error"`
	CreatedAt time.Time `json:"created_at"`
	Owner     string    `json:"owner,omitempty"`
}

// BackupDestination is an offsite target (an rclone remote) that completed
// backups are uploaded to. Config holds the rclone parameters; secrets in it are
// never returned to the client.
type BackupDestination struct {
	ID         int64             `json:"id"`
	Name       string            `json:"name"`
	Type       string            `json:"type"` // s3 | b2 | sftp | ftp | rclone
	Config     map[string]string `json:"config,omitempty"`
	RemotePath string            `json:"remote_path"`
	Enabled    bool              `json:"enabled"`
	Keep       int               `json:"keep"`
	CreatedAt  time.Time         `json:"created_at"`
}

// BackupContents is the component inventory of a backup archive, for selective
// restore.
type BackupContents struct {
	HasWeb      bool     `json:"has_web"`
	Databases   []string `json:"databases"`
	MailDomains []string `json:"mail_domains"`
	Files       []string `json:"files"` // web file paths, for single-file restore
}

type Usage struct {
	UserID      int64   `json:"user_id"`
	Username    string  `json:"username"`
	WebMB       float64 `json:"web_mb"`
	MailMB      float64 `json:"mail_mb"`
	DBMB        float64 `json:"db_mb"`
	TotalMB     float64 `json:"total_mb"`
	DiskQuotaMB int64   `json:"disk_quota_mb"` // 0 = unlimited
	// Current calendar-month web bandwidth and the account's monthly cap.
	BandwidthMB      float64 `json:"bandwidth_mb"`
	BandwidthQuotaMB int64   `json:"bandwidth_quota_mb"` // 0 = unlimited
}

// TrafficStat is the bandwidth served for one account over a reporting window.
type TrafficStat struct {
	UserID   int64           `json:"user_id"`
	Username string          `json:"username"`
	TotalMB  float64         `json:"total_mb"`
	Domains  []TrafficDomain `json:"domains"`
	Series   []TrafficDay    `json:"series"`
}

// TrafficDomain is one domain's share of an account's bandwidth in the window.
type TrafficDomain struct {
	Domain string  `json:"domain"`
	MB     float64 `json:"mb"`
}

// TrafficDay is an account's total bandwidth on one calendar day.
type TrafficDay struct {
	Day string  `json:"day"` // YYYY-MM-DD
	MB  float64 `json:"mb"`
}

// WebStats is the AWStats-style report for a single domain over a window.
type WebStats struct {
	Domain       string         `json:"domain"`
	Days         int            `json:"days"`
	Totals       WebStatsTotals `json:"totals"`
	Series       []WebStatsDay  `json:"series"`
	TopPages     []WebStatItem  `json:"top_pages"`
	TopReferrers []WebStatItem  `json:"top_referrers"`
	StatusCodes  []WebStatItem  `json:"status_codes"`
}

// WebStatsTotals are the summed metrics for the whole window. Visitors is the
// count of distinct IPs across the window.
type WebStatsTotals struct {
	Hits      int64   `json:"hits"`
	Pageviews int64   `json:"pageviews"`
	Visitors  int64   `json:"visitors"`
	MB        float64 `json:"mb"`
}

// WebStatsDay is one calendar day's metrics for a domain.
type WebStatsDay struct {
	Day       string  `json:"day"` // YYYY-MM-DD
	Hits      int64   `json:"hits"`
	Pageviews int64   `json:"pageviews"`
	Visitors  int64   `json:"visitors"`
	MB        float64 `json:"mb"`
}

// WebStatItem is a label/count pair for a top-pages / referrers / status list.
type WebStatItem struct {
	Label string `json:"label"`
	Count int64  `json:"count"`
}

// APIToken is a personal access token for the REST API. Token (the secret) is
// only populated in the response to creation; it is never stored or returned
// afterwards.
type APIToken struct {
	ID         int64      `json:"id"`
	UserID     int64      `json:"user_id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Scope      string     `json:"scope"` // full | readonly
	LastUsedAt *time.Time `json:"last_used_at"`
	ExpiresAt  *time.Time `json:"expires_at"`
	CreatedAt  time.Time  `json:"created_at"`
	Token      string     `json:"token,omitempty"`
}

type Session struct {
	Token     string
	UserID    int64
	ExpiresAt time.Time
}

type Domain struct {
	ID           int64     `json:"id"`
	UserID       int64     `json:"user_id"`
	Name         string    `json:"name"`
	DocumentRoot string    `json:"document_root"`
	PHPVersion   string    `json:"php_version"`
	Runtime      string    `json:"runtime"`      // php | node
	NodeVersion  string    `json:"node_version"` // when runtime == node
	NodePort     int       `json:"node_port,omitempty"`
	SpamFilter   bool      `json:"spam_filter"` // mail anti-spam/virus filtering (per domain)
	SSL          bool      `json:"ssl"`
	Suspended    bool      `json:"suspended"`
	WebMode      string    `json:"web_mode"` // nginx | apache | nginx-apache
	CreatedAt    time.Time `json:"created_at"`
	// Domain kind: "primary" (default), "subdomain" or "alias" (parked). ParentID
	// is the owning primary domain for subdomains/aliases.
	Kind     string `json:"kind"`
	ParentID int64  `json:"parent_id,omitempty"`
	Parent   string `json:"parent,omitempty"` // joined parent domain name (list views)
	// Forwarding: when RedirectURL is set the site is a pure redirect to it.
	RedirectURL  string `json:"redirect_url,omitempty"`
	RedirectCode int    `json:"redirect_code,omitempty"` // 301 | 302
	// PHPSettings is the raw JSON of structured per-domain PHP overrides; the
	// system layer renders it into the FPM pool. Populated by getDomainScoped.
	PHPSettings string `json:"php_settings,omitempty"`
	// Pre-rendered password-protected-directory directive blocks for the active
	// front/back server, injected into the generated vhost. Internal only.
	ProtectedNginx  string `json:"-"`
	ProtectedApache string `json:"-"`
	// Admin-editable config overrides merged into the generated config on every
	// rebuild. Omitted from list payloads; populated by getDomainScoped.
	NginxConf  string `json:"nginx_conf,omitempty"`
	ApacheConf string `json:"apache_conf,omitempty"`
	PHPConf    string `json:"php_conf,omitempty"`
	// Per-domain ModSecurity WAF settings.
	WAFEnabled bool   `json:"waf_enabled"`
	WAFMode    string `json:"waf_mode"` // on | detection
	WAFRules   string `json:"waf_rules,omitempty"`
	// Joined fields
	Owner string `json:"owner,omitempty"`
}

// PHPSettings is the structured, owner-editable subset of php.ini values exposed
// per domain. Empty fields fall back to the panel/pool defaults so a customer
// only overrides what they touch. Rendered into the PHP-FPM pool.
type PHPSettings struct {
	MemoryLimit       string `json:"memory_limit"`        // e.g. 256M
	UploadMaxFilesize string `json:"upload_max_filesize"` // e.g. 128M
	PostMaxSize       string `json:"post_max_size"`       // e.g. 128M
	MaxExecutionTime  string `json:"max_execution_time"`  // seconds
	MaxInputTime      string `json:"max_input_time"`      // seconds
	MaxInputVars      string `json:"max_input_vars"`
	DisplayErrors     bool   `json:"display_errors"`
	AllowUrlFopen     bool   `json:"allow_url_fopen"`
	DisableFunctions  string `json:"disable_functions"` // comma-separated
}

// ProtectedDir is a password-protected directory (.htpasswd) under a domain.
type ProtectedDir struct {
	ID       int64    `json:"id"`
	DomainID int64    `json:"domain_id"`
	Path     string   `json:"path"`  // URL path, e.g. /admin
	Realm    string   `json:"realm"` // browser auth prompt label
	Users    []string `json:"users"` // usernames with access (no passwords returned)
}

// DNSSECStatus reports a zone's signing state and the DS records to publish at
// the registrar when DNSSEC is enabled.
type DNSSECStatus struct {
	ZoneID    int64    `json:"zone_id"`
	Zone      string   `json:"zone"`
	Enabled   bool     `json:"enabled"`
	Available bool     `json:"available"` // BIND signing tools present
	DS        []string `json:"ds"`        // DS records for the parent/registrar
	DNSKEY    []string `json:"dnskey"`    // published DNSKEY records (informational)
}

// NodeVersionInfo reports a Node.js version's state for the admin manager
// (mirrors PHPVersionInfo).
type NodeVersionInfo struct {
	Version    string `json:"version"`
	Installed  bool   `json:"installed"`
	Installing bool   `json:"installing"`
	Error      string `json:"error,omitempty"`
}

// NodeApp is the per-domain Node application configuration and live status.
type NodeApp struct {
	DomainID int64             `json:"domain_id"`
	Version  string            `json:"version"`
	AppRoot  string            `json:"app_root"` // relative to the domain directory
	Startup  string            `json:"startup"`  // entry file relative to app root
	Port     int               `json:"port"`
	Env      map[string]string `json:"env"`
	Running  bool              `json:"running"`
	URL      string            `json:"url"`
}

// WebServerInfo describes the server-wide web server stack and the per-domain
// modes the operator may choose from. When only one mode is offered the stack
// is single-server (nginx-only or apache-only) and the UI hides the selector.
type WebServerInfo struct {
	Stack   string   `json:"stack"`   // nginx | apache | nginx-apache
	Modes   []string `json:"modes"`   // selectable per-domain modes
	Default string   `json:"default"` // default mode for new domains
}

// App is a one-click application (e.g. WordPress) installed into a domain.
type App struct {
	ID        int64     `json:"id"`
	DomainID  int64     `json:"domain_id"`
	App       string    `json:"app"`
	Status    string    `json:"status"` // installing | installed | failed
	Error     string    `json:"error"`
	URL       string    `json:"url"`
	DBName    string    `json:"db_name"`
	DBUser    string    `json:"db_user,omitempty"`
	DBPass    string    `json:"db_pass,omitempty"` // shown so the user can finish the app's installer
	AutoSetup bool      `json:"auto_setup"`        // app is ready (no browser setup needed)
	CreatedAt time.Time `json:"created_at"`
	// Joined
	Domain string `json:"domain,omitempty"`
}

// Function is a lambda-style serverless function: tenant-supplied handler code
// in a chosen runtime, invoked either by a generated URL ("url" trigger) or on
// a cron schedule ("schedule" trigger). It runs as the owning tenant's system
// user. URL functions are fronted by nginx (PHP via a dedicated PHP-FPM pool,
// Node/Python via a per-function systemd unit); scheduled functions are run by a
// cron.d entry that executes the handler through a generated CLI runner.
type Function struct {
	ID           int64     `json:"id"`
	UserID       int64     `json:"user_id"`
	Name         string    `json:"name"`
	Slug         string    `json:"slug"`
	Runtime      string    `json:"runtime"` // python | node | php
	Version      string    `json:"version"`
	Trigger      string    `json:"trigger"`       // url | schedule
	Schedule     string    `json:"schedule"`      // cron expression (schedule trigger only)
	AllowNetwork bool      `json:"allow_network"` // permit outbound network from the sandbox
	BaseDomain   string    `json:"base_domain"`   // tenant domain (zoned) or "" = panel FQDN (url only)
	Hostname     string    `json:"hostname"`      // slug.function-url.base (url only)
	Enabled      bool      `json:"enabled"`
	SSL          bool      `json:"ssl"`
	Status       string    `json:"status"` // active | failed
	Error        string    `json:"error,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	// Computed / single-get only
	URL  string `json:"url,omitempty"`
	Code string `json:"code,omitempty"`
}

// FunctionRuntime lists the installed versions of one runtime the panel can run
// functions on.
type FunctionRuntime struct {
	Runtime  string   `json:"runtime"` // python | node | php
	Versions []string `json:"versions"`
}

// FunctionInvokeResult is the outcome of a manual test invocation: the handler's
// JSON return value, anything it printed (logs), whether it completed without an
// unhandled error, and how long it ran.
type FunctionInvokeResult struct {
	Response   string `json:"response"`
	Logs       string `json:"logs"`
	OK         bool   `json:"ok"`
	DurationMS int64  `json:"duration_ms"`
}

// FunctionMeta drives the "create function" form: the runtimes/versions present
// on the host, the caller's domains eligible to host a function URL (those with
// a DNS zone here), and the default base (the panel FQDN) used when no domain is
// chosen.
type FunctionMeta struct {
	Runtimes    []FunctionRuntime `json:"runtimes"`
	Domains     []string          `json:"domains"`
	DefaultBase string            `json:"default_base"`
}

// ---- WordPress Workbench ----

// WPSite is a panel-installed WordPress site in the workbench list.
type WPSite struct {
	AppID    int64  `json:"app_id"`
	DomainID int64  `json:"domain_id"`
	Domain   string `json:"domain"`
	URL      string `json:"url"`
	Status   string `json:"status"`
}

// WPInfo is the core/overview state of one WordPress site.
type WPInfo struct {
	Version         string `json:"version"`
	UpdateVersion   string `json:"update_version"` // "" when up to date
	Title           string `json:"title"`
	Tagline         string `json:"tagline"`
	SearchVisible   bool   `json:"search_visible"` // blog_public
	URL             string `json:"url"`
	PHPVersion      string `json:"php_version"`
	PluginUpdates   int    `json:"plugin_updates"`   // count of plugins with an update
	ThemeUpdates    int    `json:"theme_updates"`    // count of themes with an update
	MaintenanceMode bool   `json:"maintenance_mode"` // wp maintenance-mode status
	Multisite       bool   `json:"multisite"`
}

// WPPlugin / WPTheme describe an installed plugin or theme.
type WPPlugin struct {
	Name          string `json:"name"`
	Title         string `json:"title"`
	Status        string `json:"status"` // active | inactive | must-use | ...
	Version       string `json:"version"`
	Update        bool   `json:"update"`
	UpdateVersion string `json:"update_version"`
	AutoUpdate    bool   `json:"auto_update"`
}

type WPTheme struct {
	Name          string `json:"name"`
	Title         string `json:"title"`
	Status        string `json:"status"` // active | inactive
	Version       string `json:"version"`
	Update        bool   `json:"update"`
	UpdateVersion string `json:"update_version"`
	AutoUpdate    bool   `json:"auto_update"`
}

// WPUser is a WordPress user account within a site.
type WPUser struct {
	ID          int64  `json:"id"`
	Login       string `json:"login"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Roles       string `json:"roles"` // comma-separated
	Registered  string `json:"registered"`
}

// WPCronEvent is a scheduled WP-cron hook.
type WPCronEvent struct {
	Hook     string `json:"hook"`
	NextRun  string `json:"next_run"` // human-readable (e.g. "in 5 minutes")
	Schedule string `json:"schedule"` // recurrence name, "" for one-off
}

// WPConfig is the subset of wp-config.php constants the workbench manages.
type WPConfig struct {
	Debug            bool   `json:"debug"`              // WP_DEBUG
	DebugLog         bool   `json:"debug_log"`          // WP_DEBUG_LOG
	DisallowFileEdit bool   `json:"disallow_file_edit"` // DISALLOW_FILE_EDIT
	MemoryLimit      string `json:"memory_limit"`       // WP_MEMORY_LIMIT
	AutoUpdateCore   string `json:"auto_update_core"`   // WP_AUTO_UPDATE_CORE: minor|true|false
}

// WPUpdateResult summarizes a bulk "update everything" run.
type WPUpdateResult struct {
	Core    bool `json:"core"`
	Plugins int  `json:"plugins"`
	Themes  int  `json:"themes"`
}

type DNSRecord struct {
	ID       int64  `json:"id"`
	ZoneID   int64  `json:"zone_id"`
	Name     string `json:"name"`
	Type     string `json:"type"` // A, AAAA, CNAME, MX, TXT, NS, SRV, CAA
	Value    string `json:"value"`
	TTL      int    `json:"ttl"`
	Priority int    `json:"priority"`
	Proxied  bool   `json:"proxied"` // Cloudflare proxy state (A/AAAA/CNAME), for CF sync
}

type DNSZone struct {
	ID          int64       `json:"id"`
	DomainID    int64       `json:"domain_id"`
	Name        string      `json:"name"`
	Serial      int64       `json:"serial"`
	DNSSEC      bool        `json:"dnssec"`
	RecordCount int         `json:"record_count"` // populated in list views
	Records     []DNSRecord `json:"records,omitempty"`
	CreatedAt   time.Time   `json:"created_at"`
	// Cloudflare sync binding. Token is never returned; HasCFToken reports whether
	// one is stored. CFSync is off | push (RePanel→CF) | pull (CF→RePanel).
	CFZoneID   string `json:"cf_zone_id"`
	CFSync     string `json:"cf_sync"`
	HasCFToken bool   `json:"has_cf_token"`
}

type Mailbox struct {
	ID           int64     `json:"id"`
	DomainID     int64     `json:"domain_id"`
	Address      string    `json:"address"` // full address user@domain
	PasswordHash string    `json:"-"`
	QuotaMB      int       `json:"quota_mb"`
	CreatedAt    time.Time `json:"created_at"`
}

// DKIMStatus reports a domain's email-authentication state: whether DKIM
// signing is enabled and the DNS records that should be published. When the
// domain's zone is hosted by this panel the records are published
// automatically; otherwise the operator copies them to their DNS provider.
type DKIMStatus struct {
	DomainID   int64  `json:"domain_id"`
	Domain     string `json:"domain"`
	Enabled    bool   `json:"enabled"`
	Selector   string `json:"selector"`
	DNSManaged bool   `json:"dns_managed"`
	DKIMName   string `json:"dkim_name"`  // e.g. repanel._domainkey
	DKIMValue  string `json:"dkim_value"` // v=DKIM1; ...
	DMARCName  string `json:"dmarc_name"` // _dmarc
	DMARCValue string `json:"dmarc_value"`
	SPFSuggest string `json:"spf_suggest"`
}

// WebmailStatus reports whether webmail (Roundcube at webmail.<domain>) is
// enabled for a domain. Available is false server-wide when Roundcube is not
// installed, in which case the UI offers no enable action.
type WebmailStatus struct {
	DomainID   int64  `json:"domain_id"`
	Domain     string `json:"domain"`
	Enabled    bool   `json:"enabled"`
	Available  bool   `json:"available"`
	URL        string `json:"url"`         // http://webmail.<domain>
	DNSManaged bool   `json:"dns_managed"` // a webmail A record is published here
}

type MailAlias struct {
	ID          int64  `json:"id"`
	DomainID    int64  `json:"domain_id"`
	Source      string `json:"source"`      // alias@domain, or @domain for a catch-all
	Destination string `json:"destination"` // one or more comma-separated targets
	KeepCopy    bool   `json:"keep_copy"`   // also deliver to the local mailbox named by source
}

// MailList is a distribution list: an address that expands to its members.
type MailList struct {
	ID        int64     `json:"id"`
	DomainID  int64     `json:"domain_id"`
	Address   string    `json:"address"`
	Members   []string  `json:"members"`
	CreatedAt time.Time `json:"created_at"`
}

// MailAutoresponder is a mailbox's vacation/auto-reply message (applied via Sieve).
type MailAutoresponder struct {
	MailboxID int64  `json:"mailbox_id"`
	Enabled   bool   `json:"enabled"`
	Subject   string `json:"subject"`
	Message   string `json:"message"`
	StartDate string `json:"start_date"` // YYYY-MM-DD, optional
	EndDate   string `json:"end_date"`   // YYYY-MM-DD, optional
}

// MailFilter is one Sieve filter rule for a mailbox.
type MailFilter struct {
	ID        int64  `json:"id"`
	MailboxID int64  `json:"mailbox_id"`
	Position  int    `json:"position"`
	Field     string `json:"field"` // from | to | subject | any
	Op        string `json:"op"`    // contains | is
	Value     string `json:"value"`
	Action    string `json:"action"` // fileinto | forward | discard | keep
	Arg       string `json:"arg"`    // folder name or forward address
}

// MailMigration is an imapsync job importing a remote mailbox.
type MailMigration struct {
	ID         int64     `json:"id"`
	MailboxID  int64     `json:"mailbox_id"`
	Mailbox    string    `json:"mailbox,omitempty"` // joined address
	RemoteHost string    `json:"remote_host"`
	RemotePort int       `json:"remote_port"`
	RemoteUser string    `json:"remote_user"`
	Status     string    `json:"status"` // running | completed | failed
	Log        string    `json:"log"`
	CreatedAt  time.Time `json:"created_at"`
}

// MailSmarthost is the server-wide outbound SMTP relay configuration.
type MailSmarthost struct {
	Enabled  bool   `json:"enabled"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	HasPass  bool   `json:"has_pass"` // whether a password is stored (never returned)
}

type DatabaseEntry struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id"`
	Name      string    `json:"name"`
	DBUser    string    `json:"db_user"`
	Engine    string    `json:"engine"` // mysql | postgres
	CreatedAt time.Time `json:"created_at"`
	SizeMB    float64   `json:"size_mb"`
}

type FTPAccount struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id"`
	Username  string    `json:"username"`
	Directory string    `json:"directory"`
	CreatedAt time.Time `json:"created_at"`
}

type CronJob struct {
	ID       int64  `json:"id"`
	UserID   int64  `json:"user_id"`
	Schedule string `json:"schedule"` // standard 5-field cron expression
	Command  string `json:"command"`
	Comment  string `json:"comment"`
	Enabled  bool   `json:"enabled"`
}

type Certificate struct {
	ID        int64     `json:"id"`
	DomainID  int64     `json:"domain_id"`
	Domain    string    `json:"domain"`
	Issuer    string    `json:"issuer"` // letsencrypt | self-signed | custom
	NotAfter  time.Time `json:"not_after"`
	CertPath  string    `json:"cert_path"`
	KeyPath   string    `json:"key_path"`
	CreatedAt time.Time `json:"created_at"`
}

// PHPVersionInfo reports a PHP version's state for the admin PHP manager:
// whether it is installed, currently being installed, or failed to install.
type PHPVersionInfo struct {
	Version    string `json:"version"`
	Installed  bool   `json:"installed"`
	Installing bool   `json:"installing"`
	Error      string `json:"error,omitempty"`
}

// PackageUpdate is one pending OS package upgrade (from apt), flagged when it
// comes from a security archive.
type PackageUpdate struct {
	Name           string `json:"name"`
	CurrentVersion string `json:"current_version"`
	NewVersion     string `json:"new_version"`
	Security       bool   `json:"security"`
}

type ServiceStatus struct {
	Name        string `json:"name"` // systemd unit name
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	Installed   bool   `json:"installed"`
	Active      bool   `json:"active"`
	Enabled     bool   `json:"enabled"`
	Version     string `json:"version"` // installed package version, "" if unknown
}

type FirewallRule struct {
	ID     int64  `json:"id"`
	Port   string `json:"port"`   // "80", "8080:8090"
	Proto  string `json:"proto"`  // tcp | udp
	Source string `json:"source"` // CIDR or "any"
	Action string `json:"action"` // allow | deny
	Note   string `json:"note"`
}

type SystemInfo struct {
	Hostname     string  `json:"hostname"`
	OS           string  `json:"os"`
	Kernel       string  `json:"kernel"`
	Uptime       int64   `json:"uptime_seconds"`
	LoadAvg      string  `json:"load_avg"`
	CPUCount     int     `json:"cpu_count"`
	CPUUsage     float64 `json:"cpu_usage_percent"`
	MemTotalMB   int64   `json:"mem_total_mb"`
	MemUsedMB    int64   `json:"mem_used_mb"`
	DiskTotalGB  float64 `json:"disk_total_gb"`
	DiskUsedGB   float64 `json:"disk_used_gb"`
	PanelVersion string  `json:"panel_version"`
}
