import { useEffect, useState } from 'react'
import { ScrollText, RefreshCw } from 'lucide-react'
import { api, useFetch } from '../api'
import { Card, PageHeader, Spinner, ErrorBanner, Empty, Btn } from '../components/ui'

const LABELS: Record<string, string> = {
  'nginx-error': 'nginx — error log',
  'nginx-access': 'nginx — access log',
  'apache-error': 'Apache — error log',
  mail: 'Mail (Postfix/Dovecot)',
  fail2ban: 'Fail2ban',
  auth: 'Auth / SSH',
  syslog: 'System log',
}

export default function Logs() {
  const { data, loading } = useFetch<{ files: string[] }>('/api/logs')
  const [key, setKey] = useState('')
  const [content, setContent] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const files = data?.files ?? []
  useEffect(() => {
    if (!key && files.length) setKey(files[0])
  }, [files, key])

  const load = async (k: string) => {
    if (!k) return
    setBusy(true)
    setError('')
    try {
      const res = await api.get<{ content: string }>(`/api/logs/${k}`)
      setContent(res.content || '(empty)')
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  useEffect(() => {
    if (key) load(key)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key])

  if (loading) return <Spinner />

  return (
    <div>
      <PageHeader title="Logs" subtitle="Tail of the server's system and service logs." />
      {!files.length ? (
        <Card>
          <Empty title="No logs available" hint="None of the known log files were found on this host." />
        </Card>
      ) : (
        <Card
          actions={
            <Btn size="sm" variant="secondary" onClick={() => load(key)} disabled={busy}>
              <RefreshCw size={13} /> Refresh
            </Btn>
          }
          title={
            <div className="flex items-center gap-2">
              <ScrollText size={15} className="text-brand-600" />
              <select value={key} onChange={(e) => setKey(e.target.value)} className="border border-slate-200 rounded px-2 py-1 text-sm bg-white">
                {files.map((f) => (
                  <option key={f} value={f}>{LABELS[f] ?? f}</option>
                ))}
              </select>
            </div>
          }
        >
          <ErrorBanner message={error} />
          {busy ? (
            <Spinner />
          ) : (
            <pre className="max-h-[65vh] overflow-auto rounded-md bg-slate-900 text-slate-100 text-xs font-mono p-3 whitespace-pre-wrap">
              {content}
            </pre>
          )}
        </Card>
      )}
    </div>
  )
}
