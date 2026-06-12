import { useState } from 'react'
import { Plus, KeyRound, Trash2, Ban, CircleCheck, HardDrive } from 'lucide-react'
import { api, useFetch, formatDate } from '../api'
import type { User, Usage } from '../types'
import { useAuth } from '../App'
import {
  Btn, Card, PageHeader, Table, Td, Modal, Field, Input, Select,
  Spinner, ErrorBanner, Empty, Badge, toast,
} from '../components/ui'

export default function UsersPage() {
  const { user: me } = useAuth()
  const { data, error, loading, reload } = useFetch<User[]>('/api/users')
  const usage = useFetch<Usage[]>('/api/usage')
  const usageBy = new Map((usage.data ?? []).map((x) => [x.user_id, x]))
  const [addOpen, setAddOpen] = useState(false)
  const [pwFor, setPwFor] = useState<User | null>(null)
  const [quotaFor, setQuotaFor] = useState<User | null>(null)
  const [quota, setQuota] = useState(0)
  const [username, setUsername] = useState('')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [role, setRole] = useState('user')
  const [busy, setBusy] = useState(false)

  const create = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      await api.post('/api/users', { username, email, password, role })
      toast(`User ${username} created`)
      setAddOpen(false)
      setUsername('')
      setEmail('')
      setPassword('')
      reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  const setNewPassword = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!pwFor) return
    setBusy(true)
    try {
      await api.put(`/api/users/${pwFor.id}`, { password })
      toast('Password updated')
      setPwFor(null)
      setPassword('')
    } catch (err) {
      toast((err as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  const saveQuota = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!quotaFor) return
    setBusy(true)
    try {
      await api.put(`/api/users/${quotaFor.id}`, { disk_quota_mb: quota })
      toast(quota > 0 ? `Quota set to ${quota} MB` : 'Quota removed (unlimited)')
      setQuotaFor(null)
      reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  const suspend = async (u: User) => {
    try {
      await api.put(`/api/users/${u.id}`, { suspended: !u.suspended })
      toast(u.suspended ? `${u.username} unsuspended` : `${u.username} suspended`)
      reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    }
  }

  const remove = async (u: User) => {
    if (!confirm(`Delete user ${u.username}?`)) return
    try {
      await api.del(`/api/users/${u.id}`)
      toast(`User ${u.username} deleted`)
      reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    }
  }

  if (loading) return <Spinner />

  const roleColor = (r: string) => (r === 'admin' ? 'red' : r === 'reseller' ? 'amber' : 'blue') as 'red' | 'amber' | 'blue'

  return (
    <div>
      <PageHeader
        title="Users"
        subtitle="Panel accounts — customers, resellers and administrators"
        actions={
          <Btn onClick={() => setAddOpen(true)}>
            <Plus size={16} /> Add User
          </Btn>
        }
      />
      <ErrorBanner message={error} />
      <Card>
        {!data?.length ? (
          <Empty title="No users" />
        ) : (
          <Table head={['Username', 'Email', 'Role', 'Disk', 'Status', 'Created', '']}>
            {data.map((u) => (
              <tr key={u.id} className="hover:bg-slate-50/60">
                <Td className="font-medium">
                  {u.username}
                  {u.id === me!.id && <span className="text-xs text-slate-400 ml-1">(you)</span>}
                </Td>
                <Td className="text-slate-600">{u.email || '—'}</Td>
                <Td>
                  <Badge color={roleColor(u.role)}>{u.role}</Badge>
                </Td>
                <Td className="whitespace-nowrap text-slate-600">
                  {usageBy.get(u.id) ? `${usageBy.get(u.id)!.total_mb.toFixed(0)} MB` : '…'}
                  <span className="text-slate-400"> / {u.disk_quota_mb > 0 ? `${u.disk_quota_mb} MB` : '∞'}</span>
                </Td>
                <Td>{u.suspended ? <Badge color="red">suspended</Badge> : <Badge color="green">active</Badge>}</Td>
                <Td className="text-slate-500">{formatDate(u.created_at)}</Td>
                <Td className="text-right whitespace-nowrap">
                  {u.id !== me!.id && (
                    <>
                      <Btn
                        size="sm"
                        variant="secondary"
                        className="mr-2"
                        title="Disk quota"
                        onClick={() => {
                          setQuotaFor(u)
                          setQuota(u.disk_quota_mb)
                        }}
                      >
                        <HardDrive size={13} />
                      </Btn>
                      <Btn size="sm" variant="secondary" className="mr-2" onClick={() => setPwFor(u)}>
                        <KeyRound size={13} />
                      </Btn>
                      <Btn size="sm" variant="secondary" className="mr-2" onClick={() => suspend(u)}>
                        {u.suspended ? <CircleCheck size={13} /> : <Ban size={13} />}
                        {u.suspended ? 'Unsuspend' : 'Suspend'}
                      </Btn>
                      <Btn size="sm" variant="danger" onClick={() => remove(u)}>
                        <Trash2 size={13} />
                      </Btn>
                    </>
                  )}
                </Td>
              </tr>
            ))}
          </Table>
        )}
      </Card>

      <Modal open={addOpen} title="Add User" onClose={() => setAddOpen(false)}>
        <form onSubmit={create}>
          <Field label="Username" hint="3-32 chars, starts with a letter">
            <Input value={username} onChange={(e) => setUsername(e.target.value)} required />
          </Field>
          <Field label="Email">
            <Input type="email" value={email} onChange={(e) => setEmail(e.target.value)} />
          </Field>
          <Field label="Password" hint="At least 8 characters">
            <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required minLength={8} />
          </Field>
          {me!.role === 'admin' && (
            <Field label="Role">
              <Select value={role} onChange={(e) => setRole(e.target.value)}>
                <option value="user">User — manages own hosting</option>
                <option value="reseller">Reseller — manages own customers</option>
                <option value="admin">Administrator — full access</option>
              </Select>
            </Field>
          )}
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

      <Modal open={!!quotaFor} title={`Disk quota — ${quotaFor?.username ?? ''}`} onClose={() => setQuotaFor(null)}>
        <form onSubmit={saveQuota}>
          <Field label="Quota (MB)" hint="0 = unlimited. When exceeded, uploads and new mailboxes/databases are blocked.">
            <Input type="number" min={0} value={quota} onChange={(e) => setQuota(Number(e.target.value))} />
          </Field>
          <div className="flex justify-end gap-2 mt-2">
            <Btn type="button" variant="secondary" onClick={() => setQuotaFor(null)}>
              Cancel
            </Btn>
            <Btn type="submit" disabled={busy}>
              Save
            </Btn>
          </div>
        </form>
      </Modal>

      <Modal open={!!pwFor} title={`Set password — ${pwFor?.username ?? ''}`} onClose={() => setPwFor(null)}>
        <form onSubmit={setNewPassword}>
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
