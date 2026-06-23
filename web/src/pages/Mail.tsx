import { useEffect, useState } from 'react'
import { Plus, KeyRound, Trash2, ShieldCheck, FileText, Mail, ExternalLink, ShieldAlert, Users, Filter, CalendarClock, DownloadCloud, Send, Loader2 } from 'lucide-react'
import { api, useFetch } from '../api'
import { useAuth } from '../App'
import type {
  Mailbox, MailAlias, MailList, MailAutoresponder, MailFilter, MailMigration, MailSmarthost,
  Domain, DKIMStatus, WebmailStatus, SpamStatus,
} from '../types'
import {
  Btn, Card, PageHeader, Table, Td, Modal, Field, Input, Select,
  Spinner, ErrorBanner, Empty, Badge, toast, inputCls,
} from '../components/ui'

interface MailData {
  mailboxes: Mailbox[]
  aliases: MailAlias[]
  lists: MailList[]
  features: boolean
  imapsync: boolean
}

export default function MailPage() {
  const { user } = useAuth()
  const isAdmin = user?.role === 'admin'
  const { data, error, loading, reload } = useFetch<MailData>('/api/mail')
  const domains = useFetch<Domain[]>('/api/domains')
  const dkim = useFetch<DKIMStatus[]>('/api/dkim')
  const webmail = useFetch<WebmailStatus[]>('/api/webmail')
  const spam = useFetch<SpamStatus>('/api/mail/spam')

  const [boxOpen, setBoxOpen] = useState(false)
  const [aliasOpen, setAliasOpen] = useState(false)
  const [pwBox, setPwBox] = useState<Mailbox | null>(null)
  const [recordsFor, setRecordsFor] = useState<DKIMStatus | null>(null)
  const [filtersFor, setFiltersFor] = useState<Mailbox | null>(null)
  const [autoFor, setAutoFor] = useState<Mailbox | null>(null)
  const [migrateFor, setMigrateFor] = useState<Mailbox | null>(null)
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
  const [aliasKeep, setAliasKeep] = useState(false)

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
        keep_copy: aliasKeep,
      })
      toast(aliasLocal ? 'Alias created' : 'Catch-all created')
      setAliasOpen(false)
      setAliasLocal('')
      setAliasDest('')
      setAliasKeep(false)
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

  const enableWebmail = async (m: WebmailStatus) => {
    try {
      const res = await api.post<WebmailStatus>(`/api/webmail/${m.domain_id}`)
      toast(
        res.dns_managed
          ? `Webmail enabled — ${res.url} (DNS record published)`
          : `Webmail enabled — point ${res.url.replace('http://', '')} at this server`,
      )
      webmail.reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    }
  }

  const disableWebmail = async (m: WebmailStatus) => {
    if (!confirm(`Disable webmail for ${m.domain}?`)) return
    try {
      await api.del(`/api/webmail/${m.domain_id}`)
      toast(`Webmail disabled for ${m.domain}`)
      webmail.reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    }
  }

  const webmailInstalled = (webmail.data ?? []).some((m) => m.available)

  const toggleSpam = async (domainId: number, enabled: boolean) => {
    try {
      await api.post(`/api/domains/${domainId}/spam`, { enabled })
      toast(enabled ? 'Spam & virus filtering enabled' : 'Spam & virus filtering disabled')
      spam.reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    }
  }

  const installAntiSpam = async () => {
    if (!confirm('Install rspamd + ClamAV? This downloads the antivirus signatures and may take several minutes.')) return
    try {
      await api.post('/api/mail/antispam/install')
      toast('Installing rspamd + ClamAV — this runs in the background')
      spam.reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    }
  }

  // Poll while the anti-spam install runs so the card flips to the toggles when done.
  useEffect(() => {
    if (!spam.data?.installing) return
    const t = setInterval(() => spam.reload(), 4000)
    return () => clearInterval(t)
  }, [spam.data?.installing, spam])

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
                  <Td className="text-slate-500">{m.quota_mb > 0 ? `${m.quota_mb} MB` : 'unlimited'}</Td>
                  <Td className="text-right whitespace-nowrap">
                    {data.features && (
                      <>
                        <button className="text-slate-400 hover:text-brand-600 mr-3 align-middle cursor-pointer" title="Auto-reply" onClick={() => setAutoFor(m)}>
                          <CalendarClock size={16} />
                        </button>
                        <button className="text-slate-400 hover:text-brand-600 mr-3 align-middle cursor-pointer" title="Filters" onClick={() => setFiltersFor(m)}>
                          <Filter size={16} />
                        </button>
                      </>
                    )}
                    {data.imapsync && (
                      <button className="text-slate-400 hover:text-brand-600 mr-3 align-middle cursor-pointer" title="Import mail from another server" onClick={() => setMigrateFor(m)}>
                        <DownloadCloud size={16} />
                      </button>
                    )}
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

        <Card
          title="Aliases & Forwarders"
          actions={
            <Btn size="sm" variant="secondary" onClick={() => setAliasOpen(true)}>
              <Plus size={14} /> Add
            </Btn>
          }
        >
          {!data?.aliases.length ? (
            <Empty title="No aliases or forwarders" hint="Forward an address elsewhere, or set a catch-all for the whole domain" />
          ) : (
            <Table head={['Address', 'Forwards to', '', '']}>
              {data.aliases.map((a) => (
                <tr key={a.id} className="hover:bg-slate-50/60">
                  <Td className="font-medium">
                    {a.source.startsWith('@') ? (
                      <span>
                        {a.source} <Badge color="blue">catch-all</Badge>
                      </span>
                    ) : (
                      a.source
                    )}
                  </Td>
                  <Td className="text-slate-600">{a.destination}</Td>
                  <Td>{a.keep_copy && <Badge color="gray">keeps a copy</Badge>}</Td>
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

        <ListsCard lists={data?.lists ?? []} domainNames={domainNames} reload={reload} />

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

        <Card title="Webmail (Roundcube)">
          {!(webmail.data ?? []).length ? (
            <Empty title="No domains" hint="Add a domain to offer webmail at webmail.<domain>" />
          ) : !webmailInstalled ? (
            <Empty
              title="Webmail not installed"
              hint="Install Roundcube on the server (re-run the installer with WITH_WEBMAIL=1) to offer webmail."
            />
          ) : (
            <Table head={['Domain', 'Webmail', 'DNS', '']}>
              {(webmail.data ?? []).map((m) => (
                <tr key={m.domain_id} className="hover:bg-slate-50/60">
                  <Td className="font-medium">{m.domain}</Td>
                  <Td>
                    {m.enabled ? (
                      <a
                        href={m.url}
                        target="_blank"
                        rel="noreferrer"
                        className="inline-flex items-center gap-1 text-brand-600 hover:underline"
                      >
                        webmail.{m.domain} <ExternalLink size={12} />
                      </a>
                    ) : (
                      <Badge color="gray">off</Badge>
                    )}
                  </Td>
                  <Td>
                    {m.enabled &&
                      (m.dns_managed ? (
                        <span className="text-xs text-slate-500">auto-published</span>
                      ) : (
                        <span className="text-xs text-amber-600">point DNS here</span>
                      ))}
                  </Td>
                  <Td className="text-right whitespace-nowrap">
                    {m.enabled ? (
                      <Btn size="sm" variant="danger" onClick={() => disableWebmail(m)}>
                        Disable
                      </Btn>
                    ) : (
                      <Btn size="sm" variant="secondary" onClick={() => enableWebmail(m)}>
                        <Mail size={13} /> Enable
                      </Btn>
                    )}
                  </Td>
                </tr>
              ))}
            </Table>
          )}
        </Card>

        <Card title="Spam & virus filtering (rspamd + ClamAV)">
          {spam.data?.installing ? (
            <div className="flex items-center gap-3 py-4 text-sm text-slate-600">
              <Spinner /> Installing rspamd + ClamAV… this can take several minutes.
            </div>
          ) : !spam.data?.available ? (
            <Empty
              title="Anti-spam not installed"
              hint={
                isAdmin
                  ? 'Install rspamd (spam filtering) and ClamAV (virus scanning) to filter incoming mail.'
                  : 'Ask an administrator to enable spam & virus filtering on this server.'
              }
              action={
                isAdmin ? (
                  <Btn onClick={installAntiSpam}>
                    <ShieldAlert size={15} /> Install rspamd + ClamAV
                  </Btn>
                ) : undefined
              }
            />
          ) : (
            <>
              {spam.data.error && <ErrorBanner message={spam.data.error} />}
              {!spam.data.clamav && (
                <p className="mb-3 text-xs text-amber-600">
                  ClamAV daemon not detected — spam scoring is active, but messages aren’t scanned for viruses yet.
                </p>
              )}
              {!spam.data.domains.length ? (
                <Empty title="No domains" hint="Add a domain to control its mail filtering" />
              ) : (
                <Table head={['Domain', 'Filtering', '']}>
                  {spam.data.domains.map((d) => (
                    <tr key={d.domain_id} className="hover:bg-slate-50/60">
                      <Td className="font-medium">{d.domain}</Td>
                      <Td>
                        {d.enabled ? <Badge color="green">on</Badge> : <Badge color="gray">off</Badge>}
                      </Td>
                      <Td className="text-right">
                        {d.enabled ? (
                          <Btn size="sm" variant="danger" onClick={() => toggleSpam(d.domain_id, false)}>
                            Disable
                          </Btn>
                        ) : (
                          <Btn size="sm" variant="secondary" onClick={() => toggleSpam(d.domain_id, true)}>
                            <ShieldCheck size={13} /> Enable
                          </Btn>
                        )}
                      </Td>
                    </tr>
                  ))}
                </Table>
              )}
            </>
          )}
        </Card>
        <MigrationsCard imapsync={!!data?.imapsync} isAdmin={isAdmin} />

        {isAdmin && <SmarthostCard />}

        {isAdmin && !data?.features && (
          <Card title="Advanced mail features (autoresponders, filters, quotas)">
            <Empty
              title="Not enabled yet"
              hint="Switch delivery to Dovecot LMTP and install the Sieve plugin to enable autoresponders, filters and enforced quotas."
              action={
                <Btn
                  onClick={async () => {
                    try {
                      await api.post('/api/mail/features/install')
                      toast('Enabling mail features in the background — reload in a minute')
                    } catch (e) {
                      toast((e as Error).message, 'err')
                    }
                  }}
                >
                  <ShieldCheck size={15} /> Enable mail features
                </Btn>
              }
            />
          </Card>
        )}
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

      <Modal open={aliasOpen} title="Add alias / forwarder" onClose={() => setAliasOpen(false)}>
        <form onSubmit={createAlias}>
          <Field label="Address" hint="Leave the name blank for a catch-all that receives all unmatched mail for the domain">
            <div className="flex items-center gap-2">
              <Input value={aliasLocal} onChange={(e) => setAliasLocal(e.target.value)} placeholder="sales (or blank = catch-all)" />
              <span className="text-slate-400">@</span>
              <Select value={aliasDomain} onChange={(e) => setAliasDomain(e.target.value)}>
                {domainNames.map((d) => (
                  <option key={d}>{d}</option>
                ))}
              </Select>
            </div>
          </Field>
          <Field label="Forward to" hint="One or more addresses, comma-separated; local or external">
            <Input value={aliasDest} onChange={(e) => setAliasDest(e.target.value)} placeholder="a@x.com, b@y.com" required />
          </Field>
          {aliasLocal !== '' && (
            <label className="flex items-center gap-2 text-sm text-slate-700 mb-4 cursor-pointer">
              <input type="checkbox" checked={aliasKeep} onChange={(e) => setAliasKeep(e.target.checked)} />
              Also keep a copy in the {aliasLocal}@{aliasDomain || domainNames[0]} mailbox
            </label>
          )}
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

      {filtersFor && <FiltersModal box={filtersFor} onClose={() => setFiltersFor(null)} />}
      {autoFor && <AutoresponderModal box={autoFor} onClose={() => setAutoFor(null)} />}
      {migrateFor && <MigrateModal box={migrateFor} onClose={() => setMigrateFor(null)} />}

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

function ListsCard({ lists, domainNames, reload }: { lists: MailList[]; domainNames: string[]; reload: () => void }) {
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<MailList | null>(null)
  const [local, setLocal] = useState('')
  const [domain, setDomain] = useState('')
  const [members, setMembers] = useState('')
  const [busy, setBusy] = useState(false)

  const parseMembers = (s: string) =>
    s.split(/[\n,]/).map((m) => m.trim()).filter(Boolean)

  const create = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      await api.post('/api/mail/lists', { address: `${local}@${domain || domainNames[0]}`, members: parseMembers(members) })
      toast('List created')
      setOpen(false)
      setLocal('')
      setMembers('')
      reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!editing) return
    setBusy(true)
    try {
      await api.put(`/api/mail/lists/${editing.id}`, { members: parseMembers(members) })
      toast('List updated')
      setEditing(null)
      reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  const remove = async (l: MailList) => {
    if (!confirm(`Delete list ${l.address}?`)) return
    try {
      await api.del(`/api/mail/lists/${l.id}`)
      toast('List deleted')
      reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    }
  }

  return (
    <Card
      title={
        <span className="flex items-center gap-2">
          <Users size={15} className="text-brand-600" /> Distribution lists
        </span>
      }
      actions={
        <Btn size="sm" variant="secondary" onClick={() => setOpen(true)}>
          <Plus size={14} /> Add
        </Btn>
      }
    >
      {!lists.length ? (
        <Empty title="No distribution lists" hint="A list address fans every message out to its members" />
      ) : (
        <Table head={['List address', 'Members', '']}>
          {lists.map((l) => (
            <tr key={l.id} className="hover:bg-slate-50/60">
              <Td className="font-medium">{l.address}</Td>
              <Td className="text-slate-500 text-xs">{l.members.length} member{l.members.length === 1 ? '' : 's'}</Td>
              <Td className="text-right whitespace-nowrap">
                <Btn size="sm" variant="secondary" className="mr-2" onClick={() => { setEditing(l); setMembers(l.members.join('\n')) }}>
                  Edit members
                </Btn>
                <Btn size="sm" variant="danger" onClick={() => remove(l)}>
                  <Trash2 size={13} /> Delete
                </Btn>
              </Td>
            </tr>
          ))}
        </Table>
      )}

      <Modal open={open} title="Create distribution list" onClose={() => setOpen(false)}>
        <form onSubmit={create}>
          <Field label="List address">
            <div className="flex items-center gap-2">
              <Input value={local} onChange={(e) => setLocal(e.target.value)} placeholder="team" required />
              <span className="text-slate-400">@</span>
              <Select value={domain} onChange={(e) => setDomain(e.target.value)}>
                {domainNames.map((d) => (
                  <option key={d}>{d}</option>
                ))}
              </Select>
            </div>
          </Field>
          <Field label="Members" hint="One address per line">
            <textarea value={members} onChange={(e) => setMembers(e.target.value)} className={`${inputCls} h-32 font-mono text-xs`} placeholder={'a@x.com\nb@y.com'} required />
          </Field>
          <div className="flex justify-end gap-2 mt-2">
            <Btn type="button" variant="secondary" onClick={() => setOpen(false)}>Cancel</Btn>
            <Btn type="submit" disabled={busy || !domainNames.length}>Create</Btn>
          </div>
        </form>
      </Modal>

      <Modal open={!!editing} title={`Members — ${editing?.address ?? ''}`} onClose={() => setEditing(null)}>
        <form onSubmit={save}>
          <Field label="Members" hint="One address per line">
            <textarea value={members} onChange={(e) => setMembers(e.target.value)} className={`${inputCls} h-40 font-mono text-xs`} required />
          </Field>
          <div className="flex justify-end gap-2 mt-2">
            <Btn type="button" variant="secondary" onClick={() => setEditing(null)}>Cancel</Btn>
            <Btn type="submit" disabled={busy}>Save</Btn>
          </div>
        </form>
      </Modal>
    </Card>
  )
}

function AutoresponderModal({ box, onClose }: { box: Mailbox; onClose: () => void }) {
  const { data, loading } = useFetch<MailAutoresponder>(`/api/mail/boxes/${box.id}/autoresponder`)
  const [enabled, setEnabled] = useState(false)
  const [subject, setSubject] = useState('')
  const [message, setMessage] = useState('')
  const [busy, setBusy] = useState(false)
  const [primed, setPrimed] = useState(false)

  if (data && !primed) {
    setEnabled(data.enabled)
    setSubject(data.subject)
    setMessage(data.message)
    setPrimed(true)
  }

  const save = async () => {
    setBusy(true)
    try {
      await api.post(`/api/mail/boxes/${box.id}/autoresponder`, { enabled, subject, message })
      toast('Auto-reply saved')
      onClose()
    } catch (e) {
      toast((e as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal open title={`Auto-reply — ${box.address}`} onClose={onClose}>
      {loading ? (
        <Spinner />
      ) : (
        <div>
          <label className="flex items-center gap-2 text-sm text-slate-700 mb-4 cursor-pointer">
            <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
            Send an automatic reply to incoming mail
          </label>
          <Field label="Subject">
            <Input value={subject} onChange={(e) => setSubject(e.target.value)} placeholder="Out of office" />
          </Field>
          <Field label="Message">
            <textarea value={message} onChange={(e) => setMessage(e.target.value)} className={`${inputCls} h-32`} placeholder="I'm away until…" />
          </Field>
          <p className="text-xs text-slate-400 mb-3">A sender receives the reply at most once a day while it's enabled.</p>
          <div className="flex justify-end gap-2">
            <Btn type="button" variant="secondary" onClick={onClose}>Cancel</Btn>
            <Btn type="button" disabled={busy} onClick={save}>Save</Btn>
          </div>
        </div>
      )}
    </Modal>
  )
}

const FILTER_FIELDS = ['from', 'to', 'subject', 'any']
const FILTER_ACTIONS = [
  { v: 'fileinto', label: 'Move to folder' },
  { v: 'forward', label: 'Forward to' },
  { v: 'discard', label: 'Discard' },
  { v: 'keep', label: 'Keep in Inbox' },
]

function FiltersModal({ box, onClose }: { box: Mailbox; onClose: () => void }) {
  const { data, loading, reload } = useFetch<MailFilter[]>(`/api/mail/boxes/${box.id}/filters`)
  const [field, setField] = useState('subject')
  const [op, setOp] = useState('contains')
  const [value, setValue] = useState('')
  const [action, setAction] = useState('fileinto')
  const [arg, setArg] = useState('')
  const [busy, setBusy] = useState(false)

  const add = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      await api.post(`/api/mail/boxes/${box.id}/filters`, { field, op, value, action, arg })
      toast('Filter added')
      setValue('')
      setArg('')
      reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  const remove = async (f: MailFilter) => {
    try {
      await api.del(`/api/mail/boxes/${box.id}/filters/${f.id}`)
      reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    }
  }

  const needsArg = action === 'fileinto' || action === 'forward'

  return (
    <Modal open title={`Filters — ${box.address}`} onClose={onClose} wide>
      <form onSubmit={add} className="grid grid-cols-[1fr_1fr_1.4fr_1fr_1.4fr_auto] gap-2 items-end mb-4">
        <Field label="If">
          <Select value={field} onChange={(e) => setField(e.target.value)}>
            {FILTER_FIELDS.map((f) => <option key={f} value={f}>{f}</option>)}
          </Select>
        </Field>
        <Field label="">
          <Select value={op} onChange={(e) => setOp(e.target.value)}>
            <option value="contains">contains</option>
            <option value="is">is</option>
          </Select>
        </Field>
        <Field label="">
          <Input value={value} onChange={(e) => setValue(e.target.value)} placeholder="value" required />
        </Field>
        <Field label="Then">
          <Select value={action} onChange={(e) => setAction(e.target.value)}>
            {FILTER_ACTIONS.map((a) => <option key={a.v} value={a.v}>{a.label}</option>)}
          </Select>
        </Field>
        <Field label="">
          <Input value={arg} onChange={(e) => setArg(e.target.value)} placeholder={action === 'forward' ? 'address' : action === 'fileinto' ? 'folder' : '—'} disabled={!needsArg} required={needsArg} />
        </Field>
        <Btn type="submit" disabled={busy} className="mb-4"><Plus size={14} /></Btn>
      </form>

      {loading ? (
        <Spinner />
      ) : !data?.length ? (
        <Empty title="No filters" hint="Rules run top to bottom on each delivered message" />
      ) : (
        <Table head={['Rule', 'Action', '']}>
          {data.map((f) => (
            <tr key={f.id} className="hover:bg-slate-50/60">
              <Td className="text-slate-700">
                <span className="font-mono text-xs">{f.field} {f.op} “{f.value}”</span>
              </Td>
              <Td className="text-slate-600 text-xs">
                {FILTER_ACTIONS.find((a) => a.v === f.action)?.label}{f.arg && `: ${f.arg}`}
              </Td>
              <Td className="text-right">
                <button className="text-slate-400 hover:text-red-600 cursor-pointer" onClick={() => remove(f)}><Trash2 size={15} /></button>
              </Td>
            </tr>
          ))}
        </Table>
      )}
    </Modal>
  )
}

function MigrateModal({ box, onClose }: { box: Mailbox; onClose: () => void }) {
  const [host, setHost] = useState('')
  const [port, setPort] = useState(993)
  const [username, setUsername] = useState(box.address)
  const [password, setPassword] = useState('')
  const [localPassword, setLocalPassword] = useState('')
  const [busy, setBusy] = useState(false)

  const start = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      await api.post(`/api/mail/boxes/${box.id}/migrate`, { host, port, username, password, local_password: localPassword })
      toast('Migration started — watch progress in the Mail import card')
      onClose()
    } catch (err) {
      toast((err as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal open title={`Import mail into ${box.address}`} onClose={onClose}>
      <form onSubmit={start}>
        <p className="text-sm text-slate-500 mb-3">Copy mail from another IMAP server into this mailbox with imapsync.</p>
        <div className="grid grid-cols-[1fr_6rem] gap-2">
          <Field label="Remote IMAP host"><Input value={host} onChange={(e) => setHost(e.target.value)} placeholder="imap.old-host.com" required /></Field>
          <Field label="Port"><Input type="number" value={port} onChange={(e) => setPort(Number(e.target.value))} /></Field>
        </div>
        <Field label="Remote username"><Input value={username} onChange={(e) => setUsername(e.target.value)} required /></Field>
        <Field label="Remote password"><Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required /></Field>
        <Field label="Destination mailbox password" hint="This mailbox's own password (needed to write the imported mail)">
          <Input type="password" value={localPassword} onChange={(e) => setLocalPassword(e.target.value)} required />
        </Field>
        <div className="flex justify-end gap-2 mt-2">
          <Btn type="button" variant="secondary" onClick={onClose}>Cancel</Btn>
          <Btn type="submit" disabled={busy}><Send size={14} /> Start import</Btn>
        </div>
      </form>
    </Modal>
  )
}

function MigrationsCard({ imapsync, isAdmin }: { imapsync: boolean; isAdmin: boolean }) {
  const { data, reload } = useFetch<{ migrations: MailMigration[]; imapsync: boolean }>('/api/mail/migrations')
  const [logFor, setLogFor] = useState<MailMigration | null>(null)
  const [installing, setInstalling] = useState(false)
  const migrations = data?.migrations ?? []
  const anyRunning = migrations.some((m) => m.status === 'running')
  // Prefer this card's freshly-fetched value so polling reflects a completed
  // install without a manual page reload.
  const installed = imapsync || !!data?.imapsync

  // Poll while a migration runs, or while an install is in progress, so the view
  // updates on its own. Installing stops once imapsync appears or after a few
  // minutes (so a failed/blocked install re-shows the button instead of spinning
  // forever — check `journalctl -u repanel` for the reason).
  useEffect(() => {
    if (!anyRunning && !installing) return
    const t = setInterval(reload, 5000)
    return () => clearInterval(t)
  }, [anyRunning, installing, reload])

  useEffect(() => {
    if (installed) setInstalling(false)
  }, [installed])

  useEffect(() => {
    if (!installing) return
    const stop = setTimeout(() => setInstalling(false), 4 * 60 * 1000)
    return () => clearTimeout(stop)
  }, [installing])

  const installImapsync = async () => {
    try {
      await api.post('/api/mail/imapsync/install')
      setInstalling(true)
      toast('Installing imapsync — this can take a minute')
    } catch (e) {
      toast((e as Error).message, 'err')
    }
  }

  return (
    <Card title="Mail import (IMAP migration)">
      {!installed ? (
        <Empty
          title={installing ? 'Installing imapsync…' : 'imapsync not installed'}
          hint={
            installing
              ? 'Installing Perl dependencies and the imapsync script — this can take a minute.'
              : isAdmin
                ? 'Install imapsync to migrate mailboxes from another server.'
                : 'Ask an administrator to install imapsync.'
          }
          action={
            isAdmin ? (
              <Btn onClick={installImapsync} disabled={installing}>
                {installing ? <Loader2 size={15} className="animate-spin" /> : <DownloadCloud size={15} />}
                {installing ? 'Installing…' : 'Install imapsync'}
              </Btn>
            ) : undefined
          }
        />
      ) : !migrations.length ? (
        <Empty title="No migrations yet" hint="Use the import icon on a mailbox to copy mail from another server" />
      ) : (
        <Table head={['Mailbox', 'From', 'Status', '']}>
          {migrations.map((m) => (
            <tr key={m.id} className="hover:bg-slate-50/60">
              <Td className="font-medium">{m.mailbox}</Td>
              <Td className="text-slate-500 text-xs">{m.remote_user}@{m.remote_host}</Td>
              <Td>
                {m.status === 'completed' ? <Badge color="green">completed</Badge>
                  : m.status === 'failed' ? <Badge color="red">failed</Badge>
                  : <Badge color="amber">running</Badge>}
              </Td>
              <Td className="text-right">
                {m.log && <Btn size="sm" variant="secondary" onClick={() => setLogFor(m)}>Log</Btn>}
              </Td>
            </tr>
          ))}
        </Table>
      )}
      <Modal open={!!logFor} title={`Migration log — ${logFor?.mailbox ?? ''}`} onClose={() => setLogFor(null)} wide>
        <pre className="max-h-96 overflow-auto rounded-md bg-slate-900 text-slate-100 text-xs font-mono p-3 whitespace-pre-wrap">{logFor?.log}</pre>
      </Modal>
    </Card>
  )
}

function SmarthostCard() {
  const { data, loading, reload } = useFetch<MailSmarthost>('/api/mail/smarthost')
  const [enabled, setEnabled] = useState(false)
  const [host, setHost] = useState('')
  const [port, setPort] = useState(587)
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const [primed, setPrimed] = useState(false)

  if (data && !primed) {
    setEnabled(data.enabled)
    setHost(data.host)
    setPort(data.port)
    setUsername(data.username)
    setPrimed(true)
  }

  const save = async () => {
    setBusy(true)
    try {
      await api.post('/api/mail/smarthost', { enabled, host, port, username, password })
      toast(enabled ? 'Smarthost relay configured' : 'Smarthost relay disabled')
      setPassword('')
      reload()
    } catch (e) {
      toast((e as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Card title="Outbound relay (smarthost)">
      {loading ? (
        <Spinner />
      ) : (
        <div>
          <p className="text-sm text-slate-500 mb-3">
            Relay all outbound mail through an external SMTP provider (SendGrid, Mailgun, your ISP…) instead of sending directly.
          </p>
          <label className="flex items-center gap-2 text-sm text-slate-700 mb-4 cursor-pointer">
            <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
            Send outbound mail through a smarthost
          </label>
          {enabled && (
            <>
              <div className="grid grid-cols-[1fr_6rem] gap-2">
                <Field label="Relay host"><Input value={host} onChange={(e) => setHost(e.target.value)} placeholder="smtp.provider.com" /></Field>
                <Field label="Port"><Input type="number" value={port} onChange={(e) => setPort(Number(e.target.value))} /></Field>
              </div>
              <Field label="Username"><Input value={username} onChange={(e) => setUsername(e.target.value)} /></Field>
              <Field label="Password" hint={data?.has_pass ? 'Leave blank to keep the stored password' : undefined}>
                <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} placeholder={data?.has_pass ? '••••••••' : ''} />
              </Field>
            </>
          )}
          <div className="flex justify-end">
            <Btn disabled={busy} onClick={save}>{enabled ? 'Save relay' : 'Disable relay'}</Btn>
          </div>
        </div>
      )}
    </Card>
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
