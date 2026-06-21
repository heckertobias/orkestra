import { useState } from 'react'
import { useAuth } from '@/lib/auth'
import { useToast } from '@/components/ui/toast'

function connectPost(procedure: string, body: unknown) {
  return fetch(`/orkestra.v1.AuthService/${procedure}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
}

export function ProfilePage() {
  const { user } = useAuth()
  const { toast } = useToast()

  const [currentPw, setCurrentPw] = useState('')
  const [newPw, setNewPw] = useState('')
  const [confirmPw, setConfirmPw] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const hasPassword = user?.hasPassword ?? false

  async function handleChangePassword(e: React.FormEvent) {
    e.preventDefault()
    setError(null)
    if (newPw !== confirmPw) { setError('New passwords do not match'); return }
    setBusy(true)
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
      setError(String(err).replace(/^Error: /, ''))
    } finally {
      setBusy(false)
    }
  }

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
              {user?.hasOidc ? (hasPassword ? ' + SSO' : 'SSO') : ''}
            </p>
          </div>
        </div>

        {/* Change password */}
        {hasPassword ? (
          <div className="rounded-lg border p-4" style={{ borderColor: 'var(--border)', backgroundColor: 'var(--surface)' }}>
            <p className="text-xs font-medium uppercase tracking-wide mb-4" style={{ color: 'var(--text-muted)' }}>Change password</p>

            {error && (
              <div className="mb-4 px-3 py-2 rounded text-sm" style={{ backgroundColor: '#2d1115', color: 'var(--error)', border: '1px solid #4a1a1f' }}>
                {error}
              </div>
            )}

            <form onSubmit={handleChangePassword} className="space-y-3">
              <div>
                <label className="block text-xs mb-1" style={{ color: 'var(--text-muted)' }}>Current password</label>
                <input
                  type="password"
                  value={currentPw}
                  onChange={e => setCurrentPw(e.target.value)}
                  className="input w-full"
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
                  className="input w-full"
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
                  className="input w-full"
                  autoComplete="new-password"
                  required
                />
              </div>
              <button
                type="submit"
                disabled={busy}
                className="px-4 py-2 rounded text-sm font-medium disabled:opacity-50"
                style={{ backgroundColor: 'var(--accent)', color: '#0d1117' }}>
                {busy ? 'Changing…' : 'Change password'}
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
