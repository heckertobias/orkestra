import { useEffect, useState } from 'react'
import { Plus, RefreshCw, Shield, X, ChevronDown, ChevronRight, Check } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { useAuth, isAdmin } from '@/lib/auth'

// ─── Types ───────────────────────────────────────────────────────────────────

interface UserBinding {
  id: string
  role: string     // admin | operator | viewer | secrets-manager
  serverId: string // '' = global
  stackId: string  // '' = all stacks
}

interface User {
  id: string
  username: string
  displayName: string
  disabled: boolean
  roles: string[]
  hasPassword: boolean
  createdAt: number
  lastLoginAt: number
  bindings: UserBinding[]
}

interface Server {
  id: string
  name: string
  assignments: { stackId: string }[]
}

interface Stack {
  id: string
  name: string
}

// ─── Matrix state ─────────────────────────────────────────────────────────────

interface MatrixRow {
  serverId: string
  serverName: string
  role: '' | 'viewer' | 'operator'
  stackIds: string[]  // empty = no restriction (all stacks)
  expanded: boolean
  availableStacks: { id: string; name: string }[]
}

interface MatrixState {
  admin: boolean
  secretsManager: boolean
  rows: MatrixRow[]  // rows[0] is always the global row (serverId='')
}

// ─── Helper: build matrix from existing bindings ──────────────────────────────

function buildMatrix(bindings: UserBinding[], servers: Server[], stacks: Stack[]): MatrixState {
  const stackById = new Map(stacks.map(s => [s.id, s]))

  const admin = bindings.some(b => b.role === 'admin')
  const secretsManager = bindings.some(b => b.role === 'secrets-manager')

  function serverRole(serverId: string): '' | 'viewer' | 'operator' {
    const relevant = bindings.filter(b =>
      b.serverId === serverId && (b.role === 'viewer' || b.role === 'operator')
    )
    if (relevant.some(b => b.role === 'operator')) return 'operator'
    if (relevant.some(b => b.role === 'viewer')) return 'viewer'
    return ''
  }

  function stackIds(serverId: string, role: string): string[] {
    if (!role) return []
    if (bindings.some(b => b.serverId === serverId && b.role === role && b.stackId === ''))
      return []
    return bindings
      .filter(b => b.serverId === serverId && b.role === role && b.stackId !== '')
      .map(b => b.stackId)
  }

  const globalRole = serverRole('')
  const rows: MatrixRow[] = [
    {
      serverId: '',
      serverName: 'Global (all servers)',
      role: globalRole,
      stackIds: [],
      expanded: false,
      availableStacks: [],
    },
  ]

  for (const server of servers) {
    const role = serverRole(server.id)
    const ids = stackIds(server.id, role)
    const availableStacks = server.assignments
      .map(a => stackById.get(a.stackId))
      .filter((s): s is Stack => s !== undefined)
      .map(s => ({ id: s.id, name: s.name }))

    rows.push({
      serverId: server.id,
      serverName: server.name,
      role,
      stackIds: ids,
      expanded: ids.length > 0,
      availableStacks,
    })
  }

  return { admin, secretsManager, rows }
}

// ─── Helper: matrix → desired bindings (no IDs) ───────────────────────────────

type DesiredBinding = Omit<UserBinding, 'id'>

function matrixToBindings(matrix: MatrixState): DesiredBinding[] {
  const result: DesiredBinding[] = []
  if (matrix.admin) result.push({ role: 'admin', serverId: '', stackId: '' })
  if (matrix.secretsManager) result.push({ role: 'secrets-manager', serverId: '', stackId: '' })

  for (const row of matrix.rows) {
    if (!row.role) continue
    if (row.stackIds.length === 0) {
      result.push({ role: row.role, serverId: row.serverId, stackId: '' })
    } else {
      for (const stackId of row.stackIds) {
        result.push({ role: row.role, serverId: row.serverId, stackId })
      }
    }
  }
  return result
}

// ─── UsersPage ────────────────────────────────────────────────────────────────

export function UsersPage() {
  const { user: me } = useAuth()
  const [users, setUsers]     = useState<User[]>([])
  const [servers, setServers] = useState<Server[]>([])
  const [stacks, setStacks]   = useState<Stack[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError]     = useState<string | null>(null)
  const [showCreate, setShowCreate]   = useState(false)
  const [editPerms, setEditPerms]     = useState<User | null>(null)
  const [form, setForm] = useState({ username: '', displayName: '', password: '' })
  const [busy, setBusy]           = useState(false)
  const [formError, setFormError] = useState<string | null>(null)

  const admin = isAdmin(me)

  async function load() {
    setLoading(true)
    setError(null)
    try {
      const [uRes, sRes, stRes] = await Promise.all([
        fetch('/orkestra.v1.AuthService/ListUsers', {
          method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}',
        }),
        fetch('/orkestra.v1.StackService/ListServers', {
          method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}',
        }),
        fetch('/orkestra.v1.StackService/ListStacks', {
          method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}',
        }),
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
          bindings:    Array.isArray(u.bindings) ? u.bindings.map((b: Record<string, unknown>) => ({
            id:       String(b.id ?? ''),
            role:     String(b.role ?? ''),
            serverId: String(b.serverId ?? b.server_id ?? ''),
            stackId:  String(b.stackId ?? b.stack_id ?? ''),
          })) : [],
        })))
      }
      if (sRes.ok) {
        const d = await sRes.json()
        setServers((d.servers ?? []).map((s: Record<string, unknown>) => ({
          id:   String(s.id ?? ''),
          name: String(s.name ?? ''),
          assignments: Array.isArray(s.assignments)
            ? s.assignments.map((a: Record<string, unknown>) => ({
                stackId: String(a.stackId ?? a.stack_id ?? ''),
              }))
            : [],
        })))
      }
      if (stRes.ok) {
        const d = await stRes.json()
        setStacks((d.stacks ?? []).map((s: Record<string, unknown>) => ({
          id:   String(s.id ?? ''),
          name: String(s.name ?? ''),
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
            {loading && (
              <tr><td colSpan={6} className="px-4 py-8 text-center" style={{ color: 'var(--text-muted)' }}>Loading…</td></tr>
            )}
            {!loading && users.length === 0 && (
              <tr><td colSpan={6} className="px-4 py-8 text-center" style={{ color: 'var(--text-muted)' }}>No users yet.</td></tr>
            )}
            {users.map(u => (
              <tr key={u.id} className="hover:bg-[var(--surface-2)]" style={{ borderBottom: '1px solid var(--border)' }}>
                <td className="px-4 py-3 font-medium" style={{ color: 'var(--text)' }}>{u.username}</td>
                <td className="px-4 py-3 text-xs" style={{ color: 'var(--text-muted)' }}>{u.displayName || '—'}</td>
                <td className="px-4 py-3">
                  <div className="flex flex-wrap gap-1">
                    {u.roles.map(r => (
                      <Badge key={r} variant={r === 'admin' ? 'warn' : 'default'}>{r}</Badge>
                    ))}
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
                    <button
                      onClick={() => setEditPerms(u)}
                      className="p-1 rounded hover:bg-[var(--surface-2)]"
                      style={{ color: 'var(--text-muted)' }}
                      title="Manage permissions"
                    >
                      <Shield size={14} />
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {/* Create user dialog */}
      {showCreate && (
        <Modal title="New User" onClose={() => { setShowCreate(false); setFormError(null) }}>
          {formError && <ErrorBar>{formError}</ErrorBar>}
          <div className="space-y-3">
            <Field label="Username *">
              <input
                value={form.username}
                onChange={e => setForm(f => ({ ...f, username: e.target.value }))}
                className={inputCls} style={inputStyle} placeholder="jsmith"
              />
            </Field>
            <Field label="Display name">
              <input
                value={form.displayName}
                onChange={e => setForm(f => ({ ...f, displayName: e.target.value }))}
                className={inputCls} style={inputStyle} placeholder="John Smith"
              />
            </Field>
            <Field label="Password *">
              <input
                type="password"
                value={form.password}
                onChange={e => setForm(f => ({ ...f, password: e.target.value }))}
                className={inputCls} style={inputStyle}
              />
            </Field>
          </div>
          <div className="flex justify-end gap-2 mt-4">
            <button
              onClick={() => { setShowCreate(false); setFormError(null) }}
              className="px-4 py-1.5 rounded border text-sm"
              style={{ borderColor: 'var(--border)', color: 'var(--text-muted)' }}
            >
              Cancel
            </button>
            <button
              onClick={createUser} disabled={busy}
              className="px-4 py-1.5 rounded text-sm font-medium disabled:opacity-50"
              style={{ backgroundColor: 'var(--accent)', color: '#0d1117' }}
            >
              {busy ? 'Creating…' : 'Create User'}
            </button>
          </div>
        </Modal>
      )}

      {/* Permissions matrix modal */}
      {editPerms && (
        <PermissionsMatrix
          user={editPerms}
          servers={servers}
          stacks={stacks}
          onClose={() => setEditPerms(null)}
          onSaved={() => { setEditPerms(null); load() }}
        />
      )}
    </div>
  )
}

// ─── PermissionsMatrix ────────────────────────────────────────────────────────

function PermissionsMatrix({
  user,
  servers,
  stacks,
  onClose,
  onSaved,
}: {
  user: User
  servers: Server[]
  stacks: Stack[]
  onClose: () => void
  onSaved: () => void
}) {
  const [matrix, setMatrix] = useState<MatrixState>(() =>
    buildMatrix(user.bindings, servers, stacks)
  )
  const [busy, setBusy]   = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function save() {
    setBusy(true)
    setError(null)
    try {
      const desired = matrixToBindings(matrix)
      const current = user.bindings.filter(b =>
        b.role === 'admin' || b.role === 'secrets-manager' ||
        b.role === 'viewer' || b.role === 'operator'
      )

      const toAdd = desired.filter(d =>
        !current.some(c => c.role === d.role && c.serverId === d.serverId && c.stackId === d.stackId)
      )
      const toRemove = current.filter(c =>
        !desired.some(d => d.role === c.role && d.serverId === c.serverId && d.stackId === c.stackId)
      )

      await Promise.all([
        ...toAdd.map(b =>
          fetch('/orkestra.v1.AuthService/AssignRole', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ userId: user.id, role: b.role, serverId: b.serverId, stackId: b.stackId }),
          }).then(r => { if (!r.ok) return r.text().then(t => { throw new Error(t) }) })
        ),
        ...toRemove.map(b =>
          fetch('/orkestra.v1.AuthService/RevokeRole', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ bindingId: b.id }),
          }).then(r => { if (!r.ok) return r.text().then(t => { throw new Error(t) }) })
        ),
      ])

      onSaved()
    } catch (e) {
      setError(String(e))
    } finally {
      setBusy(false)
    }
  }

  function patchRow(idx: number, patch: Partial<MatrixRow>) {
    setMatrix(m => ({
      ...m,
      rows: m.rows.map((r, i) => i === idx ? { ...r, ...patch } : r),
    }))
  }

  return (
    <div
      className="fixed inset-0 flex items-center justify-center z-50 p-4"
      style={{ backgroundColor: 'rgba(0,0,0,0.6)' }}
    >
      <div
        className="w-full max-w-3xl rounded-lg border flex flex-col"
        style={{
          backgroundColor: 'var(--surface)',
          borderColor: 'var(--border)',
          maxHeight: '85vh',
        }}
      >
        {/* Header */}
        <div
          className="flex items-center justify-between px-6 py-4 border-b shrink-0"
          style={{ borderColor: 'var(--border)' }}
        >
          <div>
            <h2 className="font-semibold text-base" style={{ color: 'var(--text)' }}>
              Permissions — {user.username}
            </h2>
            <p className="text-xs mt-0.5" style={{ color: 'var(--text-muted)' }}>
              Configure role-based access for this user
            </p>
          </div>
          <button onClick={onClose} style={{ color: 'var(--text-muted)' }}><X size={18} /></button>
        </div>

        {/* Scrollable content */}
        <div className="flex-1 overflow-y-auto px-6 py-4 space-y-6">
          {error && <ErrorBar>{error}</ErrorBar>}

          {/* Global special roles */}
          <div>
            <p className="text-xs font-medium mb-3 uppercase tracking-wide" style={{ color: 'var(--text-muted)' }}>
              Global Roles
            </p>
            <div className="flex gap-8">
              <Toggle
                checked={matrix.admin}
                onChange={v => setMatrix(m => ({ ...m, admin: v }))}
                label="Admin"
                description="Full system access"
                warn
              />
              <Toggle
                checked={matrix.secretsManager}
                onChange={v => setMatrix(m => ({ ...m, secretsManager: v }))}
                label="Secrets Manager"
                description="Create, edit, reveal and delete secrets"
              />
            </div>
          </div>

          {/* Access matrix */}
          <div>
            <p className="text-xs font-medium mb-3 uppercase tracking-wide" style={{ color: 'var(--text-muted)' }}>
              Access Matrix
            </p>
            <div className="rounded-lg border overflow-hidden" style={{ borderColor: 'var(--border)' }}>
              <table className="w-full text-sm">
                <thead>
                  <tr style={{ borderBottom: '1px solid var(--border)', backgroundColor: 'var(--surface-2)' }}>
                    <th className="text-left px-4 py-2.5 text-xs font-medium w-1/4" style={{ color: 'var(--text-muted)' }}>Scope</th>
                    <th className="text-left px-4 py-2.5 text-xs font-medium w-2/5" style={{ color: 'var(--text-muted)' }}>Access level</th>
                    <th className="text-left px-4 py-2.5 text-xs font-medium" style={{ color: 'var(--text-muted)' }}>Stack restriction</th>
                  </tr>
                </thead>
                <tbody>
                  {matrix.rows.map((row, idx) => (
                    <MatrixRowComp
                      key={row.serverId || '__global__'}
                      row={row}
                      isGlobal={idx === 0}
                      onChange={patch => patchRow(idx, patch)}
                    />
                  ))}
                </tbody>
              </table>
            </div>
            {servers.length === 0 && (
              <p className="text-xs mt-2" style={{ color: 'var(--text-muted)' }}>
                No servers enrolled yet. Global access applies to all future servers.
              </p>
            )}
          </div>
        </div>

        {/* Footer */}
        <div
          className="flex justify-end gap-2 px-6 py-4 border-t shrink-0"
          style={{ borderColor: 'var(--border)' }}
        >
          <button
            onClick={onClose}
            className="px-4 py-1.5 rounded border text-sm"
            style={{ borderColor: 'var(--border)', color: 'var(--text-muted)' }}
          >
            Cancel
          </button>
          <button
            onClick={save}
            disabled={busy}
            className="px-4 py-1.5 rounded text-sm font-medium disabled:opacity-50"
            style={{ backgroundColor: 'var(--accent)', color: '#0d1117' }}
          >
            {busy ? 'Saving…' : 'Save permissions'}
          </button>
        </div>
      </div>
    </div>
  )
}

// ─── Matrix row ───────────────────────────────────────────────────────────────

function MatrixRowComp({
  row,
  isGlobal,
  onChange,
}: {
  row: MatrixRow
  isGlobal: boolean
  onChange: (patch: Partial<MatrixRow>) => void
}) {
  const hasAccess  = row.role !== ''
  const hasStacks  = row.availableStacks.length > 0
  const canRestrict = hasAccess && !isGlobal && hasStacks

  return (
    <>
      <tr
        className="hover:bg-[var(--surface-2)]"
        style={{ borderBottom: row.expanded && canRestrict ? undefined : '1px solid var(--border)' }}
      >
        {/* Scope label */}
        <td className="px-4 py-3 text-sm" style={{ color: 'var(--text)' }}>
          {isGlobal ? (
            <span className="flex items-center gap-2">
              <span
                className="text-xs px-1.5 py-0.5 rounded font-medium"
                style={{ backgroundColor: 'var(--surface-2)', color: 'var(--text-muted)', border: '1px solid var(--border)' }}
              >
                Global
              </span>
            </span>
          ) : (
            row.serverName
          )}
        </td>

        {/* Radio: None / Viewer / Operator */}
        <td className="px-4 py-3">
          <div className="flex gap-4">
            {(['', 'viewer', 'operator'] as const).map(r => (
              <label key={r || 'none'} className="flex items-center gap-1.5 cursor-pointer select-none">
                <input
                  type="radio"
                  name={`role-${row.serverId || 'global'}`}
                  checked={row.role === r}
                  onChange={() => onChange({ role: r, stackIds: [], expanded: false })}
                  className="accent-[var(--accent)]"
                />
                <span
                  className="text-xs"
                  style={{ color: row.role === r ? 'var(--text)' : 'var(--text-muted)' }}
                >
                  {r === '' ? 'None' : r.charAt(0).toUpperCase() + r.slice(1)}
                </span>
              </label>
            ))}
          </div>
        </td>

        {/* Stack restriction toggle */}
        <td className="px-4 py-3">
          {canRestrict ? (
            <button
              onClick={() => onChange({ expanded: !row.expanded })}
              className="flex items-center gap-1 text-xs transition-colors"
              style={{ color: row.stackIds.length > 0 ? 'var(--accent)' : 'var(--text-muted)' }}
            >
              {row.expanded ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
              {row.stackIds.length === 0
                ? 'All stacks'
                : `${row.stackIds.length} stack${row.stackIds.length > 1 ? 's' : ''}`}
            </button>
          ) : (
            <span className="text-xs" style={{ color: 'var(--text-muted)' }}>—</span>
          )}
        </td>
      </tr>

      {/* Expanded stack picker */}
      {row.expanded && canRestrict && (
        <tr style={{ borderBottom: '1px solid var(--border)' }}>
          <td />
          <td colSpan={2} className="px-4 pb-3 pt-1">
            <div className="flex flex-wrap gap-2">
              {row.availableStacks.map(stack => {
                const selected = row.stackIds.includes(stack.id)
                return (
                  <button
                    key={stack.id}
                    onClick={() => {
                      const next = selected
                        ? row.stackIds.filter(id => id !== stack.id)
                        : [...row.stackIds, stack.id]
                      onChange({ stackIds: next })
                    }}
                    className="flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs border transition-colors"
                    style={{
                      borderColor:     selected ? 'var(--accent)' : 'var(--border)',
                      color:           selected ? 'var(--accent)' : 'var(--text-muted)',
                      backgroundColor: selected ? 'rgba(126,226,42,0.08)' : 'transparent',
                    }}
                  >
                    {selected && <Check size={10} />}
                    {stack.name}
                  </button>
                )
              })}
            </div>
            <p className="text-xs mt-2" style={{ color: 'var(--text-muted)' }}>
              {row.stackIds.length === 0
                ? 'No stacks selected — access applies to all stacks on this server'
                : `Access restricted to ${row.stackIds.length} selected stack${row.stackIds.length > 1 ? 's' : ''}`}
            </p>
          </td>
        </tr>
      )}
    </>
  )
}

// ─── Toggle ───────────────────────────────────────────────────────────────────

function Toggle({
  checked,
  onChange,
  label,
  description,
  warn,
}: {
  checked: boolean
  onChange: (v: boolean) => void
  label: string
  description: string
  warn?: boolean
}) {
  return (
    <label className="flex items-start gap-3 cursor-pointer select-none">
      <input
        type="checkbox"
        checked={checked}
        onChange={e => onChange(e.target.checked)}
        className="mt-0.5 accent-[var(--accent)]"
      />
      <div>
        <div
          className="text-sm font-medium"
          style={{ color: warn ? 'var(--warn)' : 'var(--text)' }}
        >
          {label}
        </div>
        <div className="text-xs" style={{ color: 'var(--text-muted)' }}>{description}</div>
      </div>
    </label>
  )
}

// ─── Shared small components ──────────────────────────────────────────────────

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
