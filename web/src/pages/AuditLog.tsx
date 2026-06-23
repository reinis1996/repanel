import { useState } from 'react'
import { ScrollText } from 'lucide-react'
import { useFetch } from '../api'
import type { AuditEntry } from '../types'
import { Card, PageHeader, Table, Td, Spinner, ErrorBanner, Empty, Badge, inputCls } from '../components/ui'

export default function AuditLog() {
  const [q, setQ] = useState('')
  const { data, error, loading } = useFetch<AuditEntry[]>(`/api/audit?limit=300${q ? `&q=${encodeURIComponent(q)}` : ''}`)

  const color = (action: string) =>
    action.startsWith('login.failed') ? 'red'
      : action.startsWith('login') || action.startsWith('impersonate') ? 'amber'
      : action.startsWith('DELETE') ? 'red'
      : 'gray'

  return (
    <div>
      <PageHeader
        title="Audit Log"
        subtitle="Authenticated actions and security events across the panel."
        actions={
          <input
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder="filter user / action / IP…"
            className={`${inputCls} w-64`}
          />
        }
      />
      <ErrorBanner message={error} />
      <Card>
        {loading ? (
          <Spinner />
        ) : !data?.length ? (
          <Empty title="No audit entries" hint="Actions appear here as users and admins make changes." />
        ) : (
          <Table head={['When', 'User', 'Action', 'Detail', 'IP']}>
            {data.map((e) => (
              <tr key={e.id} className="hover:bg-slate-50/60">
                <Td className="whitespace-nowrap text-slate-500">{new Date(e.created_at).toLocaleString()}</Td>
                <Td className="font-medium text-slate-700">{e.username || '—'}</Td>
                <Td>
                  <Badge color={color(e.action) as 'red' | 'amber' | 'gray'}>{e.action}</Badge>
                </Td>
                <Td className="text-slate-500 break-all">{e.detail || '—'}</Td>
                <Td className="font-mono text-xs text-slate-500">{e.ip}</Td>
              </tr>
            ))}
          </Table>
        )}
      </Card>
      <p className="mt-3 text-xs text-slate-400 flex items-center gap-1.5">
        <ScrollText size={13} /> Entries are retained for 180 days.
      </p>
    </div>
  )
}
