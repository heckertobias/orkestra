import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { RefreshCw, Plus } from 'lucide-react'
import { Badge, StatusDot } from '@/components/ui/badge'
import { AddServerDialog } from '@/components/AddServerDialog'

interface Server {
  id: string
  name: string
  hostname: string
  arch: string
  os: string
  agentVersion: string
  dockerVersion: string
  status: string
  lastSeenAt: number
  enrolledAt: number
}

function timeAgo(ms: number): string {
  if (!ms) return 'never'
  const diff = Date.now() - ms
  if (diff < 60_000)  return 'just now'
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m ago`
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h ago`
  return `${Math.floor(diff / 86_400_000)}d ago`
}

export function ServersPage() {
  const [showAdd, setShowAdd] = useState(false)

  const { data: servers = [], isPending: loading, error, refetch } = useQuery({
    queryKey: ['servers'],
    queryFn: async (): Promise<Server[]> => {
      // Use fetch against the Connect JSON API until gen/ is built in CI.
      const res = await fetch('/orkestra.v1.StackService/ListServers', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({}),
      })
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const data = await res.json()
      return (data.servers ?? []).map((s: Record<string, unknown>) => ({
        id:            String(s.id ?? ''),
        name:          String(s.name ?? ''),
        hostname:      String(s.hostname ?? ''),
        arch:          String(s.arch ?? ''),
        os:            String(s.os ?? ''),
        agentVersion:  String(s.agentVersion ?? s.agent_version ?? ''),
        dockerVersion: String(s.dockerVersion ?? s.docker_version ?? ''),
        status:        String(s.status ?? 'offline'),
        lastSeenAt:    Number(s.lastSeenAt ?? s.last_seen_at ?? 0),
        enrolledAt:    Number(s.enrolledAt ?? s.enrolled_at ?? 0),
      }))
    },
  })

  return (
    <div>
      {/* Header */}
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-xl font-semibold" style={{ color: 'var(--text)' }}>Servers</h1>
          <p style={{ color: 'var(--text-muted)' }}>Enrolled agents and their connection status</p>
        </div>
        <div className="flex gap-2">
          <button
            onClick={() => refetch()}
            className="flex items-center gap-2 px-3 py-1.5 rounded text-sm border transition-colors hover:bg-[var(--surface-2)]"
            style={{ borderColor: 'var(--border)', color: 'var(--text-muted)' }}
          >
            <RefreshCw size={14} />
            Refresh
          </button>
          <button
            onClick={() => setShowAdd(true)}
            className="flex items-center gap-2 px-3 py-1.5 rounded text-sm font-medium"
            style={{ backgroundColor: 'var(--accent)', color: '#0d1117' }}
          >
            <Plus size={14} />
            Add Server
          </button>
        </div>
      </div>

      {/* Error */}
      {error && (
        <div className="mb-4 px-4 py-3 rounded text-sm" style={{ backgroundColor: '#2d1115', color: 'var(--error)', border: '1px solid #4a1a1f' }}>
          {String(error)}
        </div>
      )}

      {/* Table */}
      <div className="rounded-lg border overflow-hidden" style={{ borderColor: 'var(--border)', backgroundColor: 'var(--surface)' }}>
        <table className="w-full text-sm">
          <thead>
            <tr style={{ borderBottom: `1px solid var(--border)` }}>
              {['Name', 'Hostname', 'Status', 'Arch', 'Agent', 'Docker', 'Last seen'].map(h => (
                <th key={h} className="text-left px-4 py-3 text-xs font-medium" style={{ color: 'var(--text-muted)' }}>
                  {h}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {loading && (
              <tr>
                <td colSpan={7} className="px-4 py-8 text-center" style={{ color: 'var(--text-muted)' }}>
                  Loading…
                </td>
              </tr>
            )}
            {!loading && servers.length === 0 && (
              <tr>
                <td colSpan={7} className="px-4 py-8 text-center" style={{ color: 'var(--text-muted)' }}>
                  No servers enrolled yet. Add a server to get started.
                </td>
              </tr>
            )}
            {servers.map(s => (
              <tr
                key={s.id}
                className="transition-colors hover:bg-[var(--surface-2)]"
                style={{ borderBottom: `1px solid var(--border)` }}
              >
                <td className="px-4 py-3 font-medium">
                  <Link to={`/servers/${s.id}`} style={{ color: 'var(--text)' }} className="hover:text-[var(--accent)]">
                    {s.name || s.hostname}
                  </Link>
                </td>
                <td className="px-4 py-3" style={{ color: 'var(--text-muted)' }}>{s.hostname}</td>
                <td className="px-4 py-3">
                  <Badge variant={s.status === 'online' ? 'online' : 'offline'}>
                    <StatusDot online={s.status === 'online'} />
                    {s.status === 'online' ? 'Online' : 'Offline'}
                  </Badge>
                </td>
                <td className="px-4 py-3" style={{ color: 'var(--text-muted)' }}>{s.arch}</td>
                <td className="px-4 py-3" style={{ color: 'var(--text-muted)' }}>{s.agentVersion || '—'}</td>
                <td className="px-4 py-3" style={{ color: 'var(--text-muted)' }}>{s.dockerVersion || '—'}</td>
                <td className="px-4 py-3" style={{ color: 'var(--text-muted)' }}>{timeAgo(s.lastSeenAt)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {showAdd && <AddServerDialog onClose={() => { setShowAdd(false); refetch() }} />}
    </div>
  )
}
