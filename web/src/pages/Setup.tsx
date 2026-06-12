import { useState } from 'react'
import { api } from '../api'
import type { User } from '../types'
import { useAuth } from '../App'
import { Btn, Field, Input, ErrorBanner } from '../components/ui'

export default function Setup({ onDone }: { onDone: () => void }) {
  const { setUser } = useAuth()
  const [username, setUsername] = useState('admin')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [serverIP, setServerIP] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      await api.post('/api/setup', { username, email, password, server_ip: serverIP })
      const u = await api.post<User>('/api/login', { username, password })
      onDone()
      setUser(u)
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="h-full flex items-center justify-center bg-gradient-to-br from-side-900 to-side-700 p-4">
      <div className="w-full max-w-md">
        <div className="text-center mb-6">
          <span className="text-3xl font-bold text-white tracking-tight">
            Re<span className="text-brand-500">Panel</span>
          </span>
          <p className="text-slate-400 text-sm mt-1">Welcome! Create the administrator account to finish installation.</p>
        </div>
        <form onSubmit={submit} className="bg-white rounded-lg shadow-xl p-6">
          <ErrorBanner message={error} />
          <Field label="Admin username">
            <Input value={username} onChange={(e) => setUsername(e.target.value)} required />
          </Field>
          <Field label="Email" hint="Used for Let's Encrypt and notifications">
            <Input type="email" value={email} onChange={(e) => setEmail(e.target.value)} />
          </Field>
          <Field label="Password" hint="At least 8 characters">
            <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required minLength={8} />
          </Field>
          <Field label="Server public IP" hint="Used as the default A record for new DNS zones">
            <Input value={serverIP} onChange={(e) => setServerIP(e.target.value)} placeholder="203.0.113.10" />
          </Field>
          <Btn type="submit" disabled={busy} className="w-full justify-center mt-2">
            {busy ? 'Creating…' : 'Create admin account'}
          </Btn>
        </form>
      </div>
    </div>
  )
}
