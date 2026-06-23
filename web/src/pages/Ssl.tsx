import { useState } from 'react'
import { Plus, Trash2, ShieldCheck, Upload, Server } from 'lucide-react'
import { api, useFetch, formatDate } from '../api'
import { useAuth } from '../App'
import type { Certificate, Domain } from '../types'
import {
  Btn, Card, PageHeader, Table, Td, Modal, Field, Select, Input,
  Spinner, ErrorBanner, Empty, Badge, toast, inputCls,
} from '../components/ui'

type IssueMethod = 'letsencrypt' | 'letsencrypt-dns' | 'self-signed'

export default function Ssl() {
  const { user } = useAuth()
  const isAdmin = user?.role === 'admin'
  const { data, error, loading, reload } = useFetch<Certificate[]>('/api/ssl')
  const domains = useFetch<Domain[]>('/api/domains')
  const [issueOpen, setIssueOpen] = useState(false)
  const [uploadOpen, setUploadOpen] = useState(false)
  const [domainId, setDomainId] = useState(0)
  const [method, setMethod] = useState<IssueMethod>('letsencrypt')
  const [busy, setBusy] = useState(false)

  const issue = async (e: React.FormEvent) => {
    e.preventDefault()
    const id = domainId || domains.data?.[0]?.id
    if (!id) return
    setBusy(true)
    try {
      await api.post('/api/ssl/issue', { domain_id: id, method })
      toast(method === 'letsencrypt-dns' ? 'Wildcard certificate issued' : 'Certificate issued and SSL enabled')
      setIssueOpen(false)
      reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  const remove = async (c: Certificate) => {
    if (!confirm(`Remove certificate for ${c.domain}?`)) return
    try {
      await api.del(`/api/ssl/${c.id}`)
      toast('Certificate removed')
      reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    }
  }

  if (loading) return <Spinner />

  const expiringSoon = (iso: string) => {
    const d = new Date(iso)
    return !isNaN(d.getTime()) && d.getTime() - Date.now() < 14 * 86400e3
  }

  return (
    <div>
      <PageHeader
        title="SSL/TLS Certificates"
        subtitle="Let's Encrypt certificates renew automatically every night"
        actions={
          <>
            <Btn variant="secondary" onClick={() => setUploadOpen(true)}>
              <Upload size={16} /> Upload
            </Btn>
            <Btn onClick={() => setIssueOpen(true)}>
              <Plus size={16} /> Issue Certificate
            </Btn>
          </>
        }
      />
      <ErrorBanner message={error} />
      <Card>
        {!data?.length ? (
          <Empty title="No certificates" hint="Issue or upload a certificate to enable HTTPS on a domain" />
        ) : (
          <Table head={['Domain', 'Issuer', 'Expires', '']}>
            {data.map((c) => (
              <tr key={c.id} className="hover:bg-slate-50/60">
                <Td>
                  <span className="inline-flex items-center gap-2 font-medium">
                    <ShieldCheck size={15} className="text-emerald-600" />
                    {c.domain}
                  </span>
                </Td>
                <Td>
                  <Badge color={c.issuer === 'letsencrypt' ? 'blue' : c.issuer === 'custom' ? 'amber' : 'gray'}>
                    {c.issuer}
                  </Badge>
                </Td>
                <Td>
                  <span className={expiringSoon(c.not_after) ? 'text-amber-600 font-medium' : 'text-slate-500'}>
                    {formatDate(c.not_after)}
                  </span>
                </Td>
                <Td className="text-right">
                  <Btn size="sm" variant="danger" onClick={() => remove(c)}>
                    <Trash2 size={13} /> Remove
                  </Btn>
                </Td>
              </tr>
            ))}
          </Table>
        )}
      </Card>

      {isAdmin && <PanelCertCard />}
      {isAdmin && <AssignmentsCard certs={data ?? []} />}

      <Modal open={issueOpen} title="Issue Certificate" onClose={() => setIssueOpen(false)}>
        <form onSubmit={issue}>
          <Field label="Domain">
            <Select value={domainId} onChange={(e) => setDomainId(Number(e.target.value))}>
              {(domains.data ?? []).map((d) => (
                <option key={d.id} value={d.id}>
                  {d.name}
                </option>
              ))}
            </Select>
          </Field>
          <Field label="Method">
            <div className="space-y-2 text-sm">
              <MethodOption
                checked={method === 'letsencrypt'}
                onChange={() => setMethod('letsencrypt')}
                title="Let's Encrypt (HTTP)"
                desc="Free trusted certificate for the domain and www. The domain must already point to this server."
              />
              <MethodOption
                checked={method === 'letsencrypt-dns'}
                onChange={() => setMethod('letsencrypt-dns')}
                title="Let's Encrypt — wildcard (DNS)"
                desc="Covers *.domain and the apex via a DNS-01 challenge. Requires this domain's DNS to be hosted on RePanel; the challenge record is published automatically."
              />
              <MethodOption
                checked={method === 'self-signed'}
                onChange={() => setMethod('self-signed')}
                title="Self-signed"
                desc="Browsers will warn; useful for testing."
              />
            </div>
          </Field>
          <div className="flex justify-end gap-2 mt-2">
            <Btn type="button" variant="secondary" onClick={() => setIssueOpen(false)}>
              Cancel
            </Btn>
            <Btn type="submit" disabled={busy || !domains.data?.length}>
              {busy ? 'Issuing…' : 'Issue'}
            </Btn>
          </div>
        </form>
      </Modal>

      {uploadOpen && <UploadModal domains={domains.data ?? []} onClose={() => setUploadOpen(false)} onSaved={reload} />}
    </div>
  )
}

function MethodOption({ checked, onChange, title, desc }: { checked: boolean; onChange: () => void; title: string; desc: string }) {
  return (
    <label className="flex items-start gap-2 p-3 border border-slate-200 rounded-md cursor-pointer has-checked:border-brand-500 has-checked:bg-brand-50">
      <input type="radio" checked={checked} onChange={onChange} className="mt-0.5" />
      <span>
        <span className="font-medium block">{title}</span>
        <span className="text-slate-500 text-xs">{desc}</span>
      </span>
    </label>
  )
}

function UploadModal({ domains, onClose, onSaved }: { domains: Domain[]; onClose: () => void; onSaved: () => void }) {
  const [domainId, setDomainId] = useState(domains[0]?.id ?? 0)
  const [cert, setCert] = useState('')
  const [key, setKey] = useState('')
  const [busy, setBusy] = useState(false)

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      await api.post('/api/ssl/upload', { domain_id: domainId || domains[0]?.id, cert, key })
      toast('Certificate uploaded and SSL enabled')
      onSaved()
      onClose()
    } catch (err) {
      toast((err as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal open title="Upload certificate" onClose={onClose} wide>
      <form onSubmit={submit}>
        <Field label="Domain">
          <Select value={domainId} onChange={(e) => setDomainId(Number(e.target.value))}>
            {domains.map((d) => (
              <option key={d.id} value={d.id}>{d.name}</option>
            ))}
          </Select>
        </Field>
        <Field label="Certificate (PEM)" hint="Paste the full chain: your certificate followed by any intermediates">
          <textarea value={cert} onChange={(e) => setCert(e.target.value)} className={`${inputCls} h-36 font-mono text-xs`} placeholder="-----BEGIN CERTIFICATE-----" required />
        </Field>
        <Field label="Private key (PEM)">
          <textarea value={key} onChange={(e) => setKey(e.target.value)} className={`${inputCls} h-28 font-mono text-xs`} placeholder="-----BEGIN PRIVATE KEY-----" required />
        </Field>
        <div className="flex justify-end gap-2 mt-2">
          <Btn type="button" variant="secondary" onClick={onClose}>Cancel</Btn>
          <Btn type="submit" disabled={busy || !domains.length}>{busy ? 'Saving…' : 'Upload'}</Btn>
        </div>
      </form>
    </Modal>
  )
}

type PanelCert = { hostname: string; issuer: string; configured: boolean; not_after?: string }

function PanelCertCard() {
  const { data, reload } = useFetch<PanelCert>('/api/ssl/panel')
  const [uploadOpen, setUploadOpen] = useState(false)
  const [busy, setBusy] = useState(false)

  const apply = async (method: string, body: Record<string, unknown> = {}) => {
    setBusy(true)
    try {
      await api.post('/api/ssl/panel', { method, ...body })
      toast('Panel certificate set — the panel is restarting, reload in a moment')
    } catch (e) {
      toast((e as Error).message, 'err')
    } finally {
      setBusy(false)
      setUploadOpen(false)
      setTimeout(reload, 3000)
    }
  }

  const host = data?.hostname
  return (
    <Card
      title={
        <span className="flex items-center gap-2">
          <Server size={15} className="text-brand-600" /> Control panel certificate
        </span>
      }
      className="mt-4"
    >
      {!host ? (
        <Empty title="Panel hostname not set" hint="Set the panel hostname in Settings, then secure it here." />
      ) : (
        <div className="space-y-3">
          <div className="text-sm text-slate-600">
            Secures the panel itself on its HTTPS port for <span className="font-medium">{host}</span>.{' '}
            {data?.configured ? (
              <>Current: <Badge color={data.issuer === 'letsencrypt' ? 'blue' : data.issuer === 'custom' ? 'amber' : 'gray'}>{data.issuer || 'custom'}</Badge>
                {data.not_after && <span className="text-slate-400"> · expires {formatDate(data.not_after)}</span>}</>
            ) : (
              <Badge color="gray">self-signed (default)</Badge>
            )}
          </div>
          <div className="flex flex-wrap gap-2">
            <Btn size="sm" disabled={busy} onClick={() => apply('letsencrypt')}>
              <ShieldCheck size={14} /> Let's Encrypt
            </Btn>
            <Btn size="sm" variant="secondary" disabled={busy} onClick={() => setUploadOpen(true)}>
              <Upload size={14} /> Upload certificate
            </Btn>
            <Btn size="sm" variant="secondary" disabled={busy} onClick={() => apply('self-signed')}>
              Self-signed
            </Btn>
          </div>
          <p className="text-xs text-slate-400">
            Let's Encrypt validates over HTTP — {host} must resolve to this server and use the nginx web server.
            The panel restarts to apply, so it is briefly unavailable.
          </p>
        </div>
      )}

      {uploadOpen && (
        <PanelUploadModal busy={busy} onClose={() => setUploadOpen(false)} onSubmit={(cert, key) => apply('custom', { cert, key })} />
      )}
    </Card>
  )
}

function PanelUploadModal({ busy, onClose, onSubmit }: { busy: boolean; onClose: () => void; onSubmit: (cert: string, key: string) => void }) {
  const [cert, setCert] = useState('')
  const [key, setKey] = useState('')
  return (
    <Modal open title="Upload panel certificate" onClose={onClose} wide>
      <form onSubmit={(e) => { e.preventDefault(); onSubmit(cert, key) }}>
        <Field label="Certificate (PEM)" hint="Paste the full chain: your certificate followed by any intermediates">
          <textarea value={cert} onChange={(e) => setCert(e.target.value)} className={`${inputCls} h-36 font-mono text-xs`} placeholder="-----BEGIN CERTIFICATE-----" required />
        </Field>
        <Field label="Private key (PEM)">
          <textarea value={key} onChange={(e) => setKey(e.target.value)} className={`${inputCls} h-28 font-mono text-xs`} placeholder="-----BEGIN PRIVATE KEY-----" required />
        </Field>
        <div className="flex justify-end gap-2 mt-2">
          <Btn type="button" variant="secondary" onClick={onClose}>Cancel</Btn>
          <Btn type="submit" disabled={busy}>{busy ? 'Saving…' : 'Upload'}</Btn>
        </div>
      </form>
    </Modal>
  )
}

const SERVICES: { key: string; label: string; note: string }[] = [
  { key: 'mail', label: 'Mail (Postfix + Dovecot)', note: 'Secures SMTP/IMAP/POP3 (e.g. mail.<domain>).' },
  { key: 'ftp', label: 'FTP (ProFTPD)', note: 'Enables FTPS.' },
  { key: 'panel', label: 'Control panel', note: 'The panel restarts to apply its own certificate.' },
]

function AssignmentsCard({ certs }: { certs: Certificate[] }) {
  const { data, reload } = useFetch<Record<string, number>>('/api/ssl/assignments')
  const [sel, setSel] = useState<Record<string, number>>({})
  const [busy, setBusy] = useState(false)

  const current = (svc: string) => sel[svc] ?? data?.[svc] ?? 0

  const assign = async (svc: string) => {
    const certId = current(svc)
    if (!certId) {
      toast('Choose a certificate first', 'err')
      return
    }
    setBusy(true)
    try {
      const res = await api.post<{ restarting?: boolean }>('/api/ssl/assign', { cert_id: certId, service: svc })
      toast(res.restarting ? 'Panel certificate set — restarting…' : 'Certificate applied')
      reload()
    } catch (e) {
      toast((e as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Card
      title={
        <span className="flex items-center gap-2">
          <Server size={15} className="text-brand-600" /> Assign certificates to services
        </span>
      }
      className="mt-4"
    >
      {!certs.length ? (
        <Empty title="No certificates yet" hint="Issue or upload a certificate, then assign it to mail, FTP or the panel." />
      ) : (
        <div className="space-y-3">
          {SERVICES.map((svc) => (
            <div key={svc.key} className="flex items-center gap-3">
              <div className="w-56 shrink-0">
                <div className="text-sm font-medium text-slate-700">{svc.label}</div>
                <div className="text-xs text-slate-400">{svc.note}</div>
              </div>
              <Select value={current(svc.key)} onChange={(e) => setSel({ ...sel, [svc.key]: Number(e.target.value) })}>
                <option value={0}>— none —</option>
                {certs.map((c) => (
                  <option key={c.id} value={c.id}>{c.domain} ({c.issuer})</option>
                ))}
              </Select>
              <Btn size="sm" variant="secondary" disabled={busy} onClick={() => assign(svc.key)}>
                Apply
              </Btn>
            </div>
          ))}
        </div>
      )}
    </Card>
  )
}
