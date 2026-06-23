import { useState } from 'react'
import { Plus, KeyRound, Trash2, Database, ExternalLink } from 'lucide-react'
import { api, useFetch, formatDate } from '../api'
import type { DatabaseEntry, DBAdminStatus } from '../types'
import { useAuth } from '../App'
import {
  Btn, Card, PageHeader, Table, Td, Modal, Field, Input, Select, Badge,
  Spinner, ErrorBanner, Empty, toast,
} from '../components/ui'

// dbPrefix mirrors the backend (internal/api/databases.go): databases and their
// users are namespaced under the account's username, e.g. "alice_". The admin
// account is the server owner and is not prefixed.
function dbPrefix(role: string, username: string): string {
  if (role === 'admin') return ''
  const p = username.toLowerCase().replace(/[^a-z0-9]+/g, '_').replace(/^_+|_+$/g, '').slice(0, 16)
  return p ? p + '_' : ''
}

// PrefixedInput renders the read-only account prefix attached to the left of a
// text input, so the user sees exactly what their database will be named.
function PrefixedInput({ prefix, ...props }: { prefix: string } & React.InputHTMLAttributes<HTMLInputElement>) {
  if (!prefix) return <Input {...props} />
  return (
    <div className="flex items-stretch">
      <span className="inline-flex items-center rounded-l-md border border-r-0 border-slate-300 bg-slate-100 px-3 text-sm font-mono text-slate-500 select-none">
        {prefix}
      </span>
      <div className="flex-1">
        <Input {...props} style={{ borderTopLeftRadius: 0, borderBottomLeftRadius: 0 }} />
      </div>
    </div>
  )
}

// adminerLink builds the Adminer URL preloaded with a database's target.
function adminerLink(base: string, d: DatabaseEntry): string {
  const driver = d.engine === 'postgres' ? 'pgsql' : 'server'
  const u = new URL(base)
  u.searchParams.set(driver, 'localhost')
  u.searchParams.set('username', d.db_user)
  u.searchParams.set('db', d.name)
  return u.toString()
}

export default function Databases() {
  const { user } = useAuth()
  const isAdmin = user!.role === 'admin'
  const prefix = dbPrefix(user!.role, user!.username)
  const { data, error, loading, reload } = useFetch<DatabaseEntry[]>('/api/databases')
  const { data: engines } = useFetch<{ mysql: boolean; postgres: boolean }>('/api/database-engines')
  const dbadmin = useFetch<DBAdminStatus>('/api/dbadmin')
  const [addOpen, setAddOpen] = useState(false)
  const [adminerOpen, setAdminerOpen] = useState(false)
  const [pwFor, setPwFor] = useState<DatabaseEntry | null>(null)
  const [name, setName] = useState('')
  const [dbUser, setDbUser] = useState('')
  const [password, setPassword] = useState('')
  const [engine, setEngine] = useState('mysql')
  const [busy, setBusy] = useState(false)

  const showEnginePicker = !!engines?.postgres

  const create = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      await api.post('/api/databases', { name, user: dbUser, password, engine })
      toast(`Database ${name} created`)
      setAddOpen(false)
      setName('')
      setDbUser('')
      setPassword('')
      setEngine('mysql')
      reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  const changePassword = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!pwFor) return
    setBusy(true)
    try {
      await api.post(`/api/databases/${pwFor.id}/password`, { password })
      toast('Password updated')
      setPwFor(null)
      setPassword('')
    } catch (err) {
      toast((err as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  const remove = async (d: DatabaseEntry) => {
    if (!confirm(`Drop database ${d.name}? This permanently deletes all its data.`)) return
    try {
      await api.del(`/api/databases/${d.id}`)
      toast(`Database ${d.name} dropped`)
      reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    }
  }

  if (loading) return <Spinner />

  return (
    <div>
      <PageHeader
        title="Databases"
        subtitle={
          showEnginePicker
            ? 'MariaDB and PostgreSQL databases, with one dedicated user per database'
            : 'MariaDB databases with one dedicated user per database'
        }
        actions={
          <>
            <Btn variant="secondary" onClick={() => setAdminerOpen(true)}>
              <Database size={16} /> Web DB admin
            </Btn>
            <Btn onClick={() => setAddOpen(true)}>
              <Plus size={16} /> Add Database
            </Btn>
          </>
        }
      />
      <ErrorBanner message={error} />
      <Card>
        {!data?.length ? (
          <Empty title="No databases" />
        ) : (
          <Table head={['Database', 'Engine', 'User', 'Size', 'Created', '']}>
            {data.map((d) => (
              <tr key={d.id} className="hover:bg-slate-50/60">
                <Td className="font-medium">{d.name}</Td>
                <Td>
                  <Badge color={d.engine === 'postgres' ? 'blue' : 'gray'}>
                    {d.engine === 'postgres' ? 'PostgreSQL' : 'MariaDB'}
                  </Badge>
                </Td>
                <Td className="text-slate-600">{d.db_user}</Td>
                <Td className="text-slate-500">{d.size_mb ? `${d.size_mb.toFixed(1)} MB` : '—'}</Td>
                <Td className="text-slate-500">{formatDate(d.created_at)}</Td>
                <Td className="text-right whitespace-nowrap">
                  {dbadmin.data?.enabled && dbadmin.data.url && (
                    <a
                      href={adminerLink(dbadmin.data.url, d)}
                      target="_blank"
                      rel="noreferrer"
                      className="inline-flex items-center gap-1 text-xs text-brand-600 hover:underline mr-3 align-middle"
                      title="Open in Adminer"
                    >
                      <Database size={13} /> Adminer <ExternalLink size={10} />
                    </a>
                  )}
                  <Btn size="sm" variant="secondary" className="mr-2" onClick={() => setPwFor(d)}>
                    <KeyRound size={13} /> Password
                  </Btn>
                  <Btn size="sm" variant="danger" onClick={() => remove(d)}>
                    <Trash2 size={13} /> Drop
                  </Btn>
                </Td>
              </tr>
            ))}
          </Table>
        )}
      </Card>

      <Modal open={addOpen} title="Add Database" onClose={() => setAddOpen(false)}>
        <form onSubmit={create}>
          {showEnginePicker && (
            <Field label="Engine">
              <Select value={engine} onChange={(e) => setEngine(e.target.value)}>
                <option value="mysql">MariaDB / MySQL</option>
                <option value="postgres">PostgreSQL</option>
              </Select>
            </Field>
          )}
          <Field label="Database name" hint={prefix ? `Created as ${prefix}…  · letters, digits and underscores` : 'Letters, digits and underscores'}>
            <PrefixedInput prefix={prefix} value={name} onChange={(e) => setName(e.target.value)} placeholder="myapp" required />
          </Field>
          <Field label="Database user" hint="Defaults to the database name">
            <PrefixedInput prefix={prefix} value={dbUser} onChange={(e) => setDbUser(e.target.value)} placeholder={name || 'myapp'} />
          </Field>
          <Field label="Password" hint="At least 8 characters">
            <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required minLength={8} />
          </Field>
          <div className="flex justify-end gap-2 mt-2">
            <Btn type="button" variant="secondary" onClick={() => setAddOpen(false)}>
              Cancel
            </Btn>
            <Btn type="submit" disabled={busy}>
              Create
            </Btn>
          </div>
        </form>
      </Modal>

      <AdminerModal
        open={adminerOpen}
        isAdmin={isAdmin}
        status={dbadmin.data}
        onClose={() => setAdminerOpen(false)}
        onChanged={dbadmin.reload}
      />

      <Modal open={!!pwFor} title={`Password — ${pwFor?.db_user ?? ''}`} onClose={() => setPwFor(null)}>
        <form onSubmit={changePassword}>
          <Field label="New password" hint="At least 8 characters">
            <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required minLength={8} />
          </Field>
          <div className="flex justify-end gap-2 mt-2">
            <Btn type="button" variant="secondary" onClick={() => setPwFor(null)}>
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

function AdminerModal({
  open,
  isAdmin,
  status,
  onClose,
  onChanged,
}: {
  open: boolean
  isAdmin: boolean
  status: DBAdminStatus | null
  onClose: () => void
  onChanged: () => void
}) {
  const [busy, setBusy] = useState(false)
  if (!open) return null

  const run = async (fn: () => Promise<unknown>, msg: string) => {
    setBusy(true)
    try {
      await fn()
      toast(msg)
      onChanged()
    } catch (e) {
      toast((e as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal open title="Web database admin (Adminer)" onClose={onClose}>
      <p className="text-sm text-slate-500 mb-4">
        Adminer is a lightweight web client for managing your databases in the browser. It is served on{' '}
        <span className="font-mono text-xs">{status?.host || 'dbadmin.<panel hostname>'}</span>.
      </p>
      {!status ? (
        <Spinner />
      ) : !isAdmin ? (
        <div className="rounded-md bg-amber-50 border border-amber-200 text-amber-800 text-sm px-4 py-3">
          {status.enabled
            ? 'Adminer is available — use the Adminer link on each database row.'
            : 'Ask an administrator to enable the web database admin.'}
        </div>
      ) : (
        <div className="space-y-3">
          <div className="flex items-center justify-between text-sm">
            <span>Adminer installed</span>
            {status.installed ? <Badge color="green">yes</Badge> : <Badge color="gray">no</Badge>}
          </div>
          <div className="flex items-center justify-between text-sm">
            <span>Served at</span>
            <span className="font-mono text-xs">{status.url || (status.host ? `http://${status.host}/` : 'set panel hostname first')}</span>
          </div>
          <div className="flex gap-2 pt-2">
            {!status.installed && (
              <Btn type="button" disabled={busy} onClick={() => run(() => api.post('/api/dbadmin/install'), 'Adminer installed')}>
                {busy ? 'Installing…' : 'Install Adminer'}
              </Btn>
            )}
            {status.installed && !status.enabled && (
              <Btn type="button" disabled={busy} onClick={() => run(() => api.post('/api/dbadmin/enable', { enabled: true }), 'Adminer enabled')}>
                Enable
              </Btn>
            )}
            {status.enabled && (
              <Btn type="button" variant="danger" disabled={busy} onClick={() => run(() => api.post('/api/dbadmin/enable', { enabled: false }), 'Adminer disabled')}>
                Disable
              </Btn>
            )}
          </div>
          <p className="text-xs text-slate-400">
            Adminer is served over plain HTTP on its own hostname; put it behind your own TLS/VPN if exposing it publicly.
          </p>
        </div>
      )}
    </Modal>
  )
}
