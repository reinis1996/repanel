import { useEffect, useState } from 'react'
import { Plus, Lock, LockOpen, Package, Loader2, ExternalLink, Trash2, Boxes, RefreshCw, Power, FileCode2, BarChart3, Shield, ShieldCheck, CornerUpRight, SlidersHorizontal, FolderLock, Globe } from 'lucide-react'
import { api, useFetch, formatDate } from '../api'
import type { Domain, User, App, WebServerInfo, NodeApp, SiteConfig, WebStats, WAFStatus, PHPSettings, ProtectedDir, CatalogApp } from '../types'
import { useAuth } from '../App'
import {
  Btn, Card, PageHeader, Table, Td, Modal, Field, Input, Select,
  Spinner, ErrorBanner, Empty, Badge, toast, inputCls,
} from '../components/ui'

// runtimeValue / parseRuntime encode the per-domain runtime selector as
// "php:<ver>" or "node:<ver>".
const runtimeValue = (d: Domain) => (d.runtime === 'node' ? `node:${d.node_version}` : `php:${d.php_version}`)
const parseRuntime = (v: string): { runtime: string; version: string } => {
  const [runtime, version = ''] = v.split(':')
  return { runtime: runtime === 'node' ? 'node' : 'php', version }
}

// Human-friendly labels for the per-domain web server modes.
const WEB_MODE_LABELS: Record<string, string> = {
  nginx: 'nginx',
  apache: 'Apache',
  'nginx-apache': 'nginx → Apache',
}

export default function Domains() {
  const { user } = useAuth()
  const isAdminish = user!.role !== 'user'
  const isAdmin = user!.role === 'admin'
  const { data, error, loading, reload } = useFetch<Domain[]>('/api/domains')
  const phpVersions = useFetch<string[]>('/api/php-versions')
  const nodeVersions = useFetch<string[]>('/api/node-versions')
  const usersList = useFetch<User[]>(isAdminish ? '/api/users' : '/api/me')
  const apps = useFetch<App[]>('/api/apps')
  const catalog = useFetch<CatalogApp[]>('/api/apps/catalog')
  const catalogByID = new Map((catalog.data ?? []).map((c) => [c.id, c]))
  const webserver = useFetch<WebServerInfo>('/api/webserver')
  const appByDomain = new Map((apps.data ?? []).map((a) => [a.domain_id, a]))
  const webModes = webserver.data?.modes ?? []
  const showWebMode = webModes.length > 1

  const [addOpen, setAddOpen] = useState(false)
  const [name, setName] = useState('')
  const [runtimeSel, setRuntimeSel] = useState('')
  const [webMode, setWebMode] = useState('')
  const [kind, setKind] = useState<'primary' | 'subdomain' | 'alias'>('primary')
  const [parentId, setParentId] = useState(0)
  // Alternative hostnames (space/comma separated). Auto-filled with www.<name>
  // for a primary domain until the user edits the field themselves.
  const [aliases, setAliases] = useState('')
  const [aliasesTouched, setAliasesTouched] = useState(false)
  const [aliasMode, setAliasMode] = useState<'mirror' | 'redirect'>('mirror')
  const [nodeFor, setNodeFor] = useState<Domain | null>(null)
  const [configFor, setConfigFor] = useState<Domain | null>(null)
  const [statsFor, setStatsFor] = useState<Domain | null>(null)
  const [wafFor, setWafFor] = useState<Domain | null>(null)
  const [redirectFor, setRedirectFor] = useState<Domain | null>(null)
  const [aliasFor, setAliasFor] = useState<Domain | null>(null)
  const [phpFor, setPhpFor] = useState<Domain | null>(null)
  const [protectedFor, setProtectedFor] = useState<Domain | null>(null)
  const [owner, setOwner] = useState(0)
  const [createDNS, setCreateDNS] = useState(true)
  const [busy, setBusy] = useState(false)

  const primaryDomains = (data ?? []).filter((d) => d.kind === 'primary')

  // WordPress install modal state.
  const [catalogFor, setCatalogFor] = useState<Domain | null>(null)
  const [removeAppFor, setRemoveAppFor] = useState<App | null>(null)
  const [purgeApp, setPurgeApp] = useState(false)
  const [installFor, setInstallFor] = useState<Domain | null>(null)
  const [wpTitle, setWpTitle] = useState('')
  const [wpUser, setWpUser] = useState('admin')
  const [wpEmail, setWpEmail] = useState('')
  const [wpPass, setWpPass] = useState('')

  // Poll while an install is in progress so status updates live.
  const anyInstalling = (apps.data ?? []).some((a) => a.status === 'installing')
  useEffect(() => {
    if (!anyInstalling) return
    const t = setInterval(apps.reload, 4000)
    return () => clearInterval(t)
  }, [anyInstalling, apps.reload])

  // The www.<name> default tracks the typed name until the user edits aliases.
  const autoAlias = (n: string, k: typeof kind) => (k === 'primary' && n ? `www.${n}` : '')
  const changeName = (v: string) => {
    setName(v)
    if (!aliasesTouched) setAliases(autoAlias(v, kind))
  }
  const changeKind = (k: typeof kind) => {
    setKind(k)
    if (!aliasesTouched) setAliases(autoAlias(name, k))
  }

  const create = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      const rt = parseRuntime(runtimeSel)
      await api.post('/api/domains', {
        name,
        runtime: rt.runtime,
        version: rt.version || undefined,
        web_mode: webMode || undefined,
        user_id: owner || undefined,
        create_dns: kind === 'subdomain' ? false : createDNS,
        kind,
        parent_id: kind === 'primary' ? undefined : parentId || undefined,
        alias_mode: kind === 'alias' ? aliasMode : undefined,
        aliases: aliases.split(/[\s,]+/).filter(Boolean),
      })
      toast(`${kind === 'primary' ? 'Domain' : kind} ${name} created`)
      setAddOpen(false)
      setName('')
      setWebMode('')
      setKind('primary')
      setParentId(0)
      setAliasMode('mirror')
      setAliases('')
      setAliasesTouched(false)
      reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  const openAdd = () => {
    setKind('primary')
    setParentId(0)
    setName('')
    setAliases('')
    setAliasesTouched(false)
    setAddOpen(true)
  }

  const remove = async (d: Domain) => {
    if (!confirm(`Delete domain ${d.name}? Site files are kept on disk.`)) return
    try {
      await api.del(`/api/domains/${d.id}`)
      toast(`Domain ${d.name} deleted`)
      reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    }
  }

  const suspend = async (d: Domain) => {
    try {
      await api.post(`/api/domains/${d.id}/suspend`)
      toast(d.suspended ? `${d.name} unsuspended` : `${d.name} suspended`)
      reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    }
  }

  const changeRuntime = async (d: Domain, value: string) => {
    const rt = parseRuntime(value)
    try {
      await api.post(`/api/domains/${d.id}/runtime`, { runtime: rt.runtime, version: rt.version })
      toast(`${d.name} switched to ${rt.runtime === 'node' ? 'Node' : 'PHP'} ${rt.version}`)
      reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    }
  }

  const changeWebMode = async (d: Domain, mode: string) => {
    try {
      await api.post(`/api/domains/${d.id}/webserver`, { mode })
      toast(`${d.name} now served by ${WEB_MODE_LABELS[mode] ?? mode}`)
      reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    }
  }

  const installWordPress = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!installFor) return
    setBusy(true)
    try {
      const res = await api.post<{ message: string }>(`/api/domains/${installFor.id}/apps`, {
        app: 'wordpress',
        title: wpTitle,
        admin_user: wpUser,
        admin_password: wpPass,
        admin_email: wpEmail,
      })
      toast(res.message)
      setInstallFor(null)
      setWpTitle('')
      setWpUser('admin')
      setWpEmail('')
      setWpPass('')
      apps.reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  // Pick an app from the catalog: WordPress opens its config form; everything
  // else installs straight away (DB auto-provisioned, finished in the browser).
  const pickApp = async (domain: Domain, app: CatalogApp) => {
    if (app.id === 'wordpress') {
      setCatalogFor(null)
      setInstallFor(domain)
      return
    }
    setBusy(true)
    try {
      const res = await api.post<{ message: string }>(`/api/domains/${domain.id}/apps`, { app: app.id })
      toast(res.message)
      setCatalogFor(null)
      apps.reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  const doRemoveApp = async () => {
    if (!removeAppFor) return
    setBusy(true)
    try {
      await api.del(`/api/apps/${removeAppFor.id}${purgeApp ? '?purge=1' : ''}`)
      toast(purgeApp ? 'App, database and files deleted' : 'App removed from the panel')
      setRemoveAppFor(null)
      apps.reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  if (loading) return <Spinner />

  return (
    <div>
      <PageHeader
        title="Websites & Domains"
        subtitle="Each domain gets its own vhost, an isolated PHP-FPM pool and its own document root"
        actions={
          <Btn onClick={openAdd}>
            <Plus size={16} /> Add Domain
          </Btn>
        }
      />
      <ErrorBanner message={error} />
      <Card>
        {!data?.length ? (
          <Empty title="No domains yet" hint="Add your first domain to start hosting" />
        ) : (
          <Table
            head={[
              'Domain',
              'Owner',
              'Runtime',
              ...(showWebMode ? ['Web'] : []),
              'SSL',
              'Application',
              'Status',
              '',
            ]}
          >
            {data.map((d) => (
              <tr key={d.id} className="hover:bg-slate-50/60">
                <Td>
                  <div className="flex items-center gap-2">
                    <a
                      href={`http${d.ssl ? 's' : ''}://${d.name}`}
                      target="_blank"
                      rel="noreferrer"
                      className="font-medium text-brand-600 hover:underline"
                    >
                      {d.name}
                    </a>
                    {d.kind === 'subdomain' && <Badge color="blue">subdomain</Badge>}
                    {d.kind === 'alias' && <Badge color="gray">alias</Badge>}
                    {d.redirect_url && <Badge color="amber">→ redirect</Badge>}
                  </div>
                  <div className="text-xs text-slate-400">
                    {d.redirect_url ? `forwards to ${d.redirect_url}` : d.document_root}
                    {d.parent && <span className="ml-1 text-slate-300">· under {d.parent}</span>}
                  </div>
                </Td>
                <Td>{d.owner ?? '—'}</Td>
                <Td>
                  {d.kind === 'alias' || d.redirect_url ? (
                    <span className="text-xs text-slate-400">{d.redirect_url ? 'redirect' : 'mirror'}</span>
                  ) : (
                  <div className="flex items-center gap-1.5">
                    <select
                      className="text-sm border border-slate-200 rounded px-1.5 py-0.5 bg-white"
                      value={runtimeValue(d)}
                      onChange={(e) => changeRuntime(d, e.target.value)}
                    >
                      <optgroup label="PHP">
                        {(phpVersions.data ?? []).map((v) => (
                          <option key={`php:${v}`} value={`php:${v}`}>
                            PHP {v}
                          </option>
                        ))}
                      </optgroup>
                      {!!nodeVersions.data?.length && (
                        <optgroup label="Node.js">
                          {nodeVersions.data.map((v) => (
                            <option key={`node:${v}`} value={`node:${v}`}>
                              Node {v}
                            </option>
                          ))}
                        </optgroup>
                      )}
                    </select>
                    {d.runtime === 'node' && (
                      <button
                        className="text-slate-400 hover:text-brand-600 cursor-pointer"
                        title="Node app settings"
                        onClick={() => setNodeFor(d)}
                      >
                        <Boxes size={15} />
                      </button>
                    )}
                  </div>
                  )}
                </Td>
                {showWebMode && (
                  <Td>
                    <select
                      className="text-sm border border-slate-200 rounded px-1.5 py-0.5 bg-white"
                      value={d.web_mode}
                      onChange={(e) => changeWebMode(d, e.target.value)}
                    >
                      {webModes.map((m) => (
                        <option key={m} value={m}>
                          {WEB_MODE_LABELS[m] ?? m}
                        </option>
                      ))}
                    </select>
                  </Td>
                )}
                <Td>
                  {d.ssl ? (
                    <span className="inline-flex items-center gap-1 text-emerald-600 text-sm">
                      <Lock size={14} /> on
                    </span>
                  ) : (
                    <span className="inline-flex items-center gap-1 text-slate-400 text-sm">
                      <LockOpen size={14} /> off
                    </span>
                  )}
                </Td>
                <Td>
                  <AppCell
                    app={appByDomain.get(d.id)}
                    catalog={catalogByID}
                    canInstall={d.runtime === 'php' && !d.redirect_url}
                    onInstall={() => setCatalogFor(d)}
                    onRemove={(a) => { setPurgeApp(false); setRemoveAppFor(a) }}
                  />
                </Td>
                <Td>{d.suspended ? <Badge color="red">suspended</Badge> : <Badge color="green">active</Badge>}</Td>
                <Td className="text-right whitespace-nowrap">
                  <button
                    className="text-slate-400 hover:text-brand-600 cursor-pointer mr-3 align-middle"
                    title="Web statistics"
                    onClick={() => setStatsFor(d)}
                  >
                    <BarChart3 size={16} />
                  </button>
                  <button
                    className={`cursor-pointer mr-3 align-middle ${d.waf_enabled ? 'text-emerald-600 hover:text-emerald-700' : 'text-slate-400 hover:text-brand-600'}`}
                    title={d.waf_enabled ? 'WAF enabled' : 'Web Application Firewall'}
                    onClick={() => setWafFor(d)}
                  >
                    {d.waf_enabled ? <ShieldCheck size={16} /> : <Shield size={16} />}
                  </button>
                  <button
                    className={`cursor-pointer mr-3 align-middle ${d.redirect_url ? 'text-amber-600 hover:text-amber-700' : 'text-slate-400 hover:text-brand-600'}`}
                    title="Forwarding / redirect"
                    onClick={() => setRedirectFor(d)}
                  >
                    <CornerUpRight size={16} />
                  </button>
                  {d.kind !== 'alias' && (
                    <button
                      className="text-slate-400 hover:text-brand-600 cursor-pointer mr-3 align-middle"
                      title="Alternative domains"
                      onClick={() => setAliasFor(d)}
                    >
                      <Globe size={16} />
                    </button>
                  )}
                  {d.runtime === 'php' && !d.redirect_url && (
                    <button
                      className="text-slate-400 hover:text-brand-600 cursor-pointer mr-3 align-middle"
                      title="PHP settings"
                      onClick={() => setPhpFor(d)}
                    >
                      <SlidersHorizontal size={16} />
                    </button>
                  )}
                  {!d.redirect_url && d.runtime !== 'node' && (
                    <button
                      className="text-slate-400 hover:text-brand-600 cursor-pointer mr-3 align-middle"
                      title="Password-protected directories"
                      onClick={() => setProtectedFor(d)}
                    >
                      <FolderLock size={16} />
                    </button>
                  )}
                  {isAdmin && (
                    <button
                      className="text-slate-400 hover:text-brand-600 cursor-pointer mr-3 align-middle"
                      title="Edit site config (nginx / Apache / PHP)"
                      onClick={() => setConfigFor(d)}
                    >
                      <FileCode2 size={16} />
                    </button>
                  )}
                  {isAdminish && (
                    <Btn size="sm" variant="secondary" className="mr-2" onClick={() => suspend(d)}>
                      {d.suspended ? 'Unsuspend' : 'Suspend'}
                    </Btn>
                  )}
                  <Btn size="sm" variant="danger" onClick={() => remove(d)}>
                    Delete
                  </Btn>
                </Td>
              </tr>
            ))}
          </Table>
        )}
      </Card>

      <Modal open={addOpen} title="Add Domain" onClose={() => setAddOpen(false)}>
        <form onSubmit={create}>
          <Field label="Type" hint="A primary domain, a subdomain of one you own, or a parked alias">
            <Select value={kind} onChange={(e) => changeKind(e.target.value as typeof kind)}>
              <option value="primary">Primary domain</option>
              <option value="subdomain" disabled={!primaryDomains.length}>Subdomain</option>
              <option value="alias" disabled={!primaryDomains.length}>Alias / parked domain</option>
            </Select>
          </Field>
          {kind !== 'primary' && (
            <Field label="Parent domain" hint={kind === 'subdomain' ? 'The subdomain name must end in this domain' : 'The domain this alias points at'}>
              <Select value={parentId} onChange={(e) => setParentId(Number(e.target.value))} required>
                <option value={0} disabled>Select a domain…</option>
                {primaryDomains.map((d) => (
                  <option key={d.id} value={d.id}>{d.name}</option>
                ))}
              </Select>
            </Field>
          )}
          <Field label={kind === 'subdomain' ? 'Subdomain name' : kind === 'alias' ? 'Alias domain name' : 'Domain name'}>
            <Input
              value={name}
              onChange={(e) => changeName(e.target.value)}
              placeholder={kind === 'subdomain' ? 'blog.example.com' : 'example.com'}
              required
            />
          </Field>
          {kind !== 'alias' && (
            <Field label="Alternative domains" hint="Extra hostnames serving the same site, included on the SSL certificate. Space or comma separated — clear to drop www.">
              <Input
                value={aliases}
                onChange={(e) => { setAliases(e.target.value); setAliasesTouched(true) }}
                placeholder="www.example.com"
              />
            </Field>
          )}
          {kind === 'alias' && (
            <Field label="Alias mode" hint="Mirror serves the same site; redirect forwards visitors to the parent">
              <Select value={aliasMode} onChange={(e) => setAliasMode(e.target.value as typeof aliasMode)}>
                <option value="mirror">Mirror — serve the parent's site</option>
                <option value="redirect">Redirect — forward to the parent</option>
              </Select>
            </Field>
          )}
          {kind !== 'alias' && (
          <Field label="Runtime" hint="PHP site, or a Node.js app fronted by nginx">
            <Select value={runtimeSel} onChange={(e) => setRuntimeSel(e.target.value)}>
              <optgroup label="PHP">
                <option value="">Default PHP</option>
                {(phpVersions.data ?? []).map((v) => (
                  <option key={`php:${v}`} value={`php:${v}`}>
                    PHP {v}
                  </option>
                ))}
              </optgroup>
              {!!nodeVersions.data?.length && (
                <optgroup label="Node.js">
                  {nodeVersions.data.map((v) => (
                    <option key={`node:${v}`} value={`node:${v}`}>
                      Node {v}
                    </option>
                  ))}
                </optgroup>
              )}
            </Select>
          </Field>
          )}
          {showWebMode && kind !== 'alias' && (
            <Field label="Web server" hint="nginx serves directly, or front Apache via nginx">
              <Select value={webMode} onChange={(e) => setWebMode(e.target.value)}>
                <option value="">Default ({WEB_MODE_LABELS[webserver.data?.default ?? ''] ?? 'nginx'})</option>
                {webModes.map((m) => (
                  <option key={m} value={m}>
                    {WEB_MODE_LABELS[m] ?? m}
                  </option>
                ))}
              </Select>
            </Field>
          )}
          {isAdminish && Array.isArray(usersList.data) && (
            <Field label="Owner">
              <Select value={owner} onChange={(e) => setOwner(Number(e.target.value))}>
                <option value={0}>Myself ({user!.username})</option>
                {(usersList.data as User[])
                  .filter((u) => u.id !== user!.id)
                  .map((u) => (
                    <option key={u.id} value={u.id}>
                      {u.username} ({u.role})
                    </option>
                  ))}
              </Select>
            </Field>
          )}
          {kind === 'subdomain' ? (
            <p className="text-xs text-slate-400 mb-4">A DNS record will be added to the parent domain's zone automatically.</p>
          ) : (
            <label className="flex items-center gap-2 text-sm text-slate-700 mb-4">
              <input type="checkbox" checked={createDNS} onChange={(e) => setCreateDNS(e.target.checked)} />
              Create DNS zone with default records
            </label>
          )}
          <div className="flex justify-end gap-2">
            <Btn type="button" variant="secondary" onClick={() => setAddOpen(false)}>
              Cancel
            </Btn>
            <Btn type="submit" disabled={busy}>
              {busy ? 'Creating…' : 'Create'}
            </Btn>
          </div>
        </form>
      </Modal>

      <Modal open={!!catalogFor} title={`Install an app — ${catalogFor?.name ?? ''}`} onClose={() => setCatalogFor(null)} wide>
        {catalog.loading ? (
          <Spinner />
        ) : !catalog.data?.length ? (
          <Empty title="No apps available" />
        ) : (
          <div className="space-y-2">
            <p className="text-xs text-slate-400 mb-2">
              The app is downloaded into the site's document root. Apps that need a database get one created
              automatically; non-WordPress apps are then finished in their own browser installer.
            </p>
            {catalog.data.map((app) => (
              <div key={app.id} className="flex items-center justify-between rounded-md border border-slate-200 px-3 py-2 hover:bg-slate-50/60">
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="font-medium text-slate-700">{app.name}</span>
                    <Badge color="gray">{app.category}</Badge>
                    {app.needs_db && <span className="text-xs text-slate-400">needs database</span>}
                  </div>
                  <div className="text-xs text-slate-500 truncate">{app.description}</div>
                </div>
                <Btn size="sm" disabled={busy} onClick={() => catalogFor && pickApp(catalogFor, app)}>
                  <Package size={13} /> Install
                </Btn>
              </div>
            ))}
          </div>
        )}
      </Modal>

      <Modal open={!!removeAppFor} title="Remove app" onClose={() => setRemoveAppFor(null)}>
        <p className="text-sm text-slate-600 mb-3">
          Remove <span className="font-medium">{catalogByID.get(removeAppFor?.app ?? '')?.name ?? removeAppFor?.app}</span> from{' '}
          <span className="font-medium">{(data ?? []).find((d) => d.id === removeAppFor?.domain_id)?.name}</span>?
        </p>
        <label className="flex items-start gap-2 rounded-md bg-red-50 border border-red-200 px-3 py-2 mb-4 cursor-pointer">
          <input type="checkbox" className="mt-0.5" checked={purgeApp} onChange={(e) => setPurgeApp(e.target.checked)} />
          <span className="text-sm text-red-700">
            Also permanently delete the app's <strong>database</strong> and all <strong>website files</strong> in the
            document root. This cannot be undone.
          </span>
        </label>
        {!purgeApp && (
          <p className="text-xs text-slate-400 mb-4">
            With this unchecked, only the panel record is removed — the files and database are kept.
          </p>
        )}
        <div className="flex justify-end gap-2">
          <Btn type="button" variant="secondary" onClick={() => setRemoveAppFor(null)}>Cancel</Btn>
          <Btn type="button" variant="danger" disabled={busy} onClick={doRemoveApp}>
            {busy ? 'Removing…' : purgeApp ? 'Delete everything' : 'Remove'}
          </Btn>
        </div>
      </Modal>

      <Modal open={!!installFor} title={`Install WordPress — ${installFor?.name ?? ''}`} onClose={() => setInstallFor(null)}>
        <form onSubmit={installWordPress}>
          <p className="text-sm text-slate-500 mb-4">
            RePanel will download WordPress, create a dedicated MariaDB database and write its
            configuration into <span className="font-mono text-xs">{installFor?.document_root}</span>.
          </p>
          <Field label="Site title">
            <Input value={wpTitle} onChange={(e) => setWpTitle(e.target.value)} placeholder="My Site" required />
          </Field>
          <Field label="Admin username">
            <Input value={wpUser} onChange={(e) => setWpUser(e.target.value)} required />
          </Field>
          <Field label="Admin email">
            <Input type="email" value={wpEmail} onChange={(e) => setWpEmail(e.target.value)} required />
          </Field>
          <Field
            label="Admin password"
            hint="Used when WP-CLI is available; otherwise finish setup in the browser."
          >
            <Input type="password" value={wpPass} onChange={(e) => setWpPass(e.target.value)} minLength={8} />
          </Field>
          <div className="flex justify-end gap-2 mt-2">
            <Btn type="button" variant="secondary" onClick={() => setInstallFor(null)}>
              Cancel
            </Btn>
            <Btn type="submit" disabled={busy}>
              {busy ? 'Starting…' : 'Install'}
            </Btn>
          </div>
        </form>
      </Modal>

      {nodeFor && (
        <NodeAppModal domain={nodeFor} nodeVersions={nodeVersions.data ?? []} onClose={() => setNodeFor(null)} />
      )}

      {configFor && <SiteConfigModal domain={configFor} onClose={() => setConfigFor(null)} />}

      {statsFor && <WebStatsModal domain={statsFor} onClose={() => setStatsFor(null)} />}

      {wafFor && <WAFModal domain={wafFor} isAdmin={isAdmin} onClose={() => setWafFor(null)} onSaved={reload} />}

      {redirectFor && <RedirectModal domain={redirectFor} onClose={() => setRedirectFor(null)} onSaved={reload} />}

      {aliasFor && <AliasesModal domain={aliasFor} onClose={() => setAliasFor(null)} onSaved={reload} />}

      {phpFor && <PHPSettingsModal domain={phpFor} onClose={() => setPhpFor(null)} />}

      {protectedFor && <ProtectedModal domain={protectedFor} onClose={() => setProtectedFor(null)} />}
    </div>
  )
}

function RedirectModal({ domain, onClose, onSaved }: { domain: Domain; onClose: () => void; onSaved: () => void }) {
  const [url, setUrl] = useState(domain.redirect_url ?? '')
  const [code, setCode] = useState(domain.redirect_code ?? 301)
  const [busy, setBusy] = useState(false)

  const save = async (clear: boolean) => {
    setBusy(true)
    try {
      await api.post(`/api/domains/${domain.id}/redirect`, { url: clear ? '' : url, code })
      toast(clear ? 'Forwarding removed' : 'Forwarding saved')
      onSaved()
      onClose()
    } catch (e) {
      toast((e as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal open title={`Forwarding — ${domain.name}`} onClose={onClose}>
      <p className="text-sm text-slate-500 mb-4">
        Forward all requests for this domain to another URL (the request path is preserved). Leave empty and remove to
        serve the site normally instead.
      </p>
      <Field label="Destination URL" hint="A full http(s):// address">
        <Input value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://example.com" />
      </Field>
      <Field label="Redirect type">
        <Select value={code} onChange={(e) => setCode(Number(e.target.value))}>
          <option value={301}>301 — permanent</option>
          <option value={302}>302 — temporary</option>
        </Select>
      </Field>
      <div className="flex justify-between gap-2 mt-2">
        {domain.redirect_url ? (
          <Btn type="button" variant="danger" disabled={busy} onClick={() => save(true)}>
            Remove forwarding
          </Btn>
        ) : (
          <span />
        )}
        <div className="flex gap-2">
          <Btn type="button" variant="secondary" onClick={onClose}>
            Cancel
          </Btn>
          <Btn type="button" disabled={busy || !url} onClick={() => save(false)}>
            {busy ? 'Saving…' : 'Save'}
          </Btn>
        </div>
      </div>
    </Modal>
  )
}

function AliasesModal({ domain, onClose, onSaved }: { domain: Domain; onClose: () => void; onSaved: () => void }) {
  const [aliases, setAliases] = useState((domain.aliases ?? []).join(' '))
  const [busy, setBusy] = useState(false)

  const save = async () => {
    setBusy(true)
    try {
      await api.post(`/api/domains/${domain.id}/aliases`, { aliases: aliases.split(/[\s,]+/).filter(Boolean) })
      toast('Alternative domains saved')
      onSaved()
      onClose()
    } catch (e) {
      toast((e as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal open title={`Alternative domains — ${domain.name}`} onClose={onClose}>
      <p className="text-sm text-slate-500 mb-4">
        Extra hostnames that serve this same site (added as <code>server_name</code> / <code>ServerAlias</code> entries and
        included on the SSL certificate). Add as many as you like, or remove <code>www.</code> — one per line, or space/comma
        separated. Re-issue SSL after changing these to cover the new names.
      </p>
      <Field label="Hostnames">
        <textarea
          value={aliases}
          onChange={(e) => setAliases(e.target.value)}
          placeholder={`www.${domain.name}`}
          spellCheck={false}
          className="w-full h-28 rounded-md border border-slate-300 px-3 py-2 text-sm font-mono focus:outline-none focus:ring-2 focus:ring-brand-500/40 focus:border-brand-500 bg-white"
        />
      </Field>
      <div className="flex justify-end gap-2 mt-2">
        <Btn type="button" variant="secondary" onClick={onClose}>Cancel</Btn>
        <Btn type="button" disabled={busy} onClick={save}>{busy ? 'Saving…' : 'Save'}</Btn>
      </div>
    </Modal>
  )
}

function PHPSettingsModal({ domain, onClose }: { domain: Domain; onClose: () => void }) {
  const { data, error, loading } = useFetch<{ runtime: string; settings: PHPSettings }>(`/api/domains/${domain.id}/php-settings`)
  const [s, setS] = useState<PHPSettings | null>(null)
  const [busy, setBusy] = useState(false)
  if (data && !s) setS(data.settings)

  const set = (k: keyof PHPSettings, v: string | boolean) => s && setS({ ...s, [k]: v })

  const save = async () => {
    if (!s) return
    setBusy(true)
    try {
      await api.post(`/api/domains/${domain.id}/php-settings`, s)
      toast('PHP settings applied')
      onClose()
    } catch (e) {
      toast((e as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal open title={`PHP settings — ${domain.name}`} onClose={onClose} wide>
      {loading || !s ? (
        <Spinner />
      ) : error ? (
        <ErrorBanner message={error} />
      ) : (
        <div>
          <p className="text-xs text-slate-400 mb-4">
            Common PHP limits for this site. Blank fields keep the server default. Applied to the site's PHP-FPM pool.
          </p>
          <div className="grid grid-cols-2 gap-3">
            <Field label="memory_limit"><Input value={s.memory_limit} onChange={(e) => set('memory_limit', e.target.value)} placeholder="256M" /></Field>
            <Field label="max_execution_time" hint="seconds"><Input value={s.max_execution_time} onChange={(e) => set('max_execution_time', e.target.value)} placeholder="30" /></Field>
            <Field label="upload_max_filesize"><Input value={s.upload_max_filesize} onChange={(e) => set('upload_max_filesize', e.target.value)} placeholder="128M" /></Field>
            <Field label="post_max_size"><Input value={s.post_max_size} onChange={(e) => set('post_max_size', e.target.value)} placeholder="128M" /></Field>
            <Field label="max_input_time" hint="seconds"><Input value={s.max_input_time} onChange={(e) => set('max_input_time', e.target.value)} placeholder="60" /></Field>
            <Field label="max_input_vars"><Input value={s.max_input_vars} onChange={(e) => set('max_input_vars', e.target.value)} placeholder="1000" /></Field>
          </div>
          <Field label="disable_functions" hint="Comma-separated function names to disable">
            <Input value={s.disable_functions} onChange={(e) => set('disable_functions', e.target.value)} placeholder="exec,system,passthru" />
          </Field>
          <div className="flex gap-6 mb-4">
            <label className="flex items-center gap-2 text-sm text-slate-700 cursor-pointer">
              <input type="checkbox" checked={s.display_errors} onChange={(e) => set('display_errors', e.target.checked)} /> display_errors
            </label>
            <label className="flex items-center gap-2 text-sm text-slate-700 cursor-pointer">
              <input type="checkbox" checked={s.allow_url_fopen} onChange={(e) => set('allow_url_fopen', e.target.checked)} /> allow_url_fopen
            </label>
          </div>
          <div className="flex justify-end gap-2">
            <Btn type="button" variant="secondary" onClick={onClose}>Cancel</Btn>
            <Btn type="button" disabled={busy} onClick={save}>{busy ? 'Applying…' : 'Apply'}</Btn>
          </div>
        </div>
      )}
    </Modal>
  )
}

function ProtectedModal({ domain, onClose }: { domain: Domain; onClose: () => void }) {
  const { data, error, loading, reload } = useFetch<ProtectedDir[]>(`/api/domains/${domain.id}/protected`)
  const [path, setPath] = useState('')
  const [realm, setRealm] = useState('')
  const [userFor, setUserFor] = useState<ProtectedDir | null>(null)
  const [uname, setUname] = useState('')
  const [upass, setUpass] = useState('')
  const [busy, setBusy] = useState(false)

  const addDir = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      await api.post(`/api/domains/${domain.id}/protected`, { path, realm })
      toast('Directory protected')
      setPath('')
      setRealm('')
      reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  const removeDir = async (d: ProtectedDir) => {
    if (!confirm(`Stop protecting ${d.path}?`)) return
    try {
      await api.del(`/api/domains/${domain.id}/protected/${d.id}`)
      reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    }
  }

  const addUser = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!userFor) return
    setBusy(true)
    try {
      await api.post(`/api/domains/${domain.id}/protected/${userFor.id}/users`, { username: uname, password: upass })
      toast('User saved')
      setUname('')
      setUpass('')
      setUserFor(null)
      reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  const removeUser = async (d: ProtectedDir, username: string) => {
    try {
      await api.del(`/api/domains/${domain.id}/protected/${d.id}/users/${encodeURIComponent(username)}`)
      reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    }
  }

  return (
    <Modal open title={`Protected directories — ${domain.name}`} onClose={onClose} wide>
      <ErrorBanner message={error} />
      {loading ? (
        <Spinner />
      ) : (
        <div className="space-y-4">
          {!data?.length ? (
            <Empty title="No protected directories" hint="Add a path below to require a password for it." />
          ) : (
            <div className="space-y-3">
              {data.map((d) => (
                <div key={d.id} className="rounded-md border border-slate-200 p-3">
                  <div className="flex items-center justify-between">
                    <div>
                      <span className="font-mono text-sm text-slate-700">{d.path}</span>
                      <span className="ml-2 text-xs text-slate-400">“{d.realm}”</span>
                      {!d.users.length && <span className="ml-2 text-xs text-amber-600">no users yet — not enforced</span>}
                    </div>
                    <div className="flex gap-2">
                      <Btn size="sm" variant="secondary" onClick={() => setUserFor(d)}>
                        <Plus size={13} /> User
                      </Btn>
                      <Btn size="sm" variant="danger" onClick={() => removeDir(d)}>
                        <Trash2 size={13} />
                      </Btn>
                    </div>
                  </div>
                  {!!d.users.length && (
                    <div className="mt-2 flex flex-wrap gap-1.5">
                      {d.users.map((un) => (
                        <span key={un} className="inline-flex items-center gap-1 rounded bg-slate-100 px-2 py-0.5 text-xs text-slate-600">
                          {un}
                          <button className="text-slate-400 hover:text-red-600" onClick={() => removeUser(d, un)} title="Remove user">
                            <Trash2 size={11} />
                          </button>
                        </span>
                      ))}
                    </div>
                  )}
                </div>
              ))}
            </div>
          )}

          {userFor ? (
            <form onSubmit={addUser} className="rounded-md border border-brand-200 bg-brand-50/40 p-3">
              <div className="text-sm font-medium text-slate-700 mb-2">Add user to {userFor.path}</div>
              <div className="grid grid-cols-2 gap-2">
                <Input value={uname} onChange={(e) => setUname(e.target.value)} placeholder="username" required />
                <Input type="password" value={upass} onChange={(e) => setUpass(e.target.value)} placeholder="password" required minLength={6} />
              </div>
              <div className="flex justify-end gap-2 mt-2">
                <Btn type="button" variant="secondary" size="sm" onClick={() => setUserFor(null)}>Cancel</Btn>
                <Btn type="submit" size="sm" disabled={busy}>Save</Btn>
              </div>
            </form>
          ) : (
            <form onSubmit={addDir} className="border-t border-slate-100 pt-4">
              <div className="text-sm font-medium text-slate-700 mb-2">Protect a directory</div>
              <div className="grid grid-cols-2 gap-2">
                <Input value={path} onChange={(e) => setPath(e.target.value)} placeholder="/admin" required />
                <Input value={realm} onChange={(e) => setRealm(e.target.value)} placeholder="Restricted area (optional)" />
              </div>
              <div className="flex justify-end mt-2">
                <Btn type="submit" size="sm" disabled={busy}>
                  <Plus size={13} /> Protect
                </Btn>
              </div>
            </form>
          )}
        </div>
      )}
    </Modal>
  )
}

function WAFModal({ domain, isAdmin, onClose, onSaved }: { domain: Domain; isAdmin: boolean; onClose: () => void; onSaved: () => void }) {
  const { data, error, loading, reload } = useFetch<WAFStatus>(`/api/domains/${domain.id}/waf`)
  const [enabled, setEnabled] = useState(false)
  const [mode, setMode] = useState<'on' | 'detection'>('on')
  const [rules, setRules] = useState('')
  const [busy, setBusy] = useState(false)
  const [primed, setPrimed] = useState(false)

  if (data && !primed) {
    setEnabled(data.enabled)
    setMode(data.mode)
    setRules(data.rules)
    setPrimed(true)
  }

  // Poll while an install runs so the form unlocks when the engine appears.
  useEffect(() => {
    if (!data?.installing) return
    const t = setInterval(reload, 4000)
    return () => clearInterval(t)
  }, [data?.installing, reload])

  const install = async () => {
    setBusy(true)
    try {
      await api.post('/api/waf/install')
      toast('Installing ModSecurity + OWASP CRS… this can take a minute')
      reload()
    } catch (e) {
      toast((e as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  const save = async () => {
    setBusy(true)
    try {
      await api.post(`/api/domains/${domain.id}/waf`, {
        enabled,
        mode,
        rules: isAdmin ? rules : undefined,
      })
      toast('WAF settings applied')
      onSaved()
      onClose()
    } catch (e) {
      toast((e as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal open title={`Web Application Firewall — ${domain.name}`} onClose={onClose} wide>
      {loading ? (
        <Spinner />
      ) : error ? (
        <ErrorBanner message={error} />
      ) : !data ? null : !data.available ? (
        <div>
          {data.installing ? (
            <div className="flex items-center gap-2 text-sm text-blue-600">
              <Loader2 size={16} className="animate-spin" /> Installing ModSecurity and the OWASP Core Rule Set…
            </div>
          ) : (
            <>
              <p className="text-sm text-slate-600 mb-3">
                ModSecurity is not installed on this server yet. It provides a Web Application Firewall using the OWASP
                Core Rule Set to block common attacks (SQL injection, XSS, path traversal, and more).
              </p>
              {data.error && <ErrorBanner message={data.error} />}
              {isAdmin ? (
                <Btn disabled={busy} onClick={install}>
                  <Shield size={15} /> Install ModSecurity + OWASP CRS
                </Btn>
              ) : (
                <div className="rounded-md bg-amber-50 border border-amber-200 text-amber-800 text-sm px-4 py-3">
                  Ask an administrator to install the WAF engine on this server.
                </div>
              )}
            </>
          )}
        </div>
      ) : (
        <div className="space-y-4">
          {!data.crs && (
            <div className="rounded-md bg-amber-50 border border-amber-200 text-amber-800 text-sm px-4 py-3">
              The OWASP Core Rule Set was not found, so the firewall has no attack rules loaded. Reinstall the WAF to add
              it.
            </div>
          )}
          <label className="flex items-center gap-2 text-sm text-slate-700 cursor-pointer">
            <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
            Enable the Web Application Firewall for this site
          </label>

          <Field label="Mode" hint="Detection-only logs attacks without blocking — useful to check for false positives first.">
            <Select value={mode} onChange={(e) => setMode(e.target.value as 'on' | 'detection')} disabled={!enabled}>
              <option value="on">Blocking (reject malicious requests)</option>
              <option value="detection">Detection only (log, don't block)</option>
            </Select>
          </Field>

          {isAdmin && (
            <div>
              <div className="text-sm font-medium text-slate-700 mb-1">Custom rules &amp; exclusions</div>
              <p className="text-xs text-slate-400 mb-1.5">
                ModSecurity directives appended after the CRS — e.g. <code>SecRuleRemoveById 941100</code> to silence a
                false positive. Validated before applying.
              </p>
              <textarea
                value={rules}
                onChange={(e) => setRules(e.target.value)}
                placeholder={'SecRuleRemoveById 920350\nSecRuleRemoveById 949110'}
                spellCheck={false}
                disabled={!enabled}
                className="w-full h-32 rounded-md border border-slate-300 px-3 py-2 text-xs font-mono focus:outline-none focus:ring-2 focus:ring-brand-500/40 focus:border-brand-500 bg-white disabled:bg-slate-50"
              />
            </div>
          )}

          <div className="flex justify-end gap-2">
            <Btn type="button" variant="secondary" onClick={onClose}>
              Cancel
            </Btn>
            <Btn type="button" disabled={busy} onClick={save}>
              {busy ? 'Applying…' : 'Apply'}
            </Btn>
          </div>
        </div>
      )}
    </Modal>
  )
}

function statFmtBytes(mb: number): string {
  if (mb >= 1024) return `${(mb / 1024).toFixed(2)} GB`
  if (mb >= 1) return `${mb.toFixed(1)} MB`
  if (mb > 0) return `${(mb * 1024).toFixed(0)} KB`
  return '0'
}

function WebStatsModal({ domain, onClose }: { domain: Domain; onClose: () => void }) {
  const [days, setDays] = useState(30)
  const [metric, setMetric] = useState<'visitors' | 'pageviews' | 'hits' | 'mb'>('pageviews')
  const { data, error, loading } = useFetch<WebStats>(`/api/webstats/${domain.id}?days=${days}`)

  const hasData = !!data && data.series.length > 0
  const peak = Math.max(1, ...(data?.series ?? []).map((d) => d[metric]))
  const metricLabel = { visitors: 'Visitors', pageviews: 'Pageviews', hits: 'Hits', mb: 'Bandwidth' }[metric]

  return (
    <Modal open title={`Statistics — ${domain.name}`} onClose={onClose} wide>
      <div className="flex justify-end mb-3">
        <select value={days} onChange={(e) => setDays(Number(e.target.value))} className={`${inputCls} w-auto`}>
          <option value={7}>Last 7 days</option>
          <option value={30}>Last 30 days</option>
          <option value={90}>Last 90 days</option>
        </select>
      </div>
      <ErrorBanner message={error} />
      {loading ? (
        <Spinner />
      ) : !hasData ? (
        <Empty
          title="No statistics yet"
          hint="Stats are parsed from the web server access log hourly — check back once the site has visitors."
        />
      ) : (
        <div className="space-y-5">
          <div className="grid grid-cols-4 gap-3">
            <StatCard label="Unique visitors" value={data!.totals.visitors.toLocaleString()} active={metric === 'visitors'} onClick={() => setMetric('visitors')} />
            <StatCard label="Pageviews" value={data!.totals.pageviews.toLocaleString()} active={metric === 'pageviews'} onClick={() => setMetric('pageviews')} />
            <StatCard label="Hits" value={data!.totals.hits.toLocaleString()} active={metric === 'hits'} onClick={() => setMetric('hits')} />
            <StatCard label="Bandwidth" value={statFmtBytes(data!.totals.mb)} active={metric === 'mb'} onClick={() => setMetric('mb')} />
          </div>

          <div>
            <div className="text-xs text-slate-400 mb-1">{metricLabel} per day</div>
            <div className="flex items-end gap-0.5 h-28">
              {data!.series.map((d) => (
                <div
                  key={d.day}
                  className="flex-1 bg-brand-500/80 hover:bg-brand-600 rounded-t min-w-[2px] transition-colors"
                  style={{ height: `${Math.max(2, (d[metric] / peak) * 100)}%` }}
                  title={`${d.day}: ${metric === 'mb' ? statFmtBytes(d.mb) : d[metric].toLocaleString()} ${metric === 'mb' ? '' : metricLabel.toLowerCase()}`}
                />
              ))}
            </div>
            <div className="flex justify-between text-[11px] text-slate-400 mt-1.5">
              <span>{data!.series[0]?.day}</span>
              <span>{data!.series[data!.series.length - 1]?.day}</span>
            </div>
          </div>

          <div className="grid grid-cols-2 gap-5">
            <StatList title="Top pages" items={data!.top_pages} empty="No page views." mono />
            <StatList title="Top referrers" items={data!.top_referrers} empty="No external referrers." />
          </div>
          <StatusCodes items={data!.status_codes} />
        </div>
      )}
    </Modal>
  )
}

function StatCard({ label, value, active, onClick }: { label: string; value: string; active: boolean; onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`rounded-md border py-3 text-center cursor-pointer transition-colors ${
        active ? 'border-brand-500 bg-brand-50/60' : 'border-slate-200 hover:border-slate-300'
      }`}
    >
      <div className="text-xl font-semibold text-slate-700">{value}</div>
      <div className="text-xs text-slate-400">{label}</div>
    </button>
  )
}

function StatList({ title, items, empty, mono }: { title: string; items: WebStats['top_pages']; empty: string; mono?: boolean }) {
  const max = Math.max(1, ...items.map((i) => i.count))
  return (
    <div>
      <div className="text-sm font-semibold text-slate-700 mb-2">{title}</div>
      {!items.length ? (
        <p className="text-sm text-slate-400">{empty}</p>
      ) : (
        <div className="space-y-1">
          {items.map((it) => (
            <div key={it.label} className="relative rounded px-2 py-1 overflow-hidden">
              <div className="absolute inset-y-0 left-0 bg-brand-100/70 rounded" style={{ width: `${(it.count / max) * 100}%` }} />
              <div className="relative flex justify-between gap-3 text-xs">
                <span className={`truncate text-slate-700 ${mono ? 'font-mono' : ''}`} title={it.label}>
                  {it.label}
                </span>
                <span className="text-slate-500 shrink-0">{it.count.toLocaleString()}</span>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

function StatusCodes({ items }: { items: WebStats['status_codes'] }) {
  if (!items.length) return null
  const color = (code: string) =>
    code.startsWith('2') ? 'green' : code.startsWith('3') ? 'blue' : code.startsWith('4') ? 'amber' : 'red'
  return (
    <div>
      <div className="text-sm font-semibold text-slate-700 mb-2">Status codes</div>
      <div className="flex flex-wrap gap-2">
        {items.map((it) => (
          <Badge key={it.label} color={color(it.label) as 'green' | 'blue' | 'amber' | 'red'}>
            {it.label}: {it.count.toLocaleString()}
          </Badge>
        ))}
      </div>
    </div>
  )
}

type ConfigTab = 'nginx' | 'apache' | 'php' | 'mail'

function SiteConfigModal({ domain, onClose }: { domain: Domain; onClose: () => void }) {
  const { data, error, loading } = useFetch<SiteConfig>(`/api/domains/${domain.id}/config`)
  const [tab, setTab] = useState<ConfigTab>('nginx')
  const [nginx, setNginx] = useState('')
  const [apache, setApache] = useState('')
  const [php, setPhp] = useState('')
  const [busy, setBusy] = useState(false)
  const [primed, setPrimed] = useState(false)

  if (data && !primed) {
    setNginx(data.nginx_conf)
    setApache(data.apache_conf)
    setPhp(data.php_conf)
    // Open the first applicable editable tab.
    setTab(data.nginx_active ? 'nginx' : data.apache_active ? 'apache' : data.php_active ? 'php' : 'mail')
    setPrimed(true)
  }

  const save = async () => {
    setBusy(true)
    try {
      await api.post(`/api/domains/${domain.id}/config`, { nginx_conf: nginx, apache_conf: apache, php_conf: php })
      toast('Configuration applied')
      onClose()
    } catch (e) {
      toast((e as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  const tabs: { key: ConfigTab; label: string; active: boolean }[] = [
    { key: 'nginx', label: 'nginx', active: !!data?.nginx_active },
    { key: 'apache', label: 'Apache', active: !!data?.apache_active },
    { key: 'php', label: 'PHP-FPM', active: !!data?.php_active },
    { key: 'mail', label: 'Mail', active: true },
  ]

  return (
    <Modal open title={`Site config — ${domain.name}`} onClose={onClose} wide>
      {loading ? (
        <Spinner />
      ) : error ? (
        <ErrorBanner message={error} />
      ) : !data ? null : (
        <div>
          <div className="flex gap-1 border-b border-slate-200 mb-4">
            {tabs.map((t) => (
              <button
                key={t.key}
                type="button"
                onClick={() => setTab(t.key)}
                className={`px-3 py-1.5 text-sm font-medium border-b-2 -mb-px cursor-pointer ${
                  tab === t.key ? 'border-brand-600 text-brand-700' : 'border-transparent text-slate-500 hover:text-slate-700'
                }`}
              >
                {t.label}
              </button>
            ))}
          </div>

          {tab === 'nginx' && (
            <ConfigPane
              hint="Directives merged into the generated server { } block. Survives SSL/PHP/suspend rebuilds. Validated with nginx -t before applying."
              placeholder={'location /api/ {\n    proxy_pass http://127.0.0.1:9000;\n}'}
              value={nginx}
              onChange={setNginx}
              rendered={data.rendered.nginx}
              disabled={!data.nginx_active}
              disabledNote={data.runtime === 'node' ? 'This domain runs a Node app; its nginx vhost is a reverse proxy and is not editable here.' : 'nginx does not serve this domain in the current stack/mode.'}
            />
          )}
          {tab === 'apache' && (
            <ConfigPane
              hint="Directives injected into the <VirtualHost> block. Validated with apachectl -t before applying."
              placeholder={'<Directory /var/www/...>\n    Options +Indexes\n</Directory>'}
              value={apache}
              onChange={setApache}
              rendered={data.rendered.apache}
              disabled={!data.apache_active}
              disabledNote="Apache does not serve this domain in the current stack/mode."
            />
          )}
          {tab === 'php' && (
            <ConfigPane
              hint="Lines appended to the PHP-FPM pool [pool] section. Validated with php-fpm -t before applying."
              placeholder={'php_admin_value[memory_limit] = 256M\nphp_value[upload_max_filesize] = 256M'}
              value={php}
              onChange={setPhp}
              rendered={data.rendered.php}
              disabled={!data.php_active}
              disabledNote="This domain has no PHP-FPM pool (Node app)."
            />
          )}
          {tab === 'mail' && (
            <div>
              <p className="text-xs text-slate-400 mb-2">
                Mail uses server-wide Postfix maps, so there is no per-site editable file. Below are this domain's effective
                mailbox and alias entries (read-only). Manage them from the Mail page.
              </p>
              <RenderedView content={data.rendered.mail} empty="No mailboxes or aliases for this domain." />
            </div>
          )}

          <div className="flex justify-end gap-2 mt-5">
            <Btn type="button" variant="secondary" onClick={onClose}>
              Close
            </Btn>
            {tab !== 'mail' && (
              <Btn type="button" disabled={busy} onClick={save}>
                {busy ? 'Applying…' : 'Apply & reload'}
              </Btn>
            )}
          </div>
        </div>
      )}
    </Modal>
  )
}

function ConfigPane({
  hint,
  placeholder,
  value,
  onChange,
  rendered,
  disabled,
  disabledNote,
}: {
  hint: string
  placeholder: string
  value: string
  onChange: (v: string) => void
  rendered: string
  disabled: boolean
  disabledNote: string
}) {
  const [showRendered, setShowRendered] = useState(false)
  return (
    <div>
      {disabled ? (
        <div className="rounded-md bg-amber-50 border border-amber-200 text-amber-800 text-sm px-4 py-3 mb-3">
          {disabledNote}
        </div>
      ) : (
        <>
          <p className="text-xs text-slate-400 mb-1.5">{hint}</p>
          <textarea
            value={value}
            onChange={(e) => onChange(e.target.value)}
            placeholder={placeholder}
            spellCheck={false}
            className="w-full h-44 rounded-md border border-slate-300 px-3 py-2 text-xs font-mono focus:outline-none focus:ring-2 focus:ring-brand-500/40 focus:border-brand-500 bg-white"
          />
        </>
      )}
      <button
        type="button"
        onClick={() => setShowRendered((v) => !v)}
        className="text-xs text-brand-600 hover:underline cursor-pointer mt-1"
      >
        {showRendered ? 'Hide' : 'Show'} generated config
      </button>
      {showRendered && <RenderedView content={rendered} empty="No generated file for this domain." />}
    </div>
  )
}

function RenderedView({ content, empty }: { content: string; empty: string }) {
  return (
    <pre className="mt-2 max-h-72 overflow-auto rounded-md bg-slate-900 text-slate-100 text-xs font-mono p-3 whitespace-pre">
      {content?.trim() ? content : empty}
    </pre>
  )
}

function NodeAppModal({
  domain,
  nodeVersions,
  onClose,
}: {
  domain: Domain
  nodeVersions: string[]
  onClose: () => void
}) {
  const { data, error, loading, reload } = useFetch<NodeApp>(`/api/domains/${domain.id}/node`)
  const [version, setVersion] = useState('')
  const [appRoot, setAppRoot] = useState('')
  const [startup, setStartup] = useState('app.js')
  const [env, setEnv] = useState<{ key: string; value: string }[]>([])
  const [busy, setBusy] = useState(false)
  const [output, setOutput] = useState('')
  const [primed, setPrimed] = useState(false)

  if (data && !primed) {
    setVersion(data.version)
    setAppRoot(data.app_root)
    setStartup(data.startup || 'app.js')
    setEnv(Object.entries(data.env || {}).map(([key, value]) => ({ key, value })))
    setPrimed(true)
  }

  const run = async (fn: () => Promise<unknown>, msg: string) => {
    setBusy(true)
    try {
      await fn()
      toast(msg)
      reload()
    } catch (e) {
      toast((e as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  const save = () => {
    const envMap: Record<string, string> = {}
    env.forEach((e) => {
      if (e.key.trim()) envMap[e.key.trim()] = e.value
    })
    run(() => api.post(`/api/domains/${domain.id}/node`, { version, app_root: appRoot, startup, env: envMap }), 'Saved & redeployed')
  }

  const npm = async () => {
    setBusy(true)
    setOutput('Running npm install…')
    try {
      const res = await api.post<{ output: string }>(`/api/domains/${domain.id}/node/npm`)
      setOutput(res.output || '(done)')
    } catch (e) {
      setOutput('Error: ' + (e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal open title={`Node app — ${domain.name}`} onClose={onClose} wide>
      <ErrorBanner message={error} />
      {loading ? (
        <Spinner />
      ) : (
        <>
          <div className="flex items-center justify-between rounded-md border border-slate-200 px-4 py-3 mb-4">
            <div className="text-sm">
              {data?.running ? <Badge color="green">running</Badge> : <Badge color="gray">stopped</Badge>}
              <a href={data?.url} target="_blank" rel="noreferrer" className="ml-3 text-xs text-brand-600 hover:underline">
                {data?.url}
              </a>
              <span className="ml-3 text-xs text-slate-400">port {data?.port}</span>
            </div>
            <div className="flex gap-2">
              <Btn size="sm" variant="secondary" disabled={busy} onClick={() => run(() => api.post(`/api/domains/${domain.id}/node/restart`), 'Restarted')}>
                <RefreshCw size={14} /> Restart
              </Btn>
              <Btn
                size="sm"
                variant="secondary"
                disabled={busy}
                onClick={() => run(() => api.post(`/api/domains/${domain.id}/node/enabled`, { enabled: !data?.running }), data?.running ? 'Stopped' : 'Started')}
              >
                <Power size={14} /> {data?.running ? 'Stop' : 'Start'}
              </Btn>
            </div>
          </div>

          <div className="grid grid-cols-2 gap-3">
            <Field label="Node version">
              <Select value={version} onChange={(e) => setVersion(e.target.value)}>
                {nodeVersions.map((v) => (
                  <option key={v} value={v}>
                    Node {v}
                  </option>
                ))}
              </Select>
            </Field>
            <Field label="Startup file" hint="relative to the app root">
              <Input value={startup} onChange={(e) => setStartup(e.target.value)} placeholder="app.js" />
            </Field>
          </div>
          <Field label="Application root" hint="relative to the domain directory (blank = domain root)">
            <Input value={appRoot} onChange={(e) => setAppRoot(e.target.value)} placeholder="(domain root)" />
          </Field>

          <div className="mb-4">
            <div className="text-sm font-medium text-slate-700 mb-1">Environment variables</div>
            {env.map((e, i) => (
              <div key={i} className="flex gap-2 mb-1.5">
                <Input value={e.key} placeholder="KEY" onChange={(ev) => setEnv(env.map((x, j) => (j === i ? { ...x, key: ev.target.value } : x)))} />
                <Input value={e.value} placeholder="value" onChange={(ev) => setEnv(env.map((x, j) => (j === i ? { ...x, value: ev.target.value } : x)))} />
                <Btn type="button" variant="secondary" onClick={() => setEnv(env.filter((_, j) => j !== i))}>
                  <Trash2 size={14} />
                </Btn>
              </div>
            ))}
            <Btn type="button" variant="secondary" size="sm" onClick={() => setEnv([...env, { key: '', value: '' }])}>
              <Plus size={13} /> Add variable
            </Btn>
          </div>

          <div className="flex items-center justify-between">
            <Btn type="button" variant="secondary" disabled={busy} onClick={npm}>
              npm install
            </Btn>
            <div className="flex gap-2">
              <Btn type="button" variant="secondary" onClick={onClose}>
                Close
              </Btn>
              <Btn type="button" disabled={busy} onClick={save}>
                Save
              </Btn>
            </div>
          </div>
          {output && (
            <pre className="mt-3 max-h-48 overflow-auto rounded-md bg-slate-900 text-slate-100 text-xs font-mono p-3 whitespace-pre-wrap break-all">
              {output}
            </pre>
          )}
        </>
      )}
    </Modal>
  )
}

function AppCell({
  app,
  catalog,
  canInstall,
  onInstall,
  onRemove,
}: {
  app: App | undefined
  catalog: Map<string, CatalogApp>
  canInstall: boolean
  onInstall: () => void
  onRemove: (a: App) => void
}) {
  if (!app) {
    if (!canInstall) return <span className="text-xs text-slate-300">—</span>
    return (
      <Btn size="sm" variant="secondary" onClick={onInstall}>
        <Package size={13} /> Install app
      </Btn>
    )
  }
  const name = catalog.get(app.app)?.name ?? app.app
  if (app.status === 'installing') {
    return (
      <span className="inline-flex items-center gap-1.5 text-sm text-blue-600">
        <Loader2 size={14} className="animate-spin" /> Installing {name}…
      </span>
    )
  }
  if (app.status === 'failed') {
    return (
      <span className="inline-flex items-center gap-2">
        <span title={app.error}>
          <Badge color="red">{name}: install failed</Badge>
        </span>
        <button onClick={() => onRemove(app)} className="text-slate-400 hover:text-red-600" title="Remove record">
          <Trash2 size={13} />
        </button>
      </span>
    )
  }
  return (
    <div className="space-y-1">
      <span className="inline-flex items-center gap-2">
        <Badge color="blue">{name}</Badge>
        <a href={app.url} target="_blank" rel="noreferrer" className="text-brand-600 hover:underline inline-flex items-center gap-0.5 text-xs">
          {app.auto_setup ? 'open' : 'finish setup'} <ExternalLink size={11} />
        </a>
        <button onClick={() => onRemove(app)} className="text-slate-400 hover:text-red-600" title="Remove record">
          <Trash2 size={13} />
        </button>
      </span>
      {!app.auto_setup && app.db_name && (
        <div className="text-[11px] text-slate-500 font-mono leading-tight">
          db: {app.db_name} · user: {app.db_user} · pass: {app.db_pass}
        </div>
      )}
    </div>
  )
}
