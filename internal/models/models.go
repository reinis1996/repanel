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

type User struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	Role         Role      `json:"role"`
	OwnerID      int64     `json:"owner_id"` // reseller that owns this account, 0 = admin
	Suspended    bool      `json:"suspended"`
	DiskQuotaMB  int64     `json:"disk_quota_mb"` // 0 = unlimited
	CreatedAt    time.Time `json:"created_at"`
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

type Usage struct {
	UserID      int64   `json:"user_id"`
	Username    string  `json:"username"`
	WebMB       float64 `json:"web_mb"`
	MailMB      float64 `json:"mail_mb"`
	DBMB        float64 `json:"db_mb"`
	TotalMB     float64 `json:"total_mb"`
	DiskQuotaMB int64   `json:"disk_quota_mb"` // 0 = unlimited
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

// APIToken is a personal access token for the REST API. Token (the secret) is
// only populated in the response to creation; it is never stored or returned
// afterwards.
type APIToken struct {
	ID         int64      `json:"id"`
	UserID     int64      `json:"user_id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
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
	SSL          bool      `json:"ssl"`
	Suspended    bool      `json:"suspended"`
	CreatedAt    time.Time `json:"created_at"`
	// Joined fields
	Owner string `json:"owner,omitempty"`
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
	AutoSetup bool      `json:"auto_setup"` // admin account created automatically
	CreatedAt time.Time `json:"created_at"`
	// Joined
	Domain string `json:"domain,omitempty"`
}

type DNSRecord struct {
	ID       int64  `json:"id"`
	ZoneID   int64  `json:"zone_id"`
	Name     string `json:"name"`
	Type     string `json:"type"` // A, AAAA, CNAME, MX, TXT, NS, SRV, CAA
	Value    string `json:"value"`
	TTL      int    `json:"ttl"`
	Priority int    `json:"priority"`
}

type DNSZone struct {
	ID        int64       `json:"id"`
	DomainID  int64       `json:"domain_id"`
	Name      string      `json:"name"`
	Serial    int64       `json:"serial"`
	Records   []DNSRecord `json:"records,omitempty"`
	CreatedAt time.Time   `json:"created_at"`
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
	Source      string `json:"source"`      // alias@domain
	Destination string `json:"destination"` // target address
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

type ServiceStatus struct {
	Name        string `json:"name"` // systemd unit name
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	Installed   bool   `json:"installed"`
	Active      bool   `json:"active"`
	Enabled     bool   `json:"enabled"`
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
