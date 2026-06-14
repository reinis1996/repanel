import { useEffect, useState } from 'react'
import { Play, Square, RotateCw, Download, Loader2, CheckCircle2 } from 'lucide-react'
import { api, useFetch } from '../api'
import type { ServiceStatus, PHPVersionInfo } from '../types'
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

      <PHPVersions onChanged={reload} />
    </div>
  )
}

// PHPVersions lets an admin install additional PHP-FPM versions. Newly
// installed versions become selectable per domain on the Websites page.
function PHPVersions({ onChanged }: { onChanged: () => void }) {
  const { data, error, loading, reload } = useFetch<PHPVersionInfo[]>('/api/php')
  const [busyFor, setBusyFor] = useState('')

  // Poll while an install is running so status (and the new version) appears.
  const anyInstalling = (data ?? []).some((v) => v.installing)
  useEffect(() => {
    if (!anyInstalling) return
    const t = setInterval(() => {
      reload()
      onChanged()
    }, 5000)
    return () => clearInterval(t)
  }, [anyInstalling, reload, onChanged])

  const install = async (v: PHPVersionInfo) => {
    if (!confirm(`Install PHP ${v.version}? This adds the distribution's PHP repository and may take a few minutes.`)) return
    setBusyFor(v.version)
    try {
      await api.post('/api/php/install', { version: v.version })
      toast(`Installing PHP ${v.version}…`)
      reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    } finally {
      setBusyFor('')
    }
  }

  return (
    <Card className="mt-6">
      <div className="mb-3">
        <h2 className="font-semibold text-slate-800">PHP versions</h2>
        <p className="text-sm text-slate-500">
          Install extra PHP-FPM versions so each website can pick the one it needs. New versions are
          pulled from the distribution's multi-version PHP repository (Sury on Debian, ondrej/php on Ubuntu).
        </p>
      </div>
      <ErrorBanner message={error} />
      {loading ? (
        <Spinner />
      ) : (
        <Table head={['Version', 'Status', '']}>
          {(data ?? []).map((v) => (
            <tr key={v.version} className="hover:bg-slate-50/60">
              <Td>
                <span className="font-medium">PHP {v.version}</span>
                <div className="text-xs text-slate-400 font-mono">php{v.version}-fpm</div>
              </Td>
              <Td>
                {v.installed ? (
                  <span className="inline-flex items-center gap-1 text-emerald-600 text-sm">
                    <CheckCircle2 size={14} /> installed
                  </span>
                ) : v.installing ? (
                  <span className="inline-flex items-center gap-1.5 text-blue-600 text-sm">
                    <Loader2 size={14} className="animate-spin" /> installing…
                  </span>
                ) : v.error ? (
                  <span title={v.error}>
                    <Badge color="red">install failed</Badge>
                  </span>
                ) : (
                  <Badge color="gray">available</Badge>
                )}
              </Td>
              <Td className="text-right">
                {!v.installed && !v.installing && (
                  <Btn size="sm" variant="secondary" disabled={!!busyFor} onClick={() => install(v)}>
                    <Download size={13} /> {v.error ? 'Retry' : 'Install'}
                  </Btn>
                )}
              </Td>
            </tr>
          ))}
        </Table>
      )}
    </Card>
  )
}
