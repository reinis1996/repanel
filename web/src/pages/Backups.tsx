import { useEffect, useState } from 'react'
import { Plus, Download, Trash2, History, Loader2 } from 'lucide-react'
import { api, useFetch, formatBytes } from '../api'
import type { Backup } from '../types'
import { useAuth } from '../App'
import { Btn, Card, PageHeader, Table, Td, Spinner, ErrorBanner, Empty, Badge, toast } from '../components/ui'

export default function Backups() {
  const { user } = useAuth()
  const { data, error, loading, reload } = useFetch<Backup[]>('/api/backups')
  const [busy, setBusy] = useState(false)

  // Poll while a backup is running so the status updates live.
  const anyRunning = (data ?? []).some((b) => b.status === 'running')
  useEffect(() => {
    if (!anyRunning) return
    const t = setInterval(reload, 5000)
    return () => clearInterval(t)
  }, [anyRunning, reload])

  const create = async () => {
    setBusy(true)
    try {
      await api.post('/api/backups', {})
      toast('Backup started')
      reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  const restore = async (b: Backup) => {
    if (
      !confirm(
        'Restore this backup? Files and databases will be OVERWRITTEN with the contents of the archive. This cannot be undone.',
      )
    )
      return
    try {
      const res = await api.post<{ message: string }>(`/api/backups/${b.id}/restore`)
      toast(res.message)
    } catch (err) {
      toast((err as Error).message, 'err')
    }
  }

  const remove = async (b: Backup) => {
    if (!confirm('Delete this backup archive?')) return
    try {
      await api.del(`/api/backups/${b.id}`)
      toast('Backup deleted')
      reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    }
  }

  if (loading) return <Spinner />

  return (
    <div>
      <PageHeader
        title="Backups"
        subtitle="Full account archives: web files, mail and database dumps. Enable nightly backups under Settings."
        actions={
          <Btn onClick={create} disabled={busy || anyRunning}>
            {anyRunning ? <Loader2 size={16} className="animate-spin" /> : <Plus size={16} />}
            {anyRunning ? 'Backup running…' : 'Back Up Now'}
          </Btn>
        }
      />
      <ErrorBanner message={error} />
      <Card>
        {!data?.length ? (
          <Empty title="No backups yet" hint="Create your first backup — it includes files, mail and databases" />
        ) : (
          <Table head={['Created', user!.role !== 'user' ? 'Account' : '', 'Size', 'Status', ''].filter(Boolean) as string[]}>
            {data.map((b) => (
              <tr key={b.id} className="hover:bg-slate-50/60">
                <Td className="whitespace-nowrap">{new Date(b.created_at).toLocaleString()}</Td>
                {user!.role !== 'user' && <Td className="text-slate-600">{b.owner}</Td>}
                <Td className="text-slate-500">{b.size_bytes ? formatBytes(b.size_bytes) : '—'}</Td>
                <Td>
                  {b.status === 'running' ? (
                    <Badge color="blue">running</Badge>
                  ) : b.status === 'completed' ? (
                    <Badge color="green">completed</Badge>
                  ) : (
                    <span title={b.error}>
                      <Badge color="red">failed</Badge>
                    </span>
                  )}
                  {b.status === 'failed' && b.error && (
                    <div className="text-xs text-red-500 mt-0.5 max-w-md break-all">{b.error}</div>
                  )}
                </Td>
                <Td className="text-right whitespace-nowrap">
                  {b.status === 'completed' && (
                    <>
                      <a href={`/api/backups/${b.id}/download`} className="inline-block align-middle mr-2">
                        <Btn size="sm" variant="secondary">
                          <Download size={13} /> Download
                        </Btn>
                      </a>
                      <Btn size="sm" variant="secondary" className="mr-2" onClick={() => restore(b)}>
                        <History size={13} /> Restore
                      </Btn>
                    </>
                  )}
                  {b.status !== 'running' && (
                    <Btn size="sm" variant="danger" onClick={() => remove(b)}>
                      <Trash2 size={13} /> Delete
                    </Btn>
                  )}
                </Td>
              </tr>
            ))}
          </Table>
        )}
      </Card>
    </div>
  )
}
