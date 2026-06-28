import { useEffect, useRef, useState } from 'react'
import { useNavigate, useParams, Link } from 'react-router-dom'
import { ArrowLeft, Download, Plus, Trash2, Upload } from 'lucide-react'
import CodeMirror from '@uiw/react-codemirror'
import { yaml } from '@codemirror/lang-yaml'
import { oneDark } from '@codemirror/theme-one-dark'
import { linter, lintGutter, type Diagnostic } from '@codemirror/lint'
import * as jsyaml from 'js-yaml'
import { downloadText, readTextFile } from '@/lib/files'
import { extractComposeVars, toDotEnv } from '@/lib/env'

// ── CodeMirror linters ────────────────────────────────────────────────────────

/** Instant client-side YAML syntax linter using js-yaml. */
const yamlSyntaxLinter = linter((view) => {
  const content = view.state.doc.toString()
  if (!content.trim()) return []
  try {
    jsyaml.load(content)
    return []
  } catch (e) {
    if (e instanceof jsyaml.YAMLException) {
      const line = (e.mark?.line ?? 0) + 1 // CodeMirror lines are 1-based
      const col  = e.mark?.column ?? 0
      const lineInfo = view.state.doc.line(Math.min(line, view.state.doc.lines))
      const from = Math.min(lineInfo.from + col, lineInfo.to)
      return [{
        from,
        to: Math.min(from + 1, lineInfo.to),
        severity: 'error' as const,
        message: e.reason ?? String(e),
      }]
    }
    return []
  }
})

/** Debounced backend linter for compose-semantic warnings (skips if YAML is invalid). */
const composeSematicLinter = linter(async (view): Promise<Diagnostic[]> => {
  const content = view.state.doc.toString()
  if (!content.trim()) return []
  // Skip if YAML is invalid — the syntax linter already handles that
  try { jsyaml.load(content) } catch { return [] }

  try {
    const res = await fetch('/orkestra.v1.StackService/ValidateCompose', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ composeYaml: content }),
    })
    if (!res.ok) return []
    const data = await res.json()
    return (data.diagnostics ?? []).map((d: { severity: string; message: string; line?: number }) => {
      const lineNum = d.line ?? 0
      let from: number, to: number
      if (lineNum > 0 && lineNum <= view.state.doc.lines) {
        const lineInfo = view.state.doc.line(lineNum)
        from = lineInfo.from
        to = lineInfo.to
      } else {
        from = 0
        to = Math.min(view.state.doc.line(1).to, view.state.doc.length)
      }
      return {
        from, to,
        severity: (d.severity === 'error' ? 'error' : 'warning') as 'error' | 'warning',
        message: d.message,
      }
    })
  } catch {
    return []
  }
}, { delay: 600 })

const cmExtensions = [yaml(), yamlSyntaxLinter, composeSematicLinter, lintGutter()]

// ─────────────────────────────────────────────────────────────────────────────

interface EnvRow {
  key: string
  value: string
}

function envRowsToMap(rows: EnvRow[]): Record<string, string> {
  const m: Record<string, string> = {}
  for (const r of rows) {
    if (r.key.trim()) m[r.key.trim()] = r.value
  }
  return m
}

function mapToEnvRows(m: Record<string, string>): EnvRow[] {
  return Object.entries(m).map(([key, value]) => ({ key, value }))
}

export function StackEditorPage() {
  const { id } = useParams<{ id?: string }>()
  const navigate = useNavigate()
  const isEdit = Boolean(id)

  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [composeYaml, setComposeYaml] = useState('')
  const [envRows, setEnvRows] = useState<EnvRow[]>([])
  const [saving, setSaving] = useState(false)
  const [loading, setLoading] = useState(isEdit)
  const [error, setError] = useState<string | null>(null)

  const fileInputRef = useRef<HTMLInputElement>(null)

  // Load existing stack data in edit mode
  useEffect(() => {
    if (!isEdit) return
    let cancelled = false

    async function load() {
      const [sRes, vRes] = await Promise.all([
        fetch('/orkestra.v1.StackService/GetStack', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ id }),
        }),
        fetch('/orkestra.v1.StackService/ListStackVersions', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ stackId: id }),
        }),
      ])
      if (cancelled) return

      if (sRes.ok) {
        const d = await sRes.json()
        setName(String(d.name ?? ''))
        setDescription(String(d.description ?? ''))
      }
      if (vRes.ok) {
        const d = await vRes.json()
        const versions: Array<Record<string, unknown>> = d.versions ?? []
        // versions are returned newest-first
        const latest = versions[0]
        if (latest) {
          setComposeYaml(String(latest.composeYaml ?? latest.compose_yaml ?? ''))
          const ev = (latest.envVars ?? latest.env_vars ?? {}) as Record<string, string>
          setEnvRows(mapToEnvRows(ev))
        }
      }
      setLoading(false)
    }

    load().catch(() => setLoading(false))
    return () => { cancelled = true }
  }, [id, isEdit])

  // ── Compose upload/download ────────────────────────────────────────────────

  async function handleComposeUpload(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0]
    if (!file) return
    const text = await readTextFile(file)
    setComposeYaml(text)
    // reset so the same file can be re-selected
    e.target.value = ''
  }

  function handleComposeDownload() {
    downloadText('compose.yaml', composeYaml, 'text/yaml')
  }

  // ── Env var helpers ────────────────────────────────────────────────────────

  function addEnvRow() {
    setEnvRows(r => [...r, { key: '', value: '' }])
  }

  function removeEnvRow(idx: number) {
    setEnvRows(r => r.filter((_, i) => i !== idx))
  }

  function patchEnvRow(idx: number, patch: Partial<EnvRow>) {
    setEnvRows(r => r.map((row, i) => i === idx ? { ...row, ...patch } : row))
  }

  function handleEnvDownload() {
    const defined = envRowsToMap(envRows)
    const referenced = extractComposeVars(composeYaml)
    // Union of referenced and defined keys (maintain consistent ordering)
    const all = [...new Set([...referenced, ...Object.keys(defined)])].sort()
    downloadText('.env', toDotEnv(all, defined))
  }

  // ── Referenced-but-undefined vars ─────────────────────────────────────────

  const referencedVars = extractComposeVars(composeYaml)
  const definedKeys = new Set(envRows.map(r => r.key.trim()).filter(Boolean))
  const undefinedVars = referencedVars.filter(v => !definedKeys.has(v))

  // ── Save ──────────────────────────────────────────────────────────────────

  async function handleSave() {
    if (!name.trim()) { setError('Name is required'); return }
    setSaving(true)
    setError(null)
    try {
      const envVars = envRowsToMap(envRows)

      if (isEdit) {
        // UpdateStack → creates a new immutable version
        const res = await fetch('/orkestra.v1.StackService/UpdateStack', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            id,
            description,
            composeYaml,
            envVars,
          }),
        })
        if (!res.ok) {
          const body = await res.json().catch(() => ({}))
          throw new Error(body?.message ?? `HTTP ${res.status}`)
        }
        navigate(`/stacks/${id}`)
      } else {
        // CreateStack
        const res = await fetch('/orkestra.v1.StackService/CreateStack', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            name: name.trim(),
            description,
            composeYaml,
            envVars,
          }),
        })
        if (!res.ok) {
          const body = await res.json().catch(() => ({}))
          throw new Error(body?.message ?? `HTTP ${res.status}`)
        }
        const data = await res.json()
        navigate(`/stacks/${data.id ?? data.Id ?? ''}`)
      }
    } catch (e) {
      setError(String(e))
    } finally {
      setSaving(false)
    }
  }

  // ── Render ────────────────────────────────────────────────────────────────

  if (loading) {
    return (
      <div className="flex items-center justify-center h-48" style={{ color: 'var(--text-muted)' }}>
        Loading…
      </div>
    )
  }

  return (
    <div className="flex flex-col h-full">
      {/* Breadcrumb */}
      <Link
        to="/stacks"
        className="flex items-center gap-1 text-sm mb-4 hover:underline w-fit"
        style={{ color: 'var(--text-muted)' }}
      >
        <ArrowLeft size={14} /> Stacks
      </Link>

      {/* Header bar */}
      <div className="flex items-start gap-3 mb-6 flex-wrap">
        <div className="flex-1 min-w-0 flex gap-3 flex-wrap">
          {/* Name — read-only in edit mode (stack name is immutable) */}
          {isEdit ? (
            <div>
              <p className="text-xs mb-1" style={{ color: 'var(--text-muted)' }}>Name</p>
              <p className="text-base font-semibold" style={{ color: 'var(--text)' }}>{name}</p>
            </div>
          ) : (
            <div className="flex-1 min-w-48">
              <label className="block text-xs mb-1" style={{ color: 'var(--text-muted)' }}>Name *</label>
              <input
                value={name}
                onChange={e => setName(e.target.value)}
                placeholder="my-app"
                className="w-full px-3 py-1.5 rounded border text-sm outline-none focus:border-[var(--accent)]"
                style={{ backgroundColor: 'var(--bg)', borderColor: 'var(--border)', color: 'var(--text)' }}
              />
            </div>
          )}
          <div className="flex-1 min-w-48">
            <label className="block text-xs mb-1" style={{ color: 'var(--text-muted)' }}>Description</label>
            <input
              value={description}
              onChange={e => setDescription(e.target.value)}
              placeholder="Optional description"
              className="w-full px-3 py-1.5 rounded border text-sm outline-none focus:border-[var(--accent)]"
              style={{ backgroundColor: 'var(--bg)', borderColor: 'var(--border)', color: 'var(--text)' }}
            />
          </div>
        </div>
        {/* Actions */}
        <div className="flex items-end gap-2 shrink-0">
          <Link
            to={isEdit ? `/stacks/${id}` : '/stacks'}
            className="px-4 py-1.5 rounded border text-sm"
            style={{ borderColor: 'var(--border)', color: 'var(--text-muted)' }}
          >
            Cancel
          </Link>
          <button
            onClick={handleSave}
            disabled={saving}
            className="px-4 py-1.5 rounded text-sm font-medium disabled:opacity-50"
            style={{ backgroundColor: 'var(--accent)', color: '#0d1117' }}
          >
            {saving ? 'Saving…' : isEdit ? 'Save new version' : 'Create Stack'}
          </button>
        </div>
      </div>

      {/* Error banner */}
      {error && (
        <div className="mb-4 px-3 py-2 rounded text-sm" style={{ backgroundColor: '#2d1115', color: 'var(--error)', border: '1px solid #4a1a1f' }}>
          {error}
        </div>
      )}

      {/* Two-column editor area */}
      <div className="grid grid-cols-[280px_1fr] gap-4 flex-1 min-h-0">

        {/* ── Left: Env Vars ── */}
        <div className="flex flex-col gap-3 min-h-0 overflow-y-auto pr-1">
          <div className="flex items-center justify-between shrink-0">
            <h2 className="text-sm font-medium" style={{ color: 'var(--text)' }}>Env Variables</h2>
            <button
              onClick={handleEnvDownload}
              title="Download .env template"
              className="flex items-center gap-1 text-xs px-2 py-1 rounded border transition-colors hover:bg-[var(--surface-2)]"
              style={{ borderColor: 'var(--border)', color: 'var(--text-muted)' }}
            >
              <Download size={12} /> .env
            </button>
          </div>

          {/* Defined env var rows */}
          <div className="space-y-1.5">
            {envRows.map((row, idx) => (
              <div key={idx} className="flex items-center gap-1.5">
                <input
                  value={row.key}
                  onChange={e => patchEnvRow(idx, { key: e.target.value })}
                  placeholder="KEY"
                  className="flex-1 min-w-0 px-2 py-1 rounded border text-xs font-mono outline-none focus:border-[var(--accent)]"
                  style={{ backgroundColor: 'var(--bg)', borderColor: 'var(--border)', color: 'var(--text)' }}
                />
                <input
                  value={row.value}
                  onChange={e => patchEnvRow(idx, { value: e.target.value })}
                  placeholder="value"
                  className="flex-1 min-w-0 px-2 py-1 rounded border text-xs font-mono outline-none focus:border-[var(--accent)]"
                  style={{ backgroundColor: 'var(--bg)', borderColor: 'var(--border)', color: 'var(--text)' }}
                />
                <button
                  onClick={() => removeEnvRow(idx)}
                  className="shrink-0 p-1 rounded hover:bg-[var(--surface-2)]"
                  style={{ color: 'var(--text-muted)' }}
                  title="Remove"
                >
                  <Trash2 size={12} />
                </button>
              </div>
            ))}
          </div>

          <button
            onClick={addEnvRow}
            className="flex items-center gap-1 text-xs px-2 py-1 rounded border w-fit transition-colors hover:bg-[var(--surface-2)]"
            style={{ borderColor: 'var(--border)', color: 'var(--text-muted)' }}
          >
            <Plus size={12} /> Add variable
          </button>

          {/* Referenced-but-undefined vars */}
          {undefinedVars.length > 0 && (
            <div className="mt-2 rounded border p-3" style={{ borderColor: 'var(--border)', backgroundColor: 'var(--surface)' }}>
              <p className="text-xs font-medium mb-2" style={{ color: 'var(--text-muted)' }}>
                Referenced in compose (not defined here):
              </p>
              <div className="space-y-1">
                {undefinedVars.map(v => (
                  <div key={v} className="flex items-center gap-2">
                    <code className="text-xs font-mono" style={{ color: 'var(--accent)' }}>{v}</code>
                    <button
                      onClick={() => setEnvRows(r => [...r, { key: v, value: '' }])}
                      className="text-xs px-1.5 py-0.5 rounded border transition-colors hover:bg-[var(--surface-2)]"
                      style={{ borderColor: 'var(--border)', color: 'var(--text-muted)' }}
                      title="Add to env vars"
                    >
                      + add
                    </button>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* When no vars defined and no refs */}
          {envRows.length === 0 && referencedVars.length === 0 && (
            <p className="text-xs" style={{ color: 'var(--text-muted)' }}>
              No variables defined. Add env vars above, or use <code>{'${VAR}'}</code> in your compose.yaml to reference them.
            </p>
          )}
        </div>

        {/* ── Right: Compose YAML Editor ── */}
        <div className="flex flex-col min-h-0">
          {/* Toolbar */}
          <div className="flex items-center justify-between mb-2 shrink-0">
            <h2 className="text-sm font-medium" style={{ color: 'var(--text)' }}>compose.yaml</h2>
            <div className="flex items-center gap-2">
              <button
                onClick={() => fileInputRef.current?.click()}
                className="flex items-center gap-1 text-xs px-2 py-1 rounded border transition-colors hover:bg-[var(--surface-2)]"
                style={{ borderColor: 'var(--border)', color: 'var(--text-muted)' }}
                title="Upload compose file"
              >
                <Upload size={12} /> Upload
              </button>
              <button
                onClick={handleComposeDownload}
                disabled={!composeYaml}
                className="flex items-center gap-1 text-xs px-2 py-1 rounded border transition-colors hover:bg-[var(--surface-2)] disabled:opacity-40"
                style={{ borderColor: 'var(--border)', color: 'var(--text-muted)' }}
                title="Download compose.yaml"
              >
                <Download size={12} /> Download
              </button>
              <input
                ref={fileInputRef}
                type="file"
                accept=".yml,.yaml"
                className="hidden"
                onChange={handleComposeUpload}
              />
            </div>
          </div>

          {/* CodeMirror editor */}
          <div className="flex-1 min-h-0 rounded-lg border overflow-hidden" style={{ borderColor: 'var(--border)' }}>
            <CodeMirror
              value={composeYaml}
              onChange={setComposeYaml}
              extensions={cmExtensions}
              theme={oneDark}
              height="100%"
              minHeight="400px"
              placeholder={'services:\n  web:\n    image: nginx:alpine\n    ports:\n      - "80:80"'}
              basicSetup={{
                lineNumbers: true,
                foldGutter: true,
                autocompletion: true,
                indentOnInput: true,
              }}
              style={{ height: '100%', fontSize: '13px' }}
            />
          </div>
        </div>
      </div>
    </div>
  )
}
