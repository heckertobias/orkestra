import { useState } from 'react'
import { X, Copy, Check, Terminal } from 'lucide-react'

interface AddServerDialogProps {
  onClose: () => void
}

interface CreatedToken {
  rawToken: string
  expiresAt: number
}

const TTL_OPTIONS: { label: string; seconds: number }[] = [
  { label: '1 hour', seconds: 3600 },
  { label: '24 hours', seconds: 86400 },
  { label: '7 days', seconds: 604800 },
]

// Best-effort default for the agent's dial-in address: the UI host on the agent
// gRPC port (4440). The operator can edit it (e.g. a public hostname behind a proxy).
function defaultMasterAddr(): string {
  const host = window.location.hostname || 'master.example.com'
  return `https://${host}:4440`
}

export function AddServerDialog({ onClose }: AddServerDialogProps) {
  const [name, setName] = useState('')
  const [ttlSeconds, setTtlSeconds] = useState(TTL_OPTIONS[0].seconds)
  const [masterAddr, setMasterAddr] = useState(defaultMasterAddr())
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [created, setCreated] = useState<CreatedToken | null>(null)
  const [copied, setCopied] = useState<string | null>(null)

  const serverName = name.trim() || 'my-server'
  const enrollCmd = created
    ? `orkestra-agent enroll \\\n  --master ${masterAddr} \\\n  --bootstrap-token ${created.rawToken} \\\n  --name ${serverName}`
    : ''

  async function copy(text: string, key: string) {
    try {
      await navigator.clipboard.writeText(text)
      setCopied(key)
      setTimeout(() => setCopied(c => (c === key ? null : c)), 1500)
    } catch {
      /* clipboard unavailable — user can select manually */
    }
  }

  async function handleCreate() {
    setSubmitting(true)
    setError(null)
    try {
      const res = await fetch('/orkestra.v1.AuthService/CreateEnrollmentToken', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ description: serverName, ttlSeconds, maxUses: 1 }),
      })
      if (!res.ok) {
        const body = await res.json().catch(() => ({}))
        throw new Error(body?.message ?? `HTTP ${res.status}`)
      }
      const d = await res.json()
      const rawToken = String(d.rawToken ?? d.raw_token ?? '')
      if (!rawToken) throw new Error('no token returned')
      setCreated({ rawToken, expiresAt: Number(d.expiresAt ?? d.expires_at ?? 0) })
    } catch (e) {
      setError(String(e))
    } finally {
      setSubmitting(false)
    }
  }

  const fieldStyle = { backgroundColor: 'var(--bg)', borderColor: 'var(--border)', color: 'var(--text)' }

  return (
    <div className="fixed inset-0 flex items-center justify-center z-50" style={{ backgroundColor: 'rgba(0,0,0,0.6)' }}>
      <div className="w-full max-w-lg rounded-lg border p-6 relative max-h-[90vh] overflow-y-auto" style={{ backgroundColor: 'var(--surface)', borderColor: 'var(--border)' }}>
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-base font-semibold" style={{ color: 'var(--text)' }}>Add Server</h2>
          <button onClick={onClose} className="p-1 rounded hover:bg-[var(--surface-2)]" style={{ color: 'var(--text-muted)' }}>
            <X size={16} />
          </button>
        </div>

        {error && (
          <div className="mb-4 px-3 py-2 rounded text-sm" style={{ backgroundColor: '#2d1115', color: 'var(--error)', border: '1px solid #4a1a1f' }}>
            {error}
          </div>
        )}

        {!created ? (
          /* ── Phase 1: mint a token ─────────────────────────────── */
          <>
            <p className="text-sm mb-4" style={{ color: 'var(--text-muted)' }}>
              Generate a one-time enrollment token, then run the printed command on the target
              server to connect its agent to this Master.
            </p>
            <div className="space-y-4">
              <div>
                <label className="block text-xs mb-1" style={{ color: 'var(--text-muted)' }}>Server name</label>
                <input
                  value={name}
                  onChange={e => setName(e.target.value)}
                  placeholder="web-01"
                  className="w-full px-3 py-1.5 rounded border text-sm outline-none focus:border-[var(--accent)]"
                  style={fieldStyle}
                />
              </div>
              <div className="flex gap-3">
                <div className="flex-1">
                  <label className="block text-xs mb-1" style={{ color: 'var(--text-muted)' }}>Token valid for</label>
                  <select
                    value={ttlSeconds}
                    onChange={e => setTtlSeconds(Number(e.target.value))}
                    className="w-full px-3 py-1.5 rounded border text-sm outline-none focus:border-[var(--accent)]"
                    style={fieldStyle}
                  >
                    {TTL_OPTIONS.map(o => (
                      <option key={o.seconds} value={o.seconds}>{o.label}</option>
                    ))}
                  </select>
                </div>
                <div className="flex-1">
                  <label className="block text-xs mb-1" style={{ color: 'var(--text-muted)' }}>Master address (for the agent)</label>
                  <input
                    value={masterAddr}
                    onChange={e => setMasterAddr(e.target.value)}
                    placeholder="https://master:4440"
                    className="w-full px-3 py-1.5 rounded border text-sm font-mono outline-none focus:border-[var(--accent)]"
                    style={fieldStyle}
                  />
                </div>
              </div>
            </div>
            <div className="flex justify-end gap-2 mt-6">
              <button onClick={onClose} className="px-4 py-1.5 rounded border text-sm" style={{ borderColor: 'var(--border)', color: 'var(--text-muted)' }}>
                Cancel
              </button>
              <button onClick={handleCreate} disabled={submitting}
                className="px-4 py-1.5 rounded text-sm font-medium disabled:opacity-50" style={{ backgroundColor: 'var(--accent)', color: '#0d1117' }}>
                {submitting ? 'Generating…' : 'Generate token'}
              </button>
            </div>
          </>
        ) : (
          /* ── Phase 2: show the token + command once ────────────── */
          <>
            <div className="mb-4 px-3 py-2 rounded text-xs" style={{ backgroundColor: '#1f2a17', color: 'var(--accent)', border: '1px solid #2f4020' }}>
              Copy this now — the token is shown only once.
            </div>

            <div className="space-y-4">
              <div>
                <label className="block text-xs mb-1" style={{ color: 'var(--text-muted)' }}>Bootstrap token</label>
                <div className="flex items-center gap-2">
                  <code className="flex-1 min-w-0 px-3 py-2 rounded border text-xs font-mono break-all" style={fieldStyle}>
                    {created.rawToken}
                  </code>
                  <button onClick={() => copy(created.rawToken, 'token')}
                    className="p-2 rounded border shrink-0" style={{ borderColor: 'var(--border)', color: 'var(--text-muted)' }} title="Copy token">
                    {copied === 'token' ? <Check size={14} color="var(--accent)" /> : <Copy size={14} />}
                  </button>
                </div>
              </div>

              <div>
                <label className="flex items-center gap-1.5 text-xs mb-1" style={{ color: 'var(--text-muted)' }}>
                  <Terminal size={12} /> Run on the target server
                </label>
                <div className="flex items-start gap-2">
                  <pre className="flex-1 min-w-0 px-3 py-2 rounded border text-xs font-mono overflow-x-auto whitespace-pre" style={fieldStyle}>
{enrollCmd}
                  </pre>
                  <button onClick={() => copy(enrollCmd, 'cmd')}
                    className="p-2 rounded border shrink-0" style={{ borderColor: 'var(--border)', color: 'var(--text-muted)' }} title="Copy command">
                    {copied === 'cmd' ? <Check size={14} color="var(--accent)" /> : <Copy size={14} />}
                  </button>
                </div>
                <p className="text-xs mt-2" style={{ color: 'var(--text-muted)' }}>
                  Install the agent first — <code className="mx-1 font-mono" style={{ color: 'var(--text)' }}>apt install orkestra-agent</code>
                  (or <code className="mx-1 font-mono" style={{ color: 'var(--text)' }}>dnf install</code>; see the deployment docs) — then run the command above and
                  <code className="mx-1 font-mono" style={{ color: 'var(--text)' }}>sudo systemctl enable --now orkestra-agent</code>.
                  The server appears here as <span style={{ color: 'var(--accent)' }}>Online</span> once connected.
                </p>
              </div>
            </div>

            <div className="flex justify-end gap-2 mt-6">
              <button onClick={onClose} className="px-4 py-1.5 rounded text-sm font-medium" style={{ backgroundColor: 'var(--accent)', color: '#0d1117' }}>
                Done
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  )
}
