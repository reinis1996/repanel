import { useEffect, useState, type ReactNode } from 'react'
import { X, AlertCircle, CheckCircle2, Inbox } from 'lucide-react'

// ---------- buttons ----------

const btnVariants = {
  primary: 'bg-brand-600 text-white hover:bg-brand-700 disabled:bg-brand-600/50',
  secondary: 'bg-white text-slate-700 border border-slate-300 hover:bg-slate-50',
  danger: 'bg-white text-red-600 border border-red-200 hover:bg-red-50',
  ghost: 'text-slate-600 hover:bg-slate-100',
} as const

export function Btn({
  variant = 'primary',
  size = 'md',
  className = '',
  ...props
}: React.ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: keyof typeof btnVariants
  size?: 'sm' | 'md'
}) {
  const sizeCls = size === 'sm' ? 'px-2.5 py-1 text-xs' : 'px-3.5 py-2 text-sm'
  return (
    <button
      className={`inline-flex items-center gap-1.5 rounded-md font-medium transition-colors cursor-pointer disabled:cursor-not-allowed ${sizeCls} ${btnVariants[variant]} ${className}`}
      {...props}
    />
  )
}

// ---------- layout primitives ----------

export function Card({
  title,
  actions,
  children,
  className = '',
}: {
  title?: ReactNode
  actions?: ReactNode
  children: ReactNode
  className?: string
}) {
  return (
    <div className={`bg-white rounded-lg border border-slate-200 shadow-sm overflow-hidden ${className}`}>
      {(title || actions) && (
        <div className="flex items-center justify-between px-5 py-3.5 border-b border-slate-100">
          <h2 className="text-sm font-semibold text-slate-700">{title}</h2>
          <div className="flex items-center gap-2">{actions}</div>
        </div>
      )}
      <div className="p-5">{children}</div>
    </div>
  )
}

export function PageHeader({ title, subtitle, actions }: { title: string; subtitle?: string; actions?: ReactNode }) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-3 mb-5">
      <div>
        <h1 className="text-xl font-semibold text-slate-800">{title}</h1>
        {subtitle && <p className="text-sm text-slate-500 mt-0.5">{subtitle}</p>}
      </div>
      <div className="flex items-center gap-2">{actions}</div>
    </div>
  )
}

export function Badge({ color, children }: { color: 'green' | 'gray' | 'red' | 'blue' | 'amber'; children: ReactNode }) {
  const colors = {
    green: 'bg-emerald-50 text-emerald-700 ring-emerald-600/20',
    gray: 'bg-slate-50 text-slate-600 ring-slate-500/20',
    red: 'bg-red-50 text-red-700 ring-red-600/20',
    blue: 'bg-blue-50 text-blue-700 ring-blue-600/20',
    amber: 'bg-amber-50 text-amber-700 ring-amber-600/20',
  }
  return (
    <span className={`inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium ring-1 ring-inset ${colors[color]}`}>
      {children}
    </span>
  )
}

// ---------- forms ----------

export function Field({ label, children, hint }: { label: string; children: ReactNode; hint?: string }) {
  return (
    <label className="block mb-4">
      <span className="block text-sm font-medium text-slate-700 mb-1">{label}</span>
      {children}
      {hint && <span className="block text-xs text-slate-400 mt-1">{hint}</span>}
    </label>
  )
}

export const inputCls =
  'w-full rounded-md border border-slate-300 px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-brand-500/40 focus:border-brand-500 bg-white'

export function Input(props: React.InputHTMLAttributes<HTMLInputElement>) {
  return <input className={inputCls} {...props} />
}

export function Select(props: React.SelectHTMLAttributes<HTMLSelectElement>) {
  return <select className={inputCls} {...props} />
}

// ---------- modal ----------

export function Modal({
  open,
  title,
  onClose,
  children,
  wide,
}: {
  open: boolean
  title: string
  onClose: () => void
  children: ReactNode
  wide?: boolean
}) {
  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [open, onClose])

  if (!open) return null
  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center pt-[10vh] bg-slate-900/40" onMouseDown={onClose}>
      <div
        className={`bg-white rounded-lg shadow-xl w-full ${wide ? 'max-w-3xl' : 'max-w-md'} mx-4`}
        onMouseDown={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between px-5 py-3.5 border-b border-slate-100">
          <h3 className="text-sm font-semibold text-slate-800">{title}</h3>
          <button onClick={onClose} className="text-slate-400 hover:text-slate-600 cursor-pointer">
            <X size={18} />
          </button>
        </div>
        <div className="p-5">{children}</div>
      </div>
    </div>
  )
}

// ---------- table ----------

export function Table({ head, children }: { head: string[]; children: ReactNode }) {
  return (
    <div className="overflow-x-auto -mx-5 -mt-5 -mb-5">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-slate-200 bg-slate-50/60">
            {head.map((h) => (
              <th key={h} className="text-left font-medium text-slate-500 px-5 py-2.5 text-xs uppercase tracking-wide">
                {h}
              </th>
            ))}
          </tr>
        </thead>
        <tbody className="divide-y divide-slate-100">{children}</tbody>
      </table>
    </div>
  )
}

export function Td({ children, className = '' }: { children: ReactNode; className?: string }) {
  return <td className={`px-5 py-3 ${className}`}>{children}</td>
}

export function Empty({ title, hint }: { title: string; hint?: string }) {
  return (
    <div className="text-center py-10 text-slate-400">
      <Inbox className="mx-auto mb-2" size={32} strokeWidth={1.5} />
      <p className="text-sm font-medium text-slate-500">{title}</p>
      {hint && <p className="text-xs mt-1">{hint}</p>}
    </div>
  )
}

// ---------- alerts / toasts ----------

export function ErrorBanner({ message }: { message: string }) {
  if (!message) return null
  return (
    <div className="flex items-start gap-2 rounded-md bg-red-50 border border-red-200 text-red-700 text-sm px-4 py-3 mb-4">
      <AlertCircle size={16} className="mt-0.5 shrink-0" />
      <span className="break-all">{message}</span>
    </div>
  )
}

type Toast = { id: number; kind: 'ok' | 'err'; text: string }
let pushToast: (t: Omit<Toast, 'id'>) => void = () => {}

export function toast(text: string, kind: 'ok' | 'err' = 'ok') {
  pushToast({ text, kind })
}

export function Toaster() {
  const [items, setItems] = useState<Toast[]>([])
  useEffect(() => {
    let n = 0
    pushToast = (t) => {
      const id = ++n
      setItems((prev) => [...prev, { ...t, id }])
      setTimeout(() => setItems((prev) => prev.filter((x) => x.id !== id)), 4000)
    }
    return () => {
      pushToast = () => {}
    }
  }, [])
  return (
    <div className="fixed bottom-4 right-4 z-[60] space-y-2">
      {items.map((t) => (
        <div
          key={t.id}
          className={`flex items-center gap-2 rounded-md px-4 py-2.5 text-sm shadow-lg text-white ${
            t.kind === 'ok' ? 'bg-emerald-600' : 'bg-red-600'
          }`}
        >
          {t.kind === 'ok' ? <CheckCircle2 size={16} /> : <AlertCircle size={16} />}
          <span className="max-w-sm break-words">{t.text}</span>
        </div>
      ))}
    </div>
  )
}

// ---------- misc ----------

export function Spinner() {
  return (
    <div className="flex justify-center py-10">
      <div className="h-6 w-6 rounded-full border-2 border-slate-300 border-t-brand-600 animate-spin" />
    </div>
  )
}

export function Meter({ label, used, total, unit }: { label: string; used: number; total: number; unit: string }) {
  const pct = total > 0 ? Math.min(100, (used / total) * 100) : 0
  const color = pct > 90 ? 'bg-red-500' : pct > 70 ? 'bg-amber-500' : 'bg-brand-500'
  return (
    <div>
      <div className="flex justify-between text-xs text-slate-500 mb-1">
        <span>{label}</span>
        <span>
          {used.toFixed(total >= 100 ? 0 : 1)} / {total.toFixed(total >= 100 ? 0 : 1)} {unit}
        </span>
      </div>
      <div className="h-2 rounded-full bg-slate-100 overflow-hidden">
        <div className={`h-full rounded-full ${color}`} style={{ width: `${pct}%` }} />
      </div>
    </div>
  )
}
