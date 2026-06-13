import { useState } from 'react'
import { Plus, KeyRound, Trash2 } from 'lucide-react'
import { api, useFetch, formatDate } from '../api'
import type { DatabaseEntry } from '../types'
import {
  Btn, Card, PageHeader, Table, Td, Modal, Field, Input, Select, Badge,
  Spinner, ErrorBanner, Empty, toast,
} from '../components/ui'

export default function Databases() {
  const { data, error, loading, reload } = useFetch<DatabaseEntry[]>('/api/databases')
  const { data: engines } = useFetch<{ mysql: boolean; postgres: boolean }>('/api/database-engines')
  const [addOpen, setAddOpen] = useState(false)
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
          <Btn onClick={() => setAddOpen(true)}>
            <Plus size={16} /> Add Database
          </Btn>
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
          <Field label="Database name" hint="Letters, digits and underscores">
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="myapp_db" required />
          </Field>
          <Field label="Database user" hint="Defaults to the database name">
            <Input value={dbUser} onChange={(e) => setDbUser(e.target.value)} placeholder={name || 'myapp_db'} />
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
