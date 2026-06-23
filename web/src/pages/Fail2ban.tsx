import { useState } from 'react'
import { ShieldX, Ban, Plus, Trash2, Save, SlidersHorizontal } from 'lucide-react'
import { api, useFetch } from '../api'
import type { Fail2banStatus, Fail2banConfig, Fail2banJailConfig, Fail2banFilter } from '../types'
import { Btn, Card, PageHeader, Table, Td, Modal, Spinner, ErrorBanner, Empty, Badge, Input, Select, Field, toast } from '../components/ui'

export default function Fail2ban() {
  const { data, error, loading, reload } = useFetch<Fail2banStatus>('/api/fail2ban')
  const [banJail, setBanJail] = useState('')
  const [banIP, setBanIP] = useState('')
  const [wl, setWl] = useState('')

  if (loading) return <Spinner />

  if (data && !data.available) {
    return (
      <div>
        <PageHeader title="Fail2ban" subtitle="Brute-force protection" />
        <Card>
          <Empty title="fail2ban not available" hint="fail2ban-client was not found on this server." />
        </Card>
      </div>
    )
  }

  const jails = data?.jails ?? []
  const whitelist = data?.whitelist ?? []

  const act = async (fn: () => Promise<unknown>, msg: string) => {
    try {
      await fn()
      toast(msg)
      reload()
    } catch (e) {
      toast((e as Error).message, 'err')
    }
  }

  const ban = (e: React.FormEvent) => {
    e.preventDefault()
    act(() => api.post('/api/fail2ban/ban', { jail: banJail || jails[0]?.name, ip: banIP }), `Banned ${banIP}`).then(() => setBanIP(''))
  }
  const unban = (jail: string, ip: string) => act(() => api.post('/api/fail2ban/ban', { jail, ip, unban: true }), `Unbanned ${ip}`)

  const addWhitelist = (e: React.FormEvent) => {
    e.preventDefault()
    if (!wl.trim()) return
    act(() => api.post('/api/fail2ban/whitelist', { entries: [...whitelist, wl.trim()] }), 'Whitelist updated').then(() => setWl(''))
  }
  const removeWhitelist = (ip: string) =>
    act(() => api.post('/api/fail2ban/whitelist', { entries: whitelist.filter((x) => x !== ip) }), 'Whitelist updated')

  return (
    <div>
      <PageHeader title="Fail2ban" subtitle="Jails, banned addresses and the never-ban whitelist." />
      <ErrorBanner message={error} />

      <div className="space-y-4">
        <Card title="Ban an address">
          <form onSubmit={ban} className="flex items-end gap-2">
            <div className="w-48">
              <label className="block text-xs text-slate-500 mb-1">Jail</label>
              <select value={banJail} onChange={(e) => setBanJail(e.target.value)} className="w-full rounded-md border border-slate-300 px-3 py-2 text-sm bg-white">
                {jails.map((j) => <option key={j.name} value={j.name}>{j.name}</option>)}
              </select>
            </div>
            <div className="flex-1">
              <label className="block text-xs text-slate-500 mb-1">IP address</label>
              <Input value={banIP} onChange={(e) => setBanIP(e.target.value)} placeholder="203.0.113.5" required />
            </div>
            <Btn type="submit"><Ban size={15} /> Ban</Btn>
          </form>
        </Card>

        {data?.config && <ConfigEditor config={data.config} filters={data.filters ?? []} onSaved={reload} />}

        {data?.available && <CustomFilters names={data.custom_filters ?? []} onChanged={reload} />}

        {jails.map((j) => (
          <Card
            key={j.name}
            title={
              <span className="flex items-center gap-2">
                <ShieldX size={15} className="text-brand-600" /> {j.name}
                <span className="text-slate-400 font-normal text-xs">
                  {j.banned.length} banned now · {j.total} total · {j.failed} currently failing
                </span>
              </span>
            }
          >
            {!j.banned.length ? (
              <p className="text-sm text-slate-400">No addresses currently banned.</p>
            ) : (
              <Table head={['Banned IP', '']}>
                {j.banned.map((ip) => (
                  <tr key={ip} className="hover:bg-slate-50/60">
                    <Td className="font-mono text-sm">{ip}</Td>
                    <Td className="text-right">
                      <Btn size="sm" variant="secondary" onClick={() => unban(j.name, ip)}>Unban</Btn>
                    </Td>
                  </tr>
                ))}
              </Table>
            )}
          </Card>
        ))}

        <Card title="Whitelist (never banned)">
          <form onSubmit={addWhitelist} className="flex items-end gap-2 mb-3">
            <div className="flex-1">
              <Input value={wl} onChange={(e) => setWl(e.target.value)} placeholder="198.51.100.0/24 or a single IP" />
            </div>
            <Btn type="submit"><Plus size={15} /> Add</Btn>
          </form>
          {!whitelist.length ? (
            <p className="text-sm text-slate-400">Only loopback is whitelisted.</p>
          ) : (
            <div className="flex flex-wrap gap-2">
              {whitelist.map((ip) => (
                <span key={ip} className="inline-flex items-center gap-1.5 rounded-full bg-slate-100 px-2.5 py-1 text-xs">
                  <span className="font-mono">{ip}</span>
                  <button onClick={() => removeWhitelist(ip)} className="text-slate-400 hover:text-red-600 cursor-pointer"><Trash2 size={12} /></button>
                </span>
              ))}
            </div>
          )}
          <Badge color="gray">loopback always whitelisted</Badge>
        </Card>
      </div>
    </div>
  )
}

const blankJail: Fail2banJailConfig = {
  name: '', enabled: true, running: false, maxretry: '', bantime: '', findtime: '', filter: '', logpath: '', port: '',
}

function ConfigEditor({ config, filters, onSaved }: { config: Fail2banConfig; filters: string[]; onSaved: () => void }) {
  const [cfg, setCfg] = useState<Fail2banConfig>(() => structuredClone(config))
  const [busy, setBusy] = useState(false)
  const [addOpen, setAddOpen] = useState(false)
  const [draft, setDraft] = useState<Fail2banJailConfig>(blankJail)

  const setDefault = (k: keyof Fail2banConfig['defaults'], v: string) =>
    setCfg((c) => ({ ...c, defaults: { ...c.defaults, [k]: v } }))

  const setJail = (i: number, patch: Partial<Fail2banJailConfig>) =>
    setCfg((c) => ({ ...c, jails: c.jails.map((j, idx) => (idx === i ? { ...j, ...patch } : j)) }))

  const removeJail = (i: number) => setCfg((c) => ({ ...c, jails: c.jails.filter((_, idx) => idx !== i) }))

  const save = async (next?: Fail2banConfig) => {
    const payload = next ?? cfg
    setBusy(true)
    try {
      await api.post('/api/fail2ban/config', payload)
      toast('Fail2ban configuration saved')
      onSaved()
    } catch (e) {
      toast((e as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  const addJail = async (e: React.FormEvent) => {
    e.preventDefault()
    const name = draft.name.trim()
    if (!name) return
    if (cfg.jails.some((j) => j.name === name)) {
      toast(`A jail named "${name}" is already listed`, 'err')
      return
    }
    // Persist immediately so fail2ban validates the new jail and reports any
    // problem (bad filter/log path) while the form is still open to fix it.
    const next = { ...cfg, jails: [...cfg.jails, { ...draft, name }] }
    setBusy(true)
    try {
      await api.post('/api/fail2ban/config', next)
      toast(`Jail "${name}" added`)
      setCfg(next)
      setAddOpen(false)
      setDraft(blankJail)
      onSaved()
    } catch (err) {
      toast((err as Error).message, 'err') // keep the modal open to correct it
    } finally {
      setBusy(false)
    }
  }

  const small = 'w-20 rounded-md border border-slate-300 px-2 py-1 text-xs bg-white'

  return (
    <Card
      title={
        <span className="flex items-center gap-2">
          <SlidersHorizontal size={15} className="text-brand-600" /> Configuration
        </span>
      }
      actions={
        <>
          <Btn size="sm" variant="secondary" onClick={() => { setDraft(blankJail); setAddOpen(true) }}>
            <Plus size={14} /> Add jail
          </Btn>
          <Btn size="sm" disabled={busy} onClick={() => save()}>
            <Save size={14} /> {busy ? 'Saving…' : 'Save'}
          </Btn>
        </>
      }
    >
      <p className="text-xs text-slate-400 mb-3">
        Global defaults apply to every jail unless a jail overrides them. Blank = use the distribution default.
        Times accept fail2ban units (e.g. <span className="font-mono">600</span>, <span className="font-mono">10m</span>,{' '}
        <span className="font-mono">1h</span>, <span className="font-mono">1d</span>; <span className="font-mono">-1</span> = permanent).
        Changes are validated before they're applied.
      </p>

      <div className="grid grid-cols-3 gap-3 mb-5 max-w-lg">
        <Field label="Ban time" hint="how long a ban lasts">
          <Input value={cfg.defaults.bantime} placeholder="10m" onChange={(e) => setDefault('bantime', e.target.value)} />
        </Field>
        <Field label="Find time" hint="window for retries">
          <Input value={cfg.defaults.findtime} placeholder="10m" onChange={(e) => setDefault('findtime', e.target.value)} />
        </Field>
        <Field label="Max retry" hint="failures before ban">
          <Input value={cfg.defaults.maxretry} placeholder="5" onChange={(e) => setDefault('maxretry', e.target.value)} />
        </Field>
      </div>

      <div className="text-sm font-medium text-slate-700 mb-2">Jails</div>
      <Table head={['Jail', 'Enabled', 'Max retry', 'Ban time', 'Find time', '']}>
        {cfg.jails.map((j, i) => (
          <tr key={j.name} className="hover:bg-slate-50/60">
            <Td>
              <span className="font-medium">{j.name}</span>
              {j.running && <Badge color="green">running</Badge>}
              {(j.filter || j.logpath) && (
                <div className="text-xs text-slate-400 font-mono">
                  {j.filter && <span>filter: {j.filter}</span>}
                  {j.filter && j.logpath && <span> · </span>}
                  {j.logpath && <span>log: {j.logpath}</span>}
                </div>
              )}
            </Td>
            <Td>
              <input type="checkbox" checked={j.enabled} onChange={(e) => setJail(i, { enabled: e.target.checked })} />
            </Td>
            <Td><input className={small} value={j.maxretry} placeholder="default" onChange={(e) => setJail(i, { maxretry: e.target.value })} /></Td>
            <Td><input className={small} value={j.bantime} placeholder="default" onChange={(e) => setJail(i, { bantime: e.target.value })} /></Td>
            <Td><input className={small} value={j.findtime} placeholder="default" onChange={(e) => setJail(i, { findtime: e.target.value })} /></Td>
            <Td className="text-right">
              <button className="text-slate-400 hover:text-red-600 cursor-pointer" title="Remove jail" onClick={() => removeJail(i)}>
                <Trash2 size={14} />
              </button>
            </Td>
          </tr>
        ))}
      </Table>

      <Modal open={addOpen} title="Add jail" onClose={() => setAddOpen(false)}>
        <form onSubmit={addJail}>
          <Field label="Jail name" hint="A unique name, e.g. my-app-auth">
            <Input value={draft.name} onChange={(e) => setDraft({ ...draft, name: e.target.value })} placeholder="my-app-auth" required />
          </Field>
          <Field label="Filter" hint="The filter.d rule that matches failures in the log">
            <Select value={draft.filter} onChange={(e) => setDraft({ ...draft, filter: e.target.value })}>
              <option value="">Select a filter…</option>
              {filters.map((f) => (
                <option key={f} value={f}>{f}</option>
              ))}
            </Select>
          </Field>
          <Field label="Log path" hint="File to watch (globs and space-separated paths allowed)">
            <Input value={draft.logpath} onChange={(e) => setDraft({ ...draft, logpath: e.target.value })} placeholder="/var/log/myapp/auth.log" />
          </Field>
          <div className="grid grid-cols-3 gap-3">
            <Field label="Port" hint="optional">
              <Input value={draft.port} onChange={(e) => setDraft({ ...draft, port: e.target.value })} placeholder="http,https" />
            </Field>
            <Field label="Max retry" hint="optional">
              <Input value={draft.maxretry} onChange={(e) => setDraft({ ...draft, maxretry: e.target.value })} placeholder="5" />
            </Field>
            <Field label="Ban time" hint="optional">
              <Input value={draft.bantime} onChange={(e) => setDraft({ ...draft, bantime: e.target.value })} placeholder="1h" />
            </Field>
          </div>
          <label className="flex items-center gap-2 text-sm text-slate-700 mb-4">
            <input type="checkbox" checked={draft.enabled} onChange={(e) => setDraft({ ...draft, enabled: e.target.checked })} />
            Enable this jail
          </label>
          <div className="flex justify-end gap-2">
            <Btn type="button" variant="secondary" onClick={() => setAddOpen(false)}>Cancel</Btn>
            <Btn type="submit" disabled={busy}>Add jail</Btn>
          </div>
        </form>
      </Modal>
    </Card>
  )
}

const blankFilter: Fail2banFilter = { name: '', failregex: '', ignoreregex: '', custom: true }

function CustomFilters({ names, onChanged }: { names: string[]; onChanged: () => void }) {
  const [editing, setEditing] = useState<Fail2banFilter | null>(null)
  const [isNew, setIsNew] = useState(false)
  const [busy, setBusy] = useState(false)

  const openNew = () => {
    setEditing({ ...blankFilter })
    setIsNew(true)
  }
  const openEdit = async (name: string) => {
    try {
      const f = await api.get<Fail2banFilter>(`/api/fail2ban/filter?name=${encodeURIComponent(name)}`)
      setEditing(f)
      setIsNew(false)
    } catch (e) {
      toast((e as Error).message, 'err')
    }
  }

  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!editing) return
    setBusy(true)
    try {
      await api.post('/api/fail2ban/filter', {
        name: editing.name.trim(),
        failregex: editing.failregex,
        ignoreregex: editing.ignoreregex,
      })
      toast(`Filter "${editing.name}" saved`)
      setEditing(null)
      onChanged()
    } catch (err) {
      toast((err as Error).message, 'err') // keep open to fix
    } finally {
      setBusy(false)
    }
  }

  const remove = async (name: string) => {
    if (!confirm(`Delete the custom filter "${name}"?`)) return
    try {
      await api.del(`/api/fail2ban/filter/${encodeURIComponent(name)}`)
      toast(`Filter "${name}" deleted`)
      onChanged()
    } catch (e) {
      toast((e as Error).message, 'err')
    }
  }

  const ta =
    'w-full h-24 rounded-md border border-slate-300 px-3 py-2 text-xs font-mono focus:outline-none focus:ring-2 focus:ring-brand-500/40 focus:border-brand-500 bg-white'

  return (
    <Card
      title={
        <span className="flex items-center gap-2">
          <ShieldX size={15} className="text-brand-600" /> Custom filters
        </span>
      }
      actions={
        <Btn size="sm" variant="secondary" onClick={openNew}>
          <Plus size={14} /> Add filter
        </Btn>
      }
    >
      <p className="text-xs text-slate-400 mb-3">
        Define a failregex that matches an app's failed-login lines (it must include the{' '}
        <span className="font-mono">&lt;HOST&gt;</span> token). Saved filters become selectable when adding a jail.
      </p>
      {!names.length ? (
        <p className="text-sm text-slate-400">No custom filters yet.</p>
      ) : (
        <div className="flex flex-wrap gap-2">
          {names.map((n) => (
            <span key={n} className="inline-flex items-center gap-1.5 rounded-full bg-slate-100 px-2.5 py-1 text-xs">
              <button className="font-mono text-brand-700 hover:underline cursor-pointer" onClick={() => openEdit(n)}>{n}</button>
              <button onClick={() => remove(n)} className="text-slate-400 hover:text-red-600 cursor-pointer" title="Delete filter">
                <Trash2 size={12} />
              </button>
            </span>
          ))}
        </div>
      )}

      <Modal open={!!editing} title={isNew ? 'Add filter' : `Edit filter — ${editing?.name ?? ''}`} onClose={() => setEditing(null)} wide>
        {editing && (
          <form onSubmit={save}>
            <Field label="Filter name" hint="Letters, digits, dot, dash, underscore">
              <Input
                value={editing.name}
                onChange={(e) => setEditing({ ...editing, name: e.target.value })}
                placeholder="my-app-auth"
                required
                disabled={!isNew}
              />
            </Field>
            <Field label="failregex" hint="One pattern per line; each must contain <HOST>">
              <textarea
                className={ta}
                spellCheck={false}
                value={editing.failregex}
                onChange={(e) => setEditing({ ...editing, failregex: e.target.value })}
                placeholder={'^.*authentication failed.* from <HOST>.*$'}
                required
              />
            </Field>
            <Field label="ignoreregex" hint="Optional — lines matching these are never counted">
              <textarea
                className={ta}
                spellCheck={false}
                value={editing.ignoreregex}
                onChange={(e) => setEditing({ ...editing, ignoreregex: e.target.value })}
              />
            </Field>
            <div className="flex justify-end gap-2">
              <Btn type="button" variant="secondary" onClick={() => setEditing(null)}>Cancel</Btn>
              <Btn type="submit" disabled={busy}>{busy ? 'Saving…' : 'Save filter'}</Btn>
            </div>
          </form>
        )}
      </Modal>
    </Card>
  )
}
