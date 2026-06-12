import { useState } from 'react'
import { Plus, Trash2, ShieldCheck } from 'lucide-react'
import { api, useFetch, formatDate } from '../api'
import type { Certificate, Domain } from '../types'
import {
  Btn, Card, PageHeader, Table, Td, Modal, Field, Select,
  Spinner, ErrorBanner, Empty, Badge, toast,
} from '../components/ui'

export default function Ssl() {
  const { data, error, loading, reload } = useFetch<Certificate[]>('/api/ssl')
  const domains = useFetch<Domain[]>('/api/domains')
  const [issueOpen, setIssueOpen] = useState(false)
  const [domainId, setDomainId] = useState(0)
  const [method, setMethod] = useState<'letsencrypt' | 'self-signed'>('letsencrypt')
  const [busy, setBusy] = useState(false)

  const issue = async (e: React.FormEvent) => {
    e.preventDefault()
    const id = domainId || domains.data?.[0]?.id
    if (!id) return
    setBusy(true)
    try {
      await api.post('/api/ssl/issue', { domain_id: id, method })
      toast('Certificate issued and SSL enabled')
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
          <Btn onClick={() => setIssueOpen(true)}>
            <Plus size={16} /> Issue Certificate
          </Btn>
        }
      />
      <ErrorBanner message={error} />
      <Card>
        {!data?.length ? (
          <Empty title="No certificates" hint="Issue a certificate to enable HTTPS on a domain" />
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
                  <Badge color={c.issuer === 'letsencrypt' ? 'blue' : 'gray'}>{c.issuer}</Badge>
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
              <label className="flex items-start gap-2 p-3 border border-slate-200 rounded-md cursor-pointer has-checked:border-brand-500 has-checked:bg-brand-50">
                <input
                  type="radio"
                  checked={method === 'letsencrypt'}
                  onChange={() => setMethod('letsencrypt')}
                  className="mt-0.5"
                />
                <span>
                  <span className="font-medium block">Let's Encrypt</span>
                  <span className="text-slate-500 text-xs">
                    Free trusted certificate. The domain must already point to this server.
                  </span>
                </span>
              </label>
              <label className="flex items-start gap-2 p-3 border border-slate-200 rounded-md cursor-pointer has-checked:border-brand-500 has-checked:bg-brand-50">
                <input
                  type="radio"
                  checked={method === 'self-signed'}
                  onChange={() => setMethod('self-signed')}
                  className="mt-0.5"
                />
                <span>
                  <span className="font-medium block">Self-signed</span>
                  <span className="text-slate-500 text-xs">Browsers will warn; useful for testing.</span>
                </span>
              </label>
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
    </div>
  )
}
