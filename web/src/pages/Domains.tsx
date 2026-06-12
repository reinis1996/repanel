import { useState } from 'react'
import { Plus, Lock, LockOpen } from 'lucide-react'
import { api, useFetch, formatDate } from '../api'
import type { Domain, User } from '../types'
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

  const [addOpen, setAddOpen] = useState(false)
  const [name, setName] = useState('')
  const [php, setPhp] = useState('')
  const [owner, setOwner] = useState(0)
  const [createDNS, setCreateDNS] = useState(true)
  const [busy, setBusy] = useState(false)

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
          <Table head={['Domain', 'Owner', 'PHP', 'SSL', 'Status', 'Created', '']}>
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
                <Td>{d.suspended ? <Badge color="red">suspended</Badge> : <Badge color="green">active</Badge>}</Td>
                <Td className="text-slate-500">{formatDate(d.created_at)}</Td>
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
    </div>
  )
}
