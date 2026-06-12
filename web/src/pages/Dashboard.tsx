import { Globe, Mail, Database, Server, Users } from 'lucide-react'
import { useFetch, formatUptime } from '../api'
import type { SystemInfo, ServiceStatus, Usage } from '../types'
import { Card, PageHeader, Spinner, ErrorBanner, Meter, Badge } from '../components/ui'
import { useAuth } from '../App'

interface DashboardData {
  system: SystemInfo
  domains: number
  mailboxes: number
  databases: number
  ftp: number
  users: number
  services?: ServiceStatus[]
}

export default function Dashboard() {
  const { user } = useAuth()
  const { data, error, loading } = useFetch<DashboardData>('/api/dashboard')
  const usage = useFetch<Usage[]>('/api/usage')
  const myUsage = (usage.data ?? []).find((x) => x.user_id === user!.id)

  if (loading) return <Spinner />
  if (error) return <ErrorBanner message={error} />
  if (!data) return null

  const sys = data.system
  const stats = [
    { label: 'Domains', value: data.domains, icon: Globe },
    { label: 'Mailboxes', value: data.mailboxes, icon: Mail },
    { label: 'Databases', value: data.databases, icon: Database },
    { label: 'FTP accounts', value: data.ftp, icon: Server },
    ...(user!.role !== 'user' ? [{ label: 'Users', value: data.users, icon: Users }] : []),
  ]

  return (
    <div>
      <PageHeader title="Dashboard" subtitle={`${sys.hostname} — ${sys.os}`} />

      <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-5 gap-4 mb-6">
        {stats.map(({ label, value, icon: Icon }) => (
          <div key={label} className="bg-white rounded-lg border border-slate-200 shadow-sm p-4 flex items-center gap-3">
            <div className="h-10 w-10 rounded-lg bg-brand-50 text-brand-600 flex items-center justify-center">
              <Icon size={20} strokeWidth={1.8} />
            </div>
            <div>
              <div className="text-2xl font-semibold text-slate-800 leading-none">{value}</div>
              <div className="text-xs text-slate-500 mt-1">{label}</div>
            </div>
          </div>
        ))}
      </div>

      <div className="grid lg:grid-cols-2 gap-4">
        <Card title="Server health">
          <div className="space-y-4">
            <Meter label={`CPU (${sys.cpu_count} cores)`} used={sys.cpu_usage_percent} total={100} unit="%" />
            <Meter label="Memory" used={sys.mem_used_mb / 1024} total={sys.mem_total_mb / 1024 || 1} unit="GB" />
            <Meter label="Disk /" used={sys.disk_used_gb} total={sys.disk_total_gb || 1} unit="GB" />
            <div className="grid grid-cols-2 gap-3 text-sm pt-2 border-t border-slate-100">
              <Info label="Uptime" value={sys.uptime_seconds ? formatUptime(sys.uptime_seconds) : '—'} />
              <Info label="Load average" value={sys.load_avg || '—'} />
              <Info label="Kernel" value={sys.kernel || '—'} />
              <Info label="Panel version" value={sys.panel_version} />
            </div>
          </div>
        </Card>

        {myUsage && (
          <Card title="My disk usage">
            <div className="space-y-4">
              <Meter
                label={myUsage.disk_quota_mb > 0 ? 'Total (quota)' : 'Total (no quota)'}
                used={myUsage.total_mb}
                total={myUsage.disk_quota_mb > 0 ? myUsage.disk_quota_mb : Math.max(myUsage.total_mb, 1)}
                unit="MB"
              />
              <div className="grid grid-cols-3 gap-3 text-sm pt-2 border-t border-slate-100">
                <Info label="Web files" value={`${myUsage.web_mb.toFixed(1)} MB`} />
                <Info label="Mail" value={`${myUsage.mail_mb.toFixed(1)} MB`} />
                <Info label="Databases" value={`${myUsage.db_mb.toFixed(1)} MB`} />
              </div>
            </div>
          </Card>
        )}

        {data.services && (
          <Card title="Services">
            <div className="divide-y divide-slate-100 -my-2">
              {data.services.map((s) => (
                <div key={s.name} className="flex items-center justify-between py-2">
                  <span className="text-sm text-slate-700">{s.display_name}</span>
                  {!s.installed ? (
                    <Badge color="gray">not installed</Badge>
                  ) : s.active ? (
                    <Badge color="green">running</Badge>
                  ) : (
                    <Badge color="red">stopped</Badge>
                  )}
                </div>
              ))}
            </div>
          </Card>
        )}
      </div>
    </div>
  )
}

function Info({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <div className="text-xs text-slate-400">{label}</div>
      <div className="text-slate-700">{value}</div>
    </div>
  )
}
