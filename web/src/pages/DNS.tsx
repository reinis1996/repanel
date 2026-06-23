import { useState } from 'react'
import { Plus, Pencil, Trash2, ArrowLeft, ShieldCheck, Cloud } from 'lucide-react'
import { api, useFetch } from '../api'
import type { DNSZone, DNSRecord, DNSSECStatus } from '../types'
import {
  Btn, Card, PageHeader, Table, Td, Modal, Field, Input, Select,
  Spinner, ErrorBanner, Empty, Badge, toast,
} from '../components/ui'

const PROXYABLE = ['A', 'AAAA', 'CNAME']

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
          <Table head={['Zone', 'Records', '']}>
            {data.map((z) => (
              <tr key={z.id} className="hover:bg-slate-50/60">
                <Td>
                  <button className="font-medium text-brand-600 hover:underline cursor-pointer mr-2" onClick={() => onOpen(z.id)}>
                    {z.name}
                  </button>
                  {z.dnssec && <Badge color="green">DNSSEC</Badge>}
                  {z.cf_sync && z.cf_sync !== 'off' && <Badge color="amber">Cloudflare {z.cf_sync}</Badge>}
                </Td>
                <Td className="text-slate-500">{z.record_count ?? 0}</Td>
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
  const [dnssecOpen, setDnssecOpen] = useState(false)
  const [cfOpen, setCfOpen] = useState(false)
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
            <Btn variant="secondary" onClick={() => setDnssecOpen(true)}>
              <ShieldCheck size={16} /> DNSSEC
            </Btn>
            <Btn variant="secondary" onClick={() => setCfOpen(true)}>
              <Cloud size={16} /> Cloudflare
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
            {PROXYABLE.includes(editing.type ?? '') && (
              <label className="flex items-center gap-2 text-sm text-slate-700 mb-2">
                <input
                  type="checkbox"
                  checked={!!editing.proxied}
                  onChange={(e) => setEditing({ ...editing, proxied: e.target.checked })}
                />
                Proxied through Cloudflare
                <span className="text-xs text-slate-400">(only applies when synced to Cloudflare)</span>
              </label>
            )}
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

      {dnssecOpen && <DNSSECModal zoneId={zoneId} onClose={() => setDnssecOpen(false)} />}

      {cfOpen && data && <CloudflareModal zone={data} onClose={() => setCfOpen(false)} onChanged={reload} />}
    </div>
  )
}

function CloudflareModal({ zone, onClose, onChanged }: { zone: DNSZone; onClose: () => void; onChanged: () => void }) {
  const [cfZoneId, setCfZoneId] = useState(zone.cf_zone_id ?? '')
  const [token, setToken] = useState('')
  const [sync, setSync] = useState(zone.cf_sync && zone.cf_sync !== 'off' ? zone.cf_sync : 'off')
  const [prune, setPrune] = useState(false)
  const [busy, setBusy] = useState('')

  const run = async (key: string, fn: () => Promise<unknown>, msg: (r: unknown) => string) => {
    setBusy(key)
    try {
      const r = await fn()
      toast(msg(r))
      onChanged()
    } catch (e) {
      toast((e as Error).message, 'err')
    } finally {
      setBusy('')
    }
  }

  const saveBinding = () =>
    run('save', () => api.post(`/api/cloudflare/${zone.id}`, { cf_zone_id: cfZoneId.trim(), token, cf_sync: sync }), () => {
      setToken('')
      return 'Cloudflare settings saved'
    })

  const bound = !!zone.cf_zone_id && zone.has_cf_token

  return (
    <Modal open title={`Cloudflare sync — ${zone.name}`} onClose={onClose} wide>
      <p className="text-sm text-slate-500 mb-4">
        Import records from Cloudflare or push RePanel's records to it. Provide a Cloudflare API token with{' '}
        <span className="font-mono text-xs">DNS:Edit</span> permission for the zone, and the zone ID (found on the
        Cloudflare dashboard's Overview page). Syncs the common record types (A, AAAA, CNAME, MX, TXT); NS/SOA and
        SRV/CAA are left untouched.
      </p>

      <div className="grid grid-cols-2 gap-3">
        <Field label="Cloudflare Zone ID">
          <Input value={cfZoneId} onChange={(e) => setCfZoneId(e.target.value)} placeholder="023e105f4ecef8ad9ca31a8372d0c353" />
        </Field>
        <Field label="API token" hint={zone.has_cf_token ? 'A token is stored — leave blank to keep it' : 'Required'}>
          <Input type="password" value={token} onChange={(e) => setToken(e.target.value)} placeholder={zone.has_cf_token ? '••••••••' : 'token'} />
        </Field>
      </div>
      <Field label="Automatic sync" hint="Push also syncs on every record change; both run hourly.">
        <Select value={sync} onChange={(e) => setSync(e.target.value)}>
          <option value="off">Off — manual only</option>
          <option value="push">Push — RePanel is authoritative (→ Cloudflare)</option>
          <option value="pull">Pull — Cloudflare is authoritative (→ RePanel)</option>
        </Select>
      </Field>
      <div className="flex justify-end">
        <Btn type="button" disabled={!!busy} onClick={saveBinding}>
          {busy === 'save' ? 'Saving…' : 'Save settings'}
        </Btn>
      </div>

      {bound && (
        <div className="mt-5 pt-4 border-t border-slate-100">
          <div className="text-sm font-medium text-slate-700 mb-2">Manual sync</div>
          <div className="flex flex-wrap items-center gap-3">
            <Btn
              type="button"
              variant="secondary"
              disabled={!!busy}
              onClick={() => run('import', () => api.post<{ imported: number }>(`/api/cloudflare/${zone.id}/import`), (r) => `Imported ${(r as { imported: number }).imported} records from Cloudflare`)}
            >
              {busy === 'import' ? 'Importing…' : 'Import from Cloudflare'}
            </Btn>
            <Btn
              type="button"
              variant="secondary"
              disabled={!!busy}
              onClick={() => run('export', () => api.post<{ result: { created: number; updated: number; deleted: number } }>(`/api/cloudflare/${zone.id}/export`, { prune }), (r) => {
                const x = (r as { result: { created: number; updated: number; deleted: number } }).result
                return `Exported to Cloudflare: ${x.created} created, ${x.updated} updated, ${x.deleted} deleted`
              })}
            >
              {busy === 'export' ? 'Exporting…' : 'Export to Cloudflare'}
            </Btn>
            <label className="flex items-center gap-1.5 text-xs text-slate-600">
              <input type="checkbox" checked={prune} onChange={(e) => setPrune(e.target.checked)} />
              also delete Cloudflare records not in RePanel
            </label>
          </div>
          <div className="mt-4">
            <Btn
              type="button"
              variant="danger"
              size="sm"
              disabled={!!busy}
              onClick={() => {
                if (!confirm('Disconnect this zone from Cloudflare? (Cloudflare records are left as-is.)')) return
                run('unbind', () => api.del(`/api/cloudflare/${zone.id}`), () => 'Disconnected from Cloudflare')
              }}
            >
              Disconnect
            </Btn>
          </div>
        </div>
      )}
    </Modal>
  )
}

function DNSSECModal({ zoneId, onClose }: { zoneId: number; onClose: () => void }) {
  const { data, error, loading, reload } = useFetch<DNSSECStatus>(`/api/dnssec/${zoneId}`)
  const [busy, setBusy] = useState(false)

  const toggle = async (enable: boolean) => {
    setBusy(true)
    try {
      if (enable) await api.post(`/api/dnssec/${zoneId}`)
      else await api.del(`/api/dnssec/${zoneId}`)
      toast(enable ? 'DNSSEC enabled — keys are being generated' : 'DNSSEC disabled')
      reload()
    } catch (e) {
      toast((e as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal open title={`DNSSEC — ${data?.zone ?? ''}`} onClose={onClose} wide>
      {loading || !data ? (
        <Spinner />
      ) : error ? (
        <ErrorBanner message={error} />
      ) : !data.available ? (
        <div className="rounded-md bg-amber-50 border border-amber-200 text-amber-800 text-sm px-4 py-3">
          DNSSEC tooling (BIND with dnssec-policy support) is not available on this server.
        </div>
      ) : (
        <div className="space-y-4">
          <div className="flex items-center justify-between">
            <div className="text-sm text-slate-600">
              {data.enabled ? (
                <span className="inline-flex items-center gap-2">
                  <Badge color="green">signed</Badge> BIND signs this zone inline and rotates keys automatically.
                </span>
              ) : (
                'Sign this zone with DNSSEC. BIND generates the keys and signs the zone; publish the DS record below at your registrar.'
              )}
            </div>
            {data.enabled ? (
              <Btn variant="danger" disabled={busy} onClick={() => toggle(false)}>
                Disable
              </Btn>
            ) : (
              <Btn disabled={busy} onClick={() => toggle(true)}>
                <ShieldCheck size={15} /> Enable DNSSEC
              </Btn>
            )}
          </div>

          {data.enabled && (
            <div>
              <div className="text-sm font-semibold text-slate-700 mb-1">DS records (submit these to your registrar)</div>
              {data.ds?.length ? (
                <pre className="max-h-40 overflow-auto rounded-md bg-slate-900 text-slate-100 text-xs font-mono p-3 whitespace-pre-wrap break-all">
                  {data.ds.join('\n')}
                </pre>
              ) : (
                <p className="text-sm text-slate-400">
                  Keys are still being generated — reopen this dialog in a few seconds to see the DS records.
                </p>
              )}
              {!!data.dnskey?.length && (
                <details className="mt-2">
                  <summary className="text-xs text-brand-600 cursor-pointer">Show DNSKEY records</summary>
                  <pre className="mt-2 max-h-40 overflow-auto rounded-md bg-slate-900 text-slate-100 text-xs font-mono p-3 whitespace-pre-wrap break-all">
                    {data.dnskey.join('\n')}
                  </pre>
                </details>
              )}
            </div>
          )}
        </div>
      )}
    </Modal>
  )
}
