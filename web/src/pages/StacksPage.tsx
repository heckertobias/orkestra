import { useQuery } from '@tanstack/react-query'
import { Link, useNavigate } from 'react-router-dom'
import { Plus, RefreshCw } from 'lucide-react'
import { Badge } from '@/components/ui/badge'

interface Stack {
  id: string
  name: string
  description: string
  version: number
  createdAt: number
}

export function StacksPage() {
  const navigate = useNavigate()
  const { data: stacks = [], isPending: loading, refetch } = useQuery({
    queryKey: ['stacks'],
    queryFn: async (): Promise<Stack[]> => {
      const res = await fetch('/orkestra.v1.StackService/ListStacks', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({}),
      })
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const data = await res.json()
      return (data.stacks ?? []).map((s: Record<string, unknown>) => ({
        id:          String(s.id ?? ''),
        name:        String(s.name ?? ''),
        description: String(s.description ?? ''),
        version:     Number(s.version ?? 0),
        createdAt:   Number(s.createdAt ?? s.created_at ?? 0),
      }))
    },
  })

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-xl font-semibold" style={{ color: 'var(--text)' }}>Stacks</h1>
          <p style={{ color: 'var(--text-muted)' }}>Compose stack definitions and deployments</p>
        </div>
        <div className="flex gap-2">
          <button onClick={() => refetch()}
            className="flex items-center gap-2 px-3 py-1.5 rounded text-sm border transition-colors hover:bg-[var(--surface-2)]"
            style={{ borderColor: 'var(--border)', color: 'var(--text-muted)' }}>
            <RefreshCw size={14} /> Refresh
          </button>
          <button
            onClick={() => navigate('/stacks/new')}
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
                No stacks yet. <button onClick={() => navigate('/stacks/new')} className="underline" style={{ color: 'var(--accent)' }}>Create one</button> to start deploying.
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
    </div>
  )
}
