import { useState } from 'react'
import { useAuth } from '@/lib/auth'
import { useToast } from '@/components/ui/toast-context'

function connectPost(procedure: string, body: unknown) {
  return fetch(`/orkestra.v1.AuthService/${procedure}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
}

function apiError(text: string): string {
  try {
    const j = JSON.parse(text)
    if (j && typeof j.message === 'string' && j.message) return j.message
  } catch { /* plain text */ }
  return text || 'Request failed'
}

export function ProfilePage() {
  const { user, refresh } = useAuth()
  const { toast } = useToast()

  const hasPassword = user?.hasPassword ?? false
  const isOidc     = user?.hasOidc ?? false

  // --- Display name ---
  const [displayName, setDisplayName] = useState(user?.displayName ?? '')
  const [nameBusy, setNameBusy]       = useState(false)
  const [nameError, setNameError]     = useState<string | null>(null)

  async function handleUpdateProfile(e: React.FormEvent) {
    e.preventDefault()
    setNameError(null)
    setNameBusy(true)
    try {
      const res = await connectPost('UpdateProfile', { display_name: displayName })
      if (!res.ok) {
        const d = await res.json().catch(() => ({}))
        throw new Error(d.message ?? 'Failed to update profile')
      }
      await refresh()
      toast('Display name updated', 'success')
    } catch (err) {
      setNameError(String(err).replace(/^Error: /, ''))
    } finally {
      setNameBusy(false)
    }
  }

  // --- Email change ---
  const [newEmail, setNewEmail]       = useState('')
  const [emailBusy, setEmailBusy]     = useState(false)
  const [emailError, setEmailError]   = useState<string | null>(null)
  const [emailSent, setEmailSent]     = useState(false)

  async function handleRequestEmailChange(e: React.FormEvent) {
    e.preventDefault()
    setEmailError(null)
    setEmailSent(false)
    setEmailBusy(true)
    try {
      const res = await connectPost('RequestEmailChange', { new_email: newEmail })
      if (!res.ok) {
        throw new Error(apiError(await res.text()))
      }
      setEmailSent(true)
      setNewEmail('')
    } catch (err) {
      setEmailError(String(err).replace(/^Error: /, ''))
    } finally {
      setEmailBusy(false)
    }
  }

  // --- Change password ---
  const [currentPw, setCurrentPw]   = useState('')
  const [newPw, setNewPw]           = useState('')
  const [confirmPw, setConfirmPw]   = useState('')
  const [pwError, setPwError]       = useState<string | null>(null)
  const [pwBusy, setPwBusy]         = useState(false)

  async function handleChangePassword(e: React.FormEvent) {
    e.preventDefault()
    setPwError(null)
    if (newPw !== confirmPw) { setPwError('New passwords do not match'); return }
    setPwBusy(true)
    try {
      const res = await connectPost('ChangePassword', {
        current_password: currentPw,
        new_password: newPw,
      })
      if (!res.ok) {
        const d = await res.json().catch(() => ({}))
        throw new Error(d.message ?? 'Failed to change password')
      }
      setCurrentPw('')
      setNewPw('')
      setConfirmPw('')
      toast('Password changed successfully', 'success')
    } catch (err) {
      setPwError(String(err).replace(/^Error: /, ''))
    } finally {
      setPwBusy(false)
    }
  }

  const inputCls   = 'w-full px-3 py-2 rounded border text-sm outline-none focus:border-[var(--accent)]'
  const inputStyle = { backgroundColor: 'var(--bg)', borderColor: 'var(--border)', color: 'var(--text)' }

  return (
    <div>
      <h1 className="text-xl font-semibold mb-1" style={{ color: 'var(--text)' }}>Profile</h1>
      <p className="mb-6" style={{ color: 'var(--text-muted)' }}>Your account details and security settings.</p>

      <div className="max-w-lg space-y-6">
        {/* Account info */}
        <div className="rounded-lg border p-4 space-y-3" style={{ borderColor: 'var(--border)', backgroundColor: 'var(--surface)' }}>
          <p className="text-xs font-medium uppercase tracking-wide" style={{ color: 'var(--text-muted)' }}>Account</p>
          <div className="space-y-1">
            <p className="text-xs" style={{ color: 'var(--text-muted)' }}>Email</p>
            <p className="text-sm" style={{ color: 'var(--text)' }}>{user?.username ?? '—'}</p>
          </div>
          {user?.displayName && (
            <div className="space-y-1">
              <p className="text-xs" style={{ color: 'var(--text-muted)' }}>Display name</p>
              <p className="text-sm" style={{ color: 'var(--text)' }}>{user.displayName}</p>
            </div>
          )}
          <div className="space-y-1">
            <p className="text-xs" style={{ color: 'var(--text-muted)' }}>Auth method</p>
            <p className="text-sm" style={{ color: 'var(--text)' }}>
              {hasPassword ? 'Local password' : ''}
              {isOidc ? (hasPassword ? ' + SSO' : 'SSO') : ''}
            </p>
          </div>
        </div>

        {/* Display name + email — blocked for OIDC users */}
        {isOidc ? (
          <div className="rounded-lg border p-4" style={{ borderColor: 'var(--border)', backgroundColor: 'var(--surface)' }}>
            <p className="text-xs font-medium uppercase tracking-wide mb-2" style={{ color: 'var(--text-muted)' }}>Profile</p>
            <p className="text-sm" style={{ color: 'var(--text-muted)' }}>
              Your display name and email address are managed by your identity provider (SSO) and updated automatically on each login.
            </p>
          </div>
        ) : (
          <>
            {/* Display name form */}
            <div className="rounded-lg border p-4" style={{ borderColor: 'var(--border)', backgroundColor: 'var(--surface)' }}>
              <p className="text-xs font-medium uppercase tracking-wide mb-4" style={{ color: 'var(--text-muted)' }}>Display name</p>

              {nameError && (
                <div className="mb-4 px-3 py-2 rounded text-sm" style={{ backgroundColor: '#2d1115', color: 'var(--error)', border: '1px solid #4a1a1f' }}>
                  {nameError}
                </div>
              )}

              <form onSubmit={handleUpdateProfile} className="space-y-3">
                <div>
                  <label className="block text-xs mb-1" style={{ color: 'var(--text-muted)' }}>Display name</label>
                  <input
                    type="text"
                    value={displayName}
                    onChange={e => setDisplayName(e.target.value)}
                    className={inputCls}
                    style={inputStyle}
                    placeholder="Jane Smith"
                    required
                  />
                </div>
                <button
                  type="submit"
                  disabled={nameBusy}
                  className="px-4 py-2 rounded text-sm font-medium disabled:opacity-50"
                  style={{ backgroundColor: 'var(--accent)', color: '#0d1117' }}
                >
                  {nameBusy ? 'Saving…' : 'Save display name'}
                </button>
              </form>
            </div>

            {/* Email change form */}
            <div className="rounded-lg border p-4" style={{ borderColor: 'var(--border)', backgroundColor: 'var(--surface)' }}>
              <p className="text-xs font-medium uppercase tracking-wide mb-4" style={{ color: 'var(--text-muted)' }}>Change email</p>

              {emailError && (
                <div className="mb-4 px-3 py-2 rounded text-sm" style={{ backgroundColor: '#2d1115', color: 'var(--error)', border: '1px solid #4a1a1f' }}>
                  {emailError}
                </div>
              )}
              {emailSent && (
                <div className="mb-4 px-3 py-2 rounded text-sm" style={{ backgroundColor: 'rgba(126,226,42,0.08)', color: 'var(--accent)', border: '1px solid rgba(126,226,42,0.3)' }}>
                  Confirmation link sent — check the inbox of your new address and click the link to confirm.
                </div>
              )}

              <form onSubmit={handleRequestEmailChange} className="space-y-3">
                <div>
                  <label className="block text-xs mb-1" style={{ color: 'var(--text-muted)' }}>New email address</label>
                  <input
                    type="email"
                    value={newEmail}
                    onChange={e => setNewEmail(e.target.value)}
                    className={inputCls}
                    style={inputStyle}
                    placeholder="new@example.com"
                    required
                  />
                </div>
                <button
                  type="submit"
                  disabled={emailBusy}
                  className="px-4 py-2 rounded text-sm font-medium disabled:opacity-50"
                  style={{ backgroundColor: 'var(--accent)', color: '#0d1117' }}
                >
                  {emailBusy ? 'Sending…' : 'Send confirmation link'}
                </button>
              </form>
            </div>
          </>
        )}

        {/* Change password */}
        {hasPassword ? (
          <div className="rounded-lg border p-4" style={{ borderColor: 'var(--border)', backgroundColor: 'var(--surface)' }}>
            <p className="text-xs font-medium uppercase tracking-wide mb-4" style={{ color: 'var(--text-muted)' }}>Change password</p>

            {pwError && (
              <div className="mb-4 px-3 py-2 rounded text-sm" style={{ backgroundColor: '#2d1115', color: 'var(--error)', border: '1px solid #4a1a1f' }}>
                {pwError}
              </div>
            )}

            <form onSubmit={handleChangePassword} className="space-y-3">
              <div>
                <label className="block text-xs mb-1" style={{ color: 'var(--text-muted)' }}>Current password</label>
                <input
                  type="password"
                  value={currentPw}
                  onChange={e => setCurrentPw(e.target.value)}
                  className={inputCls}
                  style={inputStyle}
                  autoComplete="current-password"
                  required
                />
              </div>
              <div>
                <label className="block text-xs mb-1" style={{ color: 'var(--text-muted)' }}>New password</label>
                <input
                  type="password"
                  value={newPw}
                  onChange={e => setNewPw(e.target.value)}
                  className={inputCls}
                  style={inputStyle}
                  autoComplete="new-password"
                  required
                />
              </div>
              <div>
                <label className="block text-xs mb-1" style={{ color: 'var(--text-muted)' }}>Confirm new password</label>
                <input
                  type="password"
                  value={confirmPw}
                  onChange={e => setConfirmPw(e.target.value)}
                  className={inputCls}
                  style={inputStyle}
                  autoComplete="new-password"
                  required
                />
              </div>
              <button
                type="submit"
                disabled={pwBusy}
                className="px-4 py-2 rounded text-sm font-medium disabled:opacity-50"
                style={{ backgroundColor: 'var(--accent)', color: '#0d1117' }}>
                {pwBusy ? 'Changing…' : 'Change password'}
              </button>
            </form>
          </div>
        ) : (
          <div className="rounded-lg border p-4" style={{ borderColor: 'var(--border)', backgroundColor: 'var(--surface)' }}>
            <p className="text-xs font-medium uppercase tracking-wide mb-2" style={{ color: 'var(--text-muted)' }}>Change password</p>
            <p className="text-sm" style={{ color: 'var(--text-muted)' }}>
              Your account authenticates via SSO. Password management is handled by your identity provider.
            </p>
          </div>
        )}
      </div>
    </div>
  )
}
