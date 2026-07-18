import { useEffect, useState } from 'react'
import { useSearchParams, Link } from 'react-router-dom'
import logo from '@/assets/logo.webp'

export function VerifyEmailPage() {
  const [params] = useSearchParams()
  const token = params.get('token') ?? ''

  // Initialise from the token so the effect never calls setState synchronously
  // (the no-token case is a derived initial state, not an effect side-effect).
  const [status, setStatus] = useState<'loading' | 'success' | 'error'>(token ? 'loading' : 'error')
  const [message, setMessage] = useState(
    token ? '' : 'No token found in the link. Please use the link from your confirmation email.',
  )

  useEffect(() => {
    if (!token) return

    fetch('/orkestra.v1.AuthService/ConfirmEmailChange', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ token }),
    })
      .then(async res => {
        if (res.ok) {
          setStatus('success')
        } else {
          const d = await res.json().catch(() => ({}))
          throw new Error(d.message ?? 'Failed to confirm email change')
        }
      })
      .catch(err => {
        setStatus('error')
        setMessage(String(err).replace(/^Error: /, ''))
      })
  }, [token])

  return (
    <div className="min-h-screen flex items-center justify-center" style={{ backgroundColor: 'var(--bg)' }}>
      <div className="w-full max-w-sm">
        {/* Logo */}
        <div className="flex flex-col items-center gap-3 mb-8">
          <img src={logo} alt="orkestra" className="w-[90%] h-auto" />
          <span className="text-3xl leading-none font-semibold" style={{ color: 'var(--text)' }}>
            ork<span style={{ color: 'var(--accent)' }}>estra</span>
          </span>
        </div>

        <div
          className="rounded-lg border p-6 text-center"
          style={{ backgroundColor: 'var(--surface)', borderColor: 'var(--border)' }}
        >
          {status === 'loading' && (
            <>
              <h1 className="text-base font-semibold mb-2" style={{ color: 'var(--text)' }}>
                Confirming…
              </h1>
              <p className="text-sm" style={{ color: 'var(--text-muted)' }}>
                Verifying your email address.
              </p>
            </>
          )}

          {status === 'success' && (
            <>
              <div
                className="w-10 h-10 rounded-full flex items-center justify-center mx-auto mb-3 text-lg"
                style={{ backgroundColor: 'rgba(126,226,42,0.15)', color: 'var(--accent)' }}
              >
                ✓
              </div>
              <h1 className="text-base font-semibold mb-2" style={{ color: 'var(--text)' }}>
                Email confirmed
              </h1>
              <p className="text-sm mb-4" style={{ color: 'var(--text-muted)' }}>
                Your email address has been updated. Please use your new address to sign in.
              </p>
              <Link
                to="/login"
                className="inline-block px-4 py-2 rounded text-sm font-medium"
                style={{ backgroundColor: 'var(--accent)', color: '#0d1117' }}
              >
                Go to login
              </Link>
            </>
          )}

          {status === 'error' && (
            <>
              <h1 className="text-base font-semibold mb-2" style={{ color: 'var(--error)' }}>
                Confirmation failed
              </h1>
              <p className="text-sm mb-4" style={{ color: 'var(--text-muted)' }}>
                {message || 'This link is invalid or has already been used.'}
              </p>
              <Link
                to="/"
                className="text-sm"
                style={{ color: 'var(--accent)' }}
              >
                Back to dashboard
              </Link>
            </>
          )}
        </div>
      </div>
    </div>
  )
}
