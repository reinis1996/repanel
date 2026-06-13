import { useEffect, useState } from 'react'
import { Plus, Lock, LockOpen, Package, Loader2, ExternalLink, Trash2 } from 'lucide-react'
import { api, useFetch, formatDate } from '../api'
import type { Domain, User, App } from '../types'
import { useAuth } from '../App'
import {
  Btn, Card, PageHeader, Table, Td, Modal, Field, Input, Select,
  Spinner, ErrorBanner, Empty, Badge, toast,
} from '../components/ui'

export default function Domains() {
  const { user } = useAuth()
  const isAdminish = user!.role !== 'user'
  const { data, error, loading, reload } = useFetch<Domain[]>('/api/domains')
  const phpVersions = useFetch<string[]>('/api/php-versions')
  const usersList = useFetch<User[]>(isAdminish ? '/api/users' : '/api/me')
  const apps = useFetch<App[]>('/api/apps')
  const appByDomain = new Map((apps.data ?? []).map((a) => [a.domain_id, a]))

  const [addOpen, setAddOpen] = useState(false)
  const [name, setName] = useState('')
  const [php, setPhp] = useState('')
  const [owner, setOwner] = useState(0)
  const [createDNS, setCreateDNS] = useState(true)
  const [busy, setBusy] = useState(false)

  // WordPress install modal state.
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

  const create = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      await api.post('/api/domains', {
        name,
        php_version: php || undefined,
        user_id: owner || undefined,
        create_dns: createDNS,
      })
      toast(`Domain ${name} created`)
      setAddOpen(false)
      setName('')
      reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    } finally {
      setBusy(false)
    }
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

  const changePHP = async (d: Domain, version: string) => {
    try {
      await api.post(`/api/domains/${d.id}/php`, { php_version: version })
      toast(`${d.name} switched to PHP ${version}`)
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

  const removeApp = async (a: App) => {
    if (!confirm('Remove this app from the panel? Site files and its database are kept.')) return
    try {
      await api.del(`/api/apps/${a.id}`)
      apps.reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    }
  }

  if (loading) return <Spinner />

  return (
    <div>
      <PageHeader
        title="Websites & Domains"
        subtitle="Each domain gets an nginx vhost, an isolated PHP-FPM pool and its own document root"
        actions={
          <Btn onClick={() => setAddOpen(true)}>
            <Plus size={16} /> Add Domain
          </Btn>
        }
      />
      <ErrorBanner message={error} />
      <Card>
        {!data?.length ? (
          <Empty title="No domains yet" hint="Add your first domain to start hosting" />
        ) : (
          <Table head={['Domain', 'Owner', 'PHP', 'SSL', 'Application', 'Status', '']}>
            {data.map((d) => (
              <tr key={d.id} className="hover:bg-slate-50/60">
                <Td>
                  <a
                    href={`http${d.ssl ? 's' : ''}://${d.name}`}
                    target="_blank"
                    rel="noreferrer"
                    className="font-medium text-brand-600 hover:underline"
                  >
                    {d.name}
                  </a>
                  <div className="text-xs text-slate-400">{d.document_root}</div>
                </Td>
                <Td>{d.owner ?? '—'}</Td>
                <Td>
                  <select
                    className="text-sm border border-slate-200 rounded px-1.5 py-0.5 bg-white"
                    value={d.php_version}
                    onChange={(e) => changePHP(d, e.target.value)}
                  >
                    {(phpVersions.data ?? [d.php_version]).map((v) => (
                      <option key={v} value={v}>
                        {v}
                      </option>
                    ))}
                  </select>
                </Td>
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
                    onInstall={() => setInstallFor(d)}
                    onRemove={removeApp}
                  />
                </Td>
                <Td>{d.suspended ? <Badge color="red">suspended</Badge> : <Badge color="green">active</Badge>}</Td>
                <Td className="text-right whitespace-nowrap">
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
          <Field label="Domain name">
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="example.com" required />
          </Field>
          <Field label="PHP version">
            <Select value={php} onChange={(e) => setPhp(e.target.value)}>
              <option value="">Default</option>
              {(phpVersions.data ?? []).map((v) => (
                <option key={v} value={v}>
                  PHP {v}
                </option>
              ))}
            </Select>
          </Field>
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
          <label className="flex items-center gap-2 text-sm text-slate-700 mb-4">
            <input type="checkbox" checked={createDNS} onChange={(e) => setCreateDNS(e.target.checked)} />
            Create DNS zone with default records
          </label>
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
    </div>
  )
}

function AppCell({
  app,
  onInstall,
  onRemove,
}: {
  app: App | undefined
  onInstall: () => void
  onRemove: (a: App) => void
}) {
  if (!app) {
    return (
      <Btn size="sm" variant="secondary" onClick={onInstall}>
        <Package size={13} /> WordPress
      </Btn>
    )
  }
  if (app.status === 'installing') {
    return (
      <span className="inline-flex items-center gap-1.5 text-sm text-blue-600">
        <Loader2 size={14} className="animate-spin" /> Installing…
      </span>
    )
  }
  if (app.status === 'failed') {
    return (
      <span className="inline-flex items-center gap-2">
        <span title={app.error}>
          <Badge color="red">install failed</Badge>
        </span>
        <button onClick={() => onRemove(app)} className="text-slate-400 hover:text-red-600" title="Remove record">
          <Trash2 size={13} />
        </button>
      </span>
    )
  }
  return (
    <span className="inline-flex items-center gap-2">
      <Badge color="blue">WordPress</Badge>
      <a href={app.url} target="_blank" rel="noreferrer" className="text-brand-600 hover:underline inline-flex items-center gap-0.5 text-xs">
        {app.auto_setup ? 'wp-admin' : 'finish setup'} <ExternalLink size={11} />
      </a>
      <button onClick={() => onRemove(app)} className="text-slate-400 hover:text-red-600" title="Remove record">
        <Trash2 size={13} />
      </button>
    </span>
  )
}
