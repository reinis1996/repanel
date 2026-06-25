export type Role = 'admin' | 'reseller' | 'user'

export interface User {
  id: number
  username: string
  email: string
  role: Role
  owner_id: number
  suspended: boolean
  disk_quota_mb: number
  max_domains: number
  max_mailboxes: number
  max_databases: number
  bandwidth_quota_mb: number
  cpu_quota_pct: number
  memory_max_mb: number
  processes_max: number
  plan_id: number
  permissions: string[]
  totp_enabled: boolean
  ssh_enabled: boolean
  impersonator?: string
  created_at: string
}

export interface MetricSample {
  ts: string
  cpu: number
  mem: number
  disk: number
}

export interface MetricsResp {
  samples: MetricSample[]
  traffic: { day: string; mb: number }[]
}

export interface ServiceHealth {
  name: string
  status: string
  logs: string
}

export interface AuditEntry {
  id: number
  user_id: number
  username: string
  action: string
  detail: string
  ip: string
  created_at: string
}

export interface Fail2banJail {
  name: string
  banned: string[]
  total: number
  failed: number
}

export interface Fail2banDefaults {
  bantime: string
  findtime: string
  maxretry: string
}

export interface Fail2banJailConfig {
  name: string
  enabled: boolean
  running: boolean
  maxretry: string
  bantime: string
  findtime: string
  filter: string
  logpath: string
  port: string
}

export interface Fail2banConfig {
  defaults: Fail2banDefaults
  jails: Fail2banJailConfig[]
}

export interface Fail2banFilter {
  name: string
  failregex: string
  ignoreregex: string
  custom: boolean
}

export interface Fail2banStatus {
  available: boolean
  jails?: Fail2banJail[]
  whitelist?: string[]
  config?: Fail2banConfig
  filters?: string[]
  custom_filters?: string[]
}

// MODULES is the catalog of permission-gated feature areas (mirrors the backend
// models.AllModules / ModuleLabels). Keys must match the API.
export const MODULES: { key: string; label: string }[] = [
  { key: 'domains', label: 'Websites & Domains' },
  { key: 'dns', label: 'DNS' },
  { key: 'mail', label: 'Mail' },
  { key: 'databases', label: 'Databases' },
  { key: 'files', label: 'File Manager' },
  { key: 'ftp', label: 'FTP' },
  { key: 'ssl', label: 'SSL/TLS' },
  { key: 'cron', label: 'Scheduled Tasks' },
  { key: 'functions', label: 'Functions' },
  { key: 'backups', label: 'Backups' },
  { key: 'traffic', label: 'Traffic' },
]

export interface Backup {
  id: number
  user_id: number
  filename: string
  size_bytes: number
  status: 'running' | 'completed' | 'failed'
  error: string
  created_at: string
  owner?: string
}

export interface BackupDestination {
  id: number
  name: string
  type: 's3' | 'b2' | 'sftp' | 'ftp' | 'rclone'
  remote_path: string
  enabled: boolean
  keep: number
  created_at: string
}

export interface BackupContents {
  has_web: boolean
  databases: string[]
  mail_domains: string[]
  files: string[]
}

export interface Usage {
  user_id: number
  username: string
  web_mb: number
  mail_mb: number
  db_mb: number
  total_mb: number
  disk_quota_mb: number
  bandwidth_mb: number
  bandwidth_quota_mb: number
}

export interface Plan {
  id: number
  name: string
  disk_quota_mb: number
  bandwidth_quota_mb: number
  max_domains: number
  max_mailboxes: number
  max_databases: number
  modules: string[]
  created_at: string
}

export interface Branding {
  name: string
  color: string
  logo: string
}

export interface PackageUpdate {
  name: string
  current_version: string
  new_version: string
  security: boolean
}

export interface PackageList {
  available: boolean
  updates: PackageUpdate[]
  total: number
  security: number
}

export interface PackageJob {
  started: boolean
  running: boolean
  done: boolean
  failed: boolean
  error: string
  output: string
}

export interface APIToken {
  id: number
  user_id: number
  name: string
  prefix: string
  scope: string // full | readonly
  last_used_at: string | null
  expires_at: string | null
  created_at: string
  token?: string // only returned once, at creation
}

export interface TrafficStat {
  user_id: number
  username: string
  total_mb: number
  domains: { domain: string; mb: number }[]
  series: { day: string; mb: number }[]
}

export interface WebStatItem {
  label: string
  count: number
}

export interface WAFStatus {
  available: boolean
  crs: boolean
  installing: boolean
  error: string
  enabled: boolean
  mode: 'on' | 'detection'
  rules: string
  can_edit_rules: boolean
}

export interface WebStats {
  domain: string
  days: number
  totals: { hits: number; pageviews: number; visitors: number; mb: number }
  series: { day: string; hits: number; pageviews: number; visitors: number; mb: number }[]
  top_pages: WebStatItem[]
  top_referrers: WebStatItem[]
  status_codes: WebStatItem[]
}

export interface Domain {
  id: number
  user_id: number
  name: string
  document_root: string
  php_version: string
  runtime: string // php | node
  node_version: string
  node_port?: number
  ssl: boolean
  suspended: boolean
  web_mode: string // nginx | apache | nginx-apache
  waf_enabled?: boolean
  created_at: string
  owner?: string
  kind: string // primary | subdomain | alias
  parent_id?: number
  parent?: string // parent domain name (for subdomains/aliases)
  redirect_url?: string
  redirect_code?: number
  aliases?: string[] // extra hostnames pointing at the same site (e.g. www.<name>)
}

export interface PHPSettings {
  memory_limit: string
  upload_max_filesize: string
  post_max_size: string
  max_execution_time: string
  max_input_time: string
  max_input_vars: string
  display_errors: boolean
  allow_url_fopen: boolean
  disable_functions: string
}

export interface ProtectedDir {
  id: number
  domain_id: number
  path: string
  realm: string
  users: string[]
}

export interface DNSSECStatus {
  zone_id: number
  zone: string
  enabled: boolean
  available: boolean
  ds: string[]
  dnskey: string[]
}

export interface DBAdminStatus {
  installed: boolean
  enabled: boolean
  host: string
  url: string
}

// SiteConfig is the per-site config editor payload (admin-only): editable
// "additional directives" override blocks plus read-only rendered views.
export interface SiteConfig {
  nginx_conf: string
  apache_conf: string
  php_conf: string
  nginx_active: boolean
  apache_active: boolean
  php_active: boolean
  runtime: string
  rendered: {
    nginx: string
    apache: string
    php: string
    mail: string
  }
}

export interface NodeVersionInfo {
  version: string
  installed: boolean
  installing: boolean
  error?: string
}

export interface NodeApp {
  domain_id: number
  version: string
  app_root: string
  startup: string
  port: number
  env: Record<string, string>
  running: boolean
  url: string
}

export interface WebServerInfo {
  stack: string // nginx | apache | nginx-apache
  modes: string[] // selectable per-domain modes
  default: string // default mode for new domains
}

export interface App {
  id: number
  domain_id: number
  app: string
  status: 'installing' | 'installed' | 'failed'
  error: string
  url: string
  db_name: string
  db_user?: string
  db_pass?: string
  auto_setup: boolean
  created_at: string
  domain?: string
}

export interface CatalogApp {
  id: string
  name: string
  description: string
  category: string
  needs_db: boolean
  config: 'wordpress' | 'manual' | 'none'
}

export interface DNSZone {
  id: number
  domain_id: number
  name: string
  serial: number
  dnssec?: boolean
  record_count?: number
  records?: DNSRecord[]
  created_at: string
  cf_zone_id?: string
  cf_sync?: string // off | push | pull
  has_cf_token?: boolean
}

export interface DNSRecord {
  id: number
  zone_id: number
  name: string
  type: string
  value: string
  ttl: number
  priority: number
  proxied?: boolean
}

export interface DKIMStatus {
  domain_id: number
  domain: string
  enabled: boolean
  selector: string
  dns_managed: boolean
  dkim_name: string
  dkim_value: string
  dmarc_name: string
  dmarc_value: string
  spf_suggest: string
}

export interface WebmailStatus {
  domain_id: number
  domain: string
  enabled: boolean
  available: boolean
  url: string
  dns_managed: boolean
}

export interface SpamStatus {
  available: boolean // rspamd installed
  clamav: boolean    // ClamAV daemon detected
  installing: boolean
  error: string
  domains: { domain_id: number; domain: string; enabled: boolean }[]
}

export interface Mailbox {
  id: number
  domain_id: number
  address: string
  quota_mb: number
  created_at: string
}

export interface MailAlias {
  id: number
  domain_id: number
  source: string
  destination: string
  keep_copy: boolean
}

export interface MailList {
  id: number
  domain_id: number
  address: string
  members: string[]
  created_at: string
}

export interface MailAutoresponder {
  mailbox_id: number
  enabled: boolean
  subject: string
  message: string
  start_date: string
  end_date: string
}

export interface MailFilter {
  id: number
  mailbox_id: number
  position: number
  field: string
  op: string
  value: string
  action: string
  arg: string
}

export interface MailMigration {
  id: number
  mailbox_id: number
  mailbox?: string
  remote_host: string
  remote_port: number
  remote_user: string
  status: string
  log: string
  created_at: string
}

export interface MailSmarthost {
  enabled: boolean
  host: string
  port: number
  username: string
  has_pass: boolean
}

export interface DatabaseEntry {
  id: number
  user_id: number
  name: string
  db_user: string
  engine: string // mysql | postgres
  created_at: string
  size_mb: number
}

export interface FTPAccount {
  id: number
  user_id: number
  username: string
  directory: string
  created_at: string
}

export interface CronJob {
  id: number
  user_id: number
  schedule: string
  command: string
  comment: string
  enabled: boolean
}

export interface FunctionItem {
  id: number
  user_id: number
  name: string
  slug: string
  runtime: string // python | node | php
  version: string
  trigger: string // url | schedule
  schedule: string // cron expression (schedule trigger)
  allow_network: boolean
  base_domain: string
  hostname: string
  enabled: boolean
  ssl: boolean
  status: string // active | failed
  error?: string
  created_at: string
  url?: string
  code?: string
}

export interface FunctionRuntime {
  runtime: string
  versions: string[]
}

export interface FunctionInvokeResult {
  response: string
  logs: string
  ok: boolean
  duration_ms: number
}

export interface FunctionMeta {
  runtimes: FunctionRuntime[]
  domains: string[]
  default_base: string
}

export interface WPSite {
  app_id: number
  domain_id: number
  domain: string
  url: string
  status: string
}

export interface WPInfo {
  version: string
  update_version: string
  title: string
  tagline: string
  search_visible: boolean
  url: string
  php_version: string
  plugin_updates: number
  theme_updates: number
  maintenance_mode: boolean
  multisite: boolean
}

export interface WPPlugin {
  name: string
  title: string
  status: string
  version: string
  update: boolean
  update_version: string
  auto_update: boolean
}

export interface WPTheme {
  name: string
  title: string
  status: string
  version: string
  update: boolean
  update_version: string
  auto_update: boolean
}

export interface WPUser {
  id: number
  login: string
  email: string
  display_name: string
  roles: string
  registered: string
}

export interface WPCronEvent {
  hook: string
  next_run: string
  schedule: string
}

export interface WPConfig {
  debug: boolean
  debug_log: boolean
  disallow_file_edit: boolean
  memory_limit: string
  auto_update_core: string
}

export interface Certificate {
  id: number
  domain_id: number
  domain: string
  issuer: string
  not_after: string
  created_at: string
}

export interface PHPVersionInfo {
  version: string
  installed: boolean
  installing: boolean
  error?: string
}

export interface ServiceStatus {
  name: string
  display_name: string
  description: string
  installed: boolean
  active: boolean
  enabled: boolean
  version: string
}

export interface FirewallRule {
  id: number
  port: string
  proto: string
  source: string
  action: string
  note: string
}

export interface UpdateStatus {
  current: string
  latest: string
  available: boolean
  has_token: boolean
  repo: string
  error?: string
}

export interface SystemInfo {
  hostname: string
  os: string
  kernel: string
  uptime_seconds: number
  load_avg: string
  cpu_count: number
  cpu_usage_percent: number
  mem_total_mb: number
  mem_used_mb: number
  disk_total_gb: number
  disk_used_gb: number
  panel_version: string
}

export interface FileEntry {
  name: string
  is_dir: boolean
  size: number
  mode: string
  mod_time: string
}
