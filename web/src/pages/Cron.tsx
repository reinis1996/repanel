import { useState } from 'react'
import { Plus, Pencil, Trash2 } from 'lucide-react'
import { api, useFetch } from '../api'
import type { CronJob } from '../types'
import {
  Btn, Card, PageHeader, Table, Td, Modal, Field, Input,
  Spinner, ErrorBanner, Empty, Badge, toast,
} from '../components/ui'

const emptyJob: Partial<CronJob> = { schedule: '0 3 * * *', command: '', comment: '', enabled: true }

export default function Cron() {
  const { data, error, loading, reload } = useFetch<CronJob[]>('/api/cron')
  const [editing, setEditing] = useState<Partial<CronJob> | null>(null)
  const [busy, setBusy] = useState(false)

  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!editing) return
    setBusy(true)
    try {
      if (editing.id) {
        await api.put(`/api/cron/${editing.id}`, editing)
      } else {
        await api.post('/api/cron', editing)
      }
      toast('Task saved')
      setEditing(null)
      reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  const toggle = async (j: CronJob) => {
    try {
      await api.put(`/api/cron/${j.id}`, { ...j, enabled: !j.enabled })
      reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    }
  }

  const remove = async (j: CronJob) => {
    if (!confirm('Delete this scheduled task?')) return
    try {
      await api.del(`/api/cron/${j.id}`)
      toast('Task deleted')
      reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    }
  }

  if (loading) return <Spinner />

  return (
    <div>
      <PageHeader
        title="Scheduled Tasks"
        subtitle="Cron jobs run under your account's system user"
        actions={
          <Btn onClick={() => setEditing({ ...emptyJob })}>
            <Plus size={16} /> Add Task
          </Btn>
        }
      />
      <ErrorBanner message={error} />
      <Card>
        {!data?.length ? (
          <Empty title="No scheduled tasks" />
        ) : (
          <Table head={['Schedule', 'Command', 'Comment', 'Status', '']}>
            {data.map((j) => (
              <tr key={j.id} className="hover:bg-slate-50/60">
                <Td className="font-mono text-xs whitespace-nowrap">{j.schedule}</Td>
                <Td className="font-mono text-xs break-all max-w-md">{j.command}</Td>
                <Td className="text-slate-500">{j.comment || '—'}</Td>
                <Td>
                  <button onClick={() => toggle(j)} className="cursor-pointer">
                    {j.enabled ? <Badge color="green">enabled</Badge> : <Badge color="gray">disabled</Badge>}
                  </button>
                </Td>
                <Td className="text-right whitespace-nowrap">
                  <button className="text-slate-400 hover:text-brand-600 mr-3 cursor-pointer" onClick={() => setEditing(j)}>
                    <Pencil size={15} />
                  </button>
                  <button className="text-slate-400 hover:text-red-600 cursor-pointer" onClick={() => remove(j)}>
                    <Trash2 size={15} />
                  </button>
                </Td>
              </tr>
            ))}
          </Table>
        )}
      </Card>

      <Modal open={!!editing} title={editing?.id ? 'Edit Task' : 'Add Task'} onClose={() => setEditing(null)}>
        {editing && (
          <form onSubmit={save}>
            <Field label="Schedule" hint="Standard cron format (min hour day month weekday) or @daily, @hourly…">
              <Input
                value={editing.schedule ?? ''}
                onChange={(e) => setEditing({ ...editing, schedule: e.target.value })}
                className="font-mono"
                required
              />
            </Field>
            <Field label="Command">
              <Input
                value={editing.command ?? ''}
                onChange={(e) => setEditing({ ...editing, command: e.target.value })}
                placeholder="php /var/www/me/example.com/public_html/cron.php"
                required
              />
            </Field>
            <Field label="Comment">
              <Input value={editing.comment ?? ''} onChange={(e) => setEditing({ ...editing, comment: e.target.value })} />
            </Field>
            <div className="flex justify-end gap-2 mt-2">
              <Btn type="button" variant="secondary" onClick={() => setEditing(null)}>
                Cancel
              </Btn>
              <Btn type="submit" disabled={busy}>
                Save
              </Btn>
            </div>
          </form>
        )}
      </Modal>
    </div>
  )
}
