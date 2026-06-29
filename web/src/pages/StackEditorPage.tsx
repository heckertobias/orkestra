import { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useParams, Link } from 'react-router-dom'
import { ArrowLeft, Download, Plus, Trash2, Upload } from 'lucide-react'
import CodeMirror from '@uiw/react-codemirror'
import { yaml } from '@codemirror/lang-yaml'
import { oneDark } from '@codemirror/theme-one-dark'
import { linter, lintGutter, type Diagnostic } from '@codemirror/lint'
import { keymap } from '@codemirror/view'
import { Prec } from '@codemirror/state'
import * as jsyaml from 'js-yaml'
import { downloadText, readTextFile } from '@/lib/files'
import { extractComposeVars, extractComposeVarDefaults, toDotEnv, renameComposeVar, removeComposeVar } from '@/lib/env'

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

/**
 * Custom Enter handler implementing simple YAML auto-indent: a line ending in
 * `:` indents the next line one level deeper (2 spaces); otherwise the new line
 * keeps the current indentation. High precedence so it runs before CodeMirror's
 * default newline-and-indent.
 */
const yamlAutoIndent = Prec.high(keymap.of([{
  key: 'Enter',
  run: (view) => {
    const { state } = view
    const { from, to } = state.selection.main
    if (from !== to) return false // let default handle non-empty selections
    const line = state.doc.lineAt(from)
    const indent = (line.text.match(/^[ \t]*/) ?? [''])[0]
    const beforeCursor = line.text.slice(0, from - line.from)
    const trimmed = beforeCursor.replace(/\s+#.*$/, '').trimEnd()
    const newIndent = trimmed.endsWith(':') ? indent + '  ' : indent
    const insert = '\n' + newIndent
    view.dispatch({
      changes: { from, to, insert },
      selection: { anchor: from + insert.length },
      scrollIntoView: true,
    })
    return true
  },
}]))

const cmExtensions = [yamlAutoIndent, yaml(), yamlSyntaxLinter, composeSematicLinter, lintGutter()]

// ─────────────────────────────────────────────────────────────────────────────

/** An entry in the env-var list. `manual` entries persist regardless of the
 *  compose YAML; non-manual entries are derived from `${VAR}` references and are
 *  removed automatically when their reference leaves the YAML. */
interface EnvVar {
  id: string
  name: string
  manual: boolean
}

function newEnvVar(name: string, manual: boolean): EnvVar {
  return { id: crypto.randomUUID(), name, manual }
}

export function StackEditorPage() {
  const { id } = useParams<{ id?: string }>()
  const navigate = useNavigate()
  const isEdit = Boolean(id)

  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [composeYaml, setComposeYaml] = useState('')
  const [envVars, setEnvVars] = useState<EnvVar[]>([])
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
          const yamlText = String(latest.composeYaml ?? latest.compose_yaml ?? '')
          setComposeYaml(yamlText)
          const names = (latest.envVarNames ?? latest.env_var_names ?? []) as string[]
          const referenced = new Set(extractComposeVars(yamlText))
          // Names not currently referenced in the YAML are treated as manual;
          // referenced ones are re-derived (and kept in sync) from the YAML.
          setEnvVars(names.map(n => newEnvVar(n, !referenced.has(n))))
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
    setYaml(text)
    // reset so the same file can be re-selected
    e.target.value = ''
  }

  function handleComposeDownload() {
    downloadText('compose.yaml', composeYaml, 'text/yaml')
  }

  // ── Env var helpers ────────────────────────────────────────────────────────

  const referencedVars = useMemo(() => extractComposeVars(composeYaml), [composeYaml])
  const referencedSet = useMemo(() => new Set(referencedVars), [referencedVars])
  const referencedDefaults = useMemo(() => extractComposeVarDefaults(composeYaml), [composeYaml])

  // Display order: referenced entries first, then manual, alphabetical within each
  // group. Sorted view only — state order stays stable for reconcile().
  const displayVars = useMemo(
    () => [...envVars].sort((a, b) => {
      const ar = a.name.trim() !== '' && referencedSet.has(a.name) ? 0 : 1
      const br = b.name.trim() !== '' && referencedSet.has(b.name) ? 0 : 1
      if (ar !== br) return ar - br
      // empty (freshly-added, unnamed) rows sort to the bottom of their group
      if (!a.name) return 1
      if (!b.name) return -1
      return a.name.localeCompare(b.name)
    }),
    [envVars, referencedSet],
  )

  // Reconcile a var list against the set of names referenced in the YAML:
  // keep manual entries + still-referenced ones, append newly-referenced names.
  function reconcile(list: EnvVar[], refNames: string[]): EnvVar[] {
    const refSet = new Set(refNames)
    const present = new Set(list.map(v => v.name))
    const kept = list.filter(v => v.manual || refSet.has(v.name))
    const added = refNames.filter(n => !present.has(n)).map(n => newEnvVar(n, false))
    return kept.length === list.length && added.length === 0 ? list : [...kept, ...added]
  }

  // Single entry point for YAML changes — updates the editor and keeps the
  // var list in sync (so `${VAR}` references appear/disappear automatically).
  function setYaml(next: string) {
    setComposeYaml(next)
    setEnvVars(prev => reconcile(prev, extractComposeVars(next)))
  }

  function addVar() {
    setEnvVars(v => [...v, newEnvVar('', true)])
  }

  function renameVar(id: string, nextName: string) {
    const cur = envVars.find(v => v.id === id)
    if (!cur) return
    // If the old name is a live `${VAR}` reference, rewrite it in the YAML too.
    const rewrite = !!cur.name && cur.name !== nextName && referencedSet.has(cur.name)
    const newYaml = rewrite ? renameComposeVar(composeYaml, cur.name, nextName) : composeYaml
    const refNames = extractComposeVars(newYaml)
    setEnvVars(prev => reconcile(prev.map(v => v.id === id ? { ...v, name: nextName } : v), refNames))
    if (rewrite) setComposeYaml(newYaml)
  }

  function removeVar(id: string) {
    const cur = envVars.find(v => v.id === id)
    if (!cur) return
    // Referenced entries: strip the reference from the YAML so list/YAML stay in sync.
    const rewrite = !!cur.name && referencedSet.has(cur.name)
    const newYaml = rewrite ? removeComposeVar(composeYaml, cur.name) : composeYaml
    const refNames = extractComposeVars(newYaml)
    setEnvVars(prev => reconcile(prev.filter(v => v.id !== id), refNames))
    if (rewrite) setComposeYaml(newYaml)
  }

  function handleEnvDownload() {
    const names = [...new Set([
      ...envVars.map(v => v.name.trim()).filter(Boolean),
      ...referencedVars,
    ])].sort()
    downloadText('.env', toDotEnv(names, {}))
  }

  // ── Save ──────────────────────────────────────────────────────────────────

  async function handleSave() {
    if (!name.trim()) { setError('Name is required'); return }
    setSaving(true)
    setError(null)
    try {
      const envVarNames = [...new Set([
        ...envVars.map(v => v.name.trim()).filter(Boolean),
        ...referencedVars,
      ])].sort()

      if (isEdit) {
        // UpdateStack → creates a new immutable version
        const res = await fetch('/orkestra.v1.StackService/UpdateStack', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            id,
            description,
            composeYaml,
            envVarNames,
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
            envVarNames,
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

          <p className="text-xs shrink-0" style={{ color: 'var(--text-muted)' }}>
            Declare the variables this stack needs. Values are filled in per server when you deploy.
          </p>

          {/* Variable name rows (names only — values set at deploy time) */}
          <div className="space-y-1.5">
            {displayVars.map((v) => {
              const isReferenced = v.name.trim() !== '' && referencedSet.has(v.name)
              const hasDefault = isReferenced && v.name in referencedDefaults
              return (
                <div key={v.id} className="flex items-center gap-1.5">
                  <input
                    value={v.name}
                    onChange={e => renameVar(v.id, e.target.value)}
                    placeholder="VAR_NAME"
                    className="flex-1 min-w-0 px-2 py-1 rounded border text-xs font-mono outline-none focus:border-[var(--accent)]"
                    style={{ backgroundColor: 'var(--bg)', borderColor: 'var(--border)', color: 'var(--text)' }}
                  />
                  {isReferenced && (
                    <span
                      className="shrink-0 text-[10px] px-1 py-0.5 rounded"
                      style={{ backgroundColor: 'var(--surface-2)', color: 'var(--text-muted)' }}
                      title="Referenced by ${...} in compose.yaml"
                    >
                      referenced
                    </span>
                  )}
                  {hasDefault && (
                    <span
                      className="shrink-0 text-[10px] font-mono px-1 py-0.5 rounded truncate max-w-24"
                      style={{ backgroundColor: 'var(--bg)', color: 'var(--text-muted)' }}
                      title={`Default from compose: ${referencedDefaults[v.name] || '(empty)'}`}
                    >
                      default: {referencedDefaults[v.name] || '(empty)'}
                    </span>
                  )}
                  {!isReferenced && (
                    <button
                      onClick={() => removeVar(v.id)}
                      className="shrink-0 p-1 rounded hover:bg-[var(--surface-2)]"
                      style={{ color: 'var(--text-muted)' }}
                      title="Remove"
                    >
                      <Trash2 size={12} />
                    </button>
                  )}
                </div>
              )
            })}
          </div>

          <button
            onClick={addVar}
            className="flex items-center gap-1 text-xs px-2 py-1 rounded border w-fit transition-colors hover:bg-[var(--surface-2)]"
            style={{ borderColor: 'var(--border)', color: 'var(--text-muted)' }}
          >
            <Plus size={12} /> Add variable
          </button>

          {/* When no vars defined and no refs */}
          {envVars.length === 0 && (
            <p className="text-xs" style={{ color: 'var(--text-muted)' }}>
              No variables yet. Add one above, or use <code>{'${VAR}'}</code> in your compose.yaml to reference them.
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
              onChange={setYaml}
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
