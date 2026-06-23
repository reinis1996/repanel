import { createContext, useContext, useEffect, useState, type ReactNode } from 'react'
import { BrowserRouter, Routes, Route, NavLink, Navigate, useNavigate } from 'react-router-dom'
import {
  LayoutDashboard, Globe, Network, Mail, Database, FolderOpen, Server,
  ShieldCheck, Clock, Users as UsersIcon, Activity, Shield, Settings as SettingsIcon,
  LogOut, KeyRound, ChevronDown, Loader2, Archive, BarChart3, Key, Zap, Lock, Wrench, Hexagon, DownloadCloud,
  ScrollText, ShieldX, Smartphone, UserCheck, FileText, Package, PackageCheck, SquareTerminal,
} from 'lucide-react'
import QRCode from 'qrcode'
import { api } from './api'
import type { User, Branding } from './types'
import { Toaster, toast, Modal, Field, Input, Btn } from './components/ui'
import Login from './pages/Login'
import Setup from './pages/Setup'
import Dashboard from './pages/Dashboard'
import Domains from './pages/Domains'
import WordPress from './pages/WordPress'
import DNS from './pages/DNS'
import MailPage from './pages/Mail'
import Databases from './pages/Databases'
import Files from './pages/Files'
import Ftp from './pages/Ftp'
import Ssl from './pages/Ssl'
import Cron from './pages/Cron'
import Functions from './pages/Functions'
import Backups from './pages/Backups'
import Traffic from './pages/Traffic'
import ApiTokens from './pages/ApiTokens'
import UsersPage from './pages/Users'
import Services from './pages/Services'
import Firewall from './pages/Firewall'
import Settings from './pages/Settings'
import Permissions from './pages/Permissions'
import NodeJs from './pages/NodeJs'
import Update from './pages/Update'
import AuditLog from './pages/AuditLog'
import Fail2ban from './pages/Fail2ban'
import Logs from './pages/Logs'
import Plans from './pages/Plans'
import Packages from './pages/Packages'
import Terminal from './pages/Terminal'

// ---------- auth context ----------

interface AuthCtx {
  user: User | null
  setUser: (u: User | null) => void
}
const Ctx = createContext<AuthCtx>({ user: null, setUser: () => {} })
export const useAuth = () => useContext(Ctx)

// ---------- branding (white-label) ----------

const defaultBrand: Branding = { name: 'RePanel', color: '', logo: '' }
const BrandCtx = createContext<Branding>(defaultBrand)
export const useBrand = () => useContext(BrandCtx)

// shade lightens (pct>0) or darkens (pct<0) a hex color, for deriving the
// accent's hover/active shades from a single configured color.
function shade(hex: string, pct: number): string {
  let h = hex.replace('#', '')
  if (h.length === 3) h = h.split('').map((c) => c + c).join('')
  const num = parseInt(h, 16)
  const amt = Math.round(2.55 * pct)
  const clamp = (v: number) => Math.max(0, Math.min(255, v))
  const r = clamp((num >> 16) + amt)
  const g = clamp(((num >> 8) & 0xff) + amt)
  const b = clamp((num & 0xff) + amt)
  return '#' + ((1 << 24) + (r << 16) + (g << 8) + b).toString(16).slice(1)
}

// applyBrandColor overrides the Tailwind brand CSS variables at runtime (or
// restores the defaults when no custom color is set).
function applyBrandColor(color: string) {
  const root = document.documentElement
  const vars: [string, number][] = [['--color-brand-500', 12], ['--color-brand-600', 0], ['--color-brand-700', -12]]
  for (const [name, pct] of vars) {
    if (color) root.style.setProperty(name, pct === 0 ? color : shade(color, pct))
    else root.style.removeProperty(name)
  }
}

export default function App() {
  const [user, setUser] = useState<User | null>(null)
  const [needsSetup, setNeedsSetup] = useState(false)
  const [booted, setBooted] = useState(false)
  const [brand, setBrand] = useState<Branding>(defaultBrand)

  useEffect(() => {
    Promise.all([
      api.get<{ needs_setup: boolean }>('/api/setup').catch(() => ({ needs_setup: false })),
      api.get<User>('/api/me').catch(() => null),
      api.get<Branding>('/api/branding').catch(() => defaultBrand),
    ]).then(([setup, me, b]) => {
      setNeedsSetup(setup.needs_setup)
      setUser(me)
      setBrand(b)
      document.title = b.name || 'RePanel'
      applyBrandColor(b.color)
      setBooted(true)
    })
  }, [])

  useEffect(() => {
    const onUnauthorized = () => setUser(null)
    window.addEventListener('repanel:unauthorized', onUnauthorized)
    return () => window.removeEventListener('repanel:unauthorized', onUnauthorized)
  }, [])

  if (!booted) {
    return (
      <div className="h-full flex items-center justify-center">
        <Loader2 className="animate-spin text-brand-600" size={32} />
      </div>
    )
  }

  return (
    <BrandCtx.Provider value={brand}>
      <Ctx.Provider value={{ user, setUser }}>
        <BrowserRouter>
          <Toaster />
          {needsSetup && !user ? (
            <Setup onDone={() => setNeedsSetup(false)} />
          ) : !user ? (
            <Login />
          ) : (
            <Shell />
          )}
        </BrowserRouter>
      </Ctx.Provider>
    </BrandCtx.Provider>
  )
}

// ---------- application shell ----------

// `module` gates a nav item against the user's permissions; items without one
// (Dashboard, API Tokens) are always shown.
const nav = [
  { to: '/', label: 'Dashboard', icon: LayoutDashboard },
  { to: '/domains', label: 'Websites & Domains', icon: Globe, module: 'domains' },
  { to: '/wordpress', label: 'WordPress', icon: Wrench, module: 'domains' },
  { to: '/dns', label: 'DNS', icon: Network, module: 'dns' },
  { to: '/mail', label: 'Mail', icon: Mail, module: 'mail' },
  { to: '/databases', label: 'Databases', icon: Database, module: 'databases' },
  { to: '/files', label: 'File Manager', icon: FolderOpen, module: 'files' },
  { to: '/ftp', label: 'FTP', icon: Server, module: 'ftp' },
  { to: '/ssl', label: 'SSL/TLS', icon: ShieldCheck, module: 'ssl' },
  { to: '/cron', label: 'Scheduled Tasks', icon: Clock, module: 'cron' },
  { to: '/functions', label: 'Functions', icon: Zap, module: 'functions' },
  { to: '/backups', label: 'Backups', icon: Archive, module: 'backups' },
  { to: '/traffic', label: 'Traffic', icon: BarChart3, module: 'traffic' },
  { to: '/tokens', label: 'API Tokens', icon: Key },
]

const adminNav = [
  { to: '/users', label: 'Users', icon: UsersIcon, reseller: true },
  { to: '/plans', label: 'Hosting Plans', icon: Package, reseller: false },
  { to: '/permissions', label: 'Permissions', icon: Lock, reseller: false },
  { to: '/nodejs', label: 'Node.js', icon: Hexagon, reseller: false },
  { to: '/services', label: 'Services', icon: Activity, reseller: false },
  { to: '/terminal', label: 'Terminal', icon: SquareTerminal, reseller: false },
  { to: '/packages', label: 'Package Updates', icon: PackageCheck, reseller: false },
  { to: '/firewall', label: 'Firewall', icon: Shield, reseller: false },
  { to: '/fail2ban', label: 'Fail2ban', icon: ShieldX, reseller: false },
  { to: '/logs', label: 'Logs', icon: FileText, reseller: false },
  { to: '/audit', label: 'Audit Log', icon: ScrollText, reseller: false },
  { to: '/settings', label: 'Settings', icon: SettingsIcon, reseller: false },
  { to: '/update', label: 'Update', icon: DownloadCloud, reseller: false },
]

function Shell() {
  const { user } = useAuth()
  const brand = useBrand()
  const isAdmin = user!.role === 'admin'
  const isReseller = user!.role === 'reseller'
  const perms = user!.permissions ?? []
  const can = (m?: string) => !m || perms.includes(m)

  return (
    <div className="flex h-full">
      <aside className="w-60 shrink-0 bg-side-900 text-slate-300 flex flex-col">
        <div className="px-5 py-4 border-b border-white/10 flex items-center gap-2">
          {brand.logo && <img src={brand.logo} alt="" className="h-7 max-w-[140px] object-contain" />}
          {!brand.logo && (
            <span className="text-lg font-bold text-white tracking-tight">
              {brand.name === 'RePanel' ? (
                <>Re<span className="text-brand-500">Panel</span></>
              ) : (
                brand.name
              )}
            </span>
          )}
        </div>
        <nav className="flex-1 overflow-y-auto py-3 space-y-0.5">
          {nav.filter((item) => can(item.module)).map((item) => (
            <SideLink key={item.to} {...item} />
          ))}
          {(isAdmin || isReseller) && (
            <>
              <div className="px-5 pt-4 pb-1 text-[10px] font-semibold uppercase tracking-wider text-slate-500">
                Administration
              </div>
              {adminNav
                .filter((i) => isAdmin || (isReseller && i.reseller))
                .map((item) => (
                  <SideLink key={item.to} {...item} />
                ))}
            </>
          )}
        </nav>
        <div className="px-5 py-3 text-[11px] text-slate-500 border-t border-white/10">
          {brand.name === 'RePanel' ? 'RePanel — open source hosting panel' : brand.name}
        </div>
      </aside>

      <div className="flex-1 flex flex-col min-w-0">
        <ImpersonationBanner />
        <TopBar />
        <main className="flex-1 overflow-y-auto p-6">
          <Routes>
            <Route path="/" element={<Dashboard />} />
            {can('domains') && <Route path="/domains" element={<Domains />} />}
            {can('domains') && <Route path="/wordpress" element={<WordPress />} />}
            {can('dns') && <Route path="/dns" element={<DNS />} />}
            {can('mail') && <Route path="/mail" element={<MailPage />} />}
            {can('databases') && <Route path="/databases" element={<Databases />} />}
            {can('files') && <Route path="/files" element={<Files />} />}
            {can('ftp') && <Route path="/ftp" element={<Ftp />} />}
            {can('ssl') && <Route path="/ssl" element={<Ssl />} />}
            {can('cron') && <Route path="/cron" element={<Cron />} />}
            {can('functions') && <Route path="/functions" element={<Functions />} />}
            {can('backups') && <Route path="/backups" element={<Backups />} />}
            {can('traffic') && <Route path="/traffic" element={<Traffic />} />}
            <Route path="/tokens" element={<ApiTokens />} />
            {(isAdmin || isReseller) && <Route path="/users" element={<UsersPage />} />}
            {isAdmin && (
              <>
                <Route path="/plans" element={<Plans />} />
                <Route path="/permissions" element={<Permissions />} />
                <Route path="/nodejs" element={<NodeJs />} />
                <Route path="/services" element={<Services />} />
                <Route path="/terminal" element={<Terminal />} />
                <Route path="/packages" element={<Packages />} />
                <Route path="/firewall" element={<Firewall />} />
                <Route path="/fail2ban" element={<Fail2ban />} />
                <Route path="/logs" element={<Logs />} />
                <Route path="/audit" element={<AuditLog />} />
                <Route path="/settings" element={<Settings />} />
                <Route path="/update" element={<Update />} />
              </>
            )}
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        </main>
      </div>
    </div>
  )
}

function SideLink({ to, label, icon: Icon }: { to: string; label: string; icon: typeof Globe }) {
  return (
    <NavLink
      to={to}
      end={to === '/'}
      className={({ isActive }) =>
        `flex items-center gap-3 mx-2 px-3 py-2 rounded-md text-[13px] font-medium transition-colors ${
          isActive ? 'bg-brand-600 text-white' : 'hover:bg-side-700 hover:text-white'
        }`
      }
    >
      <Icon size={16} strokeWidth={1.8} />
      {label}
    </NavLink>
  )
}

function TopBar() {
  const { user, setUser } = useAuth()
  const navigate = useNavigate()
  const [menuOpen, setMenuOpen] = useState(false)
  const [pwOpen, setPwOpen] = useState(false)
  const [twoFAOpen, setTwoFAOpen] = useState(false)
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [busy, setBusy] = useState(false)

  const logout = async () => {
    await api.post('/api/logout').catch(() => {})
    setUser(null)
    navigate('/')
  }

  const changePassword = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      await api.post('/api/me/password', { current, new: next })
      toast('Password changed')
      setPwOpen(false)
      setCurrent('')
      setNext('')
    } catch (err) {
      toast((err as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  return (
    <header className="h-14 bg-white border-b border-slate-200 flex items-center justify-end px-6 gap-4 shrink-0">
      <div className="relative">
        <button
          onClick={() => setMenuOpen((v) => !v)}
          className="flex items-center gap-2 text-sm text-slate-700 hover:text-slate-900 cursor-pointer"
        >
          <span className="h-7 w-7 rounded-full bg-brand-600 text-white flex items-center justify-center text-xs font-bold uppercase">
            {user!.username.slice(0, 2)}
          </span>
          <span className="font-medium">{user!.username}</span>
          <span className="text-[10px] uppercase tracking-wide bg-slate-100 text-slate-500 rounded px-1.5 py-0.5">
            {user!.role}
          </span>
          <ChevronDown size={14} className="text-slate-400" />
        </button>
        {menuOpen && (
          <div
            className="absolute right-0 mt-2 w-48 bg-white border border-slate-200 rounded-md shadow-lg py-1 z-40"
            onMouseLeave={() => setMenuOpen(false)}
          >
            <button
              onClick={() => {
                setMenuOpen(false)
                setPwOpen(true)
              }}
              className="w-full flex items-center gap-2 px-3 py-2 text-sm text-slate-700 hover:bg-slate-50 cursor-pointer"
            >
              <KeyRound size={14} /> Change password
            </button>
            <button
              onClick={() => {
                setMenuOpen(false)
                setTwoFAOpen(true)
              }}
              className="w-full flex items-center gap-2 px-3 py-2 text-sm text-slate-700 hover:bg-slate-50 cursor-pointer"
            >
              <Smartphone size={14} /> Two-factor auth
              {user!.totp_enabled && <span className="ml-auto text-[10px] text-emerald-600 font-semibold">ON</span>}
            </button>
            <button
              onClick={logout}
              className="w-full flex items-center gap-2 px-3 py-2 text-sm text-red-600 hover:bg-red-50 cursor-pointer"
            >
              <LogOut size={14} /> Log out
            </button>
          </div>
        )}
      </div>

      <Modal open={pwOpen} title="Change password" onClose={() => setPwOpen(false)}>
        <form onSubmit={changePassword}>
          <Field label="Current password">
            <Input type="password" value={current} onChange={(e) => setCurrent(e.target.value)} required />
          </Field>
          <Field label="New password" hint="At least 8 characters">
            <Input type="password" value={next} onChange={(e) => setNext(e.target.value)} required minLength={8} />
          </Field>
          <div className="flex justify-end gap-2 mt-2">
            <Btn type="button" variant="secondary" onClick={() => setPwOpen(false)}>
              Cancel
            </Btn>
            <Btn type="submit" disabled={busy}>
              Save
            </Btn>
          </div>
        </form>
      </Modal>

      {twoFAOpen && <TwoFactorModal onClose={() => setTwoFAOpen(false)} />}
    </header>
  )
}

// ImpersonationBanner shows a strip while an admin is impersonating an account.
function ImpersonationBanner() {
  const { user, setUser } = useAuth()
  if (!user?.impersonator) return null
  const stop = async () => {
    try {
      await api.post('/api/impersonate/stop')
      const me = await api.get<User>('/api/me')
      setUser(me)
    } catch (e) {
      toast((e as Error).message, 'err')
    }
  }
  return (
    <div className="bg-amber-500 text-white text-sm px-6 py-2 flex items-center justify-between">
      <span className="flex items-center gap-2">
        <UserCheck size={15} />
        Viewing as <strong>{user.username}</strong> (impersonated by {user.impersonator})
      </span>
      <button onClick={stop} className="font-medium underline hover:no-underline cursor-pointer">
        Stop impersonating
      </button>
    </div>
  )
}

// TwoFactorModal manages the current account's TOTP enrollment.
function TwoFactorModal({ onClose }: { onClose: () => void }) {
  const { user, setUser } = useAuth()
  const enabled = !!user?.totp_enabled
  const [secret, setSecret] = useState('')
  const [qr, setQr] = useState('')
  const [code, setCode] = useState('')
  const [password, setPassword] = useState('')
  const [recovery, setRecovery] = useState<string[]>([])
  const [busy, setBusy] = useState(false)

  const refreshUser = async () => {
    const me = await api.get<User>('/api/me')
    setUser(me)
  }

  const startSetup = async () => {
    setBusy(true)
    try {
      const res = await api.post<{ secret: string; uri: string }>('/api/me/2fa/setup')
      setSecret(res.secret)
      setQr(await QRCode.toDataURL(res.uri))
    } catch (e) {
      toast((e as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  const enable = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      const res = await api.post<{ recovery_codes: string[] }>('/api/me/2fa/enable', { code })
      setRecovery(res.recovery_codes)
      await refreshUser()
    } catch (err) {
      toast((err as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  const disable = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      await api.post('/api/me/2fa/disable', { password })
      toast('Two-factor authentication disabled')
      await refreshUser()
      onClose()
    } catch (err) {
      toast((err as Error).message, 'err')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal open title="Two-factor authentication" onClose={onClose}>
      {recovery.length > 0 ? (
        <div>
          <p className="text-sm text-slate-700 mb-2">
            Two-factor authentication is now enabled. Save these one-time recovery codes somewhere safe — each works once
            if you lose your authenticator.
          </p>
          <div className="grid grid-cols-2 gap-1.5 font-mono text-sm bg-slate-50 border border-slate-200 rounded-md p-3 mb-3">
            {recovery.map((c) => <span key={c}>{c}</span>)}
          </div>
          <div className="flex justify-end">
            <Btn onClick={onClose}>Done</Btn>
          </div>
        </div>
      ) : enabled ? (
        <form onSubmit={disable}>
          <p className="text-sm text-slate-700 mb-3">
            Two-factor authentication is <span className="text-emerald-600 font-medium">enabled</span> for your account.
            Enter your password to turn it off.
          </p>
          <Field label="Password">
            <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required />
          </Field>
          <div className="flex justify-end gap-2 mt-2">
            <Btn type="button" variant="secondary" onClick={onClose}>Cancel</Btn>
            <Btn type="submit" variant="danger" disabled={busy}>Disable 2FA</Btn>
          </div>
        </form>
      ) : !secret ? (
        <div>
          <p className="text-sm text-slate-600 mb-4">
            Protect your account with a time-based code from an authenticator app (Google Authenticator, 1Password, Authy…).
          </p>
          <div className="flex justify-end">
            <Btn onClick={startSetup} disabled={busy}><Smartphone size={15} /> Set up 2FA</Btn>
          </div>
        </div>
      ) : (
        <form onSubmit={enable}>
          <p className="text-sm text-slate-600 mb-3">Scan this QR code with your authenticator app, then enter the 6-digit code to confirm.</p>
          {qr && <img src={qr} alt="2FA QR code" className="mx-auto mb-3 w-44 h-44" />}
          <p className="text-xs text-slate-400 text-center mb-3">
            Or enter this key manually: <span className="font-mono text-slate-600 break-all">{secret}</span>
          </p>
          <Field label="Code from your app">
            <Input value={code} onChange={(e) => setCode(e.target.value)} placeholder="123456" autoFocus required />
          </Field>
          <div className="flex justify-end gap-2 mt-2">
            <Btn type="button" variant="secondary" onClick={onClose}>Cancel</Btn>
            <Btn type="submit" disabled={busy}>Enable 2FA</Btn>
          </div>
        </form>
      )}
    </Modal>
  )
}
