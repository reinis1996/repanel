import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Plus, KeyRound, Trash2, Ban, CircleCheck, HardDrive, Lock, LogIn, TerminalSquare } from 'lucide-react'
import { api, useFetch, formatDate } from '../api'
import type { User, Usage, Plan } from '../types'
import { MODULES } from '../types'
import { useAuth } from '../App'
import {
  Btn, Card, PageHeader, Table, Td, Modal, Field, Input, Select,
  Spinner, ErrorBanner, Empty, Badge, toast, ModuleChecklist,
} from '../components/ui'

interface PermData {
  modules: { key: string; label: string }[]
  user: string[]
  reseller: string[]
}

export default function UsersPage() {
  const { user: me, setUser } = useAuth()
  const navigate = useNavigate()
  const [sshFor, setSshFor] = useState<User | null>(null)
  const { data, error, loading, reload } = useFetch<User[]>('/api/users')
  const usage = useFetch<Usage[]>('/api/usage')
  const plans = useFetch<Plan[]>('/api/plans')
  const usageBy = new Map((usage.data ?? []).map((x) => [x.user_id, x]))
  const planName = new Map((plans.data ?? []).map((p) => [p.id, p.name]))
  const [planId, setPlanId] = useState(0)
  const [addOpen, setAddOpen] = useState(false)
  const [pwFor, setPwFor] = useState<User | null>(null)
  const [quotaFor, setQuotaFor] = useState<User | null>(null)
  const [quota, setQuota] = useState(0)
  const [maxDomains, setMaxDomains] = useState(0)
  const [maxMailboxes, setMaxMailboxes] = useState(0)
  const [maxDatabases, setMaxDatabases] = useState(0)
  const [bandwidth, setBandwidth] = useState(0)
  const [cpu, setCpu] = useState(0)
  const [memory, setMemory] = useState(0)
  const [procs, setProcs] = useState(0)
  const [limitPlan, setLimitPlan] = useState(0)
  const [username, setUsername] = useState('')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [role, setRole] = useState('user')
  const [perms, setPerms] = useState<string[]>([])
  const [permsFor, setPermsFor] = useState<User | null>(null)
  const [editPerms, setEditPerms] = useState<string[]>([])
  const [busy, setBusy] = useState(false)

  const permData = useFetch<PermData>('/api/permissions')

  // Modules the current operator may grant: admins all, resellers only their own.
  const available = me!.role === 'admin' ? MODULES : MODULES.filter((m) => (me!.permissions ?? []).includes(m.key))

  // The configured group default, limited to what the operator can grant.
  const groupDefault = (r: string) => {
    const defaults = (r === 'reseller' ? permData.data?.reseller : permData.data?.user) ?? []
    const allow = new Set(available.map((m) => m.key))
    return defaults.filter((k) => allow.has(k))
  }

  // Pre-fill the create form's permissions from the group default for the role.
  useEffect(() => {
    if (addOpen) setPerms(groupDefault(role))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [addOpen, role, permData.data])

  const create = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      const body: Record<string, unknown> = { username, email, password, role }
      if (role !== 'admin') {
        if (planId) body.plan_id = planId
        else body.permissions = perms
      }
      await api.post('/api/users', body)
      toast(`User ${username} created`)
      setAddOpen(false)
      setUsername('')
      setEmail('')
      setPassword('')
      setPlanId(0)
      reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  const savePerms = async () => {
    if (!permsFor) return
    setBusy(true)
    try {
      await api.put(`/api/users/${permsFor.id}`, { permissions: editPerms })
      toast('Permissions updated')
      setPermsFor(null)
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

  const openLimits = (u: User) => {
    setQuotaFor(u)
    setQuota(u.disk_quota_mb)
    setMaxDomains(u.max_domains ?? 0)
    setMaxMailboxes(u.max_mailboxes ?? 0)
    setMaxDatabases(u.max_databases ?? 0)
    setBandwidth(u.bandwidth_quota_mb ?? 0)
    setCpu(u.cpu_quota_pct ?? 0)
    setMemory(u.memory_max_mb ?? 0)
    setProcs(u.processes_max ?? 0)
    setLimitPlan(u.plan_id ?? 0)
  }

  const saveQuota = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!quotaFor) return
    setBusy(true)
    try {
      // A plan supplies all limits server-side; Custom sends the explicit values
      // and detaches any plan.
      const body = limitPlan
        ? { plan_id: limitPlan }
        : {
            plan_id: 0,
            disk_quota_mb: quota,
            max_domains: maxDomains,
            max_mailboxes: maxMailboxes,
            max_databases: maxDatabases,
            bandwidth_quota_mb: bandwidth,
            cpu_quota_pct: cpu,
            memory_max_mb: memory,
            processes_max: procs,
          }
      await api.put(`/api/users/${quotaFor.id}`, body)
      toast('Limits updated')
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

  const impersonate = async (u: User) => {
    if (!confirm(`Log in as ${u.username}? You'll act as this account until you stop impersonating.`)) return
    try {
      await api.post(`/api/users/${u.id}/impersonate`)
      const me2 = await api.get<User>('/api/me')
      setUser(me2)
      navigate('/')
      toast(`Now viewing as ${u.username}`)
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
          <Table head={['Username', 'Email', 'Role', 'Disk', 'Bandwidth', 'Status', 'Created', '']}>
            {data.map((u) => (
              <tr key={u.id} className="hover:bg-slate-50/60">
                <Td className="font-medium">
                  {u.username}
                  {u.id === me!.id && <span className="text-xs text-slate-400 ml-1">(you)</span>}
                  {u.plan_id > 0 && planName.get(u.plan_id) && (
                    <div className="mt-0.5"><Badge color="blue">{planName.get(u.plan_id)}</Badge></div>
                  )}
                </Td>
                <Td className="text-slate-600">{u.email || '—'}</Td>
                <Td>
                  <Badge color={roleColor(u.role)}>{u.role}</Badge>
                </Td>
                <Td className="whitespace-nowrap text-slate-600">
                  {usageBy.get(u.id) ? `${usageBy.get(u.id)!.total_mb.toFixed(0)} MB` : '…'}
                  <span className="text-slate-400"> / {u.disk_quota_mb > 0 ? `${u.disk_quota_mb} MB` : '∞'}</span>
                </Td>
                <Td className="whitespace-nowrap text-slate-600">
                  {usageBy.get(u.id) ? `${usageBy.get(u.id)!.bandwidth_mb.toFixed(0)} MB` : '…'}
                  <span className="text-slate-400"> / {u.bandwidth_quota_mb > 0 ? `${u.bandwidth_quota_mb} MB` : '∞'}</span>
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
                        title="Limits & quota"
                        onClick={() => openLimits(u)}
                      >
                        <HardDrive size={13} />
                      </Btn>
                      <Btn size="sm" variant="secondary" className="mr-2" onClick={() => setPwFor(u)}>
                        <KeyRound size={13} />
                      </Btn>
                      {me!.role === 'admin' && (
                        <Btn size="sm" variant="secondary" className="mr-2" title="SSH access" onClick={() => setSshFor(u)}>
                          <TerminalSquare size={13} />
                        </Btn>
                      )}
                      <Btn size="sm" variant="secondary" className="mr-2" title="Log in as this user" onClick={() => impersonate(u)}>
                        <LogIn size={13} />
                      </Btn>
                      {u.role !== 'admin' && (
                        <Btn
                          size="sm"
                          variant="secondary"
                          className="mr-2"
                          title="Permissions"
                          onClick={() => {
                            setPermsFor(u)
                            setEditPerms(u.permissions ?? [])
                          }}
                        >
                          <Lock size={13} />
                        </Btn>
                      )}
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
          {role !== 'admin' && !!plans.data?.length && (
            <Field label="Hosting plan" hint="Applies the plan's limits and modules; choose Custom to set modules manually">
              <Select value={planId} onChange={(e) => setPlanId(Number(e.target.value))}>
                <option value={0}>Custom (no plan)</option>
                {plans.data.map((p) => (
                  <option key={p.id} value={p.id}>{p.name}</option>
                ))}
              </Select>
            </Field>
          )}
          {role !== 'admin' && !planId && (
            <Field label="Module access" hint="Which sections this account can use">
              <ModuleChecklist all={available} selected={perms} onChange={setPerms} />
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

      <Modal open={!!quotaFor} title={`Limits & quota — ${quotaFor?.username ?? ''}`} onClose={() => setQuotaFor(null)}>
        <form onSubmit={saveQuota}>
          {!!plans.data?.length && (
            <Field label="Hosting plan" hint="Choose a plan to apply its limits, or Custom to set them manually">
              <Select value={limitPlan} onChange={(e) => setLimitPlan(Number(e.target.value))}>
                <option value={0}>Custom</option>
                {plans.data.map((p) => (
                  <option key={p.id} value={p.id}>{p.name}</option>
                ))}
              </Select>
            </Field>
          )}
          {limitPlan ? (
            <p className="text-sm text-slate-500 mb-3">
              This account will use the limits and modules from the selected plan.
            </p>
          ) : (
            <>
              <p className="text-xs text-slate-400 mb-3">All limits use 0 = unlimited. Counts are enforced when the account creates new resources.</p>
              <Field label="Disk quota (MB)" hint="When exceeded, uploads and new mailboxes/databases are blocked.">
                <Input type="number" min={0} value={quota} onChange={(e) => setQuota(Number(e.target.value))} />
              </Field>
              <div className="grid grid-cols-2 gap-3">
                <Field label="Max domains">
                  <Input type="number" min={0} value={maxDomains} onChange={(e) => setMaxDomains(Number(e.target.value))} />
                </Field>
                <Field label="Max mailboxes">
                  <Input type="number" min={0} value={maxMailboxes} onChange={(e) => setMaxMailboxes(Number(e.target.value))} />
                </Field>
                <Field label="Max databases">
                  <Input type="number" min={0} value={maxDatabases} onChange={(e) => setMaxDatabases(Number(e.target.value))} />
                </Field>
                <Field label="Bandwidth (MB/mo)">
                  <Input type="number" min={0} value={bandwidth} onChange={(e) => setBandwidth(Number(e.target.value))} />
                </Field>
              </div>
              <p className="text-xs font-medium text-slate-600 mt-4 mb-1">Live resource caps (cgroups)</p>
              <p className="text-xs text-slate-400 mb-2">
                Applied via a systemd slice covering the account's Node apps and shell (SSH/SFTP) sessions.
              </p>
              <div className="grid grid-cols-3 gap-3">
                <Field label="CPU (% of a core)" hint="100 = 1 core">
                  <Input type="number" min={0} value={cpu} onChange={(e) => setCpu(Number(e.target.value))} />
                </Field>
                <Field label="Memory (MB)" hint="hard cap">
                  <Input type="number" min={0} value={memory} onChange={(e) => setMemory(Number(e.target.value))} />
                </Field>
                <Field label="Max processes">
                  <Input type="number" min={0} value={procs} onChange={(e) => setProcs(Number(e.target.value))} />
                </Field>
              </div>
            </>
          )}
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

      <Modal open={!!permsFor} title={`Permissions — ${permsFor?.username ?? ''}`} onClose={() => setPermsFor(null)}>
        <ModuleChecklist all={available} selected={editPerms} onChange={setEditPerms} />
        <div className="flex justify-end gap-2 mt-4">
          <Btn type="button" variant="secondary" onClick={() => setPermsFor(null)}>
            Cancel
          </Btn>
          <Btn disabled={busy} onClick={savePerms}>
            Save
          </Btn>
        </div>
      </Modal>

      {sshFor && <SshModal user={sshFor} onClose={() => setSshFor(null)} onSaved={reload} />}

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

function SshModal({ user, onClose, onSaved }: { user: User; onClose: () => void; onSaved: () => void }) {
  const { data, loading } = useFetch<{ enabled: boolean; keys: string[] }>(`/api/users/${user.id}/ssh`)
  const [enabled, setEnabled] = useState(false)
  const [keys, setKeys] = useState('')
  const [busy, setBusy] = useState(false)
  const [primed, setPrimed] = useState(false)

  if (data && !primed) {
    setEnabled(data.enabled)
    setKeys((data.keys ?? []).join('\n'))
    setPrimed(true)
  }

  const save = async () => {
    setBusy(true)
    try {
      await api.post(`/api/users/${user.id}/ssh`, {
        enabled,
        keys: keys.split('\n').map((k) => k.trim()).filter(Boolean),
      })
      toast(enabled ? 'SSH access enabled' : 'SSH access disabled')
      onSaved()
      onClose()
    } catch (e) {
      toast((e as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal open title={`SSH access — ${user.username}`} onClose={onClose}>
      {loading ? (
        <Spinner />
      ) : (
        <div>
          <p className="text-sm text-slate-500 mb-3">
            Grant this account shell access over SSH/SFTP. The system user gets a login shell and the public keys below
            are written to its <span className="font-mono text-xs">authorized_keys</span>.
          </p>
          <label className="flex items-center gap-2 text-sm text-slate-700 mb-4 cursor-pointer">
            <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
            Allow SSH login for this account
          </label>
          <Field label="Authorized public keys" hint="One key per line (ssh-ed25519 / ssh-rsa / ecdsa-…)">
            <textarea
              value={keys}
              onChange={(e) => setKeys(e.target.value)}
              disabled={!enabled}
              spellCheck={false}
              className="w-full h-32 rounded-md border border-slate-300 px-3 py-2 text-xs font-mono focus:outline-none focus:ring-2 focus:ring-brand-500/40 focus:border-brand-500 bg-white disabled:bg-slate-50"
              placeholder="ssh-ed25519 AAAA… user@laptop"
            />
          </Field>
          <div className="flex justify-end gap-2 mt-2">
            <Btn type="button" variant="secondary" onClick={onClose}>Cancel</Btn>
            <Btn disabled={busy} onClick={save}>Save</Btn>
          </div>
        </div>
      )}
    </Modal>
  )
}
