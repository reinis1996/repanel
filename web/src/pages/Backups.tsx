import { useEffect, useState } from 'react'
import { Plus, Download, Trash2, History, Loader2, Cloud, Server, FlaskConical } from 'lucide-react'
import { api, useFetch, formatBytes } from '../api'
import type { Backup, BackupDestination, BackupContents } from '../types'
import { useAuth } from '../App'
import {
  Btn, Card, PageHeader, Table, Td, Modal, Field, Input, Select,
  Spinner, ErrorBanner, Empty, Badge, toast, inputCls,
} from '../components/ui'

export default function Backups() {
  const { user } = useAuth()
  const isAdmin = user?.role === 'admin'
  const { data, error, loading, reload } = useFetch<Backup[]>('/api/backups')
  const [busy, setBusy] = useState(false)
  const [restoreFor, setRestoreFor] = useState<Backup | null>(null)

  const anyRunning = (data ?? []).some((b) => b.status === 'running')
  useEffect(() => {
    if (!anyRunning) return
    const t = setInterval(reload, 5000)
    return () => clearInterval(t)
  }, [anyRunning, reload])

  const create = async () => {
    setBusy(true)
    try {
      await api.post('/api/backups', {})
      toast('Backup started')
      reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  const remove = async (b: Backup) => {
    if (!confirm('Delete this backup archive?')) return
    try {
      await api.del(`/api/backups/${b.id}`)
      toast('Backup deleted')
      reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    }
  }

  if (loading) return <Spinner />

  return (
    <div>
      <PageHeader
        title="Backups"
        subtitle="Full account archives: web files, mail and database dumps. Enable nightly backups under Settings."
        actions={
          <Btn onClick={create} disabled={busy || anyRunning}>
            {anyRunning ? <Loader2 size={16} className="animate-spin" /> : <Plus size={16} />}
            {anyRunning ? 'Backup running…' : 'Back Up Now'}
          </Btn>
        }
      />
      <ErrorBanner message={error} />
      <Card>
        {!data?.length ? (
          <Empty title="No backups yet" hint="Create your first backup — it includes files, mail and databases" />
        ) : (
          <Table head={['Created', user!.role !== 'user' ? 'Account' : '', 'Size', 'Status', ''].filter(Boolean) as string[]}>
            {data.map((b) => (
              <tr key={b.id} className="hover:bg-slate-50/60">
                <Td className="whitespace-nowrap">{new Date(b.created_at).toLocaleString()}</Td>
                {user!.role !== 'user' && <Td className="text-slate-600">{b.owner}</Td>}
                <Td className="text-slate-500">{b.size_bytes ? formatBytes(b.size_bytes) : '—'}</Td>
                <Td>
                  {b.status === 'running' ? (
                    <Badge color="blue">running</Badge>
                  ) : b.status === 'completed' ? (
                    <Badge color="green">completed</Badge>
                  ) : (
                    <span title={b.error}>
                      <Badge color="red">failed</Badge>
                    </span>
                  )}
                  {b.status === 'failed' && b.error && (
                    <div className="text-xs text-red-500 mt-0.5 max-w-md break-all">{b.error}</div>
                  )}
                </Td>
                <Td className="text-right whitespace-nowrap">
                  {b.status === 'completed' && (
                    <>
                      <a href={`/api/backups/${b.id}/download`} className="inline-block align-middle mr-2">
                        <Btn size="sm" variant="secondary">
                          <Download size={13} /> Download
                        </Btn>
                      </a>
                      <Btn size="sm" variant="secondary" className="mr-2" onClick={() => setRestoreFor(b)}>
                        <History size={13} /> Restore
                      </Btn>
                    </>
                  )}
                  {b.status !== 'running' && (
                    <Btn size="sm" variant="danger" onClick={() => remove(b)}>
                      <Trash2 size={13} /> Delete
                    </Btn>
                  )}
                </Td>
              </tr>
            ))}
          </Table>
        )}
      </Card>

      {isAdmin && (
        <>
          <DestinationsCard />
          <ServerBackupCard />
        </>
      )}

      {restoreFor && <RestoreModal backup={restoreFor} onClose={() => setRestoreFor(null)} />}
    </div>
  )
}

function RestoreModal({ backup, onClose }: { backup: Backup; onClose: () => void }) {
  const { data, loading, error } = useFetch<BackupContents>(`/api/backups/${backup.id}/contents`)
  const [web, setWeb] = useState(true)
  const [dbs, setDbs] = useState<Record<string, boolean>>({})
  const [mail, setMail] = useState<Record<string, boolean>>({})
  const [fileFilter, setFileFilter] = useState('')
  const [busy, setBusy] = useState(false)

  const run = async (body: unknown, msg: string) => {
    setBusy(true)
    try {
      const res = await api.post<{ message: string }>(`/api/backups/${backup.id}/restore`, body)
      toast(res.message || msg)
      onClose()
    } catch (e) {
      toast((e as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  const restoreEverything = () => {
    if (!confirm('Restore the ENTIRE backup? Web files, mail and databases will be overwritten. This cannot be undone.')) return
    run({ all: true }, 'Restore started')
  }

  const restoreSelected = () => {
    const databases = Object.keys(dbs).filter((k) => dbs[k])
    const mail_domains = Object.keys(mail).filter((k) => mail[k])
    if (!web && !databases.length && !mail_domains.length) {
      toast('Select at least one item', 'err')
      return
    }
    if (!confirm('Restore the selected items? They will be overwritten with the archive contents.')) return
    run({ web, databases, mail_domains }, 'Restore started')
  }

  const matches = (data?.files ?? []).filter((f) => !fileFilter || f.toLowerCase().includes(fileFilter.toLowerCase())).slice(0, 200)

  return (
    <Modal open title={`Restore — ${new Date(backup.created_at).toLocaleString()}`} onClose={onClose} wide>
      {loading ? (
        <Spinner />
      ) : error ? (
        <ErrorBanner message={error} />
      ) : !data ? null : (
        <div className="space-y-5">
          <div className="flex items-center justify-between rounded-md border border-amber-200 bg-amber-50 px-4 py-3">
            <span className="text-sm text-amber-800">Restore overwrites existing data. Choose what to restore.</span>
            <Btn variant="danger" disabled={busy} onClick={restoreEverything}>Restore everything</Btn>
          </div>

          <div>
            <h3 className="text-sm font-semibold text-slate-700 mb-2">Restore selected components</h3>
            <div className="space-y-1.5">
              {data.has_web && (
                <label className="flex items-center gap-2 text-sm cursor-pointer">
                  <input type="checkbox" checked={web} onChange={(e) => setWeb(e.target.checked)} /> Web files
                </label>
              )}
              {data.databases.map((d) => (
                <label key={d} className="flex items-center gap-2 text-sm cursor-pointer">
                  <input type="checkbox" checked={!!dbs[d]} onChange={(e) => setDbs({ ...dbs, [d]: e.target.checked })} />
                  Database <span className="font-mono text-xs">{d}</span>
                </label>
              ))}
              {data.mail_domains.map((d) => (
                <label key={d} className="flex items-center gap-2 text-sm cursor-pointer">
                  <input type="checkbox" checked={!!mail[d]} onChange={(e) => setMail({ ...mail, [d]: e.target.checked })} />
                  Mail for <span className="font-mono text-xs">{d}</span>
                </label>
              ))}
            </div>
            <div className="flex justify-end mt-3">
              <Btn disabled={busy} onClick={restoreSelected}>Restore selected</Btn>
            </div>
          </div>

          {data.has_web && (
            <div className="border-t border-slate-200 pt-4">
              <h3 className="text-sm font-semibold text-slate-700 mb-2">Restore a single web file</h3>
              <Input value={fileFilter} onChange={(e) => setFileFilter(e.target.value)} placeholder="filter files…" />
              <div className="mt-2 max-h-56 overflow-auto rounded-md border border-slate-200 divide-y divide-slate-100">
                {!matches.length ? (
                  <div className="text-xs text-slate-400 p-3">No matching files.</div>
                ) : (
                  matches.map((f) => (
                    <div key={f} className="flex items-center justify-between px-3 py-1.5 text-xs hover:bg-slate-50">
                      <span className="font-mono truncate mr-2" title={f}>{f}</span>
                      <button
                        className="text-brand-600 hover:underline shrink-0 cursor-pointer"
                        disabled={busy}
                        onClick={() => run({ file: f }, 'File restored')}
                      >
                        restore
                      </button>
                    </div>
                  ))
                )}
              </div>
              {(data.files.length > matches.length) && (
                <div className="text-[11px] text-slate-400 mt-1">Showing {matches.length} of {data.files.length}; refine the filter to see more.</div>
              )}
            </div>
          )}
        </div>
      )}
    </Modal>
  )
}

function ServerBackupCard() {
  return (
    <Card
      title={
        <span className="flex items-center gap-2">
          <Server size={15} className="text-brand-600" /> Server / migration backup
        </span>
      }
      className="mt-4"
    >
      <p className="text-sm text-slate-500 mb-3">
        Download a snapshot of the panel database, <span className="font-mono text-xs">/etc/repanel</span> and issued
        certificates. On a fresh server, restore it with{' '}
        <span className="font-mono text-xs">repanel restore-config -archive &lt;file&gt;</span> (panel stopped), then start
        the panel — it regenerates every service config from the database.
      </p>
      <a href="/api/backups/server" className="inline-block">
        <Btn variant="secondary"><Download size={14} /> Download server backup</Btn>
      </a>
    </Card>
  )
}

const DEST_TYPES = [
  { v: 's3', label: 'S3-compatible (AWS, Spaces, MinIO…)' },
  { v: 'b2', label: 'Backblaze B2' },
  { v: 'sftp', label: 'SFTP' },
  { v: 'ftp', label: 'FTP' },
  { v: 'rclone', label: 'Other (paste rclone config)' },
]

function DestinationsCard() {
  const { data, reload } = useFetch<{ destinations: BackupDestination[]; rclone: boolean }>('/api/backups/destinations')
  const [open, setOpen] = useState(false)
  const dests = data?.destinations ?? []

  const installRclone = async () => {
    try {
      await api.post('/api/backups/rclone/install')
      toast('Installing rclone in the background')
    } catch (e) {
      toast((e as Error).message, 'err')
    }
  }

  const test = async (d: BackupDestination) => {
    try {
      await api.post(`/api/backups/destinations/${d.id}/test`)
      toast(`${d.name}: connection OK`)
    } catch (e) {
      toast((e as Error).message, 'err')
    }
  }

  const remove = async (d: BackupDestination) => {
    if (!confirm(`Delete destination ${d.name}?`)) return
    try {
      await api.del(`/api/backups/destinations/${d.id}`)
      toast('Destination deleted')
      reload()
    } catch (e) {
      toast((e as Error).message, 'err')
    }
  }

  const toggle = async (d: BackupDestination) => {
    try {
      await api.put(`/api/backups/destinations/${d.id}`, { name: d.name, type: d.type, remote_path: d.remote_path, keep: d.keep, enabled: !d.enabled })
      reload()
    } catch (e) {
      toast((e as Error).message, 'err')
    }
  }

  return (
    <Card
      title={
        <span className="flex items-center gap-2">
          <Cloud size={15} className="text-brand-600" /> Offsite destinations
        </span>
      }
      actions={
        data && !data.rclone ? (
          <Btn size="sm" onClick={installRclone}>Install rclone</Btn>
        ) : (
          <Btn size="sm" variant="secondary" onClick={() => setOpen(true)}>
            <Plus size={14} /> Add destination
          </Btn>
        )
      }
      className="mt-4"
    >
      {data && !data.rclone ? (
        <Empty title="rclone not installed" hint="Install rclone to copy backups to S3, B2, SFTP, FTP, Dropbox, Google Drive and more." />
      ) : !dests.length ? (
        <Empty title="No offsite destinations" hint="Completed backups upload to every enabled destination automatically." />
      ) : (
        <Table head={['Name', 'Type', 'Path', 'Keep', 'Status', '']}>
          {dests.map((d) => (
            <tr key={d.id} className="hover:bg-slate-50/60">
              <Td className="font-medium">{d.name}</Td>
              <Td className="text-slate-500">{d.type}</Td>
              <Td className="font-mono text-xs text-slate-500">{d.remote_path || '—'}</Td>
              <Td className="text-slate-500">{d.keep}</Td>
              <Td>{d.enabled ? <Badge color="green">enabled</Badge> : <Badge color="gray">disabled</Badge>}</Td>
              <Td className="text-right whitespace-nowrap">
                <button className="text-slate-400 hover:text-brand-600 mr-3 cursor-pointer" title="Test connection" onClick={() => test(d)}>
                  <FlaskConical size={15} />
                </button>
                <Btn size="sm" variant="secondary" className="mr-2" onClick={() => toggle(d)}>{d.enabled ? 'Disable' : 'Enable'}</Btn>
                <Btn size="sm" variant="danger" onClick={() => remove(d)}><Trash2 size={13} /></Btn>
              </Td>
            </tr>
          ))}
        </Table>
      )}

      {open && <DestinationModal onClose={() => setOpen(false)} onSaved={reload} />}
    </Card>
  )
}

function DestinationModal({ onClose, onSaved }: { onClose: () => void; onSaved: () => void }) {
  const [name, setName] = useState('')
  const [type, setType] = useState('s3')
  const [remotePath, setRemotePath] = useState('')
  const [keep, setKeep] = useState(7)
  const [fields, setFields] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState(false)

  const f = (k: string) => fields[k] ?? ''
  const setF = (k: string, v: string) => setFields({ ...fields, [k]: v })

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      await api.post('/api/backups/destinations', { name, type, remote_path: remotePath, keep, enabled: true, fields })
      toast('Destination added')
      onSaved()
      onClose()
    } catch (err) {
      toast((err as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal open title="Add offsite destination" onClose={onClose}>
      <form onSubmit={submit}>
        <Field label="Name">
          <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="Backblaze nightly" required />
        </Field>
        <Field label="Type">
          <Select value={type} onChange={(e) => { setType(e.target.value); setFields({}) }}>
            {DEST_TYPES.map((t) => <option key={t.v} value={t.v}>{t.label}</option>)}
          </Select>
        </Field>

        {type === 's3' && (
          <>
            <Field label="Access key ID"><Input value={f('access_key_id')} onChange={(e) => setF('access_key_id', e.target.value)} required /></Field>
            <Field label="Secret access key"><Input type="password" value={f('secret_access_key')} onChange={(e) => setF('secret_access_key', e.target.value)} required /></Field>
            <Field label="Endpoint" hint="Leave blank for AWS S3; set for Spaces/MinIO/etc."><Input value={f('endpoint')} onChange={(e) => setF('endpoint', e.target.value)} placeholder="https://nyc3.digitaloceanspaces.com" /></Field>
            <Field label="Region" hint="optional"><Input value={f('region')} onChange={(e) => setF('region', e.target.value)} placeholder="us-east-1" /></Field>
          </>
        )}
        {type === 'b2' && (
          <>
            <Field label="Account ID / Key ID"><Input value={f('account')} onChange={(e) => setF('account', e.target.value)} required /></Field>
            <Field label="Application key"><Input type="password" value={f('key')} onChange={(e) => setF('key', e.target.value)} required /></Field>
          </>
        )}
        {(type === 'sftp' || type === 'ftp') && (
          <>
            <div className="grid grid-cols-[1fr_6rem] gap-2">
              <Field label="Host"><Input value={f('host')} onChange={(e) => setF('host', e.target.value)} required /></Field>
              <Field label="Port"><Input value={f('port')} onChange={(e) => setF('port', e.target.value)} placeholder={type === 'sftp' ? '22' : '21'} /></Field>
            </div>
            <Field label="Username"><Input value={f('user')} onChange={(e) => setF('user', e.target.value)} required /></Field>
            <Field label="Password"><Input type="password" value={f('pass')} onChange={(e) => setF('pass', e.target.value)} required /></Field>
          </>
        )}
        {type === 'rclone' && (
          <Field label="rclone remote config" hint="Paste the body of an rclone remote (e.g. from `rclone config show`), without the [name] header — covers Dropbox, Google Drive, etc.">
            <textarea value={f('raw')} onChange={(e) => setF('raw', e.target.value)} className={`${inputCls} h-32 font-mono text-xs`} placeholder={'type = drive\ntoken = {"access_token":...}'} required />
          </Field>
        )}

        <Field label={type === 'sftp' || type === 'ftp' ? 'Remote path' : 'Bucket / path'} hint="Where archives are stored, e.g. my-bucket/backups or /backups">
          <Input value={remotePath} onChange={(e) => setRemotePath(e.target.value)} placeholder="my-bucket/repanel" required />
        </Field>
        <Field label="Keep (newest N per account on the remote)">
          <Input type="number" value={keep} onChange={(e) => setKeep(Number(e.target.value))} min={1} />
        </Field>

        <div className="flex justify-end gap-2 mt-2">
          <Btn type="button" variant="secondary" onClick={onClose}>Cancel</Btn>
          <Btn type="submit" disabled={busy}>Add destination</Btn>
        </div>
      </form>
    </Modal>
  )
}
