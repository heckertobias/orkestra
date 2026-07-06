import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAuth } from '@/lib/auth'

export function LoggedOutPage() {
  const navigate = useNavigate()
  const { logout } = useAuth()
  const ran = useRef(false)
  const [ssoLogoutUrl, setSsoLogoutUrl] = useState<string | null>(null)
  const [done, setDone] = useState(false)

  // Perform the logout here (not in the sidebar): this route is public, so clearing the
  // user does not trigger AuthGuard's redirect to /login. The ref guard keeps it to a
  // single run despite StrictMode's double-invoke in dev (a second call would return an
  // empty URL and hide the SSO option).
  useEffect(() => {
    if (ran.current) return
    ran.current = true
    logout().then(url => {
      setSsoLogoutUrl(url)
      setDone(true)
    })
  }, [logout])

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
          {!done ? (
            <p className="text-sm" style={{ color: 'var(--text-muted)' }}>Signing out…</p>
          ) : (
            <>
              <h1 className="text-base font-semibold mb-1" style={{ color: 'var(--text)' }}>Signed out</h1>
              <p className="text-sm mb-5" style={{ color: 'var(--text-muted)' }}>
                {ssoLogoutUrl
                  ? 'You have been signed out of orkestra. Your single sign-on (SSO) session is still active — sign out of it too to fully log out.'
                  : 'You have been signed out of orkestra.'}
              </p>

              <div className="space-y-3">
                <button
                  onClick={() => navigate('/login', { replace: true })}
                  className="w-full py-2 rounded text-sm font-medium"
                  style={{ backgroundColor: 'var(--accent)', color: '#0d1117' }}>
                  Back to sign in
                </button>

                {ssoLogoutUrl && (
                  <button
                    onClick={() => { window.location.href = ssoLogoutUrl }}
                    className="w-full py-2 rounded text-sm font-medium flex items-center justify-center border transition-colors hover:bg-[var(--surface-2)]"
                    style={{ borderColor: 'var(--border)', color: 'var(--text)' }}>
                    Also sign out of SSO
                  </button>
                )}
              </div>
            </>
          )}
        </div>
      </div>
    </div>
  )
}
