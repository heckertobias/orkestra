import { useState } from 'react'
import { Link } from 'react-router-dom'

export function ForgotPasswordPage() {
  const [email, setEmail] = useState('')
  const [submitted, setSubmitted] = useState(false)
  const [busy, setBusy] = useState(false)

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    try {
      await fetch('/orkestra.v1.AuthService/RequestPasswordReset', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email }),
      })
    } finally {
      // Always show success — never reveal whether the email exists.
      setBusy(false)
      setSubmitted(true)
    }
  }

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
          <h1 className="text-base font-semibold mb-1" style={{ color: 'var(--text)' }}>Reset your password</h1>
          <p className="text-sm mb-5" style={{ color: 'var(--text-muted)' }}>
            Enter your email address and we'll send you a link to set a new password.
          </p>

          {submitted ? (
            <div className="text-sm space-y-4">
              <div className="px-3 py-3 rounded text-sm" style={{ backgroundColor: 'rgba(126,226,42,0.08)', border: '1px solid var(--accent)', color: 'var(--accent)' }}>
                If that email is associated with an account, you'll receive a reset link shortly.
              </div>
              <Link to="/login" className="block text-center text-xs hover:underline" style={{ color: 'var(--accent)' }}>
                Back to sign in
              </Link>
            </div>
          ) : (
            <form onSubmit={handleSubmit} className="space-y-3">
              <div>
                <label className="block text-xs mb-1" style={{ color: 'var(--text-muted)' }}>Email address</label>
                <input
                  type="email"
                  value={email}
                  onChange={e => setEmail(e.target.value)}
                  className="w-full px-3 py-2 rounded border text-sm outline-none focus:border-[var(--accent)]"
                  style={{ backgroundColor: 'var(--bg)', borderColor: 'var(--border)', color: 'var(--text)' }}
                  autoComplete="email"
                  placeholder="you@example.com"
                  required
                />
              </div>
              <button
                type="submit"
                disabled={busy}
                className="w-full py-2 rounded text-sm font-medium disabled:opacity-50"
                style={{ backgroundColor: 'var(--accent)', color: '#0d1117' }}>
                {busy ? 'Sending…' : 'Send reset link'}
              </button>
              <Link to="/login" className="block text-center text-xs hover:underline" style={{ color: 'var(--text-muted)' }}>
                Back to sign in
              </Link>
            </form>
          )}
        </div>
      </div>
    </div>
  )
}
