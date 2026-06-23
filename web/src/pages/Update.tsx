import { useEffect, useState } from 'react'
import { RefreshCw, DownloadCloud, Save } from 'lucide-react'
import { api } from '../api'
import type { UpdateStatus } from '../types'
import { Btn, Card, PageHeader, Field, Input, Spinner, Badge, toast } from '../components/ui'

export default function Update() {
  const [data, setData] = useState<UpdateStatus | null>(null)
  const [repo, setRepo] = useState('')
  const [token, setToken] = useState('')
  const [busy, setBusy] = useState(false)
  const [updating, setUpdating] = useState(false)

  const load = (refresh?: boolean) =>
    api
      .get<UpdateStatus>(`/api/update${refresh ? '?refresh=1' : ''}`)
      .then((d) => {
        setData(d)
        setRepo(d.repo)
      })
      .catch((e) => toast((e as Error).message, 'err'))

  useEffect(() => {
    load()
  }, [])

  const saveConfig = async () => {
    setBusy(true)
    try {
      await api.post('/api/update/config', { repo, token })
      setToken('')
      toast('Saved')
      await load(true)
    } catch (e) {
      toast((e as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  const check = async () => {
    setBusy(true)
    await load(true)
    setBusy(false)
    toast('Checked for updates')
  }

  const update = async () => {
    if (!confirm('Download the latest version and restart the panel now?')) return
    setUpdating(true)
    const oldVersion = data?.current
    try {
      await api.post('/api/update')
      toast('Updating — the panel will restart shortly')
      waitForRestart(oldVersion)
    } catch (e) {
      toast((e as Error).message, 'err')
      setUpdating(false)
    }
  }

  // After the binary is swapped the service restarts (briefly unreachable). Poll
  // until the panel answers again with a *different* version, then reload to pick
  // up the new UI. A plain fetch avoids the global 401 handler on transient
  // failures during the restart; we give up after a couple of minutes so a stuck
  // update doesn't spin forever.
  const waitForRestart = (oldVersion?: string) => {
    const start = Date.now()
    const tick = async () => {
      try {
        const res = await fetch('/api/update', { credentials: 'same-origin', cache: 'no-store' })
        if (res.ok) {
          const s = (await res.json()) as UpdateStatus
          if (s.current && s.current !== oldVersion) {
            window.location.reload()
            return
          }
        }
      } catch {
        // panel still restarting — keep polling
      }
      if (Date.now() - start < 120000) {
        setTimeout(tick, 2500)
      } else {
        setUpdating(false) // give up; the user can reload manually
      }
    }
    setTimeout(tick, 4000) // let the restart begin before probing
  }

  if (!data) return <Spinner />

  return (
    <div>
      <PageHeader
        title="Update"
        subtitle="Update the RePanel and CLI binaries to the latest release"
        actions={
          <Btn variant="secondary" onClick={check} disabled={busy}>
            <RefreshCw size={15} /> Check
          </Btn>
        }
      />

      <Card className="max-w-2xl mb-4">
        <div className="flex items-center justify-between">
          <div className="text-sm">
            <div>
              Current version: <span className="font-medium text-slate-700">{data.current}</span>
            </div>
            <div className="text-slate-500">
              Latest: {data.latest || (data.has_token ? '—' : 'set a GitHub token below to check')}
            </div>
            {!data.latest && data.error && (
              <div className="text-xs text-red-600 mt-1 max-w-md">{data.error}</div>
            )}
          </div>
          {data.available ? (
            <Badge color="amber">update available</Badge>
          ) : data.latest ? (
            <Badge color="green">up to date</Badge>
          ) : null}
        </div>
        {data.available && (
          <div className="mt-4">
            <Btn onClick={update} disabled={updating}>
              <DownloadCloud size={15} /> {updating ? 'Updating…' : `Update to ${data.latest}`}
            </Btn>
            {updating && (
              <p className="text-xs text-slate-500 mt-2">
                Downloading and restarting — this page will reload automatically when the panel is back.
              </p>
            )}
          </div>
        )}
      </Card>

      <Card title="Source" className="max-w-2xl">
        <Field label="GitHub repository" hint="owner/repo">
          <Input value={repo} onChange={(e) => setRepo(e.target.value)} placeholder="reinis1996/repanel" />
        </Field>
        <Field
          label="GitHub token"
          hint={data.has_token ? 'A token is set — leave blank to keep it' : 'Required for a private repository (Contents: read)'}
        >
          <Input
            type="password"
            value={token}
            onChange={(e) => setToken(e.target.value)}
            placeholder={data.has_token ? '••••••••' : 'ghp_…'}
          />
        </Field>
        <div className="flex justify-end">
          <Btn onClick={saveConfig} disabled={busy}>
            <Save size={15} /> Save
          </Btn>
        </div>
      </Card>
    </div>
  )
}
