import { useState, useEffect } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { useAuth } from '@/lib/auth'

export function LoginPage() {
  const { login, user } = useAuth()
  const navigate = useNavigate()
  const [params] = useSearchParams()
  const setupToken = params.get('setup') ?? ''

  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  // Setup mode state
  const [setupDisplayName, setSetupDisplayName] = useState('')

  useEffect(() => {
    if (user) navigate('/', { replace: true })
  }, [user, navigate])

  async function handleLogin(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError(null)
    try {
      await login(username, password)
      navigate('/', { replace: true })
    } catch (err) {
      setError(parseConnectError(String(err)))
    } finally {
      setBusy(false)
    }
  }

  async function handleSetup(e: React.FormEvent) {
    e.preventDefault()
    if (!username || !password) { setError('Username and password are required'); return }
    setBusy(true)
    setError(null)
    try {
      const res = await fetch('/api/setup', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ token: setupToken, username, password, displayName: setupDisplayName }),
      })
      if (!res.ok) {
        throw new Error(await res.text())
      }
      // Log in after setup
      await login(username, password)
      navigate('/', { replace: true })
    } catch (err) {
      setError(String(err))
    } finally {
      setBusy(false)
    }
  }

  const isSetup = Boolean(setupToken)

  return (
    <div className="min-h-screen flex items-center justify-center" style={{ backgroundColor: 'var(--bg)' }}>
      <div className="w-full max-w-sm">
        {/* Logo */}
        <div className="flex items-center justify-center gap-3 mb-8">
          <div className="w-9 h-9 rounded-lg flex items-center justify-center text-sm font-bold"
            style={{ backgroundColor: 'var(--accent)', color: '#0d1117' }}>
            O
          </div>
          <span className="text-xl font-semibold" style={{ color: 'var(--text)' }}>
            ork<span style={{ color: 'var(--accent)' }}>estra</span>
          </span>
        </div>

        <div className="rounded-lg border p-6" style={{ backgroundColor: 'var(--surface)', borderColor: 'var(--border)' }}>
          <h1 className="text-base font-semibold mb-1" style={{ color: 'var(--text)' }}>
            {isSetup ? 'Initial Setup' : 'Sign in'}
          </h1>
          <p className="text-sm mb-5" style={{ color: 'var(--text-muted)' }}>
            {isSetup ? 'Create the first administrator account.' : 'Sign in to your orkestra instance.'}
          </p>

          {error && (
            <div className="mb-4 px-3 py-2 rounded text-sm" style={{ backgroundColor: '#2d1115', color: 'var(--error)', border: '1px solid #4a1a1f' }}>
              {error}
            </div>
          )}

          <form onSubmit={isSetup ? handleSetup : handleLogin} className="space-y-3">
            <div>
              <label className="block text-xs mb-1" style={{ color: 'var(--text-muted)' }}>Username</label>
              <input
                value={username}
                onChange={e => setUsername(e.target.value)}
                className="w-full px-3 py-2 rounded border text-sm outline-none focus:border-[var(--accent)]"
                style={{ backgroundColor: 'var(--bg)', borderColor: 'var(--border)', color: 'var(--text)' }}
                autoComplete="username"
                placeholder="admin"
                required
              />
            </div>

            {isSetup && (
              <div>
                <label className="block text-xs mb-1" style={{ color: 'var(--text-muted)' }}>Display name</label>
                <input
                  value={setupDisplayName}
                  onChange={e => setSetupDisplayName(e.target.value)}
                  className="w-full px-3 py-2 rounded border text-sm outline-none focus:border-[var(--accent)]"
                  style={{ backgroundColor: 'var(--bg)', borderColor: 'var(--border)', color: 'var(--text)' }}
                  placeholder="Administrator"
                />
              </div>
            )}

            <div>
              <label className="block text-xs mb-1" style={{ color: 'var(--text-muted)' }}>Password</label>
              <input
                type="password"
                value={password}
                onChange={e => setPassword(e.target.value)}
                className="w-full px-3 py-2 rounded border text-sm outline-none focus:border-[var(--accent)]"
                style={{ backgroundColor: 'var(--bg)', borderColor: 'var(--border)', color: 'var(--text)' }}
                autoComplete={isSetup ? 'new-password' : 'current-password'}
                required
              />
            </div>

            <button
              type="submit"
              disabled={busy}
              className="w-full py-2 rounded text-sm font-medium disabled:opacity-50"
              style={{ backgroundColor: 'var(--accent)', color: '#0d1117' }}>
              {busy ? (isSetup ? 'Setting up…' : 'Signing in…') : (isSetup ? 'Create account & sign in' : 'Sign in')}
            </button>
          </form>

          {!isSetup && (
            <p className="mt-4 text-center text-xs" style={{ color: 'var(--text-muted)' }}>
              First time? Check the master logs for the setup URL.
            </p>
          )}
        </div>
      </div>
    </div>
  )
}

function parseConnectError(raw: string): string {
  try {
    const obj = JSON.parse(raw.replace(/^.*?: /, ''))
    return obj.message ?? raw
  } catch {
    return raw.replace(/^Error: /, '')
  }
}
