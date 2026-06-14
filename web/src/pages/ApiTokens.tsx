import { useState } from 'react'
import { Plus, Trash2, Copy, Check, KeyRound } from 'lucide-react'
import { api, useFetch, formatDate } from '../api'
import type { APIToken } from '../types'
import {
  Btn, Card, PageHeader, Table, Td, Modal, Field, Input, Select,
  Spinner, ErrorBanner, Empty, toast,
} from '../components/ui'

export default function ApiTokens() {
  const { data, error, loading, reload } = useFetch<APIToken[]>('/api/tokens')
  const [addOpen, setAddOpen] = useState(false)
  const [name, setName] = useState('')
  const [expires, setExpires] = useState('0')
  const [scope, setScope] = useState('full')
  const [busy, setBusy] = useState(false)
  const [created, setCreated] = useState<APIToken | null>(null)
  const [copied, setCopied] = useState(false)

  const create = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      const token = await api.post<APIToken>('/api/tokens', {
        name,
        expires_days: Number(expires),
        scope,
      })
      setAddOpen(false)
      setName('')
      setExpires('0')
      setScope('full')
      setCreated(token)
      reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  const remove = async (t: APIToken) => {
    if (!confirm(`Revoke token "${t.name}"? Any client using it will stop working immediately.`)) return
    try {
      await api.del(`/api/tokens/${t.id}`)
      toast('Token revoked')
      reload()
    } catch (err) {
      toast((err as Error).message, 'err')
    }
  }

  const copy = (text: string) => {
    navigator.clipboard?.writeText(text)
    setCopied(true)
    setTimeout(() => setCopied(false), 1500)
  }

  if (loading) return <Spinner />

  return (
    <div>
      <PageHeader
        title="API Tokens"
        subtitle="Personal access tokens for the REST API. A token carries your account's permissions."
        actions={
          <Btn onClick={() => setAddOpen(true)}>
            <Plus size={16} /> New Token
          </Btn>
        }
      />
      <ErrorBanner message={error} />
      <Card>
        {!data?.length ? (
          <Empty
            title="No API tokens"
            hint="Create a token to use the panel from scripts or the command line"
          />
        ) : (
          <Table head={['Name', 'Token', 'Scope', 'Last used', 'Expires', 'Created', '']}>
            {data.map((t) => (
              <tr key={t.id} className="hover:bg-slate-50/60">
                <Td className="font-medium">{t.name}</Td>
                <Td className="font-mono text-xs text-slate-500">{t.prefix}…</Td>
                <Td className="text-slate-500">{t.scope === 'readonly' ? 'read-only' : 'full'}</Td>
                <Td className="text-slate-500">{t.last_used_at ? formatDate(t.last_used_at) : 'never'}</Td>
                <Td className="text-slate-500">{t.expires_at ? formatDate(t.expires_at) : 'never'}</Td>
                <Td className="text-slate-500">{formatDate(t.created_at)}</Td>
                <Td className="text-right whitespace-nowrap">
                  <Btn size="sm" variant="danger" onClick={() => remove(t)}>
                    <Trash2 size={13} /> Revoke
                  </Btn>
                </Td>
              </tr>
            ))}
          </Table>
        )}
      </Card>

      <Modal open={addOpen} title="New API Token" onClose={() => setAddOpen(false)}>
        <form onSubmit={create}>
          <Field label="Name" hint="A label to remember what this token is for">
            <Input value={name} onChange={(e) => setName(e.target.value)} required maxLength={64} placeholder="e.g. deploy script" />
          </Field>
          <Field label="Expiry">
            <Select value={expires} onChange={(e) => setExpires(e.target.value)}>
              <option value="0">Never</option>
              <option value="7">7 days</option>
              <option value="30">30 days</option>
              <option value="90">90 days</option>
              <option value="365">1 year</option>
            </Select>
          </Field>
          <Field label="Scope" hint="Read-only tokens can only perform GET requests">
            <Select value={scope} onChange={(e) => setScope(e.target.value)}>
              <option value="full">Full access</option>
              <option value="readonly">Read-only</option>
            </Select>
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

      <Modal open={!!created} title="Token created" onClose={() => setCreated(null)}>
        <div className="flex items-start gap-2 rounded-md bg-amber-50 border border-amber-200 text-amber-800 text-sm px-4 py-3 mb-4">
          <KeyRound size={16} className="mt-0.5 shrink-0" />
          <span>Copy this token now — it won't be shown again.</span>
        </div>
        <div className="flex items-center gap-2 mb-4">
          <code className="flex-1 break-all rounded-md bg-slate-100 border border-slate-200 px-3 py-2 text-xs font-mono text-slate-800">
            {created?.token}
          </code>
          <Btn variant="secondary" onClick={() => created?.token && copy(created.token)}>
            {copied ? <Check size={15} /> : <Copy size={15} />}
          </Btn>
        </div>
        <p className="text-xs text-slate-500 mb-4">
          Use it as a bearer token, e.g.<br />
          <code className="font-mono text-slate-600">curl -H "Authorization: Bearer {created?.prefix}…" https://&lt;panel&gt;/api/domains</code>
        </p>
        <div className="flex justify-end">
          <Btn onClick={() => setCreated(null)}>Done</Btn>
        </div>
      </Modal>
    </div>
  )
}
