import { useState } from 'react'
import {
  ExternalLink, Settings2, RefreshCw, Trash2, Power, Plus, ArrowUpCircle,
  LogIn, Zap, ShieldCheck, Database, Wrench, Users as UsersIcon, KeyRound,
} from 'lucide-react'
import { api, useFetch } from '../api'
import type { WPSite, WPInfo, WPPlugin, WPTheme, WPUser, WPCronEvent, WPConfig } from '../types'
import {
  Btn, Card, PageHeader, Table, Td, Modal, Field, Input, Select,
  Spinner, ErrorBanner, Empty, Badge, toast,
} from '../components/ui'

type SitesResp = { wp_cli: boolean; sites: WPSite[] }

const WP_ROLES = ['administrator', 'editor', 'author', 'contributor', 'subscriber']

export default function WordPress() {
  const { data, error, loading, reload } = useFetch<SitesResp>('/api/wordpress')
  const [managing, setManaging] = useState<WPSite | null>(null)
  const [installing, setInstalling] = useState(false)

  if (loading) return <Spinner />

  const installCLI = async () => {
    setInstalling(true)
    try {
      await api.post('/api/wordpress/install-cli')
      toast('WP-CLI installed')
      reload()
    } catch (e) {
      toast((e as Error).message, 'err')
    } finally {
      setInstalling(false)
    }
  }

  return (
    <div>
      <PageHeader title="WordPress" subtitle="Manage core, plugins, themes, users, database and configuration for your WordPress sites" />
      <ErrorBanner message={error} />
      {data && !data.wp_cli && (
        <div className="rounded-md bg-amber-50 border border-amber-200 text-amber-800 text-sm px-4 py-3 mb-4 flex items-center justify-between gap-4">
          <span>
            WP-CLI (<code>wp</code>) is not installed on this server, so management actions won't work.
          </span>
          <Btn size="sm" onClick={installCLI} disabled={installing}>
            <ArrowUpCircle size={14} /> Install WP-CLI
          </Btn>
        </div>
      )}
      <Card>
        {!data?.sites.length ? (
          <Empty title="No WordPress sites" hint="Install WordPress on a domain from the Websites & Domains page." />
        ) : (
          <Table head={['Domain', 'URL', 'Status', '']}>
            {data.sites.map((s) => (
              <tr key={s.app_id} className="hover:bg-slate-50/60">
                <Td className="font-medium text-slate-700">{s.domain}</Td>
                <Td>
                  <a
                    href={s.url}
                    target="_blank"
                    rel="noreferrer"
                    className="inline-flex items-center gap-1 text-xs text-brand-600 hover:underline break-all"
                  >
                    {s.url}
                    <ExternalLink size={12} className="shrink-0" />
                  </a>
                </Td>
                <Td>
                  {s.status === 'installed' ? (
                    <Badge color="green">installed</Badge>
                  ) : (
                    <Badge color={s.status === 'failed' ? 'red' : 'amber'}>{s.status}</Badge>
                  )}
                </Td>
                <Td className="text-right">
                  <Btn size="sm" variant="secondary" onClick={() => setManaging(s)}>
                    <Settings2 size={13} /> Manage
                  </Btn>
                </Td>
              </tr>
            ))}
          </Table>
        )}
      </Card>

      {managing && <ManageModal site={managing} onClose={() => setManaging(null)} />}
    </div>
  )
}

type Tab = 'overview' | 'plugins' | 'themes' | 'users' | 'database' | 'tools'

function TabBtn({ active, onClick, children }: { active: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`px-3 py-1.5 text-sm font-medium border-b-2 -mb-px cursor-pointer whitespace-nowrap ${
        active ? 'border-brand-600 text-brand-700' : 'border-transparent text-slate-500 hover:text-slate-700'
      }`}
    >
      {children}
    </button>
  )
}

function ManageModal({ site, onClose }: { site: WPSite; onClose: () => void }) {
  const [tab, setTab] = useState<Tab>('overview')
  const id = site.app_id
  return (
    <Modal open title={`WordPress — ${site.domain}`} onClose={onClose} wide>
      <div className="flex gap-1 border-b border-slate-200 mb-4 overflow-x-auto">
        <TabBtn active={tab === 'overview'} onClick={() => setTab('overview')}>Overview</TabBtn>
        <TabBtn active={tab === 'plugins'} onClick={() => setTab('plugins')}>Plugins</TabBtn>
        <TabBtn active={tab === 'themes'} onClick={() => setTab('themes')}>Themes</TabBtn>
        <TabBtn active={tab === 'users'} onClick={() => setTab('users')}>Users</TabBtn>
        <TabBtn active={tab === 'database'} onClick={() => setTab('database')}>Database</TabBtn>
        <TabBtn active={tab === 'tools'} onClick={() => setTab('tools')}>Tools</TabBtn>
      </div>
      {tab === 'overview' && <OverviewTab id={id} />}
      {tab === 'plugins' && <PluginsTab id={id} />}
      {tab === 'themes' && <ThemesTab id={id} />}
      {tab === 'users' && <UsersTab id={id} />}
      {tab === 'database' && <DatabaseTab id={id} />}
      {tab === 'tools' && <ToolsTab id={id} />}
    </Modal>
  )
}

/** Wraps a mutation with busy state + toast + optional reload. */
function useAction(reload?: () => void) {
  const [busy, setBusy] = useState(false)
  const run = async (fn: () => Promise<unknown>, msg?: string) => {
    setBusy(true)
    try {
      await fn()
      if (msg) toast(msg)
      reload?.()
      return true
    } catch (e) {
      toast((e as Error).message, 'err')
      return false
    } finally {
      setBusy(false)
    }
  }
  return { busy, run }
}

function OverviewTab({ id }: { id: number }) {
  const { data, error, loading, reload } = useFetch<WPInfo>(`/api/wordpress/${id}`)
  const [title, setTitle] = useState('')
  const [tagline, setTagline] = useState('')
  const [search, setSearch] = useState(true)
  const [primed, setPrimed] = useState(false)
  const { busy, run } = useAction(reload)

  if (data && !primed) {
    setTitle(data.title)
    setTagline(data.tagline)
    setSearch(data.search_visible)
    setPrimed(true)
  }

  if (loading) return <Spinner />
  if (error) return <ErrorBanner message={error} />
  if (!data) return null

  const totalUpdates = (data.update_version ? 1 : 0) + data.plugin_updates + data.theme_updates

  return (
    <div className="space-y-5">
      <div className="flex items-center justify-between rounded-md border border-slate-200 px-4 py-3">
        <div className="text-sm">
          <div className="font-medium text-slate-700 flex items-center gap-2">
            WordPress {data.version}
            {data.multisite && <Badge color="blue">multisite</Badge>}
            {data.maintenance_mode && <Badge color="amber">maintenance</Badge>}
          </div>
          <div className="text-xs text-slate-400 mt-0.5">
            PHP {data.php_version || '—'}
            {data.update_version ? (
              <span className="text-amber-700"> · core update {data.update_version} available</span>
            ) : (
              <span> · core up to date</span>
            )}
          </div>
        </div>
        <div className="flex gap-2">
          {data.update_version && (
            <Btn variant="secondary" disabled={busy} onClick={() => run(() => api.post(`/api/wordpress/${id}/core/update`), 'Core updated')}>
              <ArrowUpCircle size={15} /> Update core
            </Btn>
          )}
          {totalUpdates > 0 && (
            <Btn disabled={busy} onClick={() => run(() => api.post(`/api/wordpress/${id}/update-all`), 'Everything updated')}>
              <Zap size={15} /> Update everything ({totalUpdates})
            </Btn>
          )}
        </div>
      </div>

      <div className="grid grid-cols-3 gap-3 text-center">
        <Stat label="Plugin updates" value={data.plugin_updates} />
        <Stat label="Theme updates" value={data.theme_updates} />
        <Stat label="Core" value={data.update_version ? 'update' : 'current'} />
      </div>

      <div>
        <Field label="Site title">
          <Input value={title} onChange={(e) => setTitle(e.target.value)} />
        </Field>
        <Field label="Tagline">
          <Input value={tagline} onChange={(e) => setTagline(e.target.value)} />
        </Field>
        <label className="flex items-center gap-2 text-sm text-slate-700 cursor-pointer mb-4">
          <input type="checkbox" checked={search} onChange={(e) => setSearch(e.target.checked)} />
          Allow search engines to index this site
        </label>
        <div className="flex justify-end">
          <Btn disabled={busy} onClick={() => run(() => api.post(`/api/wordpress/${id}/settings`, { title, tagline, search_visible: search }), 'Settings saved')}>
            Save settings
          </Btn>
        </div>
      </div>
    </div>
  )
}

function Stat({ label, value }: { label: string; value: number | string }) {
  return (
    <div className="rounded-md border border-slate-200 py-3">
      <div className="text-2xl font-semibold text-slate-700">{value}</div>
      <div className="text-xs text-slate-400">{label}</div>
    </div>
  )
}

/** Shared install + list shell for plugins and themes. */
function ExtensionTab({ id, kind }: { id: number; kind: 'plugin' | 'theme' }) {
  const path = kind === 'plugin' ? 'plugins' : 'themes'
  const { data, error, loading, reload } = useFetch<(WPPlugin | WPTheme)[]>(`/api/wordpress/${id}/${path}`)
  const [slug, setSlug] = useState('')
  const [activate, setActivate] = useState(kind === 'plugin')
  const { busy, run } = useAction(reload)

  const install = (e: React.FormEvent) => {
    e.preventDefault()
    run(() => api.post(`/api/wordpress/${id}/${path}`, { slug, activate }), `Installed ${slug}`).then((ok) => ok && setSlug(''))
  }
  const action = (name: string, act: string, msg: string) =>
    run(() => api.post(`/api/wordpress/${id}/${path}/action`, { slug: name, action: act }), msg)
  const toggleAuto = (name: string, enable: boolean) =>
    run(() => api.post(`/api/wordpress/${id}/auto-update`, { kind, slug: name, enable }), enable ? 'Auto-updates on' : 'Auto-updates off')

  const example = kind === 'plugin' ? 'woocommerce' : 'twentytwentyfour'

  return (
    <div>
      <form onSubmit={install} className="flex items-end gap-2 mb-4">
        <div className="flex-1">
          <Field label={`Install a ${kind}`} hint={`WordPress.org slug, e.g. ${example}`}>
            <Input value={slug} onChange={(e) => setSlug(e.target.value)} placeholder={`${kind}-slug`} required />
          </Field>
        </div>
        <label className="flex items-center gap-1.5 text-sm text-slate-600 mb-4">
          <input type="checkbox" checked={activate} onChange={(e) => setActivate(e.target.checked)} /> activate
        </label>
        <Btn type="submit" disabled={busy} className="mb-4">
          <Plus size={15} /> Install
        </Btn>
      </form>

      <ErrorBanner message={error} />
      {loading ? (
        <Spinner />
      ) : !data?.length ? (
        <Empty title={`No ${path} installed`} />
      ) : (
        <Table head={[kind === 'plugin' ? 'Plugin' : 'Theme', 'Status', 'Version', 'Auto', '']}>
          {data.map((item) => {
            const active = item.status === 'active'
            return (
              <tr key={item.name} className="hover:bg-slate-50/60">
                <Td className="font-medium">{item.title || item.name}</Td>
                <Td>{active ? <Badge color="green">active</Badge> : <Badge color="gray">{item.status}</Badge>}</Td>
                <Td className="whitespace-nowrap text-slate-600">
                  {item.version}
                  {item.update && <span className="ml-2 text-xs text-amber-700">→ {item.update_version}</span>}
                </Td>
                <Td>
                  <button
                    className={`text-xs cursor-pointer ${item.auto_update ? 'text-emerald-600' : 'text-slate-400 hover:text-slate-600'}`}
                    disabled={busy}
                    title={item.auto_update ? 'Disable auto-updates' : 'Enable auto-updates'}
                    onClick={() => toggleAuto(item.name, !item.auto_update)}
                  >
                    {item.auto_update ? 'on' : 'off'}
                  </button>
                </Td>
                <Td className="text-right whitespace-nowrap">
                  {item.update && (
                    <button className="text-slate-400 hover:text-brand-600 mr-3 cursor-pointer" title="Update" disabled={busy} onClick={() => action(item.name, 'update', `Updated ${item.name}`)}>
                      <RefreshCw size={15} />
                    </button>
                  )}
                  {kind === 'plugin' ? (
                    <button
                      className="text-slate-400 hover:text-brand-600 mr-3 cursor-pointer"
                      title={active ? 'Deactivate' : 'Activate'}
                      disabled={busy}
                      onClick={() => action(item.name, active ? 'deactivate' : 'activate', active ? `Deactivated ${item.name}` : `Activated ${item.name}`)}
                    >
                      <Power size={15} className={active ? 'text-emerald-600' : ''} />
                    </button>
                  ) : (
                    !active && (
                      <button className="text-slate-400 hover:text-brand-600 mr-3 cursor-pointer" title="Activate" disabled={busy} onClick={() => action(item.name, 'activate', `Activated ${item.name}`)}>
                        <Power size={15} />
                      </button>
                    )
                  )}
                  {!(kind === 'theme' && active) && (
                    <button
                      className="text-slate-400 hover:text-red-600 cursor-pointer"
                      title="Delete"
                      disabled={busy}
                      onClick={() => confirm(`Delete ${kind} ${item.name}?`) && action(item.name, 'delete', `Deleted ${item.name}`)}
                    >
                      <Trash2 size={15} />
                    </button>
                  )}
                </Td>
              </tr>
            )
          })}
        </Table>
      )}
    </div>
  )
}

function PluginsTab({ id }: { id: number }) {
  return <ExtensionTab id={id} kind="plugin" />
}
function ThemesTab({ id }: { id: number }) {
  return <ExtensionTab id={id} kind="theme" />
}

function UsersTab({ id }: { id: number }) {
  const { data, error, loading, reload } = useFetch<WPUser[]>(`/api/wordpress/${id}/users`)
  const { busy, run } = useAction(reload)
  const [adding, setAdding] = useState(false)
  const [pwUser, setPwUser] = useState<WPUser | null>(null)

  const login = (u: WPUser) =>
    run(async () => {
      const { url } = await api.post<{ url: string }>(`/api/wordpress/${id}/users/login`, { user_id: u.id })
      window.open(url, '_blank', 'noopener')
    })

  const setRole = (u: WPUser, role: string) =>
    run(() => api.post(`/api/wordpress/${id}/users/update`, { user_id: u.id, role }), `Role updated for ${u.login}`)

  const del = (u: WPUser) =>
    confirm(`Delete user ${u.login}? Their content is reassigned to user 1 (admin).`) &&
    run(() => api.post(`/api/wordpress/${id}/users/delete`, { user_id: u.id, reassign_to: 1 }), `Deleted ${u.login}`)

  return (
    <div>
      <div className="flex justify-between items-center mb-4">
        <div className="text-sm text-slate-500 flex items-center gap-1.5">
          <UsersIcon size={15} /> WordPress accounts
        </div>
        <Btn size="sm" onClick={() => setAdding(true)}>
          <Plus size={14} /> Add user
        </Btn>
      </div>
      <ErrorBanner message={error} />
      {loading ? (
        <Spinner />
      ) : !data?.length ? (
        <Empty title="No users" />
      ) : (
        <Table head={['User', 'Email', 'Role', '']}>
          {data.map((u) => (
            <tr key={u.id} className="hover:bg-slate-50/60">
              <Td className="font-medium">
                {u.display_name || u.login}
                <span className="text-xs text-slate-400 ml-1">@{u.login}</span>
              </Td>
              <Td className="text-slate-600 break-all">{u.email}</Td>
              <Td>
                <Select value={u.roles.split(',')[0] || ''} disabled={busy} onChange={(e) => setRole(u, e.target.value)}>
                  {WP_ROLES.map((r) => (
                    <option key={r} value={r}>{r}</option>
                  ))}
                  {u.roles && !WP_ROLES.includes(u.roles.split(',')[0]) && <option value={u.roles.split(',')[0]}>{u.roles}</option>}
                </Select>
              </Td>
              <Td className="text-right whitespace-nowrap">
                <button className="text-slate-400 hover:text-brand-600 mr-3 cursor-pointer" title="Log in as this user" disabled={busy} onClick={() => login(u)}>
                  <LogIn size={15} />
                </button>
                <button className="text-slate-400 hover:text-brand-600 mr-3 cursor-pointer" title="Reset password" disabled={busy} onClick={() => setPwUser(u)}>
                  <KeyRound size={15} />
                </button>
                <button className="text-slate-400 hover:text-red-600 cursor-pointer" title="Delete" disabled={busy} onClick={() => del(u)}>
                  <Trash2 size={15} />
                </button>
              </Td>
            </tr>
          ))}
        </Table>
      )}

      {adding && <AddUserModal id={id} onClose={() => setAdding(false)} onSaved={reload} />}
      {pwUser && <ResetPasswordModal id={id} user={pwUser} onClose={() => setPwUser(null)} />}
    </div>
  )
}

function AddUserModal({ id, onClose, onSaved }: { id: number; onClose: () => void; onSaved: () => void }) {
  const [login, setLogin] = useState('')
  const [email, setEmail] = useState('')
  const [role, setRole] = useState('subscriber')
  const [password, setPassword] = useState('')
  const { busy, run } = useAction()

  const submit = (e: React.FormEvent) => {
    e.preventDefault()
    run(() => api.post(`/api/wordpress/${id}/users`, { login, email, role, password }), `Created ${login}`).then((ok) => {
      if (ok) {
        onSaved()
        onClose()
      }
    })
  }

  return (
    <Modal open title="Add WordPress user" onClose={onClose}>
      <form onSubmit={submit}>
        <Field label="Username">
          <Input value={login} onChange={(e) => setLogin(e.target.value)} required />
        </Field>
        <Field label="Email">
          <Input type="email" value={email} onChange={(e) => setEmail(e.target.value)} required />
        </Field>
        <Field label="Role">
          <Select value={role} onChange={(e) => setRole(e.target.value)}>
            {WP_ROLES.map((r) => (
              <option key={r} value={r}>{r}</option>
            ))}
          </Select>
        </Field>
        <Field label="Password" hint="At least 8 characters">
          <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required minLength={8} />
        </Field>
        <div className="flex justify-end gap-2 mt-4">
          <Btn type="button" variant="secondary" onClick={onClose}>Cancel</Btn>
          <Btn type="submit" disabled={busy}>Create user</Btn>
        </div>
      </form>
    </Modal>
  )
}

function ResetPasswordModal({ id, user, onClose }: { id: number; user: WPUser; onClose: () => void }) {
  const [password, setPassword] = useState('')
  const { busy, run } = useAction()
  const submit = (e: React.FormEvent) => {
    e.preventDefault()
    run(() => api.post(`/api/wordpress/${id}/users/update`, { user_id: user.id, password }), `Password reset for ${user.login}`).then(
      (ok) => ok && onClose(),
    )
  }
  return (
    <Modal open title={`Reset password — ${user.login}`} onClose={onClose}>
      <form onSubmit={submit}>
        <Field label="New password" hint="At least 8 characters">
          <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required minLength={8} autoFocus />
        </Field>
        <div className="flex justify-end gap-2 mt-4">
          <Btn type="button" variant="secondary" onClick={onClose}>Cancel</Btn>
          <Btn type="submit" disabled={busy}>Reset password</Btn>
        </div>
      </form>
    </Modal>
  )
}

function DatabaseTab({ id }: { id: number }) {
  const [from, setFrom] = useState('')
  const [to, setTo] = useState('')
  const [report, setReport] = useState('')
  const { busy, run } = useAction()

  const searchReplace = (dryRun: boolean) => {
    setReport('')
    run(async () => {
      const res = await api.post<{ report: string }>(`/api/wordpress/${id}/search-replace`, { from, to, dry_run: dryRun })
      setReport(res.report || (dryRun ? 'No changes.' : 'Done.'))
    }, dryRun ? undefined : 'Search-replace applied')
  }

  return (
    <div className="space-y-6">
      <div>
        <h3 className="text-sm font-semibold text-slate-700 mb-1 flex items-center gap-1.5">
          <Database size={15} /> Search &amp; replace
        </h3>
        <p className="text-xs text-slate-400 mb-3">
          Rewrite values across all database tables — the core step when changing domains. Preview with a dry run first.
        </p>
        <div className="grid grid-cols-2 gap-3">
          <Field label="Search for">
            <Input value={from} onChange={(e) => setFrom(e.target.value)} placeholder="http://old-domain.com" />
          </Field>
          <Field label="Replace with">
            <Input value={to} onChange={(e) => setTo(e.target.value)} placeholder="https://new-domain.com" />
          </Field>
        </div>
        <div className="flex gap-2">
          <Btn variant="secondary" disabled={busy || !from || !to} onClick={() => searchReplace(true)}>Dry run</Btn>
          <Btn disabled={busy || !from || !to} onClick={() => confirm('Apply search-replace to the live database?') && searchReplace(false)}>
            Apply
          </Btn>
        </div>
        {report && <pre className="mt-3 text-xs bg-slate-50 border border-slate-200 rounded-md p-3 overflow-x-auto whitespace-pre-wrap">{report}</pre>}
      </div>

      <div className="border-t border-slate-200 pt-5">
        <h3 className="text-sm font-semibold text-slate-700 mb-3">Maintenance</h3>
        <div className="flex gap-2">
          <Btn variant="secondary" disabled={busy} onClick={() => run(() => api.post(`/api/wordpress/${id}/tool`, { tool: 'optimize-db' }), 'Database optimized')}>
            Optimize database
          </Btn>
          <a
            href={`/api/wordpress/${id}/db/export`}
            className="inline-flex items-center gap-1.5 rounded-md border border-slate-300 bg-white px-3 py-1.5 text-sm font-medium text-slate-700 hover:bg-slate-50"
          >
            Export (.sql)
          </a>
        </div>
      </div>
    </div>
  )
}

function ToolsTab({ id }: { id: number }) {
  const { data, error, loading, reload } = useFetch<WPInfo>(`/api/wordpress/${id}`)
  const cron = useFetch<WPCronEvent[]>(`/api/wordpress/${id}/cron`)
  const { busy, run } = useAction(reload)

  const tool = (name: string, msg: string) => run(() => api.post(`/api/wordpress/${id}/tool`, { tool: name }), msg)
  const maintenance = (enable: boolean) =>
    run(() => api.post(`/api/wordpress/${id}/maintenance`, { enable }), enable ? 'Maintenance mode on' : 'Maintenance mode off')

  return (
    <div className="space-y-6">
      <ErrorBanner message={error} />
      <div>
        <h3 className="text-sm font-semibold text-slate-700 mb-3 flex items-center gap-1.5">
          <Wrench size={15} /> Operations
        </h3>
        <div className="flex flex-wrap gap-2">
          {loading ? (
            <Spinner />
          ) : data?.maintenance_mode ? (
            <Btn variant="secondary" disabled={busy} onClick={() => maintenance(false)}>Disable maintenance mode</Btn>
          ) : (
            <Btn variant="secondary" disabled={busy} onClick={() => maintenance(true)}>Enable maintenance mode</Btn>
          )}
          <Btn variant="secondary" disabled={busy} onClick={() => tool('flush-cache', 'Cache flushed')}>Flush cache</Btn>
          <Btn variant="secondary" disabled={busy} onClick={() => tool('delete-transients', 'Transients cleared')}>Clear transients</Btn>
          <Btn variant="secondary" disabled={busy} onClick={() => tool('flush-rewrites', 'Permalinks flushed')}>Flush permalinks</Btn>
          <Btn variant="secondary" disabled={busy} onClick={() => tool('run-cron', 'Due cron events ran')}>Run cron now</Btn>
          <Btn variant="secondary" disabled={busy} onClick={() => tool('verify-checksums', 'Core files verified — no changes')}>
            <ShieldCheck size={14} /> Verify core
          </Btn>
          <Btn variant="secondary" disabled={busy} onClick={() => confirm('Regenerate salts? This logs everyone out.') && tool('regenerate-salts', 'Salts regenerated')}>
            Regenerate salts
          </Btn>
        </div>
      </div>

      <ConfigSection id={id} />

      <div className="border-t border-slate-200 pt-5">
        <h3 className="text-sm font-semibold text-slate-700 mb-3">Scheduled cron events</h3>
        {cron.loading ? (
          <Spinner />
        ) : cron.error ? (
          <ErrorBanner message={cron.error} />
        ) : !cron.data?.length ? (
          <Empty title="No scheduled events" />
        ) : (
          <Table head={['Hook', 'Next run', 'Schedule']}>
            {cron.data.map((e, i) => (
              <tr key={`${e.hook}-${i}`} className="hover:bg-slate-50/60">
                <Td className="font-mono text-xs">{e.hook}</Td>
                <Td className="text-slate-600">{e.next_run}</Td>
                <Td className="text-slate-500">{e.schedule || 'one-off'}</Td>
              </tr>
            ))}
          </Table>
        )}
      </div>
    </div>
  )
}

function ConfigSection({ id }: { id: number }) {
  const { data, error, loading, reload } = useFetch<WPConfig>(`/api/wordpress/${id}/config`)
  const [cfg, setCfg] = useState<WPConfig | null>(null)
  const { busy, run } = useAction(reload)

  if (data && !cfg) setCfg(data)
  const c = cfg

  const save = () =>
    c && run(() => api.post(`/api/wordpress/${id}/config`, c), 'Configuration saved')

  return (
    <div className="border-t border-slate-200 pt-5">
      <h3 className="text-sm font-semibold text-slate-700 mb-3 flex items-center gap-1.5">
        <Settings2 size={15} /> wp-config constants
      </h3>
      <ErrorBanner message={error} />
      {loading || !c ? (
        <Spinner />
      ) : (
        <div className="space-y-2">
          <ConfigToggle label="Debug mode (WP_DEBUG)" checked={c.debug} onChange={(v) => setCfg({ ...c, debug: v })} />
          <ConfigToggle label="Log debug to file (WP_DEBUG_LOG)" checked={c.debug_log} onChange={(v) => setCfg({ ...c, debug_log: v })} />
          <ConfigToggle label="Disable theme/plugin file editor (DISALLOW_FILE_EDIT)" checked={c.disallow_file_edit} onChange={(v) => setCfg({ ...c, disallow_file_edit: v })} />
          <div className="grid grid-cols-2 gap-3 pt-1">
            <Field label="Memory limit (WP_MEMORY_LIMIT)" hint="e.g. 256M, blank for default">
              <Input value={c.memory_limit} onChange={(e) => setCfg({ ...c, memory_limit: e.target.value })} placeholder="256M" />
            </Field>
            <Field label="Core auto-updates (WP_AUTO_UPDATE_CORE)">
              <Select value={c.auto_update_core || 'minor'} onChange={(e) => setCfg({ ...c, auto_update_core: e.target.value })}>
                <option value="minor">minor only</option>
                <option value="true">all releases</option>
                <option value="false">disabled</option>
              </Select>
            </Field>
          </div>
          <div className="flex justify-end pt-1">
            <Btn disabled={busy} onClick={save}>Save configuration</Btn>
          </div>
        </div>
      )}
    </div>
  )
}

function ConfigToggle({ label, checked, onChange }: { label: string; checked: boolean; onChange: (v: boolean) => void }) {
  return (
    <label className="flex items-center gap-2 text-sm text-slate-700 cursor-pointer">
      <input type="checkbox" checked={checked} onChange={(e) => onChange(e.target.checked)} />
      {label}
    </label>
  )
}
