import { useState } from 'react'
import { api } from '../api'
import type { User } from '../types'
import { useAuth } from '../App'
import { Btn, Field, Input, ErrorBanner } from '../components/ui'

export default function Login() {
  const { setUser } = useAuth()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      const u = await api.post<User>('/api/login', { username, password })
      setUser(u)
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="h-full flex items-center justify-center bg-gradient-to-br from-side-900 to-side-700 p-4">
      <div className="w-full max-w-sm">
        <div className="text-center mb-6">
          <span className="text-3xl font-bold text-white tracking-tight">
            Re<span className="text-brand-500">Panel</span>
          </span>
          <p className="text-slate-400 text-sm mt-1">Server Control Panel</p>
        </div>
        <form onSubmit={submit} className="bg-white rounded-lg shadow-xl p-6">
          <ErrorBanner message={error} />
          <Field label="Username">
            <Input value={username} onChange={(e) => setUsername(e.target.value)} autoFocus required />
          </Field>
          <Field label="Password">
            <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required />
          </Field>
          <Btn type="submit" disabled={busy} className="w-full justify-center mt-2">
            {busy ? 'Signing in…' : 'Sign in'}
          </Btn>
        </form>
      </div>
    </div>
  )
}
