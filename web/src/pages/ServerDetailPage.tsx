import { Link, getRouteApi } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { ArrowLeft, RefreshCw, Play, Square, RotateCcw, Trash2, Terminal } from 'lucide-react'
import { Badge, StatusDot } from '@/components/ui/badge'
import {
  mapServerState,
  containerRows,
  failedStacks,
  containerStateVariant,
  displayName,
  healthVariant,
  versionLabel,
  type ServerState,
} from '@/lib/serverState'

const routeApi = getRouteApi('/_authenticated/servers/$id')

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

function reportedAgo(ms: number): string {
  if (!ms) return 'never reported'
  const s = Math.max(0, Math.floor((Date.now() - ms) / 1000))
  if (s < 60) return `reported ${s}s ago`
  if (s < 3600) return `reported ${Math.floor(s / 60)}m ago`
  return `reported ${Math.floor(s / 3600)}h ago`
}

export function ServerDetailPage() {
  const { id } = routeApi.useParams()

  const { data: server, isPending: loading, error: serverError } = useQuery({
    queryKey: ['server', id],
    queryFn: async (): Promise<Server> => {
      // Use fetch against the Connect JSON API until gen/ is built in CI.
      const res = await fetch('/orkestra.v1.StackService/GetServer', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ id }),
      })
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const data = await res.json()
      return {
        id:            String(data.id ?? ''),
        name:          String(data.name ?? ''),
        hostname:      String(data.hostname ?? ''),
        arch:          String(data.arch ?? ''),
        os:            String(data.os ?? ''),
        agentVersion:  String(data.agentVersion ?? data.agent_version ?? ''),
        dockerVersion: String(data.dockerVersion ?? data.docker_version ?? ''),
        status:        String(data.status ?? 'offline'),
        lastSeenAt:    Number(data.lastSeenAt ?? data.last_seen_at ?? 0),
      }
    },
  })

  const {
    data: state,
    isPending: stateLoading,
    error: stateError,
    refetch,
    isFetching,
  } = useQuery({
    queryKey: ['serverState', id],
    queryFn: async (): Promise<ServerState> => {
      const res = await fetch('/orkestra.v1.StackService/GetServerState', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ serverId: id }),
      })
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      return mapServerState(await res.json())
    },
    // The agent reports every 30 s; polling faster only re-renders the same snapshot.
    refetchInterval: 30_000,
  })

  const rows = containerRows(state)
  const failed = failedStacks(state)
  const error = serverError ?? stateError

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

      {error && (
        <div className="mb-4 px-4 py-3 rounded text-sm" style={{ backgroundColor: '#2d1115', color: 'var(--error)', border: '1px solid #4a1a1f' }}>
          {String(error)}
        </div>
      )}

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

      {/* Reconcile failures the agent reported, per stack */}
      {failed.length > 0 && (
        <div className="mb-4 rounded-lg border overflow-hidden" style={{ borderColor: '#4a1a1f', backgroundColor: '#2d1115' }}>
          {failed.map(s => (
            <div key={s.stackId} className="px-4 py-3 text-sm" style={{ color: 'var(--error)' }}>
              <span className="font-medium">{s.stackName || s.stackId}</span>: {s.error}
              {s.serviceErrors.length > 0 && (
                <ul className="mt-1 ml-4 text-xs list-disc">
                  {s.serviceErrors.map(e => (
                    <li key={e.serviceName}>
                      <span className="font-mono">{e.serviceName}</span> — {e.error}
                    </li>
                  ))}
                </ul>
              )}
            </div>
          ))}
        </div>
      )}

      {/* Containers section */}
      <div className="flex items-center justify-between mb-3">
        <div className="flex items-baseline gap-2">
          <h2 className="font-semibold" style={{ color: 'var(--text)' }}>Containers</h2>
          {state && (
            <span className="text-xs" style={{ color: 'var(--text-muted)' }}>{reportedAgo(state.reportedAt)}</span>
          )}
        </div>
        <button
          onClick={() => void refetch()}
          disabled={isFetching}
          className="flex items-center gap-1 text-sm px-2 py-1 rounded border transition-colors enabled:hover:bg-[var(--surface-2)] disabled:opacity-40"
          style={{ borderColor: 'var(--border)', color: 'var(--text-muted)' }}
        >
          <RefreshCw size={13} /> Refresh
        </button>
      </div>

      <div className="rounded-lg border overflow-hidden" style={{ borderColor: 'var(--border)', backgroundColor: 'var(--surface)' }}>
        <table className="w-full text-sm">
          <thead>
            <tr style={{ borderBottom: '1px solid var(--border)' }}>
              {['Container', 'Stack / Service', 'Image', 'State', 'Health', 'Status', 'Restarts', 'Actions'].map(h => (
                <th key={h} className="text-left px-4 py-3 text-xs font-medium" style={{ color: 'var(--text-muted)' }}>{h}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {stateLoading && (
              <tr>
                <td colSpan={8} className="px-4 py-8 text-center" style={{ color: 'var(--text-muted)' }}>Loading…</td>
              </tr>
            )}
            {!stateLoading && rows.length === 0 && (
              <tr>
                <td colSpan={8} className="px-4 py-8 text-center" style={{ color: 'var(--text-muted)' }}>
                  {server?.status === 'online' ? 'No containers running.' : 'Server is offline — showing its last report.'}
                </td>
              </tr>
            )}
            {rows.map(c => (
              <tr key={c.id} style={{ borderBottom: '1px solid var(--border)' }} className="hover:bg-[var(--surface-2)]">
                <td className="px-4 py-3 font-mono text-xs whitespace-nowrap" style={{ color: 'var(--text)' }} title={c.name || c.id}>
                  {displayName(c)}
                </td>
                <td className="px-4 py-3 text-xs" style={{ color: 'var(--text-muted)' }}>
                  <Link
                    to="/stacks/$id"
                    params={{ id: c.stackId }}
                    className="hover:underline"
                    style={{ color: 'var(--text)' }}
                  >
                    {c.stackName || c.stackId.slice(0, 8)}
                  </Link>
                  {' / '}{c.serviceName || '—'}
                </td>
                <td className="px-4 py-3 text-xs" style={{ color: 'var(--text-muted)' }}>{c.image}</td>
                <td className="px-4 py-3">
                  <Badge variant={containerStateVariant(c.state)}>{c.state}</Badge>
                </td>
                <td className="px-4 py-3">
                  {c.health
                    ? <Badge variant={healthVariant(c.health)}>{c.health}</Badge>
                    : <span className="text-xs" style={{ color: 'var(--text-muted)' }}>—</span>}
                </td>
                <td className="px-4 py-3 text-xs" style={{ color: 'var(--text-muted)' }}>{c.status}</td>
                <td className="px-4 py-3 text-xs" style={{ color: 'var(--text-muted)' }}>{c.restartCount}</td>
                <td className="px-4 py-3">
                  <div className="flex items-center gap-1">
                    <ActionBtn icon={<Play size={13} />} label="Start"   disabled />
                    <ActionBtn icon={<Square size={13} />} label="Stop"  disabled />
                    <ActionBtn icon={<RotateCcw size={13} />} label="Restart" disabled />
                    <ActionBtn icon={<Terminal size={13} />} label="Logs" disabled />
                    <ActionBtn icon={<Trash2 size={13} />} label="Remove" disabled danger />
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {/* Per-stack versions, so a mixed rollout is visible next to the containers */}
      {(state?.stacks.length ?? 0) > 0 && (
        <p className="mt-3 text-xs" style={{ color: 'var(--text-muted)' }}>
          {state!.stacks.map(s => `${s.stackName || s.stackId.slice(0, 8)} ${versionLabel(s)}`).join(' · ')}
        </p>
      )}
    </div>
  )
}

function ActionBtn({ icon, label, onClick, danger, disabled }: {
  icon: React.ReactNode
  label: string
  onClick?: () => void
  danger?: boolean
  disabled?: boolean
}) {
  return (
    <button
      title={disabled ? `${label} — not available yet` : label}
      onClick={onClick}
      disabled={disabled}
      className="p-1.5 rounded border transition-colors enabled:hover:bg-[var(--surface-2)] disabled:opacity-40 disabled:cursor-not-allowed"
      style={{
        borderColor: 'var(--border)',
        color: danger ? 'var(--error)' : 'var(--text-muted)',
      }}
    >
      {icon}
    </button>
  )
}
