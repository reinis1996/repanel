import { useState } from 'react'
import { api } from '../api'
import type { User } from '../types'
import { useAuth, useBrand } from '../App'
import { Btn, Field, Input, ErrorBanner } from '../components/ui'

export default function Login() {
  const { setUser } = useAuth()
  const brand = useBrand()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [code, setCode] = useState('')
  const [needCode, setNeedCode] = useState(false)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      const res = await api.post<User & { totp_required?: boolean }>('/api/login', { username, password, code })
      if (res.totp_required) {
        setNeedCode(true)
        setError(code ? 'Invalid authentication code' : '')
        return
      }
      setUser(res)
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
          {brand.logo ? (
            <img src={brand.logo} alt={brand.name} className="h-10 mx-auto object-contain" />
          ) : (
            <span className="text-3xl font-bold text-white tracking-tight">
              {brand.name === 'RePanel' ? (
                <>Re<span className="text-brand-500">Panel</span></>
              ) : (
                brand.name
              )}
            </span>
          )}
          <p className="text-slate-400 text-sm mt-1">Server Control Panel</p>
        </div>
        <form onSubmit={submit} className="bg-white rounded-lg shadow-xl p-6">
          <ErrorBanner message={error} />
          {!needCode ? (
            <>
              <Field label="Username">
                <Input value={username} onChange={(e) => setUsername(e.target.value)} autoFocus required />
              </Field>
              <Field label="Password">
                <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required />
              </Field>
            </>
          ) : (
            <Field label="Authentication code" hint="Enter the 6-digit code from your authenticator app, or a recovery code">
              <Input
                value={code}
                onChange={(e) => setCode(e.target.value)}
                autoFocus
                inputMode="text"
                placeholder="123456"
                required
              />
            </Field>
          )}
          <Btn type="submit" disabled={busy} className="w-full justify-center mt-2">
            {busy ? 'Signing in…' : needCode ? 'Verify' : 'Sign in'}
          </Btn>
          {needCode && (
            <button
              type="button"
              className="mt-2 w-full text-center text-xs text-slate-500 hover:text-slate-700 cursor-pointer"
              onClick={() => { setNeedCode(false); setCode(''); setError('') }}
            >
              ← Back
            </button>
          )}
        </form>
      </div>
    </div>
  )
}
