import { useState } from 'react'
import { Play, Square, RotateCw } from 'lucide-react'
import { api, useFetch } from '../api'
import type { ServiceStatus } from '../types'
import { Btn, Card, PageHeader, Table, Td, Spinner, ErrorBanner, Badge, toast } from '../components/ui'

export default function Services() {
  const { data, error, loading, reload } = useFetch<ServiceStatus[]>('/api/services')
  const [busyFor, setBusyFor] = useState('')

  const act = async (s: ServiceStatus, action: string) => {
    if (action === 'stop' && !confirm(`Stop ${s.display_name}?`)) return
    setBusyFor(s.name + action)
    try {
      await api.post(`/api/services/${s.name}/${action}`)
      toast(`${s.display_name}: ${action} OK`)
      reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    } finally {
      setBusyFor('')
    }
  }

  if (loading) return <Spinner />

  return (
    <div>
      <PageHeader title="Services" subtitle="Status of every component managed by the panel" />
      <ErrorBanner message={error} />
      <Card>
        <Table head={['Service', 'Description', 'Status', 'Autostart', '']}>
          {(data ?? []).map((s) => (
            <tr key={s.name} className="hover:bg-slate-50/60">
              <Td>
                <span className="font-medium">{s.display_name}</span>
                <div className="text-xs text-slate-400 font-mono">{s.name}</div>
              </Td>
              <Td className="text-slate-500">{s.description}</Td>
              <Td>
                {!s.installed ? (
                  <Badge color="gray">not installed</Badge>
                ) : s.active ? (
                  <Badge color="green">running</Badge>
                ) : (
                  <Badge color="red">stopped</Badge>
                )}
              </Td>
              <Td>{s.installed ? (s.enabled ? <Badge color="blue">enabled</Badge> : <Badge color="gray">disabled</Badge>) : '—'}</Td>
              <Td className="text-right whitespace-nowrap">
                {s.installed && s.name !== 'repanel' && (
                  <>
                    {!s.active ? (
                      <Btn size="sm" variant="secondary" className="mr-2" disabled={!!busyFor} onClick={() => act(s, 'start')}>
                        <Play size={13} /> Start
                      </Btn>
                    ) : (
                      <>
                        <Btn size="sm" variant="secondary" className="mr-2" disabled={!!busyFor} onClick={() => act(s, 'restart')}>
                          <RotateCw size={13} /> Restart
                        </Btn>
                        <Btn size="sm" variant="danger" disabled={!!busyFor} onClick={() => act(s, 'stop')}>
                          <Square size={13} /> Stop
                        </Btn>
                      </>
                    )}
                  </>
                )}
              </Td>
            </tr>
          ))}
        </Table>
      </Card>
    </div>
  )
}
