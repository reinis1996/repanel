export type Role = 'admin' | 'reseller' | 'user'

export interface User {
  id: number
  username: string
  email: string
  role: Role
  owner_id: number
  suspended: boolean
  disk_quota_mb: number
  created_at: string
}

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

export interface Usage {
  user_id: number
  username: string
  web_mb: number
  mail_mb: number
  db_mb: number
  total_mb: number
  disk_quota_mb: number
}

export interface APIToken {
  id: number
  user_id: number
  name: string
  prefix: string
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

export interface Domain {
  id: number
  user_id: number
  name: string
  document_root: string
  php_version: string
  ssl: boolean
  suspended: boolean
  created_at: string
  owner?: string
}

export interface App {
  id: number
  domain_id: number
  app: string
  status: 'installing' | 'installed' | 'failed'
  error: string
  url: string
  db_name: string
  auto_setup: boolean
  created_at: string
  domain?: string
}

export interface DNSZone {
  id: number
  domain_id: number
  name: string
  serial: number
  records?: DNSRecord[]
  created_at: string
}

export interface DNSRecord {
  id: number
  zone_id: number
  name: string
  type: string
  value: string
  ttl: number
  priority: number
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

export interface Certificate {
  id: number
  domain_id: number
  domain: string
  issuer: string
  not_after: string
  created_at: string
}

export interface ServiceStatus {
  name: string
  display_name: string
  description: string
  installed: boolean
  active: boolean
  enabled: boolean
}

export interface FirewallRule {
  id: number
  port: string
  proto: string
  source: string
  action: string
  note: string
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
