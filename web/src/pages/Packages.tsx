import { useEffect, useRef, useState } from 'react'
import { RefreshCw, DownloadCloud, ShieldAlert, CheckCircle2, Loader2 } from 'lucide-react'
import { api, useFetch } from '../api'
import type { PackageList, PackageJob } from '../types'
import { Btn, Card, PageHeader, Table, Td, Spinner, ErrorBanner, Empty, Badge, toast } from '../components/ui'

export default function Packages() {
  const { data, error, loading, reload } = useFetch<PackageList>('/api/packages')
  const [checking, setChecking] = useState(false)
  const [job, setJob] = useState<PackageJob | null>(null)
  const [running, setRunning] = useState(false)
  const logRef = useRef<HTMLPreElement>(null)

  // Resume showing a job that's already running (e.g. after navigating away and
  // back) so the live log isn't lost.
  useEffect(() => {
    api
      .get<PackageJob>('/api/packages/job')
      .then((j) => {
        if (j.started && (j.running || j.output)) {
          setJob(j)
          if (j.running) setRunning(true)
        }
      })
      .catch(() => {})
  }, [])

  // Poll the upgrade job while it runs; refresh the package list when it finishes.
  useEffect(() => {
    if (!running) return
    const t = setInterval(async () => {
      try {
        const j = await api.get<PackageJob>('/api/packages/job')
        setJob(j)
        if (j.done) {
          setRunning(false)
          reload()
          toast(j.failed ? 'Update finished with errors' : 'Packages updated', j.failed ? 'err' : 'ok')
        }
      } catch {
        /* transient */
      }
    }, 2000)
    return () => clearInterval(t)
  }, [running, reload])

  // Keep the log scrolled to the bottom as output streams in.
  useEffect(() => {
    if (logRef.current) logRef.current.scrollTop = logRef.current.scrollHeight
  }, [job?.output])

  const check = async () => {
    setChecking(true)
    try {
      await reloadWithRefresh()
      toast('Checked for package updates')
    } catch (e) {
      toast((e as Error).message, 'err')
    } finally {
      setChecking(false)
    }
  }

  // reload but force apt to refresh its lists first.
  const reloadWithRefresh = () =>
    api.get<PackageList>('/api/packages?refresh=1').then(() => reload())

  const upgrade = async () => {
    if (!confirm('Update all packages now? The server will download and install the latest versions.')) return
    try {
      await api.post('/api/packages/upgrade')
      setJob({ started: true, running: true, done: false, failed: false, error: '', output: '' })
      setRunning(true)
      toast('Updating packages — watch the log below')
    } catch (e) {
      toast((e as Error).message, 'err')
    }
  }

  if (loading) return <Spinner />

  const security = data?.security ?? 0
  const total = data?.total ?? 0

  return (
    <div>
      <PageHeader
        title="Package Updates"
        subtitle="Operating-system package updates from the distribution (apt)"
        actions={
          <>
            <Btn variant="secondary" onClick={check} disabled={checking || running}>
              {checking ? <Loader2 size={15} className="animate-spin" /> : <RefreshCw size={15} />} Check for updates
            </Btn>
            {total > 0 && (
              <Btn onClick={upgrade} disabled={running}>
                {running ? <Loader2 size={15} className="animate-spin" /> : <DownloadCloud size={15} />}
                {running ? 'Updating…' : 'Update all'}
              </Btn>
            )}
          </>
        }
      />
      <ErrorBanner message={error} />

      {data && !data.available ? (
        <Card>
          <div className="rounded-md bg-amber-50 border border-amber-200 text-amber-800 text-sm px-4 py-3">
            Package updates are managed with apt, which is not available on this host.
          </div>
        </Card>
      ) : (
        <>
          <div className="grid grid-cols-2 gap-4 max-w-lg mb-4">
            <SummaryCard
              label="Updates available"
              value={total}
              tone={total > 0 ? 'amber' : 'green'}
              icon={total > 0 ? <DownloadCloud size={20} /> : <CheckCircle2 size={20} />}
            />
            <SummaryCard
              label="Security updates"
              value={security}
              tone={security > 0 ? 'red' : 'green'}
              icon={<ShieldAlert size={20} />}
            />
          </div>

          {running || job?.output ? (
            <Card title="Update log" className="mb-4">
              <pre
                ref={logRef}
                className="max-h-96 overflow-auto rounded-md bg-slate-900 text-slate-100 text-xs font-mono p-3 whitespace-pre-wrap"
              >
                {job?.output?.trim() || 'Starting…'}
              </pre>
              {job?.failed && job.error && <ErrorBanner message={job.error} />}
              {job?.done && !job.failed && (
                <p className="mt-2 inline-flex items-center gap-1.5 text-sm text-emerald-600">
                  <CheckCircle2 size={15} /> Update complete.
                </p>
              )}
            </Card>
          ) : null}

          <Card title={total > 0 ? `${total} package${total === 1 ? '' : 's'} can be updated` : 'Packages'}>
            {!data?.updates.length ? (
              <Empty title="Everything is up to date" hint="No package updates are pending." />
            ) : (
              <Table head={['Package', 'Installed', 'Available', '']}>
                {data.updates.map((p) => (
                  <tr key={p.name} className="hover:bg-slate-50/60">
                    <Td className="font-medium font-mono text-xs">{p.name}</Td>
                    <Td className="text-slate-500 font-mono text-xs">{p.current_version || '—'}</Td>
                    <Td className="text-slate-700 font-mono text-xs">{p.new_version}</Td>
                    <Td className="text-right">{p.security && <Badge color="red">security</Badge>}</Td>
                  </tr>
                ))}
              </Table>
            )}
          </Card>
        </>
      )}
    </div>
  )
}

function SummaryCard({
  label,
  value,
  tone,
  icon,
}: {
  label: string
  value: number
  tone: 'green' | 'amber' | 'red'
  icon: React.ReactNode
}) {
  const tones = {
    green: 'text-emerald-600',
    amber: 'text-amber-600',
    red: 'text-red-600',
  }
  return (
    <div className="bg-white rounded-lg border border-slate-200 shadow-sm p-4 flex items-center gap-3">
      <span className={tones[tone]}>{icon}</span>
      <div>
        <div className="text-2xl font-semibold text-slate-800">{value}</div>
        <div className="text-xs text-slate-400">{label}</div>
      </div>
    </div>
  )
}
