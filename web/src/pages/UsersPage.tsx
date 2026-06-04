import { useEffect, useState } from 'react'
import { Plus, RefreshCw, Shield, X, Trash2 } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { useAuth, isAdmin } from '@/lib/auth'

interface User {
  id: string
  username: string
  displayName: string
  disabled: boolean
  roles: string[]
  hasPassword: boolean
  createdAt: number
  lastLoginAt: number
}

interface RoleBinding {
  id: string
  userId: string
  role: string
  serverId: string
  stackId: string
}

export function UsersPage() {
  const { user: me } = useAuth()
  const [users, setUsers] = useState<User[]>([])
  const [bindings, setBindings] = useState<RoleBinding[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [showCreate, setShowCreate] = useState(false)
  const [showAssignRole, setShowAssignRole] = useState<User | null>(null)
  const [form, setForm] = useState({ username: '', displayName: '', password: '' })
  const [roleForm, setRoleForm] = useState({ role: 'viewer' })
  const [busy, setBusy] = useState(false)
  const [formError, setFormError] = useState<string | null>(null)

  async function load() {
    setLoading(true)
    setError(null)
    try {
      const [uRes, bRes] = await Promise.all([
        fetch('/orkestra.v1.AuthService/ListUsers', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' }),
        fetch('/orkestra.v1.AuthService/ListRoleBindings', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' }),
      ])
      if (uRes.ok) {
        const d = await uRes.json()
        setUsers((d.users ?? []).map((u: Record<string, unknown>) => ({
          id:          String(u.id ?? ''),
          username:    String(u.username ?? ''),
          displayName: String(u.displayName ?? u.display_name ?? ''),
          disabled:    Boolean(u.disabled ?? false),
          roles:       Array.isArray(u.roles) ? u.roles.map(String) : [],
          hasPassword: Boolean(u.hasPassword ?? u.has_password ?? false),
          createdAt:   Number(u.createdAt ?? u.created_at ?? 0),
          lastLoginAt: Number(u.lastLoginAt ?? u.last_login_at ?? 0),
        })))
      }
      if (bRes.ok) {
        const d = await bRes.json()
        setBindings((d.bindings ?? []).map((b: Record<string, unknown>) => ({
          id:       String(b.id ?? ''),
          userId:   String(b.userId ?? b.user_id ?? ''),
          role:     String(b.role ?? ''),
          serverId: String(b.serverId ?? b.server_id ?? ''),
          stackId:  String(b.stackId ?? b.stack_id ?? ''),
        })))
      }
    } catch (e) {
      setError(String(e))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() }, [])

  async function createUser() {
    if (!form.username || !form.password) { setFormError('Username and password required'); return }
    setBusy(true)
    setFormError(null)
    try {
      const res = await fetch('/orkestra.v1.AuthService/CreateUser', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username: form.username, displayName: form.displayName, password: form.password }),
      })
      if (!res.ok) throw new Error(await res.text())
      setShowCreate(false)
      setForm({ username: '', displayName: '', password: '' })
      load()
    } catch (e) {
      setFormError(String(e))
    } finally {
      setBusy(false)
    }
  }

  async function assignRole(userId: string) {
    setBusy(true)
    setFormError(null)
    try {
      const res = await fetch('/orkestra.v1.AuthService/AssignRole', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ userId, role: roleForm.role }),
      })
      if (!res.ok) throw new Error(await res.text())
      setShowAssignRole(null)
      load()
    } catch (e) {
      setFormError(String(e))
    } finally {
      setBusy(false)
    }
  }

  async function revokeBinding(id: string) {
    try {
      const res = await fetch('/orkestra.v1.AuthService/RevokeRole', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ bindingId: id }),
      })
      if (!res.ok) throw new Error(await res.text())
      load()
    } catch (e) {
      setError(String(e))
    }
  }

  const admin = isAdmin(me)

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-xl font-semibold" style={{ color: 'var(--text)' }}>Users & Roles</h1>
          <p style={{ color: 'var(--text-muted)' }}>Manage users, authentication, and role-based access</p>
        </div>
        <div className="flex gap-2">
          <button onClick={load}
            className="flex items-center gap-2 px-3 py-1.5 rounded text-sm border transition-colors hover:bg-[var(--surface-2)]"
            style={{ borderColor: 'var(--border)', color: 'var(--text-muted)' }}>
            <RefreshCw size={14} /> Refresh
          </button>
          {admin && (
            <button onClick={() => setShowCreate(true)}
              className="flex items-center gap-2 px-3 py-1.5 rounded text-sm font-medium"
              style={{ backgroundColor: 'var(--accent)', color: '#0d1117' }}>
              <Plus size={14} /> New User
            </button>
          )}
        </div>
      </div>

      {error && <ErrorBar>{error}</ErrorBar>}

      <div className="rounded-lg border overflow-hidden" style={{ borderColor: 'var(--border)', backgroundColor: 'var(--surface)' }}>
        <table className="w-full text-sm">
          <thead>
            <tr style={{ borderBottom: '1px solid var(--border)' }}>
              {['Username', 'Display name', 'Roles', 'Status', 'Last login', ''].map(h => (
                <th key={h} className="text-left px-4 py-3 text-xs font-medium" style={{ color: 'var(--text-muted)' }}>{h}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {loading && <tr><td colSpan={6} className="px-4 py-8 text-center" style={{ color: 'var(--text-muted)' }}>Loading…</td></tr>}
            {!loading && users.length === 0 && (
              <tr><td colSpan={6} className="px-4 py-8 text-center" style={{ color: 'var(--text-muted)' }}>No users yet.</td></tr>
            )}
            {users.map(u => (
              <tr key={u.id} className="hover:bg-[var(--surface-2)]" style={{ borderBottom: '1px solid var(--border)' }}>
                <td className="px-4 py-3 font-medium" style={{ color: 'var(--text)' }}>{u.username}</td>
                <td className="px-4 py-3 text-xs" style={{ color: 'var(--text-muted)' }}>{u.displayName || '—'}</td>
                <td className="px-4 py-3">
                  <div className="flex flex-wrap gap-1">
                    {u.roles.map(r => <Badge key={r} variant={r === 'admin' ? 'warn' : 'default'}>{r}</Badge>)}
                    {u.roles.length === 0 && <span className="text-xs" style={{ color: 'var(--text-muted)' }}>—</span>}
                  </div>
                </td>
                <td className="px-4 py-3">
                  <Badge variant={u.disabled ? 'offline' : 'online'}>{u.disabled ? 'Disabled' : 'Active'}</Badge>
                </td>
                <td className="px-4 py-3 text-xs" style={{ color: 'var(--text-muted)' }}>
                  {u.lastLoginAt ? new Date(u.lastLoginAt).toLocaleString() : 'Never'}
                </td>
                <td className="px-4 py-3">
                  {admin && (
                    <button onClick={() => { setShowAssignRole(u); setFormError(null) }}
                      className="p-1 rounded hover:bg-[var(--surface-2)]"
                      style={{ color: 'var(--text-muted)' }} title="Assign role">
                      <Shield size={14} />
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {/* Role bindings detail */}
      {bindings.length > 0 && (
        <div className="mt-6">
          <h2 className="text-sm font-medium mb-3" style={{ color: 'var(--text)' }}>Role Bindings</h2>
          <div className="rounded-lg border overflow-hidden" style={{ borderColor: 'var(--border)', backgroundColor: 'var(--surface)' }}>
            <table className="w-full text-sm">
              <thead>
                <tr style={{ borderBottom: '1px solid var(--border)' }}>
                  {['User', 'Role', 'Scope', ''].map(h => (
                    <th key={h} className="text-left px-4 py-3 text-xs font-medium" style={{ color: 'var(--text-muted)' }}>{h}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {bindings.map(b => {
                  const username = users.find(u => u.id === b.userId)?.username ?? b.userId
                  const scope = b.serverId ? `server:${b.serverId}` : b.stackId ? `stack:${b.stackId}` : 'global'
                  return (
                    <tr key={b.id} className="hover:bg-[var(--surface-2)]" style={{ borderBottom: '1px solid var(--border)' }}>
                      <td className="px-4 py-2 text-xs" style={{ color: 'var(--text)' }}>{username}</td>
                      <td className="px-4 py-2"><Badge variant={b.role === 'admin' ? 'warn' : 'default'}>{b.role}</Badge></td>
                      <td className="px-4 py-2 text-xs" style={{ color: 'var(--text-muted)' }}>{scope}</td>
                      <td className="px-4 py-2">
                        {admin && (
                          <button onClick={() => revokeBinding(b.id)}
                            className="p-1 rounded hover:bg-[var(--surface-2)]"
                            style={{ color: 'var(--text-muted)' }}>
                            <Trash2 size={12} />
                          </button>
                        )}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {/* Create user dialog */}
      {showCreate && (
        <Modal title="New User" onClose={() => { setShowCreate(false); setFormError(null) }}>
          {formError && <ErrorBar>{formError}</ErrorBar>}
          <div className="space-y-3">
            <Field label="Username *">
              <input value={form.username} onChange={e => setForm(f => ({ ...f, username: e.target.value }))}
                className={inputCls} style={inputStyle} placeholder="jsmith" />
            </Field>
            <Field label="Display name">
              <input value={form.displayName} onChange={e => setForm(f => ({ ...f, displayName: e.target.value }))}
                className={inputCls} style={inputStyle} placeholder="John Smith" />
            </Field>
            <Field label="Password *">
              <input type="password" value={form.password} onChange={e => setForm(f => ({ ...f, password: e.target.value }))}
                className={inputCls} style={inputStyle} />
            </Field>
          </div>
          <div className="flex justify-end gap-2 mt-4">
            <button onClick={() => { setShowCreate(false); setFormError(null) }}
              className="px-4 py-1.5 rounded border text-sm"
              style={{ borderColor: 'var(--border)', color: 'var(--text-muted)' }}>Cancel</button>
            <button onClick={createUser} disabled={busy}
              className="px-4 py-1.5 rounded text-sm font-medium disabled:opacity-50"
              style={{ backgroundColor: 'var(--accent)', color: '#0d1117' }}>
              {busy ? 'Creating…' : 'Create User'}
            </button>
          </div>
        </Modal>
      )}

      {/* Assign role dialog */}
      {showAssignRole && (
        <Modal title={`Assign Role — ${showAssignRole.username}`} onClose={() => setShowAssignRole(null)}>
          {formError && <ErrorBar>{formError}</ErrorBar>}
          <Field label="Role">
            <select value={roleForm.role} onChange={e => setRoleForm({ role: e.target.value })}
              className={inputCls} style={inputStyle}>
              <option value="viewer">viewer</option>
              <option value="operator">operator</option>
              <option value="admin">admin</option>
            </select>
          </Field>
          <div className="flex justify-end gap-2 mt-4">
            <button onClick={() => setShowAssignRole(null)}
              className="px-4 py-1.5 rounded border text-sm"
              style={{ borderColor: 'var(--border)', color: 'var(--text-muted)' }}>Cancel</button>
            <button onClick={() => assignRole(showAssignRole.id)} disabled={busy}
              className="px-4 py-1.5 rounded text-sm font-medium disabled:opacity-50"
              style={{ backgroundColor: 'var(--accent)', color: '#0d1117' }}>
              {busy ? 'Assigning…' : 'Assign Role'}
            </button>
          </div>
        </Modal>
      )}
    </div>
  )
}

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
