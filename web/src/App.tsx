import { createContext, useContext, useEffect, useState, type ReactNode } from 'react'
import { BrowserRouter, Routes, Route, NavLink, Navigate, useNavigate } from 'react-router-dom'
import {
  LayoutDashboard, Globe, Network, Mail, Database, FolderOpen, Server,
  ShieldCheck, Clock, Users as UsersIcon, Activity, Shield, Settings as SettingsIcon,
  LogOut, KeyRound, ChevronDown, Loader2, Archive,
} from 'lucide-react'
import { api } from './api'
import type { User } from './types'
import { Toaster, toast, Modal, Field, Input, Btn } from './components/ui'
import Login from './pages/Login'
import Setup from './pages/Setup'
import Dashboard from './pages/Dashboard'
import Domains from './pages/Domains'
import DNS from './pages/DNS'
import MailPage from './pages/Mail'
import Databases from './pages/Databases'
import Files from './pages/Files'
import Ftp from './pages/Ftp'
import Ssl from './pages/Ssl'
import Cron from './pages/Cron'
import Backups from './pages/Backups'
import UsersPage from './pages/Users'
import Services from './pages/Services'
import Firewall from './pages/Firewall'
import Settings from './pages/Settings'

// ---------- auth context ----------

interface AuthCtx {
  user: User | null
  setUser: (u: User | null) => void
}
const Ctx = createContext<AuthCtx>({ user: null, setUser: () => {} })
export const useAuth = () => useContext(Ctx)

export default function App() {
  const [user, setUser] = useState<User | null>(null)
  const [needsSetup, setNeedsSetup] = useState(false)
  const [booted, setBooted] = useState(false)

  useEffect(() => {
    Promise.all([
      api.get<{ needs_setup: boolean }>('/api/setup').catch(() => ({ needs_setup: false })),
      api.get<User>('/api/me').catch(() => null),
    ]).then(([setup, me]) => {
      setNeedsSetup(setup.needs_setup)
      setUser(me)
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
  )
}

// ---------- application shell ----------

const nav = [
  { to: '/', label: 'Dashboard', icon: LayoutDashboard },
  { to: '/domains', label: 'Websites & Domains', icon: Globe },
  { to: '/dns', label: 'DNS', icon: Network },
  { to: '/mail', label: 'Mail', icon: Mail },
  { to: '/databases', label: 'Databases', icon: Database },
  { to: '/files', label: 'File Manager', icon: FolderOpen },
  { to: '/ftp', label: 'FTP', icon: Server },
  { to: '/ssl', label: 'SSL/TLS', icon: ShieldCheck },
  { to: '/cron', label: 'Scheduled Tasks', icon: Clock },
  { to: '/backups', label: 'Backups', icon: Archive },
]

const adminNav = [
  { to: '/users', label: 'Users', icon: UsersIcon, reseller: true },
  { to: '/services', label: 'Services', icon: Activity, reseller: false },
  { to: '/firewall', label: 'Firewall', icon: Shield, reseller: false },
  { to: '/settings', label: 'Settings', icon: SettingsIcon, reseller: false },
]

function Shell() {
  const { user } = useAuth()
  const isAdmin = user!.role === 'admin'
  const isReseller = user!.role === 'reseller'

  return (
    <div className="flex h-full">
      <aside className="w-60 shrink-0 bg-side-900 text-slate-300 flex flex-col">
        <div className="px-5 py-4 border-b border-white/10">
          <span className="text-lg font-bold text-white tracking-tight">
            Re<span className="text-brand-500">Panel</span>
          </span>
        </div>
        <nav className="flex-1 overflow-y-auto py-3 space-y-0.5">
          {nav.map((item) => (
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
          RePanel — open source hosting panel
        </div>
      </aside>

      <div className="flex-1 flex flex-col min-w-0">
        <TopBar />
        <main className="flex-1 overflow-y-auto p-6">
          <Routes>
            <Route path="/" element={<Dashboard />} />
            <Route path="/domains" element={<Domains />} />
            <Route path="/dns" element={<DNS />} />
            <Route path="/mail" element={<MailPage />} />
            <Route path="/databases" element={<Databases />} />
            <Route path="/files" element={<Files />} />
            <Route path="/ftp" element={<Ftp />} />
            <Route path="/ssl" element={<Ssl />} />
            <Route path="/cron" element={<Cron />} />
            <Route path="/backups" element={<Backups />} />
            {(isAdmin || isReseller) && <Route path="/users" element={<UsersPage />} />}
            {isAdmin && (
              <>
                <Route path="/services" element={<Services />} />
                <Route path="/firewall" element={<Firewall />} />
                <Route path="/settings" element={<Settings />} />
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
    </header>
  )
}
