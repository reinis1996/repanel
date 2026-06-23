import { useState } from 'react'
import { Plus, Trash2, Shield, ShieldOff } from 'lucide-react'
import { api, useFetch } from '../api'
import type { FirewallRule } from '../types'
import {
  Btn, Card, PageHeader, Table, Td, Modal, Field, Input, Select,
  Spinner, ErrorBanner, Empty, Badge, toast,
} from '../components/ui'

interface FirewallData {
  status: string
  rules: FirewallRule[]
  node_isolation: boolean
}

export default function Firewall() {
  const { data, error, loading, reload } = useFetch<FirewallData>('/api/firewall')
  const [addOpen, setAddOpen] = useState(false)
  const [port, setPort] = useState('')
  const [proto, setProto] = useState('tcp')
  const [source, setSource] = useState('')
  const [action, setAction] = useState('allow')
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)

  const create = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      await api.post('/api/firewall', { port, proto, source: source || 'any', action, note })
      toast('Rule added')
      setAddOpen(false)
      setPort('')
      setNote('')
      reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  const remove = async (r: FirewallRule) => {
    if (!confirm(`Delete rule for port ${r.port}/${r.proto}?`)) return
    try {
      await api.del(`/api/firewall/${r.id}`)
      toast('Rule deleted')
      reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    }
  }

  const toggle = async () => {
    const enabling = data?.status !== 'active'
    if (
      !confirm(
        enabling
          ? 'Enable the firewall? SSH and the panel port stay open automatically.'
          : 'Disable the firewall? All ports will be open.',
      )
    )
      return
    try {
      await api.post('/api/firewall/toggle', { enable: enabling })
      toast(enabling ? 'Firewall enabled' : 'Firewall disabled')
      reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    }
  }

  if (loading) return <Spinner />

  const active = data?.status === 'active'

  return (
    <div>
      <PageHeader
        title="Firewall"
        subtitle="Backed by ufw — rules apply immediately"
        actions={
          <>
            {data?.status !== 'not installed' && (
              <Btn variant={active ? 'danger' : 'secondary'} onClick={toggle}>
                {active ? <ShieldOff size={16} /> : <Shield size={16} />}
                {active ? 'Disable firewall' : 'Enable firewall'}
              </Btn>
            )}
            <Btn onClick={() => setAddOpen(true)}>
              <Plus size={16} /> Add Rule
            </Btn>
          </>
        }
      />
      <ErrorBanner message={error} />

      <div className="mb-4 flex flex-wrap items-center gap-x-6 gap-y-2">
        <span>
          Status:{' '}
          <Badge color={active ? 'green' : data?.status === 'not installed' ? 'gray' : 'amber'}>{data?.status}</Badge>
        </span>
        <span className="text-sm text-slate-600">
          Node app isolation:{' '}
          {data?.node_isolation ? (
            <Badge color="green">active</Badge>
          ) : (
            <Badge color="gray">off</Badge>
          )}
          <span className="text-xs text-slate-400 ml-1.5">
            loopback ports restricted to nginx &amp; the panel, so tenants can't reach each other's Node apps
          </span>
        </span>
      </div>

      <Card>
        {!data?.rules.length ? (
          <Empty title="No rules stored" hint="System defaults (SSH, panel port) are managed automatically" />
        ) : (
          <Table head={['Action', 'Port', 'Protocol', 'Source', 'Note', '']}>
            {data.rules.map((r) => (
              <tr key={r.id} className="hover:bg-slate-50/60">
                <Td>
                  <Badge color={r.action === 'allow' ? 'green' : 'red'}>{r.action}</Badge>
                </Td>
                <Td className="font-mono">{r.port}</Td>
                <Td className="uppercase text-slate-500">{r.proto}</Td>
                <Td className="font-mono text-slate-500">{r.source}</Td>
                <Td className="text-slate-500">{r.note || '—'}</Td>
                <Td className="text-right">
                  <Btn size="sm" variant="danger" onClick={() => remove(r)}>
                    <Trash2 size={13} /> Delete
                  </Btn>
                </Td>
              </tr>
            ))}
          </Table>
        )}
      </Card>

      <Modal open={addOpen} title="Add Firewall Rule" onClose={() => setAddOpen(false)}>
        <form onSubmit={create}>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Port" hint="Single port or range like 8000:8100">
              <Input value={port} onChange={(e) => setPort(e.target.value)} placeholder="443" required />
            </Field>
            <Field label="Protocol">
              <Select value={proto} onChange={(e) => setProto(e.target.value)}>
                <option value="tcp">TCP</option>
                <option value="udp">UDP</option>
              </Select>
            </Field>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Action">
              <Select value={action} onChange={(e) => setAction(e.target.value)}>
                <option value="allow">Allow</option>
                <option value="deny">Deny</option>
              </Select>
            </Field>
            <Field label="Source" hint="IP or CIDR; empty = any">
              <Input value={source} onChange={(e) => setSource(e.target.value)} placeholder="any" />
            </Field>
          </div>
          <Field label="Note">
            <Input value={note} onChange={(e) => setNote(e.target.value)} />
          </Field>
          <div className="flex justify-end gap-2 mt-2">
            <Btn type="button" variant="secondary" onClick={() => setAddOpen(false)}>
              Cancel
            </Btn>
            <Btn type="submit" disabled={busy}>
              Add
            </Btn>
          </div>
        </form>
      </Modal>
    </div>
  )
}
