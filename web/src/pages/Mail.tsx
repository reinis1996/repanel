import { useState } from 'react'
import { Plus, KeyRound, Trash2 } from 'lucide-react'
import { api, useFetch } from '../api'
import type { Mailbox, MailAlias, Domain } from '../types'
import {
  Btn, Card, PageHeader, Table, Td, Modal, Field, Input, Select,
  Spinner, ErrorBanner, Empty, toast,
} from '../components/ui'

interface MailData {
  mailboxes: Mailbox[]
  aliases: MailAlias[]
}

export default function MailPage() {
  const { data, error, loading, reload } = useFetch<MailData>('/api/mail')
  const domains = useFetch<Domain[]>('/api/domains')

  const [boxOpen, setBoxOpen] = useState(false)
  const [aliasOpen, setAliasOpen] = useState(false)
  const [pwBox, setPwBox] = useState<Mailbox | null>(null)
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
      </div>

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
