import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { Server, Layers, Activity } from 'lucide-react'
import { StatusDot, Badge } from '@/components/ui/badge'

interface ServerSummary {
  id: string
  name: string
  hostname: string
  status: string
  lastSeenAt: number
}

export function DashboardPage() {
  const [servers, setServers] = useState<ServerSummary[]>([])

  useEffect(() => {
    fetch('/orkestra.v1.StackService/ListServers', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({}),
    })
      .then(r => r.json())
      .then(d => setServers((d.servers ?? []).map((s: Record<string, unknown>) => ({
        id:         String(s.id ?? ''),
        name:       String(s.name ?? ''),
        hostname:   String(s.hostname ?? ''),
        status:     String(s.status ?? 'offline'),
        lastSeenAt: Number(s.lastSeenAt ?? s.last_seen_at ?? 0),
      }))))
      .catch(() => {})
  }, [])

  const online = servers.filter(s => s.status === 'online').length
  const offline = servers.length - online

  const stats = [
    { label: 'Servers',    value: servers.length, icon: Server,    color: 'var(--accent)' },
    { label: 'Online',     value: online,          icon: Activity,  color: 'var(--online)' },
    { label: 'Offline',    value: offline,         icon: Server,    color: 'var(--text-muted)' },
    { label: 'Stacks',     value: 0,               icon: Layers,    color: 'var(--accent)' },
  ]

  return (
    <div>
      <h1 className="text-xl font-semibold mb-1" style={{ color: 'var(--text)' }}>Dashboard</h1>
      <p className="mb-6" style={{ color: 'var(--text-muted)' }}>Fleet overview</p>

      {/* Stats bar */}
      <div className="grid grid-cols-4 gap-4 mb-8">
        {stats.map(({ label, value, icon: Icon, color }) => (
          <div key={label} className="rounded-lg border p-4 flex items-center gap-4"
            style={{ borderColor: 'var(--border)', backgroundColor: 'var(--surface)' }}
          >
            <div className="w-9 h-9 rounded flex items-center justify-center"
              style={{ backgroundColor: 'var(--surface-2)' }}>
              <Icon size={18} style={{ color }} />
            </div>
            <div>
              <p className="text-2xl font-bold" style={{ color: 'var(--text)' }}>{value}</p>
              <p className="text-xs" style={{ color: 'var(--text-muted)' }}>{label}</p>
            </div>
          </div>
        ))}
      </div>

      {/* Server list */}
      <h2 className="font-semibold mb-3" style={{ color: 'var(--text)' }}>Servers</h2>
      <div className="rounded-lg border overflow-hidden" style={{ borderColor: 'var(--border)', backgroundColor: 'var(--surface)' }}>
        <table className="w-full text-sm">
          <thead>
            <tr style={{ borderBottom: '1px solid var(--border)' }}>
              {['Name', 'Status', 'Last seen'].map(h => (
                <th key={h} className="text-left px-4 py-3 text-xs font-medium" style={{ color: 'var(--text-muted)' }}>{h}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {servers.length === 0 && (
              <tr>
                <td colSpan={3} className="px-4 py-8 text-center" style={{ color: 'var(--text-muted)' }}>
                  No servers enrolled. Use <code className="px-1 rounded" style={{ backgroundColor: 'var(--surface-2)' }}>orkestra-agent enroll</code> to add one.
                </td>
              </tr>
            )}
            {servers.map(s => (
              <tr key={s.id} className="hover:bg-[var(--surface-2)]" style={{ borderBottom: '1px solid var(--border)' }}>
                <td className="px-4 py-3">
                  <Link to={`/servers/${s.id}`} className="font-medium hover:underline" style={{ color: 'var(--text)' }}>
                    {s.name || s.hostname}
                  </Link>
                </td>
                <td className="px-4 py-3">
                  <Badge variant={s.status === 'online' ? 'online' : 'offline'}>
                    <StatusDot online={s.status === 'online'} />
                    {s.status === 'online' ? 'Online' : 'Offline'}
                  </Badge>
                </td>
                <td className="px-4 py-3 text-xs" style={{ color: 'var(--text-muted)' }}>
                  {s.lastSeenAt ? new Date(s.lastSeenAt).toLocaleString() : '—'}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}
