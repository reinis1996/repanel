import { useState } from 'react'
import { Plus, Pencil, Trash2, Package } from 'lucide-react'
import { api, useFetch } from '../api'
import type { Plan } from '../types'
import { MODULES } from '../types'
import {
  Btn, Card, PageHeader, Table, Td, Modal, Field, Input,
  Spinner, ErrorBanner, Empty, Badge, toast, ModuleChecklist,
} from '../components/ui'

const blankPlan: Plan = {
  id: 0, name: '', disk_quota_mb: 0, bandwidth_quota_mb: 0,
  max_domains: 0, max_mailboxes: 0, max_databases: 0, modules: [], created_at: '',
}

const lim = (n: number) => (n > 0 ? n.toLocaleString() : '∞')

export default function Plans() {
  const { data, error, loading, reload } = useFetch<Plan[]>('/api/plans')
  const [editing, setEditing] = useState<Plan | null>(null)
  const [busy, setBusy] = useState(false)

  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!editing) return
    setBusy(true)
    try {
      if (editing.id) await api.put(`/api/plans/${editing.id}`, editing)
      else await api.post('/api/plans', editing)
      toast(`Plan ${editing.name} saved`)
      setEditing(null)
      reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  const remove = async (p: Plan) => {
    if (!confirm(`Delete plan "${p.name}"? Accounts on it keep their current limits but are detached.`)) return
    try {
      await api.del(`/api/plans/${p.id}`)
      toast(`Plan ${p.name} deleted`)
      reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    }
  }

  if (loading) return <Spinner />

  return (
    <div>
      <PageHeader
        title="Hosting Plans"
        subtitle="Templates of resource limits and modules — assign them to accounts on the Users page"
        actions={
          <Btn onClick={() => setEditing({ ...blankPlan })}>
            <Plus size={16} /> Add Plan
          </Btn>
        }
      />
      <ErrorBanner message={error} />
      <Card>
        {!data?.length ? (
          <Empty title="No plans yet" hint="Create a plan, then assign it when creating or editing a user." />
        ) : (
          <Table head={['Plan', 'Disk', 'Bandwidth', 'Domains', 'Mailboxes', 'Databases', 'Modules', '']}>
            {data.map((p) => (
              <tr key={p.id} className="hover:bg-slate-50/60">
                <Td className="font-medium">{p.name}</Td>
                <Td className="text-slate-600">{p.disk_quota_mb > 0 ? `${p.disk_quota_mb} MB` : '∞'}</Td>
                <Td className="text-slate-600">{p.bandwidth_quota_mb > 0 ? `${p.bandwidth_quota_mb} MB` : '∞'}</Td>
                <Td className="text-slate-600">{lim(p.max_domains)}</Td>
                <Td className="text-slate-600">{lim(p.max_mailboxes)}</Td>
                <Td className="text-slate-600">{lim(p.max_databases)}</Td>
                <Td><Badge color="blue">{p.modules.length} modules</Badge></Td>
                <Td className="text-right whitespace-nowrap">
                  <button className="text-slate-400 hover:text-brand-600 mr-3 cursor-pointer" onClick={() => setEditing(p)}>
                    <Pencil size={15} />
                  </button>
                  <button className="text-slate-400 hover:text-red-600 cursor-pointer" onClick={() => remove(p)}>
                    <Trash2 size={15} />
                  </button>
                </Td>
              </tr>
            ))}
          </Table>
        )}
      </Card>

      <Modal open={!!editing} title={editing?.id ? `Edit plan — ${editing.name}` : 'Add plan'} onClose={() => setEditing(null)} wide>
        {editing && (
          <form onSubmit={save}>
            <Field label="Plan name">
              <Input value={editing.name} onChange={(e) => setEditing({ ...editing, name: e.target.value })} placeholder="Starter" required />
            </Field>
            <p className="text-xs text-slate-400 mb-3 flex items-center gap-1.5">
              <Package size={13} /> All limits use 0 = unlimited.
            </p>
            <div className="grid grid-cols-2 gap-3">
              <Field label="Disk quota (MB)">
                <Input type="number" min={0} value={editing.disk_quota_mb} onChange={(e) => setEditing({ ...editing, disk_quota_mb: Number(e.target.value) })} />
              </Field>
              <Field label="Bandwidth (MB/mo)">
                <Input type="number" min={0} value={editing.bandwidth_quota_mb} onChange={(e) => setEditing({ ...editing, bandwidth_quota_mb: Number(e.target.value) })} />
              </Field>
              <Field label="Max domains">
                <Input type="number" min={0} value={editing.max_domains} onChange={(e) => setEditing({ ...editing, max_domains: Number(e.target.value) })} />
              </Field>
              <Field label="Max mailboxes">
                <Input type="number" min={0} value={editing.max_mailboxes} onChange={(e) => setEditing({ ...editing, max_mailboxes: Number(e.target.value) })} />
              </Field>
              <Field label="Max databases">
                <Input type="number" min={0} value={editing.max_databases} onChange={(e) => setEditing({ ...editing, max_databases: Number(e.target.value) })} />
              </Field>
            </div>
            <Field label="Modules" hint="Which sections accounts on this plan can use">
              <ModuleChecklist all={MODULES} selected={editing.modules} onChange={(m) => setEditing({ ...editing, modules: m })} />
            </Field>
            <div className="flex justify-end gap-2 mt-2">
              <Btn type="button" variant="secondary" onClick={() => setEditing(null)}>Cancel</Btn>
              <Btn type="submit" disabled={busy}>{busy ? 'Saving…' : 'Save'}</Btn>
            </div>
          </form>
        )}
      </Modal>
    </div>
  )
}
