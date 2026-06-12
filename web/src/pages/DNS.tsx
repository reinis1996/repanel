import { useState } from 'react'
import { Plus, Pencil, Trash2, ArrowLeft } from 'lucide-react'
import { api, useFetch } from '../api'
import type { DNSZone, DNSRecord } from '../types'
import {
  Btn, Card, PageHeader, Table, Td, Modal, Field, Input, Select,
  Spinner, ErrorBanner, Empty, toast,
} from '../components/ui'

const RECORD_TYPES = ['A', 'AAAA', 'CNAME', 'MX', 'TXT', 'NS', 'SRV', 'CAA']

export default function DNS() {
  const [zoneId, setZoneId] = useState<number | null>(null)
  return zoneId === null ? <ZoneList onOpen={setZoneId} /> : <ZoneEditor zoneId={zoneId} onBack={() => setZoneId(null)} />
}

function ZoneList({ onOpen }: { onOpen: (id: number) => void }) {
  const { data, error, loading } = useFetch<DNSZone[]>('/api/dns')
  if (loading) return <Spinner />
  return (
    <div>
      <PageHeader title="DNS" subtitle="Zones are created together with domains and served by BIND" />
      <ErrorBanner message={error} />
      <Card>
        {!data?.length ? (
          <Empty title="No DNS zones" hint="Create a domain with 'Create DNS zone' checked" />
        ) : (
          <Table head={['Zone', 'Serial', '']}>
            {data.map((z) => (
              <tr key={z.id} className="hover:bg-slate-50/60">
                <Td>
                  <button className="font-medium text-brand-600 hover:underline cursor-pointer" onClick={() => onOpen(z.id)}>
                    {z.name}
                  </button>
                </Td>
                <Td className="text-slate-500">{z.serial}</Td>
                <Td className="text-right">
                  <Btn size="sm" variant="secondary" onClick={() => onOpen(z.id)}>
                    Manage records
                  </Btn>
                </Td>
              </tr>
            ))}
          </Table>
        )}
      </Card>
    </div>
  )
}

const emptyRecord = { name: '', type: 'A', value: '', ttl: 3600, priority: 0 }

function ZoneEditor({ zoneId, onBack }: { zoneId: number; onBack: () => void }) {
  const { data, error, loading, reload } = useFetch<DNSZone>(`/api/dns/${zoneId}`)
  const [editing, setEditing] = useState<Partial<DNSRecord> | null>(null)
  const [busy, setBusy] = useState(false)

  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!editing) return
    setBusy(true)
    try {
      if (editing.id) {
        await api.put(`/api/dns/records/${editing.id}`, editing)
      } else {
        await api.post(`/api/dns/${zoneId}/records`, editing)
      }
      toast('Record saved')
      setEditing(null)
      reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  const remove = async (r: DNSRecord) => {
    if (!confirm(`Delete ${r.type} record "${r.name || '@'}"?`)) return
    try {
      await api.del(`/api/dns/records/${r.id}`)
      toast('Record deleted')
      reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    }
  }

  if (loading) return <Spinner />

  return (
    <div>
      <PageHeader
        title={data?.name ?? 'Zone'}
        subtitle="Changes are written to the BIND zone file immediately"
        actions={
          <>
            <Btn variant="secondary" onClick={onBack}>
              <ArrowLeft size={16} /> All zones
            </Btn>
            <Btn onClick={() => setEditing({ ...emptyRecord })}>
              <Plus size={16} /> Add Record
            </Btn>
          </>
        }
      />
      <ErrorBanner message={error} />
      <Card>
        {!data?.records?.length ? (
          <Empty title="No records" />
        ) : (
          <Table head={['Name', 'Type', 'Value', 'TTL', 'Priority', '']}>
            {data.records.map((r) => (
              <tr key={r.id} className="hover:bg-slate-50/60">
                <Td className="font-mono text-xs">{r.name || '@'}</Td>
                <Td>
                  <span className="text-xs font-semibold bg-slate-100 rounded px-1.5 py-0.5">{r.type}</span>
                </Td>
                <Td className="font-mono text-xs break-all max-w-md">{r.value}</Td>
                <Td className="text-slate-500">{r.ttl}</Td>
                <Td className="text-slate-500">{['MX', 'SRV'].includes(r.type) ? r.priority : '—'}</Td>
                <Td className="text-right whitespace-nowrap">
                  <button className="text-slate-400 hover:text-brand-600 mr-3 cursor-pointer" onClick={() => setEditing(r)}>
                    <Pencil size={15} />
                  </button>
                  <button className="text-slate-400 hover:text-red-600 cursor-pointer" onClick={() => remove(r)}>
                    <Trash2 size={15} />
                  </button>
                </Td>
              </tr>
            ))}
          </Table>
        )}
      </Card>

      <Modal open={!!editing} title={editing?.id ? 'Edit Record' : 'Add Record'} onClose={() => setEditing(null)}>
        {editing && (
          <form onSubmit={save}>
            <div className="grid grid-cols-2 gap-3">
              <Field label="Name" hint="@ or empty for the zone root">
                <Input value={editing.name ?? ''} onChange={(e) => setEditing({ ...editing, name: e.target.value })} />
              </Field>
              <Field label="Type">
                <Select value={editing.type} onChange={(e) => setEditing({ ...editing, type: e.target.value })}>
                  {RECORD_TYPES.map((t) => (
                    <option key={t}>{t}</option>
                  ))}
                </Select>
              </Field>
            </div>
            <Field label="Value">
              <Input value={editing.value ?? ''} onChange={(e) => setEditing({ ...editing, value: e.target.value })} required />
            </Field>
            <div className="grid grid-cols-2 gap-3">
              <Field label="TTL (seconds)">
                <Input
                  type="number"
                  value={editing.ttl ?? 3600}
                  onChange={(e) => setEditing({ ...editing, ttl: Number(e.target.value) })}
                />
              </Field>
              {['MX', 'SRV'].includes(editing.type ?? '') && (
                <Field label="Priority">
                  <Input
                    type="number"
                    value={editing.priority ?? 0}
                    onChange={(e) => setEditing({ ...editing, priority: Number(e.target.value) })}
                  />
                </Field>
              )}
            </div>
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
