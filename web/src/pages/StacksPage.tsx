import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { Plus, RefreshCw, X } from 'lucide-react'
import { Badge } from '@/components/ui/badge'

interface Stack {
  id: string
  name: string
  description: string
  version: number
  createdAt: number
}

interface CreateForm {
  name: string
  description: string
  composeYaml: string
}

export function StacksPage() {
  const [stacks, setStacks] = useState<Stack[]>([])
  const [loading, setLoading] = useState(true)
  const [showCreate, setShowCreate] = useState(false)
  const [form, setForm] = useState<CreateForm>({ name: '', description: '', composeYaml: '' })
  const [creating, setCreating] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function load() {
    setLoading(true)
    try {
      const res = await fetch('/orkestra.v1.StackService/ListStacks', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({}),
      })
      if (res.ok) {
        const data = await res.json()
        setStacks((data.stacks ?? []).map((s: Record<string, unknown>) => ({
          id:          String(s.id ?? ''),
          name:        String(s.name ?? ''),
          description: String(s.description ?? ''),
          version:     Number(s.version ?? 0),
          createdAt:   Number(s.createdAt ?? s.created_at ?? 0),
        })))
      }
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() }, [])

  async function createStack() {
    if (!form.name) { setError('Name is required'); return }
    setCreating(true)
    setError(null)
    try {
      const res = await fetch('/orkestra.v1.StackService/CreateStack', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          name: form.name,
          description: form.description,
          composeYaml: form.composeYaml,
        }),
      })
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      setShowCreate(false)
      setForm({ name: '', description: '', composeYaml: '' })
      load()
    } catch (e) {
      setError(String(e))
    } finally {
      setCreating(false)
    }
  }

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-xl font-semibold" style={{ color: 'var(--text)' }}>Stacks</h1>
          <p style={{ color: 'var(--text-muted)' }}>Compose stack definitions and deployments</p>
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
            <Plus size={14} /> New Stack
          </button>
        </div>
      </div>

      {/* Table */}
      <div className="rounded-lg border overflow-hidden" style={{ borderColor: 'var(--border)', backgroundColor: 'var(--surface)' }}>
        <table className="w-full text-sm">
          <thead>
            <tr style={{ borderBottom: '1px solid var(--border)' }}>
              {['Name', 'Description', 'Latest version', 'Created'].map(h => (
                <th key={h} className="text-left px-4 py-3 text-xs font-medium" style={{ color: 'var(--text-muted)' }}>{h}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {loading && <tr><td colSpan={4} className="px-4 py-8 text-center" style={{ color: 'var(--text-muted)' }}>Loading…</td></tr>}
            {!loading && stacks.length === 0 && (
              <tr><td colSpan={4} className="px-4 py-8 text-center" style={{ color: 'var(--text-muted)' }}>
                No stacks yet. Create one to start deploying.
              </td></tr>
            )}
            {stacks.map(s => (
              <tr key={s.id} className="hover:bg-[var(--surface-2)]" style={{ borderBottom: '1px solid var(--border)' }}>
                <td className="px-4 py-3 font-medium">
                  <Link to={`/stacks/${s.id}`} className="hover:underline" style={{ color: 'var(--text)' }}>{s.name}</Link>
                </td>
                <td className="px-4 py-3 text-xs" style={{ color: 'var(--text-muted)' }}>{s.description || '—'}</td>
                <td className="px-4 py-3">
                  {s.version > 0 ? <Badge>v{s.version}</Badge> : <span style={{ color: 'var(--text-muted)' }}>—</span>}
                </td>
                <td className="px-4 py-3 text-xs" style={{ color: 'var(--text-muted)' }}>
                  {s.createdAt ? new Date(s.createdAt).toLocaleDateString() : '—'}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {/* Create dialog */}
      {showCreate && (
        <div className="fixed inset-0 flex items-center justify-center z-50" style={{ backgroundColor: 'rgba(0,0,0,0.6)' }}>
          <div className="w-full max-w-lg rounded-lg border p-6 relative" style={{ backgroundColor: 'var(--surface)', borderColor: 'var(--border)' }}>
            <div className="flex items-center justify-between mb-4">
              <h2 className="font-semibold text-base" style={{ color: 'var(--text)' }}>New Stack</h2>
              <button onClick={() => setShowCreate(false)} style={{ color: 'var(--text-muted)' }}><X size={18} /></button>
            </div>

            {error && <div className="mb-3 px-3 py-2 rounded text-sm" style={{ backgroundColor: '#2d1115', color: 'var(--error)', border: '1px solid #4a1a1f' }}>{error}</div>}

            <div className="space-y-3">
              <Field label="Name *">
                <input value={form.name} onChange={e => setForm(f => ({ ...f, name: e.target.value }))}
                  className="w-full px-3 py-1.5 rounded border text-sm outline-none focus:border-[var(--accent)]"
                  style={{ backgroundColor: 'var(--bg)', borderColor: 'var(--border)', color: 'var(--text)' }}
                  placeholder="my-app" />
              </Field>
              <Field label="Description">
                <input value={form.description} onChange={e => setForm(f => ({ ...f, description: e.target.value }))}
                  className="w-full px-3 py-1.5 rounded border text-sm outline-none focus:border-[var(--accent)]"
                  style={{ backgroundColor: 'var(--bg)', borderColor: 'var(--border)', color: 'var(--text)' }}
                  placeholder="Optional description" />
              </Field>
              <Field label="compose.yaml">
                <textarea value={form.composeYaml} onChange={e => setForm(f => ({ ...f, composeYaml: e.target.value }))}
                  rows={10}
                  className="w-full px-3 py-2 rounded border text-xs font-mono outline-none focus:border-[var(--accent)] resize-none"
                  style={{ backgroundColor: 'var(--bg)', borderColor: 'var(--border)', color: 'var(--text)' }}
                  placeholder={'services:\n  web:\n    image: nginx:alpine\n    ports:\n      - "80:80"'} />
              </Field>
            </div>

            <div className="flex justify-end gap-2 mt-4">
              <button onClick={() => setShowCreate(false)}
                className="px-4 py-1.5 rounded border text-sm"
                style={{ borderColor: 'var(--border)', color: 'var(--text-muted)' }}>
                Cancel
              </button>
              <button onClick={createStack} disabled={creating}
                className="px-4 py-1.5 rounded text-sm font-medium disabled:opacity-50"
                style={{ backgroundColor: 'var(--accent)', color: '#0d1117' }}>
                {creating ? 'Creating…' : 'Create Stack'}
              </button>
            </div>
          </div>
        </div>
      )}
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
