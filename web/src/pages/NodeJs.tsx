import { useEffect } from 'react'
import { Download, Loader2 } from 'lucide-react'
import { api, useFetch } from '../api'
import type { NodeVersionInfo } from '../types'
import { Btn, Card, PageHeader, Table, Td, Spinner, ErrorBanner, Badge, toast } from '../components/ui'

export default function NodeJs() {
  const { data, error, loading, reload } = useFetch<NodeVersionInfo[]>('/api/node')
  const installing = (data ?? []).some((v) => v.installing)

  // Poll while a download/install is in progress.
  useEffect(() => {
    if (!installing) return
    const t = setInterval(reload, 4000)
    return () => clearInterval(t)
  }, [installing, reload])

  const install = async (v: string) => {
    try {
      await api.post('/api/node/install', { version: v })
      toast(`Installing Node ${v}…`)
      reload()
    } catch (e) {
      toast((e as Error).message, 'err')
    }
  }

  if (loading) return <Spinner />

  return (
    <div>
      <PageHeader
        title="Node.js"
        subtitle="Install Node.js versions for Node web apps — official binaries under /opt/repanel/node"
      />
      <ErrorBanner message={error} />
      <Card>
        <Table head={['Version', 'Status', '']}>
          {(data ?? []).map((v) => (
            <tr key={v.version} className="hover:bg-slate-50/60">
              <Td className="font-medium">Node {v.version}</Td>
              <Td>
                {v.installed ? (
                  <Badge color="green">installed</Badge>
                ) : v.installing ? (
                  <span className="inline-flex items-center gap-1.5 text-blue-600 text-sm">
                    <Loader2 size={14} className="animate-spin" /> installing…
                  </span>
                ) : v.error ? (
                  <span title={v.error}>
                    <Badge color="red">failed</Badge>
                  </span>
                ) : (
                  <Badge color="gray">not installed</Badge>
                )}
              </Td>
              <Td className="text-right">
                {!v.installed && !v.installing && (
                  <Btn size="sm" variant="secondary" onClick={() => install(v.version)}>
                    <Download size={13} /> Install
                  </Btn>
                )}
              </Td>
            </tr>
          ))}
        </Table>
      </Card>
      <p className="text-xs text-slate-400 mt-3">
        Installed versions appear in the per-domain <strong>Runtime</strong> selector on Websites &amp; Domains.
      </p>
    </div>
  )
}
