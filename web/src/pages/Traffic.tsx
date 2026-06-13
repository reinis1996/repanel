import { useState } from 'react'
import { Activity } from 'lucide-react'
import { api, useFetch } from '../api'
import type { TrafficStat } from '../types'
import { useAuth } from '../App'
import { Card, PageHeader, Spinner, ErrorBanner, Empty, Table, Td, inputCls } from '../components/ui'

function formatMB(mb: number): string {
  if (mb >= 1024) return `${(mb / 1024).toFixed(2)} GB`
  if (mb >= 1) return `${mb.toFixed(1)} MB`
  if (mb > 0) return `${(mb * 1024).toFixed(0)} KB`
  return '0'
}

export default function Traffic() {
  const { user } = useAuth()
  const [days, setDays] = useState(30)
  const { data, error, loading } = useFetch<TrafficStat[]>(`/api/traffic?days=${days}`)

  if (loading) return <Spinner />

  const accounts = (data ?? []).filter((a) => user!.role !== 'user' || a.user_id === user!.id)
  const anyTraffic = accounts.some((a) => a.total_mb > 0)

  return (
    <div>
      <PageHeader
        title="Traffic"
        subtitle="Bandwidth served by your websites, tallied from the nginx access logs."
        actions={
          <select
            value={days}
            onChange={(e) => setDays(Number(e.target.value))}
            className={`${inputCls} w-auto`}
          >
            <option value={7}>Last 7 days</option>
            <option value={30}>Last 30 days</option>
            <option value={90}>Last 90 days</option>
            <option value={365}>Last year</option>
          </select>
        }
      />
      <ErrorBanner message={error} />

      {!accounts.length ? (
        <Card>
          <Empty title="No accounts to report" />
        </Card>
      ) : !anyTraffic ? (
        <Card>
          <Empty
            title="No traffic recorded yet"
            hint="Counters update hourly from the web server logs — check back once your sites have visitors."
          />
        </Card>
      ) : (
        <div className="space-y-5">
          {accounts.map((a) => (
            <AccountTraffic key={a.user_id} stat={a} showOwner={user!.role !== 'user'} />
          ))}
        </div>
      )}
    </div>
  )
}

function AccountTraffic({ stat, showOwner }: { stat: TrafficStat; showOwner: boolean }) {
  const peak = Math.max(1, ...stat.series.map((d) => d.mb))
  return (
    <Card
      title={
        <span className="flex items-center gap-2">
          <Activity size={15} className="text-brand-600" />
          {showOwner ? stat.username : 'My traffic'}
          <span className="text-slate-400 font-normal">— {formatMB(stat.total_mb)} total</span>
        </span>
      }
    >
      {stat.series.length > 0 && (
        <div className="mb-5">
          <div className="flex items-end gap-0.5 h-24">
            {stat.series.map((d) => (
              <div
                key={d.day}
                className="flex-1 bg-brand-500/80 hover:bg-brand-600 rounded-t min-w-[2px] transition-colors"
                style={{ height: `${Math.max(2, (d.mb / peak) * 100)}%` }}
                title={`${d.day}: ${formatMB(d.mb)}`}
              />
            ))}
          </div>
          <div className="flex justify-between text-[11px] text-slate-400 mt-1.5">
            <span>{stat.series[0]?.day}</span>
            <span>{stat.series[stat.series.length - 1]?.day}</span>
          </div>
        </div>
      )}

      {stat.domains.length > 0 ? (
        <Table head={['Domain', 'Bandwidth']}>
          {stat.domains.map((d) => (
            <tr key={d.domain} className="hover:bg-slate-50/60">
              <Td className="text-slate-700">{d.domain}</Td>
              <Td className="text-slate-500">{formatMB(d.mb)}</Td>
            </tr>
          ))}
        </Table>
      ) : (
        <p className="text-sm text-slate-400">No domains.</p>
      )}
    </Card>
  )
}
