import { useEffect, useState } from 'react'
import { KeyRound, Plus, RefreshCw, Trash2, Eye, EyeOff, X } from 'lucide-react'
import { Badge } from '@/components/ui/badge'

interface SecretMeta {
  id: string
  name: string
  description: string
  provider: string
  version: number
  baoMount: string
  baoPath: string
  baoKey: string
  createdAt: number
  updatedAt: number
  bindingCount: number
}

interface CreateForm {
  name: string
  description: string
  provider: 'builtin' | 'openbao'
  value: string
  baoMount: string
  baoPath: string
  baoKey: string
}

const empty: CreateForm = { name: '', description: '', provider: 'builtin', value: '', baoMount: '', baoPath: '', baoKey: '' }

export function SecretsPage() {
  const [secrets, setSecrets] = useState<SecretMeta[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [showCreate, setShowCreate] = useState(false)
  const [form, setForm] = useState<CreateForm>(empty)
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)
  const [showValue, setShowValue] = useState(false)
  const [revealState, setRevealState] = useState<{ id: string; value: string; password: string; showing: boolean } | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<SecretMeta | null>(null)

  async function load() {
    setLoading(true)
    setError(null)
    try {
      const res = await fetch('/orkestra.v1.SecretService/ListSecrets', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({}),
      })
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const data = await res.json()
      setSecrets((data.secrets ?? []).map((s: Record<string, unknown>) => ({
        id:           String(s.id ?? ''),
        name:         String(s.name ?? ''),
        description:  String(s.description ?? ''),
        provider:     String(s.provider ?? 'builtin'),
        version:      Number(s.version ?? 1),
        baoMount:     String(s.baoMount ?? s.bao_mount ?? ''),
        baoPath:      String(s.baoPath ?? s.bao_path ?? ''),
        baoKey:       String(s.baoKey ?? s.bao_key ?? ''),
        createdAt:    Number(s.createdAt ?? s.created_at ?? 0),
        updatedAt:    Number(s.updatedAt ?? s.updated_at ?? 0),
        bindingCount: Number(s.bindingCount ?? s.binding_count ?? 0),
      })))
    } catch (e) {
      setError(String(e))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() }, [])

  async function createSecret() {
    if (!form.name) { setCreateError('Name is required'); return }
    if (form.provider === 'builtin' && !form.value) { setCreateError('Secret value is required'); return }
    setCreating(true)
    setCreateError(null)
    try {
      const body: Record<string, unknown> = {
        name: form.name,
        description: form.description,
        provider: form.provider,
      }
      if (form.provider === 'builtin') {
        body.valueBytes = btoa(form.value)
      } else {
        body.baoMount = form.baoMount
        body.baoPath = form.baoPath
        body.baoKey = form.baoKey
      }
      const res = await fetch('/orkestra.v1.SecretService/CreateSecret', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      })
      if (!res.ok) {
        const t = await res.text().catch(() => `HTTP ${res.status}`)
        throw new Error(t)
      }
      setShowCreate(false)
      setForm(empty)
      load()
    } catch (e) {
      setCreateError(String(e))
    } finally {
      setCreating(false)
    }
  }

  async function deleteSecret(s: SecretMeta) {
    try {
      const res = await fetch('/orkestra.v1.SecretService/DeleteSecret', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ id: s.id }),
      })
      if (!res.ok) {
        const t = await res.text().catch(() => `HTTP ${res.status}`)
        throw new Error(t)
      }
      setDeleteTarget(null)
      load()
    } catch (e) {
      setError(String(e))
    }
  }

  async function revealSecret(id: string) {
    try {
      const res = await fetch('/orkestra.v1.SecretService/RevealSecret', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ id, reauthPassword: revealState?.password ?? '' }),
      })
      if (!res.ok) {
        const t = await res.text().catch(() => `HTTP ${res.status}`)
        throw new Error(t)
      }
      const data = await res.json()
      const value = atob(String(data.valueBytes ?? ''))
      setRevealState(prev => prev ? { ...prev, value, showing: true } : null)
    } catch (e) {
      setError(String(e))
    }
  }

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-xl font-semibold" style={{ color: 'var(--text)' }}>Secrets</h1>
          <p style={{ color: 'var(--text-muted)' }}>Centrally managed secrets, never persisted in plaintext</p>
        </div>
        <div className="flex gap-2">
          <button onClick={load}
            className="flex items-center gap-2 px-3 py-1.5 rounded text-sm border transition-colors hover:bg-[var(--surface-2)]"
            style={{ borderColor: 'var(--border)', color: 'var(--text-muted)' }}>
            <RefreshCw size={14} /> Refresh
          </button>
          <button onClick={() => setShowCreate(true)}
            className="flex items-center gap-2 px-3 py-1.5 rounded text-sm font-medium"
            style={{ backgroundColor: 'var(--accent)', color: '#0d1117' }}>
            <Plus size={14} /> New Secret
          </button>
        </div>
      </div>

      {error && (
        <div className="mb-4 px-4 py-3 rounded text-sm" style={{ backgroundColor: '#2d1115', color: 'var(--error)', border: '1px solid #4a1a1f' }}>
          {error}
        </div>
      )}

      <div className="rounded-lg border overflow-hidden" style={{ borderColor: 'var(--border)', backgroundColor: 'var(--surface)' }}>
        <table className="w-full text-sm">
          <thead>
            <tr style={{ borderBottom: '1px solid var(--border)' }}>
              {['Name', 'Description', 'Provider', 'Version', 'Bindings', 'Updated', ''].map(h => (
                <th key={h} className="text-left px-4 py-3 text-xs font-medium" style={{ color: 'var(--text-muted)' }}>{h}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {loading && <tr><td colSpan={7} className="px-4 py-8 text-center" style={{ color: 'var(--text-muted)' }}>Loading…</td></tr>}
            {!loading && secrets.length === 0 && (
              <tr><td colSpan={7} className="px-4 py-8 text-center" style={{ color: 'var(--text-muted)' }}>
                No secrets yet. Create one to bind to stacks.
              </td></tr>
            )}
            {secrets.map(s => (
              <tr key={s.id} className="hover:bg-[var(--surface-2)]" style={{ borderBottom: '1px solid var(--border)' }}>
                <td className="px-4 py-3 font-medium" style={{ color: 'var(--text)' }}>
                  <div className="flex items-center gap-2">
                    <KeyRound size={14} style={{ color: 'var(--accent)' }} />
                    {s.name}
                  </div>
                </td>
                <td className="px-4 py-3 text-xs" style={{ color: 'var(--text-muted)' }}>{s.description || '—'}</td>
                <td className="px-4 py-3">
                  <Badge variant="default">{s.provider}</Badge>
                </td>
                <td className="px-4 py-3 text-xs" style={{ color: 'var(--text-muted)' }}>v{s.version}</td>
                <td className="px-4 py-3 text-xs" style={{ color: 'var(--text-muted)' }}>{s.bindingCount}</td>
                <td className="px-4 py-3 text-xs" style={{ color: 'var(--text-muted)' }}>
                  {s.updatedAt ? new Date(s.updatedAt).toLocaleDateString() : '—'}
                </td>
                <td className="px-4 py-3">
                  <div className="flex gap-2">
                    {s.provider === 'builtin' && (
                      <button
                        onClick={() => setRevealState({ id: s.id, value: '', password: '', showing: false })}
                        className="p-1 rounded hover:bg-[var(--surface-2)]"
                        style={{ color: 'var(--text-muted)' }}
                        title="Reveal value">
                        <Eye size={14} />
                      </button>
                    )}
                    <button
                      onClick={() => setDeleteTarget(s)}
                      className="p-1 rounded hover:bg-[var(--surface-2)]"
                      style={{ color: 'var(--text-muted)' }}
                      title="Delete secret">
                      <Trash2 size={14} />
                    </button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {/* Create dialog */}
      {showCreate && (
        <Modal title="New Secret" onClose={() => { setShowCreate(false); setForm(empty); setCreateError(null) }}>
          {createError && <ErrorBar>{createError}</ErrorBar>}
          <div className="space-y-3">
            <Field label="Name *">
              <input value={form.name} onChange={e => setForm(f => ({ ...f, name: e.target.value }))}
                className={inputCls} style={inputStyle}
                placeholder="db-password" />
            </Field>
            <Field label="Description">
              <input value={form.description} onChange={e => setForm(f => ({ ...f, description: e.target.value }))}
                className={inputCls} style={inputStyle}
                placeholder="Optional" />
            </Field>
            <Field label="Provider">
              <div className="flex gap-2">
                {(['builtin', 'openbao'] as const).map(p => (
                  <button key={p} onClick={() => setForm(f => ({ ...f, provider: p }))}
                    className="px-3 py-1.5 rounded text-sm border transition-colors"
                    style={{
                      borderColor: form.provider === p ? 'var(--accent)' : 'var(--border)',
                      color: form.provider === p ? 'var(--accent)' : 'var(--text-muted)',
                      backgroundColor: form.provider === p ? 'rgba(126,226,42,0.08)' : 'transparent',
                    }}>
                    {p}
                  </button>
                ))}
              </div>
            </Field>
            {form.provider === 'builtin' ? (
              <Field label="Value *">
                <div className="relative">
                  <input type={showValue ? 'text' : 'password'} value={form.value}
                    onChange={e => setForm(f => ({ ...f, value: e.target.value }))}
                    className={inputCls + ' pr-8'} style={inputStyle}
                    placeholder="secret value" />
                  <button type="button" onClick={() => setShowValue(v => !v)}
                    className="absolute right-2 top-1/2 -translate-y-1/2" style={{ color: 'var(--text-muted)' }}>
                    {showValue ? <EyeOff size={14} /> : <Eye size={14} />}
                  </button>
                </div>
              </Field>
            ) : (
              <>
                <Field label="Vault Mount"><input value={form.baoMount} onChange={e => setForm(f => ({ ...f, baoMount: e.target.value }))} className={inputCls} style={inputStyle} placeholder="secret" /></Field>
                <Field label="Vault Path"><input value={form.baoPath} onChange={e => setForm(f => ({ ...f, baoPath: e.target.value }))} className={inputCls} style={inputStyle} placeholder="myapp/config" /></Field>
                <Field label="Key"><input value={form.baoKey} onChange={e => setForm(f => ({ ...f, baoKey: e.target.value }))} className={inputCls} style={inputStyle} placeholder="password" /></Field>
              </>
            )}
          </div>
          <div className="flex justify-end gap-2 mt-4">
            <button onClick={() => { setShowCreate(false); setForm(empty); setCreateError(null) }}
              className="px-4 py-1.5 rounded border text-sm"
              style={{ borderColor: 'var(--border)', color: 'var(--text-muted)' }}>Cancel</button>
            <button onClick={createSecret} disabled={creating}
              className="px-4 py-1.5 rounded text-sm font-medium disabled:opacity-50"
              style={{ backgroundColor: 'var(--accent)', color: '#0d1117' }}>
              {creating ? 'Creating…' : 'Create Secret'}
            </button>
          </div>
        </Modal>
      )}

      {/* Reveal dialog */}
      {revealState && (
        <Modal title="Reveal Secret" onClose={() => setRevealState(null)}>
          {revealState.showing ? (
            <div>
              <p className="text-xs mb-2" style={{ color: 'var(--text-muted)' }}>Secret value:</p>
              <div className="px-3 py-2 rounded font-mono text-sm break-all"
                style={{ backgroundColor: 'var(--bg)', border: '1px solid var(--border)', color: 'var(--text)' }}>
                {revealState.value}
              </div>
              <div className="flex justify-end mt-4">
                <button onClick={() => setRevealState(null)}
                  className="px-4 py-1.5 rounded border text-sm"
                  style={{ borderColor: 'var(--border)', color: 'var(--text-muted)' }}>Close</button>
              </div>
            </div>
          ) : (
            <div>
              <p className="text-sm mb-3" style={{ color: 'var(--text-muted)' }}>Enter your password to reveal the secret value.</p>
              <Field label="Password">
                <input type="password" value={revealState.password}
                  onChange={e => setRevealState(prev => prev ? { ...prev, password: e.target.value } : null)}
                  className={inputCls} style={inputStyle}
                  placeholder="your password" />
              </Field>
              <div className="flex justify-end gap-2 mt-4">
                <button onClick={() => setRevealState(null)}
                  className="px-4 py-1.5 rounded border text-sm"
                  style={{ borderColor: 'var(--border)', color: 'var(--text-muted)' }}>Cancel</button>
                <button onClick={() => revealSecret(revealState.id)}
                  className="px-4 py-1.5 rounded text-sm font-medium"
                  style={{ backgroundColor: 'var(--accent)', color: '#0d1117' }}>Reveal</button>
              </div>
            </div>
          )}
        </Modal>
      )}

      {/* Delete confirmation */}
      {deleteTarget && (
        <Modal title="Delete Secret" onClose={() => setDeleteTarget(null)}>
          <p className="text-sm mb-4" style={{ color: 'var(--text-muted)' }}>
            Delete <strong style={{ color: 'var(--text)' }}>{deleteTarget.name}</strong>?
            {deleteTarget.bindingCount > 0 && (
              <span style={{ color: 'var(--error)' }}> This secret has {deleteTarget.bindingCount} active binding(s) and cannot be deleted.</span>
            )}
          </p>
          <div className="flex justify-end gap-2">
            <button onClick={() => setDeleteTarget(null)}
              className="px-4 py-1.5 rounded border text-sm"
              style={{ borderColor: 'var(--border)', color: 'var(--text-muted)' }}>Cancel</button>
            {deleteTarget.bindingCount === 0 && (
              <button onClick={() => deleteSecret(deleteTarget)}
                className="px-4 py-1.5 rounded text-sm font-medium"
                style={{ backgroundColor: 'var(--error)', color: '#fff' }}>Delete</button>
            )}
          </div>
        </Modal>
      )}
    </div>
  )
}

// ─── shared sub-components ──────────────────────────────────────────────────

function Modal({ title, onClose, children }: { title: string; onClose: () => void; children: React.ReactNode }) {
  return (
    <div className="fixed inset-0 flex items-center justify-center z-50" style={{ backgroundColor: 'rgba(0,0,0,0.6)' }}>
      <div className="w-full max-w-md rounded-lg border p-6 relative" style={{ backgroundColor: 'var(--surface)', borderColor: 'var(--border)' }}>
        <div className="flex items-center justify-between mb-4">
          <h2 className="font-semibold text-base" style={{ color: 'var(--text)' }}>{title}</h2>
          <button onClick={onClose} style={{ color: 'var(--text-muted)' }}><X size={18} /></button>
        </div>
        {children}
      </div>
    </div>
  )
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <label className="block text-xs mb-1" style={{ color: 'var(--text-muted)' }}>{label}</label>
      {children}
    </div>
  )
}

function ErrorBar({ children }: { children: React.ReactNode }) {
  return (
    <div className="mb-3 px-3 py-2 rounded text-sm" style={{ backgroundColor: '#2d1115', color: 'var(--error)', border: '1px solid #4a1a1f' }}>
      {children}
    </div>
  )
}

const inputCls = 'w-full px-3 py-1.5 rounded border text-sm outline-none focus:border-[var(--accent)]'
const inputStyle = { backgroundColor: 'var(--bg)', borderColor: 'var(--border)', color: 'var(--text)' } as React.CSSProperties
