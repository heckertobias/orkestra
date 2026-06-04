import { useEffect, useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { ArrowLeft, RefreshCw, Play, Square, RotateCcw, Trash2, Terminal } from 'lucide-react'
import { Badge, StatusDot } from '@/components/ui/badge'

interface Container {
  id: string
  names: string[]
  image: string
  state: string
  status: string
  restartCount: number
}

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
}

export function ServerDetailPage() {
  const { id } = useParams<{ id: string }>()
  const [server, setServer] = useState<Server | null>(null)
  const [containers] = useState<Container[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    async function load() {
      setLoading(true)
      try {
        const res = await fetch('/orkestra.v1.StackService/GetServer', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ id }),
        })
        if (res.ok) {
          const data = await res.json()
          setServer({
            id:            String(data.id ?? ''),
            name:          String(data.name ?? ''),
            hostname:      String(data.hostname ?? ''),
            arch:          String(data.arch ?? ''),
            os:            String(data.os ?? ''),
            agentVersion:  String(data.agentVersion ?? data.agent_version ?? ''),
            dockerVersion: String(data.dockerVersion ?? data.docker_version ?? ''),
            status:        String(data.status ?? 'offline'),
            lastSeenAt:    Number(data.lastSeenAt ?? data.last_seen_at ?? 0),
          })
        }
      } finally {
        setLoading(false)
      }
    }
    load()
  }, [id])

  async function execCommand(containerId: string, cmd: string) {
    await fetch('/orkestra.v1.StackService/ExecOnContainer', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ serverId: id, containerId, commandType: cmd }),
    })
  }

  if (loading) {
    return <div style={{ color: 'var(--text-muted)' }}>Loading…</div>
  }

  return (
    <div>
      {/* Back + Header */}
      <div className="mb-6">
        <Link to="/servers" className="flex items-center gap-1 text-sm mb-3 hover:underline" style={{ color: 'var(--text-muted)' }}>
          <ArrowLeft size={14} /> Servers
        </Link>
        <div className="flex items-center gap-3">
          <div>
            <h1 className="text-xl font-semibold" style={{ color: 'var(--text)' }}>
              {server?.name || server?.hostname || id}
            </h1>
            <p style={{ color: 'var(--text-muted)' }}>{server?.hostname} · {server?.os}/{server?.arch}</p>
          </div>
          {server && (
            <Badge variant={server.status === 'online' ? 'online' : 'offline'}>
              <StatusDot online={server.status === 'online'} />
              {server.status === 'online' ? 'Online' : 'Offline'}
            </Badge>
          )}
        </div>
      </div>

      {/* Info cards */}
      {server && (
        <div className="grid grid-cols-4 gap-3 mb-6">
          {[
            { label: 'Agent version', value: server.agentVersion || '—' },
            { label: 'Docker version', value: server.dockerVersion || '—' },
            { label: 'Architecture', value: server.arch },
            { label: 'OS', value: server.os },
          ].map(({ label, value }) => (
            <div key={label} className="rounded-lg border p-3" style={{ borderColor: 'var(--border)', backgroundColor: 'var(--surface)' }}>
              <p className="text-xs mb-1" style={{ color: 'var(--text-muted)' }}>{label}</p>
              <p className="font-medium text-sm" style={{ color: 'var(--text)' }}>{value}</p>
            </div>
          ))}
        </div>
      )}

      {/* Containers section */}
      <div className="flex items-center justify-between mb-3">
        <h2 className="font-semibold" style={{ color: 'var(--text)' }}>Containers</h2>
        <button
          onClick={() => {}}
          className="flex items-center gap-1 text-sm px-2 py-1 rounded border transition-colors hover:bg-[var(--surface-2)]"
          style={{ borderColor: 'var(--border)', color: 'var(--text-muted)' }}
        >
          <RefreshCw size={13} /> Refresh
        </button>
      </div>

      <div className="rounded-lg border overflow-hidden" style={{ borderColor: 'var(--border)', backgroundColor: 'var(--surface)' }}>
        <table className="w-full text-sm">
          <thead>
            <tr style={{ borderBottom: '1px solid var(--border)' }}>
              {['Container', 'Image', 'State', 'Status', 'Restarts', 'Actions'].map(h => (
                <th key={h} className="text-left px-4 py-3 text-xs font-medium" style={{ color: 'var(--text-muted)' }}>{h}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {containers.length === 0 && (
              <tr>
                <td colSpan={6} className="px-4 py-8 text-center" style={{ color: 'var(--text-muted)' }}>
                  {server?.status === 'online' ? 'No containers running.' : 'Server is offline — cannot fetch containers.'}
                </td>
              </tr>
            )}
            {containers.map(c => (
              <tr key={c.id} style={{ borderBottom: '1px solid var(--border)' }} className="hover:bg-[var(--surface-2)]">
                <td className="px-4 py-3 font-mono text-xs" style={{ color: 'var(--text)' }}>
                  {c.names[0]?.replace(/^\//, '') ?? c.id.slice(0, 12)}
                </td>
                <td className="px-4 py-3 text-xs" style={{ color: 'var(--text-muted)' }}>{c.image}</td>
                <td className="px-4 py-3">
                  <Badge variant={c.state === 'running' ? 'online' : c.state === 'exited' ? 'offline' : 'warn'}>
                    {c.state}
                  </Badge>
                </td>
                <td className="px-4 py-3 text-xs" style={{ color: 'var(--text-muted)' }}>{c.status}</td>
                <td className="px-4 py-3 text-xs" style={{ color: 'var(--text-muted)' }}>{c.restartCount}</td>
                <td className="px-4 py-3">
                  <div className="flex items-center gap-1">
                    <ActionBtn icon={<Play size={13} />} label="Start"   onClick={() => execCommand(c.id, 'start')} />
                    <ActionBtn icon={<Square size={13} />} label="Stop"  onClick={() => execCommand(c.id, 'stop')} />
                    <ActionBtn icon={<RotateCcw size={13} />} label="Restart" onClick={() => execCommand(c.id, 'restart')} />
                    <ActionBtn icon={<Terminal size={13} />} label="Logs" onClick={() => {}} />
                    <ActionBtn icon={<Trash2 size={13} />} label="Remove" onClick={() => execCommand(c.id, 'remove')} danger />
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

function ActionBtn({ icon, label, onClick, danger }: {
  icon: React.ReactNode
  label: string
  onClick: () => void
  danger?: boolean
}) {
  return (
    <button
      title={label}
      onClick={onClick}
      className="p-1.5 rounded border transition-colors hover:bg-[var(--surface-2)]"
      style={{
        borderColor: 'var(--border)',
        color: danger ? 'var(--error)' : 'var(--text-muted)',
      }}
    >
      {icon}
    </button>
  )
}
