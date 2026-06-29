import { useEffect, useMemo, useState } from 'react'
import { X } from 'lucide-react'

export interface DeployVersion {
  id: string
  version: number
  envVarNames: string[]
  envDefaults: Record<string, string>
}

interface AssignmentLite {
  stackId: string
  stackVersionId: string
  desiredStatus: string
  envValues: Record<string, string>
}

interface ServerLite {
  id: string
  name: string
  hostname: string
  assignments: AssignmentLite[]
}

interface DeployStackDialogProps {
  stackId: string
  stackName: string
  versions: DeployVersion[] // newest-first
  onClose: () => void
  onDeployed?: () => void
}

export function DeployStackDialog({ stackId, stackName, versions, onClose, onDeployed }: DeployStackDialogProps) {
  const [servers, setServers] = useState<ServerLite[]>([])
  const [loadingServers, setLoadingServers] = useState(true)
  const [serverId, setServerId] = useState('')
  const [versionId, setVersionId] = useState(versions[0]?.id ?? '')
  const [desiredStatus, setDesiredStatus] = useState('running')
  const [values, setValues] = useState<Record<string, string>>({})
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const version = useMemo(() => versions.find(v => v.id === versionId), [versions, versionId])
  const names = useMemo(() => version?.envVarNames ?? [], [version])

  // Prefill desired status + values from the server's existing assignment for
  // this stack: carry over values for names that still exist, new names empty.
  function prefill(srvId: string, verId: string, srvList: ServerLite[]) {
    const ver = versions.find(v => v.id === verId)
    const nm = ver?.envVarNames ?? []
    const def = ver?.envDefaults ?? {}
    const existing = srvList.find(s => s.id === srvId)?.assignments.find(a => a.stackId === stackId)
    setDesiredStatus(existing?.desiredStatus || 'running')
    const prior = existing?.envValues ?? {}
    // existing assignment value wins; otherwise fall back to the compose default.
    setValues(Object.fromEntries(nm.map(n => [n, (n in prior) ? prior[n] : (def[n] ?? '')])))
  }

  function selectServer(id: string) {
    setServerId(id)
    prefill(id, versionId, servers)
  }

  function selectVersion(id: string) {
    setVersionId(id)
    prefill(serverId, id, servers)
  }

  // Load servers (with their current assignments, so we can prefill values).
  useEffect(() => {
    let cancelled = false
    async function load() {
      try {
        const res = await fetch('/orkestra.v1.StackService/ListServers', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({}),
        })
        if (!res.ok) throw new Error(`HTTP ${res.status}`)
        const data = await res.json()
        if (cancelled) return
        const list: ServerLite[] = (data.servers ?? []).map((s: Record<string, unknown>) => ({
          id: String(s.id ?? ''),
          name: String(s.name ?? ''),
          hostname: String(s.hostname ?? ''),
          assignments: ((s.assignments ?? []) as Array<Record<string, unknown>>).map(a => ({
            stackId: String(a.stackId ?? a.stack_id ?? ''),
            stackVersionId: String(a.stackVersionId ?? a.stack_version_id ?? ''),
            desiredStatus: String(a.desiredStatus ?? a.desired_status ?? ''),
            envValues: (a.envValues ?? a.env_values ?? {}) as Record<string, string>,
          })),
        }))
        setServers(list)
        const initialServer = serverId || (list[0]?.id ?? '')
        if (initialServer) setServerId(initialServer)
        prefill(initialServer, versionId, list)
      } catch (e) {
        if (!cancelled) setError(String(e))
      } finally {
        if (!cancelled) setLoadingServers(false)
      }
    }
    load()
    return () => { cancelled = true }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const missingCount = names.filter(n => !(values[n] ?? '').trim()).length

  async function handleDeploy() {
    if (!serverId) { setError('Select a server'); return }
    setSubmitting(true)
    setError(null)
    try {
      const res = await fetch('/orkestra.v1.StackService/AssignStack', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ serverId, stackId, stackVersionId: versionId, desiredStatus, envValues: values }),
      })
      if (!res.ok) {
        const body = await res.json().catch(() => ({}))
        throw new Error(body?.message ?? `HTTP ${res.status}`)
      }
      onDeployed?.()
      onClose()
    } catch (e) {
      setError(String(e))
    } finally {
      setSubmitting(false)
    }
  }

  const fieldStyle = { backgroundColor: 'var(--bg)', borderColor: 'var(--border)', color: 'var(--text)' }

  return (
    <div className="fixed inset-0 flex items-center justify-center z-50" style={{ backgroundColor: 'rgba(0,0,0,0.6)' }}>
      <div className="w-full max-w-md rounded-lg border p-6 relative max-h-[90vh] overflow-y-auto" style={{ backgroundColor: 'var(--surface)', borderColor: 'var(--border)' }}>
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-base font-semibold" style={{ color: 'var(--text)' }}>Deploy <span style={{ color: 'var(--accent)' }}>{stackName}</span></h2>
          <button onClick={onClose} className="p-1 rounded hover:bg-[var(--surface-2)]" style={{ color: 'var(--text-muted)' }}>
            <X size={16} />
          </button>
        </div>

        {error && (
          <div className="mb-4 px-3 py-2 rounded text-sm" style={{ backgroundColor: '#2d1115', color: 'var(--error)', border: '1px solid #4a1a1f' }}>
            {error}
          </div>
        )}

        <div className="space-y-4">
          {/* Server */}
          <div>
            <label className="block text-xs mb-1" style={{ color: 'var(--text-muted)' }}>Server</label>
            {loadingServers ? (
              <p className="text-xs" style={{ color: 'var(--text-muted)' }}>Loading…</p>
            ) : servers.length === 0 ? (
              <p className="text-xs" style={{ color: 'var(--text-muted)' }}>No servers enrolled.</p>
            ) : (
              <select value={serverId} onChange={e => selectServer(e.target.value)}
                className="w-full px-3 py-1.5 rounded border text-sm outline-none focus:border-[var(--accent)]" style={fieldStyle}>
                {servers.map(s => (
                  <option key={s.id} value={s.id}>{s.name || s.hostname || s.id}</option>
                ))}
              </select>
            )}
          </div>

          {/* Version + status */}
          <div className="flex gap-3">
            <div className="flex-1">
              <label className="block text-xs mb-1" style={{ color: 'var(--text-muted)' }}>Version</label>
              <select value={versionId} onChange={e => selectVersion(e.target.value)}
                className="w-full px-3 py-1.5 rounded border text-sm outline-none focus:border-[var(--accent)]" style={fieldStyle}>
                {versions.map(v => (
                  <option key={v.id} value={v.id}>v{v.version}</option>
                ))}
              </select>
            </div>
            <div className="flex-1">
              <label className="block text-xs mb-1" style={{ color: 'var(--text-muted)' }}>Status</label>
              <select value={desiredStatus} onChange={e => setDesiredStatus(e.target.value)}
                className="w-full px-3 py-1.5 rounded border text-sm outline-none focus:border-[var(--accent)]" style={fieldStyle}>
                <option value="running">running</option>
                <option value="stopped">stopped</option>
              </select>
            </div>
          </div>

          {/* Env values */}
          <div>
            <label className="block text-xs mb-1" style={{ color: 'var(--text-muted)' }}>Environment values</label>
            {names.length === 0 ? (
              <p className="text-xs" style={{ color: 'var(--text-muted)' }}>This version declares no variables.</p>
            ) : (
              <div className="space-y-1.5">
                {names.map(n => (
                  <div key={n} className="flex items-center gap-2">
                    <code className="text-xs font-mono w-2/5 shrink-0 truncate" style={{ color: 'var(--accent)' }} title={n}>{n}</code>
                    <input
                      value={values[n] ?? ''}
                      onChange={e => setValues(v => ({ ...v, [n]: e.target.value }))}
                      placeholder="value"
                      className="flex-1 min-w-0 px-2 py-1 rounded border text-xs font-mono outline-none focus:border-[var(--accent)]"
                      style={fieldStyle}
                    />
                  </div>
                ))}
              </div>
            )}
            {missingCount > 0 && (
              <p className="text-xs mt-2" style={{ color: 'var(--text-muted)' }}>
                {missingCount} variable{missingCount === 1 ? '' : 's'} without a value.
              </p>
            )}
          </div>
        </div>

        <div className="flex justify-end gap-2 mt-6">
          <button onClick={onClose} className="px-4 py-1.5 rounded border text-sm" style={{ borderColor: 'var(--border)', color: 'var(--text-muted)' }}>
            Cancel
          </button>
          <button onClick={handleDeploy} disabled={submitting || !serverId || !versionId}
            className="px-4 py-1.5 rounded text-sm font-medium disabled:opacity-50" style={{ backgroundColor: 'var(--accent)', color: '#0d1117' }}>
            {submitting ? 'Deploying…' : 'Deploy'}
          </button>
        </div>
      </div>
    </div>
  )
}
