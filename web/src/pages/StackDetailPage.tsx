import { useEffect, useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { ArrowLeft } from 'lucide-react'
import { Badge } from '@/components/ui/badge'

interface StackVersion {
  id: string
  version: number
  createdAt: number
  composeYaml: string
}

interface Stack {
  id: string
  name: string
  description: string
  version: number
}

export function StackDetailPage() {
  const { id } = useParams<{ id: string }>()
  const [stack, setStack] = useState<Stack | null>(null)
  const [versions, setVersions] = useState<StackVersion[]>([])
  const [selected, setSelected] = useState<StackVersion | null>(null)

  useEffect(() => {
    async function load() {
      const [sRes, vRes] = await Promise.all([
        fetch('/orkestra.v1.StackService/GetStack', {
          method: 'POST', headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ id }),
        }),
        fetch('/orkestra.v1.StackService/ListStackVersions', {
          method: 'POST', headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ stackId: id }),
        }),
      ])
      if (sRes.ok) {
        const d = await sRes.json()
        setStack({ id: String(d.id ?? ''), name: String(d.name ?? ''), description: String(d.description ?? ''), version: Number(d.version ?? 0) })
      }
      if (vRes.ok) {
        const d = await vRes.json()
        const vs = (d.versions ?? []).map((v: Record<string, unknown>) => ({
          id:          String(v.id ?? ''),
          version:     Number(v.version ?? 0),
          createdAt:   Number(v.createdAt ?? v.created_at ?? 0),
          composeYaml: String(v.composeYaml ?? v.compose_yaml ?? ''),
        }))
        setVersions(vs)
        if (vs.length > 0) setSelected(vs[0])
      }
    }
    load()
  }, [id])

  return (
    <div>
      <Link to="/stacks" className="flex items-center gap-1 text-sm mb-4 hover:underline" style={{ color: 'var(--text-muted)' }}>
        <ArrowLeft size={14} /> Stacks
      </Link>

      <div className="flex items-center gap-3 mb-6">
        <h1 className="text-xl font-semibold" style={{ color: 'var(--text)' }}>{stack?.name ?? id}</h1>
        {stack && stack.version > 0 && <Badge>v{stack.version}</Badge>}
      </div>

      {stack?.description && (
        <p className="mb-4 text-sm" style={{ color: 'var(--text-muted)' }}>{stack.description}</p>
      )}

      <div className="grid grid-cols-3 gap-4">
        {/* Version list */}
        <div>
          <h2 className="font-medium text-sm mb-2" style={{ color: 'var(--text-muted)' }}>Versions</h2>
          <div className="space-y-1">
            {versions.map(v => (
              <button key={v.id}
                onClick={() => setSelected(v)}
                className="w-full text-left px-3 py-2 rounded border text-sm transition-colors"
                style={{
                  borderColor: selected?.id === v.id ? 'var(--accent)' : 'var(--border)',
                  backgroundColor: selected?.id === v.id ? 'rgba(126,226,42,0.06)' : 'var(--surface)',
                  color: selected?.id === v.id ? 'var(--accent)' : 'var(--text)',
                }}>
                <div className="font-medium">v{v.version}</div>
                <div className="text-xs" style={{ color: 'var(--text-muted)' }}>
                  {v.createdAt ? new Date(v.createdAt).toLocaleDateString() : '—'}
                </div>
              </button>
            ))}
            {versions.length === 0 && (
              <p className="text-sm" style={{ color: 'var(--text-muted)' }}>No versions yet.</p>
            )}
          </div>
        </div>

        {/* Compose YAML viewer */}
        <div className="col-span-2">
          <h2 className="font-medium text-sm mb-2" style={{ color: 'var(--text-muted)' }}>
            {selected ? `compose.yaml — v${selected.version}` : 'compose.yaml'}
          </h2>
          <pre className="rounded-lg border p-4 text-xs font-mono overflow-auto max-h-96"
            style={{ borderColor: 'var(--border)', backgroundColor: 'var(--surface)', color: 'var(--text)' }}>
            {selected?.composeYaml || 'Select a version to view its compose YAML.'}
          </pre>
        </div>
      </div>
    </div>
  )
}
