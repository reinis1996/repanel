import { useState } from 'react'
import { Plus, KeyRound, Trash2 } from 'lucide-react'
import { api, useFetch, formatDate } from '../api'
import type { FTPAccount } from '../types'
import {
  Btn, Card, PageHeader, Table, Td, Modal, Field, Input,
  Spinner, ErrorBanner, Empty, toast,
} from '../components/ui'

export default function Ftp() {
  const { data, error, loading, reload } = useFetch<FTPAccount[]>('/api/ftp')
  const [addOpen, setAddOpen] = useState(false)
  const [pwFor, setPwFor] = useState<FTPAccount | null>(null)
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [directory, setDirectory] = useState('')
  const [busy, setBusy] = useState(false)

  const create = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      await api.post('/api/ftp', { username, password, directory })
      toast(`FTP account ${username} created`)
      setAddOpen(false)
      setUsername('')
      setPassword('')
      setDirectory('')
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
      await api.post(`/api/ftp/${pwFor.id}/password`, { password })
      toast('Password updated')
      setPwFor(null)
      setPassword('')
    } catch (err) {
      toast((err as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  const remove = async (f: FTPAccount) => {
    if (!confirm(`Delete FTP account ${f.username}?`)) return
    try {
      await api.del(`/api/ftp/${f.id}`)
      toast('FTP account deleted')
      reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    }
  }

  if (loading) return <Spinner />

  return (
    <div>
      <PageHeader
        title="FTP Accounts"
        subtitle="Accounts are jailed to their directory inside your web space"
        actions={
          <Btn onClick={() => setAddOpen(true)}>
            <Plus size={16} /> Add FTP Account
          </Btn>
        }
      />
      <ErrorBanner message={error} />
      <Card>
        {!data?.length ? (
          <Empty title="No FTP accounts" />
        ) : (
          <Table head={['Username', 'Directory', 'Created', '']}>
            {data.map((f) => (
              <tr key={f.id} className="hover:bg-slate-50/60">
                <Td className="font-medium">{f.username}</Td>
                <Td className="font-mono text-xs text-slate-500">{f.directory}</Td>
                <Td className="text-slate-500">{formatDate(f.created_at)}</Td>
                <Td className="text-right whitespace-nowrap">
                  <Btn size="sm" variant="secondary" className="mr-2" onClick={() => setPwFor(f)}>
                    <KeyRound size={13} /> Password
                  </Btn>
                  <Btn size="sm" variant="danger" onClick={() => remove(f)}>
                    <Trash2 size={13} /> Delete
                  </Btn>
                </Td>
              </tr>
            ))}
          </Table>
        )}
      </Card>

      <Modal open={addOpen} title="Add FTP Account" onClose={() => setAddOpen(false)}>
        <form onSubmit={create}>
          <Field label="Username" hint="3-31 chars: lowercase letters, digits, - or _">
            <Input value={username} onChange={(e) => setUsername(e.target.value)} required />
          </Field>
          <Field label="Password" hint="At least 8 characters">
            <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required minLength={8} />
          </Field>
          <Field label="Directory" hint="Relative to your web space, e.g. example.com/public_html">
            <Input value={directory} onChange={(e) => setDirectory(e.target.value)} placeholder="/" />
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

      <Modal open={!!pwFor} title={`Password — ${pwFor?.username ?? ''}`} onClose={() => setPwFor(null)}>
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
