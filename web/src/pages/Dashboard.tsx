import { useState } from 'react'
import { Globe, Mail, Database, Server, Users } from 'lucide-react'
import { useFetch, formatUptime } from '../api'
import type { SystemInfo, ServiceStatus, Usage, UpdateStatus, MetricsResp } from '../types'
import { Card, PageHeader, Spinner, ErrorBanner, Meter, Badge } from '../components/ui'
import { useAuth } from '../App'

interface DashboardData {
  system?: SystemInfo
  domains: number
  mailboxes: number
  databases: number
  ftp: number
  users: number
  services?: ServiceStatus[]
}

export default function Dashboard() {
  const { user } = useAuth()
  const isAdmin = user!.role === 'admin'
  const { data, error, loading } = useFetch<DashboardData>('/api/dashboard')
  const update = useFetch<UpdateStatus>(isAdmin ? '/api/update' : '/api/me')
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
      <PageHeader title="Dashboard" subtitle={sys ? `${sys.hostname} — ${sys.os}` : 'Overview of your hosting account'} />

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
        {isAdmin && sys && (
          <Card title="Server health">
            <div className="space-y-4">
              <Meter label={`CPU (${sys.cpu_count} cores)`} used={sys.cpu_usage_percent} total={100} unit="%" />
              <Meter label="Memory" used={sys.mem_used_mb / 1024} total={sys.mem_total_mb / 1024 || 1} unit="GB" />
              <Meter label="Disk /" used={sys.disk_used_gb} total={sys.disk_total_gb || 1} unit="GB" />
              <div className="grid grid-cols-2 gap-3 text-sm pt-2 border-t border-slate-100">
                <Info label="Uptime" value={sys.uptime_seconds ? formatUptime(sys.uptime_seconds) : '—'} />
                <Info label="Load average" value={sys.load_avg || '—'} />
                <Info label="Kernel" value={sys.kernel || '—'} />
                <div>
                  <div className="text-xs text-slate-400">Panel version</div>
                  <div className="text-slate-700">
                    {sys.panel_version}
                    {update.data?.available && (
                      <span className="ml-2 text-xs text-amber-700">update {update.data.latest} available</span>
                    )}
                  </div>
                </div>
              </div>
            </div>
          </Card>
        )}

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

        {isAdmin && data.services && (
          <Card title="Services">
            <div className="divide-y divide-slate-100 -my-2">
              {data.services.map((s) => (
                <div key={s.name} className="flex items-center justify-between py-2">
                  <span className="text-sm text-slate-700">
                    {s.display_name}
                    {s.version && <span className="ml-1.5 text-xs text-slate-400 font-mono">v{s.version}</span>}
                  </span>
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

      {isAdmin && <ResourceHistory />}
    </div>
  )
}

const RANGES = [
  { label: '24 hours', hours: 24 },
  { label: '7 days', hours: 168 },
  { label: '30 days', hours: 720 },
]

function ResourceHistory() {
  const [hours, setHours] = useState(24)
  const { data, loading } = useFetch<MetricsResp>(`/api/metrics?hours=${hours}`)

  return (
    <Card
      title="Resource history"
      className="mt-4"
      actions={
        <div className="flex gap-1">
          {RANGES.map((r) => (
            <button
              key={r.hours}
              onClick={() => setHours(r.hours)}
              className={`text-xs px-2 py-1 rounded cursor-pointer ${hours === r.hours ? 'bg-brand-600 text-white' : 'text-slate-500 hover:bg-slate-100'}`}
            >
              {r.label}
            </button>
          ))}
        </div>
      }
    >
      {loading ? (
        <Spinner />
      ) : !data?.samples.length ? (
        <p className="text-sm text-slate-400">No samples yet — metrics are recorded every 5 minutes.</p>
      ) : (
        <div className="grid md:grid-cols-3 gap-4">
          <Sparkline label="CPU" unit="%" values={data.samples.map((s) => s.cpu)} color="bg-brand-500" />
          <Sparkline label="Memory" unit="%" values={data.samples.map((s) => s.mem)} color="bg-emerald-500" />
          <Sparkline label="Disk" unit="%" values={data.samples.map((s) => s.disk)} color="bg-amber-500" />
        </div>
      )}
      {data && data.traffic.length > 0 && (
        <div className="mt-5 pt-4 border-t border-slate-100">
          <Sparkline
            label="Daily traffic"
            unit=" MB"
            values={data.traffic.map((t) => t.mb)}
            color="bg-blue-500"
            fixed={0}
          />
        </div>
      )}
    </Card>
  )
}

function Sparkline({ label, unit, values, color, fixed = 1 }: { label: string; unit: string; values: number[]; color: string; fixed?: number }) {
  const peak = Math.max(1, ...values)
  const latest = values[values.length - 1] ?? 0
  return (
    <div>
      <div className="flex items-baseline justify-between mb-1">
        <span className="text-xs font-medium text-slate-500">{label}</span>
        <span className="text-sm font-semibold text-slate-700">{latest.toFixed(fixed)}{unit}</span>
      </div>
      <div className="flex items-end gap-px h-16 overflow-hidden">
        {values.map((v, i) => (
          <div
            key={i}
            className={`flex-1 ${color} opacity-80 hover:opacity-100 rounded-t`}
            style={{ height: `${Math.max(2, (v / peak) * 100)}%` }}
            title={`${v.toFixed(fixed)}${unit}`}
          />
        ))}
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
