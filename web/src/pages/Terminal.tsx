import { useEffect, useRef, useState } from 'react'
import { Terminal as XTerm } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import { PageHeader, Btn } from '../components/ui'

type Status = 'connecting' | 'open' | 'closed'

export default function Terminal() {
  const containerRef = useRef<HTMLDivElement>(null)
  const [status, setStatus] = useState<Status>('connecting')
  const [attempt, setAttempt] = useState(0) // bump to reconnect

  useEffect(() => {
    const el = containerRef.current
    if (!el) return

    setStatus('connecting')
    const term = new XTerm({
      cursorBlink: true,
      fontSize: 13,
      fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace',
      theme: { background: '#0f172a', foreground: '#e2e8f0', cursor: '#e2e8f0' },
      scrollback: 5000,
    })
    const fit = new FitAddon()
    term.loadAddon(fit)
    term.open(el)
    // Fit after layout settles so cols/rows match the container.
    requestAnimationFrame(() => {
      try {
        fit.fit()
      } catch {
        /* container not measurable yet */
      }
    })

    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    const ws = new WebSocket(`${proto}://${location.host}/api/terminal`)
    ws.binaryType = 'arraybuffer'

    const sendResize = () => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send('1' + JSON.stringify({ cols: term.cols, rows: term.rows }))
      }
    }

    ws.onopen = () => {
      setStatus('open')
      term.focus()
      sendResize()
    }
    ws.onmessage = (e) => {
      if (e.data instanceof ArrayBuffer) term.write(new Uint8Array(e.data))
      else term.write(e.data as string)
    }
    ws.onclose = () => {
      setStatus('closed')
      term.write('\r\n\x1b[33m[session closed]\x1b[0m\r\n')
    }
    ws.onerror = () => setStatus('closed')

    const dataSub = term.onData((d) => {
      if (ws.readyState === WebSocket.OPEN) ws.send('0' + d)
    })

    const onResize = () => {
      try {
        fit.fit()
        sendResize()
      } catch {
        /* ignore */
      }
    }
    window.addEventListener('resize', onResize)
    const ro = new ResizeObserver(onResize)
    ro.observe(el)

    return () => {
      window.removeEventListener('resize', onResize)
      ro.disconnect()
      dataSub.dispose()
      try {
        ws.close()
      } catch {
        /* already closed */
      }
      term.dispose()
    }
  }, [attempt])

  return (
    <div>
      <PageHeader
        title="Terminal"
        subtitle="Root shell on this server — actions here run as root. Use with care."
      />
      <div className="rounded-lg overflow-hidden border border-slate-700 shadow-sm">
        <div className="flex items-center justify-between px-3 py-1.5 bg-slate-800 text-xs text-slate-300">
          <span className="inline-flex items-center gap-1.5">
            <span
              className={
                status === 'open'
                  ? 'text-emerald-400'
                  : status === 'connecting'
                    ? 'text-amber-400'
                    : 'text-red-400'
              }
            >
              ●
            </span>
            {status === 'open' ? 'connected' : status === 'connecting' ? 'connecting…' : 'disconnected'}
          </span>
          {status === 'closed' && (
            <Btn size="sm" variant="secondary" onClick={() => setAttempt((a) => a + 1)}>
              Reconnect
            </Btn>
          )}
        </div>
        <div ref={containerRef} className="h-[70vh] bg-[#0f172a] p-2" />
      </div>
    </div>
  )
}
