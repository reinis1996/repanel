import { useState } from 'react'
import { Plus, KeyRound, Trash2, ShieldCheck, FileText } from 'lucide-react'
import { api, useFetch } from '../api'
import type { Mailbox, MailAlias, Domain, DKIMStatus } from '../types'
import {
  Btn, Card, PageHeader, Table, Td, Modal, Field, Input, Select,
  Spinner, ErrorBanner, Empty, Badge, toast,
} from '../components/ui'

interface MailData {
  mailboxes: Mailbox[]
  aliases: MailAlias[]
}

export default function MailPage() {
  const { data, error, loading, reload } = useFetch<MailData>('/api/mail')
  const domains = useFetch<Domain[]>('/api/domains')
  const dkim = useFetch<DKIMStatus[]>('/api/dkim')

  const [boxOpen, setBoxOpen] = useState(false)
  const [aliasOpen, setAliasOpen] = useState(false)
  const [pwBox, setPwBox] = useState<Mailbox | null>(null)
  const [recordsFor, setRecordsFor] = useState<DKIMStatus | null>(null)
  const [busy, setBusy] = useState(false)

  // mailbox form
  const [local, setLocal] = useState('')
  const [domain, setDomain] = useState('')
  const [password, setPassword] = useState('')
  const [quota, setQuota] = useState(1024)
  // alias form
  const [aliasLocal, setAliasLocal] = useState('')
  const [aliasDomain, setAliasDomain] = useState('')
  const [aliasDest, setAliasDest] = useState('')

  const domainNames = (domains.data ?? []).map((d) => d.name)

  const createBox = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      await api.post('/api/mail/boxes', {
        address: `${local}@${domain || domainNames[0]}`,
        password,
        quota_mb: quota,
      })
      toast('Mailbox created')
      setBoxOpen(false)
      setLocal('')
      setPassword('')
      reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  const createAlias = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      await api.post('/api/mail/aliases', {
        source: `${aliasLocal}@${aliasDomain || domainNames[0]}`,
        destination: aliasDest,
      })
      toast('Alias created')
      setAliasOpen(false)
      setAliasLocal('')
      setAliasDest('')
      reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  const changePassword = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!pwBox) return
    setBusy(true)
    try {
      await api.post(`/api/mail/boxes/${pwBox.id}/password`, { password })
      toast('Password updated')
      setPwBox(null)
      setPassword('')
    } catch (err) {
      toast((err as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  const removeBox = async (m: Mailbox) => {
    if (!confirm(`Delete mailbox ${m.address}? Stored mail is kept on disk.`)) return
    try {
      await api.del(`/api/mail/boxes/${m.id}`)
      toast('Mailbox deleted')
      reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    }
  }

  const removeAlias = async (a: MailAlias) => {
    if (!confirm(`Delete alias ${a.source}?`)) return
    try {
      await api.del(`/api/mail/aliases/${a.id}`)
      toast('Alias deleted')
      reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    }
  }

  const enableDKIM = async (d: DKIMStatus) => {
    try {
      const res = await api.post<DKIMStatus>(`/api/dkim/${d.domain_id}`)
      toast(res.dns_managed ? `DKIM enabled — records published to ${d.domain}` : `DKIM enabled for ${d.domain}`)
      dkim.reload()
      setRecordsFor(res)
    } catch (err) {
      toast((err as Error).message, 'err')
    }
  }

  const disableDKIM = async (d: DKIMStatus) => {
    if (!confirm(`Disable DKIM signing for ${d.domain}? Outgoing mail will no longer be signed.`)) return
    try {
      await api.del(`/api/dkim/${d.domain_id}`)
      toast(`DKIM disabled for ${d.domain}`)
      dkim.reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    }
  }

  if (loading) return <Spinner />

  return (
    <div>
      <PageHeader
        title="Mail"
        subtitle="Mailboxes are served by Postfix (SMTP) and Dovecot (IMAP/POP3)"
        actions={
          <>
            <Btn variant="secondary" onClick={() => setAliasOpen(true)}>
              <Plus size={16} /> Add Alias
            </Btn>
            <Btn onClick={() => setBoxOpen(true)}>
              <Plus size={16} /> Add Mailbox
            </Btn>
          </>
        }
      />
      <ErrorBanner message={error} />

      <div className="space-y-4">
        <Card title="Mailboxes">
          {!data?.mailboxes.length ? (
            <Empty title="No mailboxes" hint="Add a domain first, then create mailboxes under it" />
          ) : (
            <Table head={['Address', 'Quota', '']}>
              {data.mailboxes.map((m) => (
                <tr key={m.id} className="hover:bg-slate-50/60">
                  <Td className="font-medium">{m.address}</Td>
                  <Td className="text-slate-500">{m.quota_mb} MB</Td>
                  <Td className="text-right whitespace-nowrap">
                    <Btn size="sm" variant="secondary" className="mr-2" onClick={() => setPwBox(m)}>
                      <KeyRound size={13} /> Password
                    </Btn>
                    <Btn size="sm" variant="danger" onClick={() => removeBox(m)}>
                      <Trash2 size={13} /> Delete
                    </Btn>
                  </Td>
                </tr>
              ))}
            </Table>
          )}
        </Card>

        <Card title="Aliases / Forwarders">
          {!data?.aliases.length ? (
            <Empty title="No aliases" />
          ) : (
            <Table head={['Alias', 'Forwards to', '']}>
              {data.aliases.map((a) => (
                <tr key={a.id} className="hover:bg-slate-50/60">
                  <Td className="font-medium">{a.source}</Td>
                  <Td className="text-slate-600">{a.destination}</Td>
                  <Td className="text-right">
                    <Btn size="sm" variant="danger" onClick={() => removeAlias(a)}>
                      <Trash2 size={13} /> Delete
                    </Btn>
                  </Td>
                </tr>
              ))}
            </Table>
          )}
        </Card>

        <Card title="DKIM & DMARC (email authentication)">
          {!(dkim.data ?? []).length ? (
            <Empty title="No domains" hint="Add a domain to configure email authentication" />
          ) : (
            <Table head={['Domain', 'DKIM', 'DNS', '']}>
              {(dkim.data ?? []).map((d) => (
                <tr key={d.domain_id} className="hover:bg-slate-50/60">
                  <Td className="font-medium">{d.domain}</Td>
                  <Td>
                    {d.enabled ? <Badge color="green">signing</Badge> : <Badge color="gray">off</Badge>}
                  </Td>
                  <Td>
                    {d.dns_managed ? (
                      <span className="text-xs text-slate-500">auto-published</span>
                    ) : (
                      <span className="text-xs text-amber-600">manual</span>
                    )}
                  </Td>
                  <Td className="text-right whitespace-nowrap">
                    {d.enabled && (
                      <Btn size="sm" variant="secondary" className="mr-2" onClick={() => setRecordsFor(d)}>
                        <FileText size={13} /> Records
                      </Btn>
                    )}
                    {d.enabled ? (
                      <Btn size="sm" variant="danger" onClick={() => disableDKIM(d)}>
                        Disable
                      </Btn>
                    ) : (
                      <Btn size="sm" variant="secondary" onClick={() => enableDKIM(d)}>
                        <ShieldCheck size={13} /> Enable DKIM
                      </Btn>
                    )}
                  </Td>
                </tr>
              ))}
            </Table>
          )}
        </Card>
      </div>

      <Modal open={!!recordsFor} title={`DNS records — ${recordsFor?.domain ?? ''}`} onClose={() => setRecordsFor(null)} wide>
        {recordsFor && (
          <div className="space-y-4">
            <p className="text-sm text-slate-500">
              {recordsFor.dns_managed
                ? 'These records are published automatically in the zone RePanel hosts for this domain. They are shown here for reference.'
                : 'This domain’s DNS is not hosted here — add the following TXT records at your DNS provider so receivers can verify your mail.'}
            </p>
            <DnsRecord label="DKIM" name={recordsFor.dkim_name} value={recordsFor.dkim_value} />
            <DnsRecord label="DMARC" name={recordsFor.dmarc_name} value={recordsFor.dmarc_value} />
            <DnsRecord label="SPF (recommended)" name="@" value={recordsFor.spf_suggest} />
            <div className="flex justify-end">
              <Btn onClick={() => setRecordsFor(null)}>Done</Btn>
            </div>
          </div>
        )}
      </Modal>

      <Modal open={boxOpen} title="Add Mailbox" onClose={() => setBoxOpen(false)}>
        <form onSubmit={createBox}>
          <Field label="Address">
            <div className="flex items-center gap-2">
              <Input value={local} onChange={(e) => setLocal(e.target.value)} placeholder="info" required />
              <span className="text-slate-400">@</span>
              <Select value={domain} onChange={(e) => setDomain(e.target.value)}>
                {domainNames.map((d) => (
                  <option key={d}>{d}</option>
                ))}
              </Select>
            </div>
          </Field>
          <Field label="Password" hint="At least 8 characters">
            <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required minLength={8} />
          </Field>
          <Field label="Quota (MB)">
            <Input type="number" value={quota} onChange={(e) => setQuota(Number(e.target.value))} min={1} />
          </Field>
          <div className="flex justify-end gap-2 mt-2">
            <Btn type="button" variant="secondary" onClick={() => setBoxOpen(false)}>
              Cancel
            </Btn>
            <Btn type="submit" disabled={busy || !domainNames.length}>
              Create
            </Btn>
          </div>
        </form>
      </Modal>

      <Modal open={aliasOpen} title="Add Alias" onClose={() => setAliasOpen(false)}>
        <form onSubmit={createAlias}>
          <Field label="Alias address">
            <div className="flex items-center gap-2">
              <Input value={aliasLocal} onChange={(e) => setAliasLocal(e.target.value)} placeholder="sales" required />
              <span className="text-slate-400">@</span>
              <Select value={aliasDomain} onChange={(e) => setAliasDomain(e.target.value)}>
                {domainNames.map((d) => (
                  <option key={d}>{d}</option>
                ))}
              </Select>
            </div>
          </Field>
          <Field label="Forward to" hint="Any mail address, local or external">
            <Input type="email" value={aliasDest} onChange={(e) => setAliasDest(e.target.value)} required />
          </Field>
          <div className="flex justify-end gap-2 mt-2">
            <Btn type="button" variant="secondary" onClick={() => setAliasOpen(false)}>
              Cancel
            </Btn>
            <Btn type="submit" disabled={busy || !domainNames.length}>
              Create
            </Btn>
          </div>
        </form>
      </Modal>

      <Modal open={!!pwBox} title={`Password — ${pwBox?.address ?? ''}`} onClose={() => setPwBox(null)}>
        <form onSubmit={changePassword}>
          <Field label="New password" hint="At least 8 characters">
            <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required minLength={8} />
          </Field>
          <div className="flex justify-end gap-2 mt-2">
            <Btn type="button" variant="secondary" onClick={() => setPwBox(null)}>
              Cancel
            </Btn>
            <Btn type="submit" disabled={busy}>
              Save
            </Btn>
          </div>
        </form>
      </Modal>
    </div>
  )
}

function DnsRecord({ label, name, value }: { label: string; name: string; value: string }) {
  const copy = () => navigator.clipboard?.writeText(value).then(() => toast('Copied'))
  return (
    <div>
      <div className="flex items-center justify-between mb-1">
        <span className="text-xs font-semibold uppercase tracking-wide text-slate-500">{label}</span>
        <button onClick={copy} className="text-xs text-brand-600 hover:underline">
          Copy value
        </button>
      </div>
      <div className="grid grid-cols-[5rem_1fr] gap-x-3 gap-y-1 text-xs">
        <span className="text-slate-400">Type</span>
        <span className="font-mono">TXT</span>
        <span className="text-slate-400">Name</span>
        <span className="font-mono break-all">{name}</span>
        <span className="text-slate-400">Value</span>
        <span className="font-mono break-all bg-slate-50 border border-slate-200 rounded px-2 py-1">{value}</span>
      </div>
    </div>
  )
}
