import { useEffect, useMemo, useState } from 'react'
import { Plus, Trash2, Pencil, ExternalLink, ShieldCheck, Clock, Play } from 'lucide-react'
import { api, useFetch } from '../api'
import type { FunctionItem, FunctionMeta, FunctionInvokeResult } from '../types'
import {
  Btn, Card, PageHeader, Table, Td, Modal, Field, Input, Select,
  Spinner, ErrorBanner, Empty, Badge, toast,
} from '../components/ui'

const FUNCTION_LABEL = 'function-url'
const DEFAULT_SCHEDULE = '0 * * * *' // hourly

const runtimeLabels: Record<string, string> = { python: 'Python', node: 'Node.js', php: 'PHP' }

interface FnSettings {
  name: string
  runtime: string
  version: string
  trigger: string
  schedule: string
  allow_network: boolean
  base_domain: string
}

export default function Functions() {
  const { data, error, loading, reload } = useFetch<FunctionItem[]>('/api/functions')
  const { data: meta } = useFetch<FunctionMeta>('/api/functions/meta')
  const [creating, setCreating] = useState<FnSettings | null>(null)
  const [editing, setEditing] = useState<FunctionItem | null>(null)

  return (
    <div>
      <PageHeader
        title="Functions"
        subtitle="Run Python, Node or PHP code at a URL or on a schedule — serverless style"
        actions={
          <Btn
            onClick={() =>
              setCreating({ name: '', runtime: '', version: '', trigger: 'url', schedule: DEFAULT_SCHEDULE, allow_network: false, base_domain: '' })
            }
          >
            <Plus size={16} /> Add Function
          </Btn>
        }
      />
      <ErrorBanner message={error} />

      {loading ? (
        <Spinner />
      ) : (
        <Card>
          {!data?.length ? (
            <Empty title="No functions yet" hint="Create one to run code at a URL or on a schedule." />
          ) : (
            <Table head={['Name', 'Trigger', 'Runtime', 'Status', '']}>
              {data.map((fn) => (
                <tr key={fn.id} className="hover:bg-slate-50/60">
                  <Td className="font-medium text-slate-700">{fn.name}</Td>
                  <Td>
                    {fn.trigger === 'schedule' ? (
                      <span className="inline-flex items-center gap-1.5 text-xs text-slate-600">
                        <Clock size={13} className="text-slate-400" />
                        <span className="font-mono">{fn.schedule}</span>
                      </span>
                    ) : (
                      <a
                        href={fn.url}
                        target="_blank"
                        rel="noreferrer"
                        className="inline-flex items-center gap-1 font-mono text-xs text-brand-600 hover:underline break-all"
                      >
                        {fn.hostname}
                        <ExternalLink size={12} className="shrink-0" />
                      </a>
                    )}
                  </Td>
                  <Td className="whitespace-nowrap">
                    <Badge color="blue">
                      {runtimeLabels[fn.runtime] ?? fn.runtime} {fn.version}
                    </Badge>
                  </Td>
                  <Td>
                    <StatusToggle fn={fn} onChange={reload} />
                  </Td>
                  <Td className="text-right whitespace-nowrap">
                    <button
                      className="text-slate-400 hover:text-brand-600 mr-3 cursor-pointer"
                      title="Edit"
                      onClick={() => setEditing(fn)}
                    >
                      <Pencil size={15} />
                    </button>
                    <button
                      className="text-slate-400 hover:text-red-600 cursor-pointer"
                      title="Delete"
                      onClick={async () => {
                        if (!confirm(`Delete function "${fn.name}"? Its code will be removed.`)) return
                        try {
                          await api.del(`/api/functions/${fn.id}`)
                          toast('Function deleted')
                          reload()
                        } catch (err) {
                          toast((err as Error).message, 'err')
                        }
                      }}
                    >
                      <Trash2 size={15} />
                    </button>
                  </Td>
                </tr>
              ))}
            </Table>
          )}
        </Card>
      )}

      {creating && meta && (
        <CreateModal
          meta={meta}
          draft={creating}
          setDraft={setCreating}
          onClose={() => setCreating(null)}
          onDone={() => {
            setCreating(null)
            reload()
          }}
        />
      )}

      {editing && meta && <EditModal fn={editing} meta={meta} onClose={() => setEditing(null)} onDone={reload} />}
    </div>
  )
}

function StatusToggle({ fn, onChange }: { fn: FunctionItem; onChange: () => void }) {
  const toggle = async () => {
    try {
      await api.put(`/api/functions/${fn.id}`, { enabled: !fn.enabled })
      onChange()
    } catch (err) {
      toast((err as Error).message, 'err')
    }
  }
  return (
    <button onClick={toggle} className="cursor-pointer">
      {fn.enabled ? <Badge color="green">enabled</Badge> : <Badge color="gray">disabled</Badge>}
    </button>
  )
}

// SettingsFields renders the form shared by the create and edit modals. `slug`,
// when given, is used in the URL preview (otherwise a placeholder).
function SettingsFields({
  draft,
  setDraft,
  meta,
  slug,
}: {
  draft: FnSettings
  setDraft: (d: FnSettings) => void
  meta: FunctionMeta
  slug?: string
}) {
  const versions = useMemo(
    () => meta.runtimes.find((r) => r.runtime === draft.runtime)?.versions ?? [],
    [meta, draft.runtime],
  )
  const isSchedule = draft.trigger === 'schedule'
  const base = draft.base_domain || meta.default_base || '<panel-hostname>'
  const previewURL = `https://${slug ?? 'xxxx'}.${FUNCTION_LABEL}.${base}`

  return (
    <>
      <Field label="Name">
        <Input
          value={draft.name}
          onChange={(e) => setDraft({ ...draft, name: e.target.value })}
          placeholder="my-function"
          required
        />
      </Field>
      <div className="grid grid-cols-2 gap-3">
        <Field label="Runtime">
          <Select
            value={draft.runtime}
            onChange={(e) => {
              const r = meta.runtimes.find((x) => x.runtime === e.target.value)
              setDraft({ ...draft, runtime: e.target.value, version: r?.versions[0] ?? '' })
            }}
          >
            {meta.runtimes.map((r) => (
              <option key={r.runtime} value={r.runtime}>
                {runtimeLabels[r.runtime] ?? r.runtime}
              </option>
            ))}
          </Select>
        </Field>
        <Field label="Version">
          <Select value={draft.version} onChange={(e) => setDraft({ ...draft, version: e.target.value })}>
            {versions.map((v) => (
              <option key={v} value={v}>
                {v}
              </option>
            ))}
          </Select>
        </Field>
      </div>

      <Field label="Trigger" hint="Invoke over a public URL, or run automatically on a schedule.">
        <Select value={draft.trigger} onChange={(e) => setDraft({ ...draft, trigger: e.target.value })}>
          <option value="url">Function URL (HTTP)</option>
          <option value="schedule">Schedule (cron)</option>
        </Select>
      </Field>

      {isSchedule ? (
        <Field label="Schedule" hint="Standard cron format (min hour day month weekday) or @daily, @hourly…">
          <Input
            value={draft.schedule}
            onChange={(e) => setDraft({ ...draft, schedule: e.target.value })}
            className="font-mono"
            required
          />
        </Field>
      ) : (
        <>
          <Field label="URL base" hint="Pick one of your DNS-hosted domains, or use the panel's default URL.">
            <Select value={draft.base_domain} onChange={(e) => setDraft({ ...draft, base_domain: e.target.value })}>
              <option value="">Panel URL ({meta.default_base || 'not configured'})</option>
              {meta.domains.map((d) => (
                <option key={d} value={d}>
                  {d}
                </option>
              ))}
            </Select>
          </Field>
          <div className="rounded-md bg-slate-50 border border-slate-200 px-3 py-2 text-xs text-slate-500 mb-4">
            URL: <span className="font-mono text-slate-700 break-all">{previewURL}</span>
          </div>
        </>
      )}

      <label className="flex items-start gap-2 mb-4 cursor-pointer">
        <input
          type="checkbox"
          checked={draft.allow_network}
          onChange={(e) => setDraft({ ...draft, allow_network: e.target.checked })}
          className="mt-0.5"
        />
        <span className="text-sm text-slate-700">
          Allow outbound network access
          <span className="block text-xs text-slate-400">
            Off by default — the function is fully network-isolated. Enable only if it needs to call external
            services or a database.
          </span>
        </span>
      </label>
    </>
  )
}

function CreateModal({
  meta,
  draft,
  setDraft,
  onClose,
  onDone,
}: {
  meta: FunctionMeta
  draft: FnSettings
  setDraft: (d: FnSettings) => void
  onClose: () => void
  onDone: () => void
}) {
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (!draft.runtime && meta.runtimes.length) {
      const r = meta.runtimes[0]
      setDraft({ ...draft, runtime: r.runtime, version: r.versions[0] ?? '' })
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [meta])

  const create = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      await api.post('/api/functions', draft)
      toast('Function created')
      onDone()
    } catch (err) {
      toast((err as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  if (!meta.runtimes.length) {
    return (
      <Modal open title="Add Function" onClose={onClose}>
        <p className="text-sm text-slate-600">
          No supported runtimes (Python, Node or PHP) are installed on this server. Install one, then try again.
        </p>
        <div className="flex justify-end mt-4">
          <Btn variant="secondary" onClick={onClose}>
            Close
          </Btn>
        </div>
      </Modal>
    )
  }

  return (
    <Modal open title="Add Function" onClose={onClose}>
      <form onSubmit={create}>
        <SettingsFields draft={draft} setDraft={setDraft} meta={meta} />
        <div className="flex justify-end gap-2">
          <Btn type="button" variant="secondary" onClick={onClose}>
            Cancel
          </Btn>
          <Btn type="submit" disabled={busy}>
            Create
          </Btn>
        </div>
      </form>
    </Modal>
  )
}

function EditModal({
  fn,
  meta,
  onClose,
  onDone,
}: {
  fn: FunctionItem
  meta: FunctionMeta
  onClose: () => void
  onDone: () => void
}) {
  const { data, error, loading } = useFetch<FunctionItem>(`/api/functions/${fn.id}`)
  const [tab, setTab] = useState<'code' | 'settings' | 'test'>('code')
  const [settings, setSettings] = useState<FnSettings>({
    name: fn.name,
    runtime: fn.runtime,
    version: fn.version,
    trigger: fn.trigger,
    schedule: fn.schedule || DEFAULT_SCHEDULE,
    allow_network: fn.allow_network,
    base_domain: fn.base_domain,
  })
  const [code, setCode] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (!data) return
    if (data.code !== undefined) setCode(data.code)
    setSettings({
      name: data.name,
      runtime: data.runtime,
      version: data.version,
      trigger: data.trigger,
      schedule: data.schedule || DEFAULT_SCHEDULE,
      allow_network: data.allow_network,
      base_domain: data.base_domain,
    })
  }, [data])

  const save = async () => {
    setBusy(true)
    try {
      await api.put(`/api/functions/${fn.id}`, { ...settings, code })
      toast('Saved')
      onDone()
      onClose()
    } catch (err) {
      toast((err as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  const issueLE = async () => {
    setBusy(true)
    try {
      await api.post(`/api/functions/${fn.id}/ssl`)
      toast("Let's Encrypt certificate issued")
      onDone()
    } catch (err) {
      toast((err as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal open title={fn.name} onClose={onClose} wide>
      <ErrorBanner message={error} />
      <div className="mb-3 text-xs text-slate-500">
        {fn.trigger === 'schedule' ? (
          <span className="inline-flex items-center gap-1">
            <Clock size={12} /> scheduled
          </span>
        ) : (
          <a href={fn.url} target="_blank" rel="noreferrer" className="font-mono text-brand-600 hover:underline">
            {fn.hostname}
          </a>
        )}{' '}
        · {runtimeLabels[fn.runtime] ?? fn.runtime} {fn.version}
      </div>

      <div className="flex gap-1 border-b border-slate-200 mb-4 -mt-1">
        <TabBtn active={tab === 'code'} onClick={() => setTab('code')}>
          Code
        </TabBtn>
        <TabBtn active={tab === 'settings'} onClick={() => setTab('settings')}>
          Settings
        </TabBtn>
        <TabBtn active={tab === 'test'} onClick={() => setTab('test')}>
          Test
        </TabBtn>
      </div>

      {loading ? (
        <Spinner />
      ) : tab === 'test' ? (
        <TestTab fn={fn} />
      ) : (
        <>
          {tab === 'settings' ? (
            <SettingsFields draft={settings} setDraft={setSettings} meta={meta} slug={fn.slug} />
          ) : (
            <Field label="Handler code">
              <textarea
                value={code}
                onChange={(e) => setCode(e.target.value)}
                spellCheck={false}
                className="w-full h-72 rounded-md border border-slate-300 px-3 py-2 text-xs font-mono focus:outline-none focus:ring-2 focus:ring-brand-500/40 focus:border-brand-500 bg-white"
              />
            </Field>
          )}
          <div className="flex items-center justify-between gap-2">
            {fn.trigger === 'url' ? (
              <Btn type="button" variant="secondary" onClick={issueLE} disabled={busy}>
                <ShieldCheck size={15} /> Issue Let's Encrypt
              </Btn>
            ) : (
              <span />
            )}
            <div className="flex gap-2">
              <Btn type="button" variant="secondary" onClick={onClose}>
                Close
              </Btn>
              <Btn type="button" onClick={save} disabled={busy}>
                Save
              </Btn>
            </div>
          </div>
        </>
      )}
    </Modal>
  )
}

function TabBtn({ active, onClick, children }: { active: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`px-3 py-1.5 text-sm font-medium border-b-2 -mb-px cursor-pointer ${
        active ? 'border-brand-600 text-brand-700' : 'border-transparent text-slate-500 hover:text-slate-700'
      }`}
    >
      {children}
    </button>
  )
}

function TestTab({ fn }: { fn: FunctionItem }) {
  const [payload, setPayload] = useState('{\n  "key": "value"\n}')
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState<FunctionInvokeResult | null>(null)

  const run = async () => {
    setBusy(true)
    setResult(null)
    try {
      const res = await api.post<FunctionInvokeResult>(`/api/functions/${fn.id}/invoke`, { payload })
      setResult(res)
    } catch (err) {
      toast((err as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  let prettyResponse = result?.response ?? ''
  if (result?.response) {
    try {
      prettyResponse = JSON.stringify(JSON.parse(result.response), null, 2)
    } catch {
      prettyResponse = result.response
    }
  }

  return (
    <>
      <Field label="Event payload (JSON)" hint="Passed to your handler as the event argument.">
        <textarea
          value={payload}
          onChange={(e) => setPayload(e.target.value)}
          spellCheck={false}
          className="w-full h-40 rounded-md border border-slate-300 px-3 py-2 text-xs font-mono focus:outline-none focus:ring-2 focus:ring-brand-500/40 focus:border-brand-500 bg-white"
        />
      </Field>
      <div className="flex justify-end mb-4">
        <Btn type="button" onClick={run} disabled={busy}>
          <Play size={15} /> {busy ? 'Running…' : 'Test'}
        </Btn>
      </div>

      {result && (
        <div className="space-y-3">
          <div className="flex items-center gap-3 text-xs">
            {result.ok ? <Badge color="green">Succeeded</Badge> : <Badge color="red">Failed</Badge>}
            <span className="text-slate-500">Duration: {result.duration_ms} ms</span>
          </div>
          <div>
            <div className="text-xs font-medium text-slate-600 mb-1">Response</div>
            <pre className="max-h-48 overflow-auto rounded-md bg-slate-900 text-slate-100 text-xs font-mono p-3 whitespace-pre-wrap break-all">
              {prettyResponse || '(no response)'}
            </pre>
          </div>
          {result.logs && (
            <div>
              <div className="text-xs font-medium text-slate-600 mb-1">Logs</div>
              <pre className="max-h-40 overflow-auto rounded-md bg-slate-100 text-slate-700 text-xs font-mono p-3 whitespace-pre-wrap break-all">
                {result.logs}
              </pre>
            </div>
          )}
        </div>
      )}
    </>
  )
}
