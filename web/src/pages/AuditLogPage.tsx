import { useQuery } from '@tanstack/react-query'
import { RefreshCw } from 'lucide-react'

interface AuditEntry {
  id: number
  ts: number
  actorId: string
  actorName: string
  action: string
  targetType: string
  targetId: string
  ipAddress: string
  error: string
}

export function AuditLogPage() {
  const { data: entries = [], isPending: loading, error, refetch } = useQuery({
    queryKey: ['audit'],
    queryFn: async (): Promise<AuditEntry[]> => {
      const res = await fetch('/api/audit')
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const data = await res.json()
      return (data.entries ?? []).map((e: Record<string, unknown>) => ({
        id:         Number(e.id ?? 0),
        ts:         Number(e.ts ?? 0),
        actorId:    String(e.actorId ?? e.actor_id ?? ''),
        actorName:  String(e.actorName ?? e.actor_name ?? ''),
        action:     String(e.action ?? ''),
        targetType: String(e.targetType ?? e.target_type ?? ''),
        targetId:   String(e.targetId ?? e.target_id ?? ''),
        ipAddress:  String(e.ipAddress ?? e.ip_address ?? ''),
        error:      String(e.error ?? ''),
      }))
    },
  })

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-xl font-semibold" style={{ color: 'var(--text)' }}>Audit Log</h1>
          <p style={{ color: 'var(--text-muted)' }}>All privileged actions and authentication events</p>
        </div>
        <button onClick={() => refetch()}
          className="flex items-center gap-2 px-3 py-1.5 rounded text-sm border transition-colors hover:bg-[var(--surface-2)]"
          style={{ borderColor: 'var(--border)', color: 'var(--text-muted)' }}>
          <RefreshCw size={14} /> Refresh
        </button>
      </div>

      {error && (
        <div className="mb-4 px-4 py-3 rounded text-sm" style={{ backgroundColor: '#2d1115', color: 'var(--error)', border: '1px solid #4a1a1f' }}>
          {String(error)}
        </div>
      )}

      <div className="rounded-lg border overflow-hidden" style={{ borderColor: 'var(--border)', backgroundColor: 'var(--surface)' }}>
        <table className="w-full text-sm">
          <thead>
            <tr style={{ borderBottom: '1px solid var(--border)' }}>
              {['Time', 'Actor', 'Action', 'Target', 'IP', 'Error'].map(h => (
                <th key={h} className="text-left px-4 py-3 text-xs font-medium" style={{ color: 'var(--text-muted)' }}>{h}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {loading && <tr><td colSpan={6} className="px-4 py-8 text-center" style={{ color: 'var(--text-muted)' }}>Loading…</td></tr>}
            {!loading && entries.length === 0 && (
              <tr><td colSpan={6} className="px-4 py-8 text-center" style={{ color: 'var(--text-muted)' }}>No audit entries yet.</td></tr>
            )}
            {entries.map((e, i) => (
              <tr key={`${e.id}-${i}`} className="hover:bg-[var(--surface-2)]" style={{ borderBottom: '1px solid var(--border)' }}>
                <td className="px-4 py-2 text-xs font-mono" style={{ color: 'var(--text-muted)', whiteSpace: 'nowrap' }}>
                  {e.ts ? new Date(e.ts).toLocaleString() : '—'}
                </td>
                <td className="px-4 py-2 text-xs" style={{ color: 'var(--text)' }}>{e.actorName || e.actorId || '—'}</td>
                <td className="px-4 py-2 text-xs font-mono" style={{ color: 'var(--accent)' }}>{e.action}</td>
                <td className="px-4 py-2 text-xs" style={{ color: 'var(--text-muted)' }}>
                  {e.targetType}{e.targetId ? `:${e.targetId.slice(0, 8)}` : ''}
                </td>
                <td className="px-4 py-2 text-xs" style={{ color: 'var(--text-muted)' }}>{e.ipAddress || '—'}</td>
                <td className="px-4 py-2 text-xs" style={{ color: e.error ? 'var(--error)' : 'var(--text-muted)' }}>
                  {e.error || '—'}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}
