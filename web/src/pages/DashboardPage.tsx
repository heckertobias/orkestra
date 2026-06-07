import { useEffect, useState, useRef } from 'react'
import { Link } from 'react-router-dom'
import { Server, Layers, Activity, AlertCircle, Info, AlertTriangle } from 'lucide-react'
import { StatusDot, Badge } from '@/components/ui/badge'

interface ServerSummary {
  id: string
  name: string
  hostname: string
  status: string
  lastSeenAt: number
}

interface Event {
  id: number
  ts: number
  serverId: string
  stackId: string
  eventType: string
  severity: string
  message: string
}

function SeverityIcon({ severity }: { severity: string }) {
  if (severity === 'error') return <AlertCircle size={13} style={{ color: 'var(--error)', flexShrink: 0 }} />
  if (severity === 'warn')  return <AlertTriangle size={13} style={{ color: 'var(--warn)', flexShrink: 0 }} />
  return <Info size={13} style={{ color: 'var(--accent)', flexShrink: 0 }} />
}

export function DashboardPage() {
  const [servers, setServers] = useState<ServerSummary[]>([])
  const [stackCount, setStackCount] = useState(0)
  const [events, setEvents] = useState<Event[]>([])
  const [loadingServers, setLoadingServers] = useState(true)
  const eventsRef = useRef<AbortController | null>(null)

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
      .finally(() => setLoadingServers(false))

    fetch('/orkestra.v1.StackService/ListStacks', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({}),
    })
      .then(r => r.json())
      .then(d => setStackCount((d.stacks ?? []).length))
      .catch(() => {})
  }, [])

  // Streaming events via server-sent Connect stream
  useEffect(() => {
    const ac = new AbortController()
    eventsRef.current = ac

    ;(async () => {
      try {
        const res = await fetch('/orkestra.v1.StackService/StreamEvents', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({}),
          signal: ac.signal,
        })
        if (!res.body) return
        const reader = res.body.getReader()
        const decoder = new TextDecoder()
        let buf = ''
        while (true) {
          const { done, value } = await reader.read()
          if (done) break
          buf += decoder.decode(value, { stream: true })
          const lines = buf.split('\n')
          buf = lines.pop() ?? ''
          for (const line of lines) {
            const trimmed = line.trim()
            if (!trimmed || trimmed.startsWith('data: [DONE]')) continue
            const jsonStr = trimmed.startsWith('data: ') ? trimmed.slice(6) : trimmed
            try {
              const ev = JSON.parse(jsonStr)
              setEvents(prev => [{
                id:        Number(ev.id ?? 0),
                ts:        Number(ev.ts ?? 0),
                serverId:  String(ev.serverId ?? ev.server_id ?? ''),
                stackId:   String(ev.stackId ?? ev.stack_id ?? ''),
                eventType: String(ev.eventType ?? ev.event_type ?? ''),
                severity:  String(ev.severity ?? 'info'),
                message:   String(ev.message ?? ''),
              }, ...prev].slice(0, 50))
            } catch { /* ignore malformed frames */ }
          }
        }
      } catch (err: unknown) {
        if (err instanceof Error && err.name !== 'AbortError') {
          // Stream ended or failed — silently ignore for Dashboard
        }
      }
    })()

    return () => ac.abort()
  }, [])

  const online = servers.filter(s => s.status === 'online').length
  const offline = servers.length - online

  const stats = [
    { label: 'Servers',    value: servers.length, icon: Server,    color: 'var(--accent)' },
    { label: 'Online',     value: online,          icon: Activity,  color: 'var(--online)' },
    { label: 'Offline',    value: offline,         icon: Server,    color: 'var(--text-muted)' },
    { label: 'Stacks',     value: stackCount,      icon: Layers,    color: 'var(--accent)' },
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
              {loadingServers && label !== 'Stacks' ? (
                <div className="h-7 w-8 rounded animate-pulse mb-1" style={{ backgroundColor: 'var(--surface-2)' }} />
              ) : (
                <p className="text-2xl font-bold" style={{ color: 'var(--text)' }}>{value}</p>
              )}
              <p className="text-xs" style={{ color: 'var(--text-muted)' }}>{label}</p>
            </div>
          </div>
        ))}
      </div>

      <div className="grid grid-cols-5 gap-6">
        {/* Server list — 3/5 width */}
        <div className="col-span-3">
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
                {loadingServers ? (
                  [0, 1, 2].map(i => (
                    <tr key={i} style={{ borderBottom: '1px solid var(--border)' }}>
                      {[0, 1, 2].map(j => (
                        <td key={j} className="px-4 py-3">
                          <div className="h-4 rounded animate-pulse" style={{ backgroundColor: 'var(--surface-2)', width: j === 1 ? '60px' : '80%' }} />
                        </td>
                      ))}
                    </tr>
                  ))
                ) : servers.length === 0 ? (
                  <tr>
                    <td colSpan={3} className="px-4 py-8 text-center" style={{ color: 'var(--text-muted)' }}>
                      No servers enrolled. Use <code className="px-1 rounded" style={{ backgroundColor: 'var(--surface-2)' }}>orkestra-agent enroll</code> to add one.
                    </td>
                  </tr>
                ) : servers.map(s => (
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

        {/* Event feed — 2/5 width */}
        <div className="col-span-2">
          <h2 className="font-semibold mb-3" style={{ color: 'var(--text)' }}>Event feed</h2>
          <div className="rounded-lg border overflow-hidden" style={{ borderColor: 'var(--border)', backgroundColor: 'var(--surface)', maxHeight: '400px', overflowY: 'auto' }}>
            {events.length === 0 ? (
              <div className="px-4 py-8 text-center text-sm" style={{ color: 'var(--text-muted)' }}>
                No events yet. Events appear here as agents connect and stacks reconcile.
              </div>
            ) : (
              <div className="divide-y" style={{ '--tw-divide-opacity': 1, borderColor: 'var(--border)' } as React.CSSProperties}>
                {events.map((ev, i) => (
                  <div key={ev.id || i} className="flex gap-2 px-3 py-2.5">
                    <SeverityIcon severity={ev.severity} />
                    <div className="min-w-0 flex-1">
                      <p className="text-xs leading-snug" style={{ color: 'var(--text)' }}>{ev.message}</p>
                      <p className="text-xs mt-0.5" style={{ color: 'var(--text-muted)' }}>
                        {ev.eventType}{ev.ts ? ` · ${new Date(ev.ts).toLocaleTimeString()}` : ''}
                      </p>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}
