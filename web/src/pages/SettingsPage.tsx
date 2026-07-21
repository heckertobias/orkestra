import { useState, useEffect } from 'react'
import { Key, Shield, Plus, Trash2, Eye, EyeOff, Copy, Lock, Mail, Globe } from 'lucide-react'
import { useToast } from '@/components/ui/toast-context'

// ─── Types ───────────────────────────────────────────────────────────────────

interface OIDCConfig {
  enabled: boolean
  issuerUrl: string
  clientId: string
  scopes: string[]
  claimMapping: Record<string, string>
  groupsClaim: string
}

interface APIKey {
  id: string
  name: string
  createdAt: number
  lastUsedAt: number
  expiresAt: number
  revoked: boolean
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

function connectPost(procedure: string, body: unknown) {
  return fetch(`/orkestra.v1.AuthService/${procedure}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
}

// ─── Main Component ───────────────────────────────────────────────────────────

export function SettingsPage() {
  const [tab, setTab] = useState<'server' | 'oidc' | 'policy' | 'smtp' | 'apikeys'>('server')

  return (
    <div>
      <h1 className="text-xl font-semibold mb-1" style={{ color: 'var(--text)' }}>Settings</h1>
      <p className="mb-6" style={{ color: 'var(--text-muted)' }}>Server, SSO, email, password policy, and API keys.</p>

      <div className="flex gap-1 mb-6 border-b" style={{ borderColor: 'var(--border)' }}>
        {([
          ['server',  'General',          Globe],
          ['oidc',    'SSO / OIDC',      Shield],
          ['policy',  'Password Policy',  Lock],
          ['smtp',    'Email / SMTP',     Mail],
          ['apikeys', 'API Keys',         Key],
        ] as const).map(([id, label, Icon]) => (
          <button
            key={id}
            onClick={() => setTab(id)}
            className="flex items-center gap-2 px-4 py-2 text-sm border-b-2 -mb-px transition-colors"
            style={{
              borderColor: tab === id ? 'var(--accent)' : 'transparent',
              color: tab === id ? 'var(--accent)' : 'var(--text-muted)',
            }}
          >
            <Icon size={14} />
            {label}
          </button>
        ))}
      </div>

      {tab === 'server'  && <ServerTab />}
      {tab === 'oidc'    && <OIDCTab />}
      {tab === 'policy'  && <PasswordPolicyTab />}
      {tab === 'smtp'    && <SMTPTab />}
      {tab === 'apikeys' && <APIKeysTab />}
    </div>
  )
}

// ─── Server / General Tab ─────────────────────────────────────────────────────

function ServerTab() {
  const { toast } = useToast()
  const [publicUrl, setPublicUrl] = useState('')
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    connectPost('GetServerConfig', {})
      .then(r => r.json())
      .then(d => setPublicUrl(String(d.publicUrl ?? d.public_url ?? '')))
      .catch(() => {})
      .finally(() => setLoading(false))
  }, [])

  async function handleSave(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    try {
      const res = await connectPost('UpdateServerConfig', { public_url: publicUrl })
      if (!res.ok) throw new Error(await res.text())
      toast('Server configuration saved', 'success')
    } catch (err) {
      toast(String(err), 'error')
    } finally {
      setBusy(false)
    }
  }

  if (loading) return <Skeleton lines={3} />

  return (
    <form onSubmit={handleSave} className="max-w-lg space-y-5">
      <Field
        label="Public URL"
        hint="Browser-facing base URL of this orkestra instance (e.g. https://orkestra.example.com). Used for the OIDC redirect, email links, and the setup link. Overrides the ORKESTRA_PUBLIC_URL env var; leave blank to use it. Behind TLS the OIDC redirect URI registered at the IdP must be this URL plus /auth/oidc/callback."
      >
        <input value={publicUrl} onChange={e => setPublicUrl(e.target.value)}
          className="input" placeholder="https://orkestra.example.com" />
      </Field>

      <button type="submit" disabled={busy}
        className="px-4 py-2 rounded text-sm font-medium disabled:opacity-50"
        style={{ backgroundColor: 'var(--accent)', color: '#0d1117' }}>
        {busy ? 'Saving…' : 'Save configuration'}
      </button>
    </form>
  )
}

// ─── OIDC Tab ─────────────────────────────────────────────────────────────────

function OIDCTab() {
  const { toast } = useToast()
  const [cfg, setCfg] = useState<OIDCConfig>({
    enabled: false, issuerUrl: '', clientId: '', scopes: ['openid', 'profile', 'email'], claimMapping: {}, groupsClaim: 'groups',
  })
  const [clientSecret, setClientSecret] = useState('')
  const [showSecret, setShowSecret] = useState(false)
  const [busy, setBusy] = useState(false)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    connectPost('GetOIDCConfig', {})
      .then(r => r.json())
      .then(d => {
        setCfg({
          enabled: d.enabled ?? false,
          issuerUrl: d.issuerUrl ?? d.issuer_url ?? '',
          clientId: d.clientId ?? d.client_id ?? '',
          scopes: d.scopes ?? ['openid', 'profile', 'email'],
          claimMapping: d.claimMapping ?? d.claim_mapping ?? {},
          groupsClaim: d.groupsClaim ?? d.groups_claim ?? 'groups',
        })
      })
      .catch(() => {})
      .finally(() => setLoading(false))
  }, [])

  async function handleSave(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    try {
      const res = await connectPost('UpdateOIDCConfig', {
        enabled: cfg.enabled,
        issuerUrl: cfg.issuerUrl,
        clientId: cfg.clientId,
        clientSecret: clientSecret || undefined,
        scopes: cfg.scopes,
        claimMapping: cfg.claimMapping,
        groupsClaim: cfg.groupsClaim || 'groups',
      })
      if (!res.ok) throw new Error(await res.text())
      setClientSecret('')
      toast('OIDC configuration saved', 'success')
    } catch (err) {
      toast(String(err), 'error')
    } finally {
      setBusy(false)
    }
  }

  if (loading) return <Skeleton lines={6} />

  return (
    <form onSubmit={handleSave} className="max-w-lg space-y-5">
      <div className="flex items-center justify-between p-4 rounded-lg border"
        style={{ borderColor: 'var(--border)', backgroundColor: 'var(--surface)' }}>
        <div>
          <p className="text-sm font-medium" style={{ color: 'var(--text)' }}>Enable SSO / OIDC</p>
          <p className="text-xs" style={{ color: 'var(--text-muted)' }}>Allow users to sign in with your identity provider.</p>
        </div>
        <Toggle checked={cfg.enabled} onChange={v => setCfg(p => ({ ...p, enabled: v }))} />
      </div>

      <Field label="Issuer URL" hint="e.g. https://accounts.google.com">
        <input value={cfg.issuerUrl} onChange={e => setCfg(p => ({ ...p, issuerUrl: e.target.value }))}
          className="input" placeholder="https://sso.example.com" />
      </Field>

      <Field label="Client ID">
        <input value={cfg.clientId} onChange={e => setCfg(p => ({ ...p, clientId: e.target.value }))}
          className="input" placeholder="your-client-id" />
      </Field>

      <Field label="Client Secret" hint="Leave blank to keep the existing secret.">
        <div className="relative">
          <input
            type={showSecret ? 'text' : 'password'}
            value={clientSecret}
            onChange={e => setClientSecret(e.target.value)}
            className="input pr-10"
            placeholder="••••••••"
            autoComplete="new-password"
          />
          <button type="button" onClick={() => setShowSecret(s => !s)}
            className="absolute right-3 top-1/2 -translate-y-1/2 opacity-60 hover:opacity-100"
            style={{ color: 'var(--text-muted)' }}>
            {showSecret ? <EyeOff size={14} /> : <Eye size={14} />}
          </button>
        </div>
      </Field>

      <Field label="Scopes" hint="Space-separated list of OIDC scopes.">
        <input value={cfg.scopes.join(' ')}
          onChange={e => setCfg(p => ({ ...p, scopes: e.target.value.split(/\s+/).filter(Boolean) }))}
          className="input" placeholder="openid profile email" />
      </Field>

      <Field label="Groups claim" hint="The token claim that contains group memberships (default: groups).">
        <input value={cfg.groupsClaim}
          onChange={e => setCfg(p => ({ ...p, groupsClaim: e.target.value }))}
          className="input" placeholder="groups" />
      </Field>

      <div>
        <p className="text-xs mb-1 font-medium" style={{ color: 'var(--text-muted)' }}>
          Group value → Role mapping
          <span className="ml-1 font-normal">(map a group value to an orkestra role)</span>
        </p>
        <ClaimMappingEditor value={cfg.claimMapping} onChange={m => setCfg(p => ({ ...p, claimMapping: m }))} />
      </div>

      <button type="submit" disabled={busy}
        className="px-4 py-2 rounded text-sm font-medium disabled:opacity-50"
        style={{ backgroundColor: 'var(--accent)', color: '#0d1117' }}>
        {busy ? 'Saving…' : 'Save configuration'}
      </button>
    </form>
  )
}

function ClaimMappingEditor({ value, onChange }: {
  value: Record<string, string>
  onChange: (v: Record<string, string>) => void
}) {
  const entries = Object.entries(value)
  function update(i: number, k: string, v: string) {
    const next = [...entries]
    next[i] = [k, v]
    onChange(Object.fromEntries(next))
  }
  function remove(i: number) {
    const next = entries.filter((_, j) => j !== i)
    onChange(Object.fromEntries(next))
  }
  function add() {
    onChange({ ...value, '': 'viewer' })
  }
  return (
    <div className="space-y-2">
      {entries.map(([k, v], i) => (
        <div key={i} className="flex gap-2 items-center">
          <input value={k} onChange={e => update(i, e.target.value, v)} className="input flex-1" placeholder="group value (e.g. ork-admins)" />
          <span style={{ color: 'var(--text-muted)' }}>→</span>
          <select value={v} onChange={e => update(i, k, e.target.value)}
            className="input w-32"
            style={{ backgroundColor: 'var(--bg)' }}>
            {['admin', 'operator', 'viewer'].map(r => <option key={r} value={r}>{r}</option>)}
          </select>
          <button type="button" onClick={() => remove(i)} className="p-1 rounded hover:bg-[var(--surface-2)]" style={{ color: 'var(--text-muted)' }}>
            <Trash2 size={14} />
          </button>
        </div>
      ))}
      <button type="button" onClick={add}
        className="flex items-center gap-1 text-xs px-3 py-1.5 rounded border"
        style={{ borderColor: 'var(--border)', color: 'var(--text-muted)' }}>
        <Plus size={12} /> Add mapping
      </button>
    </div>
  )
}

// ─── API Keys Tab ─────────────────────────────────────────────────────────────

function APIKeysTab() {
  const { toast } = useToast()
  const [keys, setKeys] = useState<APIKey[]>([])
  const [loading, setLoading] = useState(true)
  const [newName, setNewName] = useState('')
  const [newKey, setNewKey] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  function loadKeys() {
    connectPost('ListAPIKeys', {})
      .then(r => r.json())
      .then(d => setKeys((d.keys ?? []).map((k: Record<string, unknown>) => ({
        id: String(k.id ?? ''),
        name: String(k.name ?? ''),
        createdAt: Number(k.createdAt ?? k.created_at ?? 0),
        lastUsedAt: Number(k.lastUsedAt ?? k.last_used_at ?? 0),
        expiresAt: Number(k.expiresAt ?? k.expires_at ?? 0),
        revoked: Boolean(k.revoked),
      }))))
      .catch(() => {})
      .finally(() => setLoading(false))
  }

  useEffect(loadKeys, [])

  async function handleCreate(e: React.FormEvent) {
    e.preventDefault()
    if (!newName.trim()) return
    setBusy(true)
    setNewKey(null)
    try {
      const res = await connectPost('CreateAPIKey', { name: newName.trim() })
      const d = await res.json()
      if (!res.ok) throw new Error(d.message ?? 'Failed')
      setNewKey(d.rawKey ?? d.raw_key ?? '')
      setNewName('')
      loadKeys()
      toast('API key created — copy it now, it won\'t be shown again.', 'success')
    } catch (err) {
      toast(String(err), 'error')
    } finally {
      setBusy(false)
    }
  }

  async function handleRevoke(id: string) {
    try {
      await connectPost('RevokeAPIKey', { id })
      loadKeys()
      toast('API key revoked', 'success')
    } catch {
      toast('Failed to revoke key', 'error')
    }
  }

  return (
    <div className="max-w-lg space-y-6">
      {/* New key raw value banner */}
      {newKey && (
        <div className="p-4 rounded-lg border" style={{ borderColor: 'var(--accent)', backgroundColor: 'rgba(126,226,42,0.06)' }}>
          <p className="text-xs font-medium mb-2" style={{ color: 'var(--accent)' }}>New API key — copy it now</p>
          <div className="flex gap-2 items-center">
            <code className="flex-1 text-xs break-all" style={{ color: 'var(--text)' }}>{newKey}</code>
            <button onClick={() => { navigator.clipboard.writeText(newKey); toast('Copied!', 'success') }}
              className="p-1.5 rounded hover:bg-[var(--surface-2)]" style={{ color: 'var(--text-muted)' }}>
              <Copy size={14} />
            </button>
          </div>
        </div>
      )}

      {/* Create form */}
      <form onSubmit={handleCreate} className="flex gap-2">
        <input value={newName} onChange={e => setNewName(e.target.value)}
          className="input flex-1" placeholder="Key name (e.g. CI/CD pipeline)" />
        <button type="submit" disabled={busy || !newName.trim()}
          className="px-3 py-2 rounded text-sm font-medium flex items-center gap-2 disabled:opacity-50"
          style={{ backgroundColor: 'var(--accent)', color: '#0d1117' }}>
          <Plus size={14} /> Create
        </button>
      </form>

      {/* Keys list */}
      {loading ? <Skeleton lines={3} /> : (
        <div className="rounded-lg border overflow-hidden" style={{ borderColor: 'var(--border)', backgroundColor: 'var(--surface)' }}>
          {keys.length === 0 ? (
            <div className="px-4 py-8 text-center text-sm" style={{ color: 'var(--text-muted)' }}>
              No API keys yet. Create one above.
            </div>
          ) : (
            <table className="w-full text-sm">
              <thead>
                <tr style={{ borderBottom: '1px solid var(--border)' }}>
                  {['Name', 'Created', 'Last used', ''].map(h => (
                    <th key={h} className="text-left px-4 py-3 text-xs font-medium" style={{ color: 'var(--text-muted)' }}>{h}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {keys.filter(k => !k.revoked).map(k => (
                  <tr key={k.id} style={{ borderBottom: '1px solid var(--border)' }}>
                    <td className="px-4 py-3 font-medium" style={{ color: 'var(--text)' }}>{k.name}</td>
                    <td className="px-4 py-3 text-xs" style={{ color: 'var(--text-muted)' }}>
                      {k.createdAt ? new Date(k.createdAt).toLocaleDateString() : '—'}
                    </td>
                    <td className="px-4 py-3 text-xs" style={{ color: 'var(--text-muted)' }}>
                      {k.lastUsedAt ? new Date(k.lastUsedAt).toLocaleString() : 'Never'}
                    </td>
                    <td className="px-4 py-3 text-right">
                      <button onClick={() => handleRevoke(k.id)}
                        className="p-1.5 rounded hover:bg-[var(--surface-2)]"
                        style={{ color: 'var(--text-muted)' }}
                        title="Revoke key">
                        <Trash2 size={14} />
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      )}
    </div>
  )
}

// ─── Password Policy Tab ──────────────────────────────────────────────────────

interface PolicyForm {
  minLength: number
  specialMin: number; specialMax: number
  digitMin: number;   digitMax: number
  upperMin: number;   upperMax: number
  lowerMin: number;   lowerMax: number
}

const emptyPolicy: PolicyForm = {
  minLength: 0,
  specialMin: 0, specialMax: 0,
  digitMin: 0,   digitMax: 0,
  upperMin: 0,   upperMax: 0,
  lowerMin: 0,   lowerMax: 0,
}

function PasswordPolicyTab() {
  const { toast } = useToast()
  const [form, setForm] = useState<PolicyForm>(emptyPolicy)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    connectPost('GetPasswordPolicy', {})
      .then(r => r.json())
      .then(d => setForm({
        minLength:  Number(d.minLength  ?? d.min_length  ?? 0),
        specialMin: Number(d.specialMin ?? d.special_min ?? 0),
        specialMax: Number(d.specialMax ?? d.special_max ?? 0),
        digitMin:   Number(d.digitMin   ?? d.digit_min   ?? 0),
        digitMax:   Number(d.digitMax   ?? d.digit_max   ?? 0),
        upperMin:   Number(d.upperMin   ?? d.upper_min   ?? 0),
        upperMax:   Number(d.upperMax   ?? d.upper_max   ?? 0),
        lowerMin:   Number(d.lowerMin   ?? d.lower_min   ?? 0),
        lowerMax:   Number(d.lowerMax   ?? d.lower_max   ?? 0),
      }))
      .catch(() => {})
      .finally(() => setLoading(false))
  }, [])

  async function handleSave(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    try {
      const res = await connectPost('UpdatePasswordPolicy', {
        min_length:  form.minLength,
        special_min: form.specialMin, special_max: form.specialMax,
        digit_min:   form.digitMin,   digit_max:   form.digitMax,
        upper_min:   form.upperMin,   upper_max:   form.upperMax,
        lower_min:   form.lowerMin,   lower_max:   form.lowerMax,
      })
      if (!res.ok) throw new Error(await res.text())
      toast('Password policy saved', 'success')
    } catch (err) {
      toast(String(err), 'error')
    } finally {
      setBusy(false)
    }
  }

  if (loading) return <Skeleton lines={6} />

  function num(key: keyof PolicyForm) {
    return (
      <input
        type="number" min={0}
        value={form[key] === 0 ? '' : form[key]}
        placeholder="0"
        onChange={e => setForm(f => ({ ...f, [key]: Number(e.target.value) || 0 }))}
        className="input w-20"
      />
    )
  }

  return (
    <form onSubmit={handleSave} className="max-w-lg space-y-5">
      <p className="text-xs" style={{ color: 'var(--text-muted)' }}>
        Applies to <strong>internal users only</strong> (not SSO/OIDC). Set to 0 for no limit.
      </p>

      <Field label="Minimum length">
        {num('minLength')}
      </Field>

      {([
        ['Special characters', 'specialMin', 'specialMax'],
        ['Digits',             'digitMin',   'digitMax'],
        ['Uppercase letters',  'upperMin',   'upperMax'],
        ['Lowercase letters',  'lowerMin',   'lowerMax'],
      ] as const).map(([label, minKey, maxKey]) => (
        <div key={label}>
          <label className="block text-xs mb-1 font-medium" style={{ color: 'var(--text-muted)' }}>{label}</label>
          <div className="flex items-center gap-2">
            <span className="text-xs" style={{ color: 'var(--text-muted)' }}>min</span>
            {num(minKey)}
            <span className="text-xs" style={{ color: 'var(--text-muted)' }}>max</span>
            {num(maxKey)}
          </div>
        </div>
      ))}

      <button type="submit" disabled={busy}
        className="px-4 py-2 rounded text-sm font-medium disabled:opacity-50"
        style={{ backgroundColor: 'var(--accent)', color: '#0d1117' }}>
        {busy ? 'Saving…' : 'Save policy'}
      </button>
    </form>
  )
}

// ─── SMTP Tab ─────────────────────────────────────────────────────────────────

interface SMTPForm {
  enabled: boolean
  host: string
  port: number
  username: string
  password: string
  fromAddress: string
  starttls: boolean
}

function SMTPTab() {
  const { toast } = useToast()
  const [form, setForm] = useState<SMTPForm>({
    enabled: false, host: '', port: 587, username: '', password: '', fromAddress: '', starttls: true,
  })
  const [showPw, setShowPw] = useState(false)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    connectPost('GetSMTPConfig', {})
      .then(r => r.json())
      .then(d => setForm(f => ({
        ...f,
        enabled:     Boolean(d.enabled ?? false),
        host:        String(d.host ?? ''),
        port:        Number(d.port ?? 587),
        username:    String(d.username ?? ''),
        fromAddress: String(d.fromAddress ?? d.from_address ?? ''),
        starttls:    Boolean(d.starttls ?? true),
      })))
      .catch(() => {})
      .finally(() => setLoading(false))
  }, [])

  async function handleSave(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    try {
      const res = await connectPost('UpdateSMTPConfig', {
        enabled:      form.enabled,
        host:         form.host,
        port:         form.port,
        username:     form.username,
        password:     form.password || undefined,
        from_address: form.fromAddress,
        starttls:     form.starttls,
      })
      if (!res.ok) throw new Error(await res.text())
      setForm(f => ({ ...f, password: '' }))
      toast('SMTP configuration saved', 'success')
    } catch (err) {
      toast(String(err), 'error')
    } finally {
      setBusy(false)
    }
  }

  if (loading) return <Skeleton lines={6} />

  return (
    <form onSubmit={handleSave} className="max-w-lg space-y-5">
      <div className="flex items-center justify-between p-4 rounded-lg border"
        style={{ borderColor: 'var(--border)', backgroundColor: 'var(--surface)' }}>
        <div>
          <p className="text-sm font-medium" style={{ color: 'var(--text)' }}>Enable email sending</p>
          <p className="text-xs" style={{ color: 'var(--text-muted)' }}>Send invite and password-reset emails.</p>
        </div>
        <Toggle checked={form.enabled} onChange={v => setForm(f => ({ ...f, enabled: v }))} />
      </div>

      <div className="flex gap-3">
        <Field label="SMTP Host" hint="e.g. smtp.example.com">
          <input value={form.host} onChange={e => setForm(f => ({ ...f, host: e.target.value }))}
            className="input" placeholder="smtp.example.com" />
        </Field>
        <Field label="Port">
          <input type="number" value={form.port} onChange={e => setForm(f => ({ ...f, port: Number(e.target.value) }))}
            className="input w-24" />
        </Field>
      </div>

      <Field label="Username">
        <input value={form.username} onChange={e => setForm(f => ({ ...f, username: e.target.value }))}
          className="input" placeholder="no-reply@example.com" autoComplete="off" />
      </Field>

      <Field label="Password" hint="Leave blank to keep the existing password.">
        <div className="relative">
          <input
            type={showPw ? 'text' : 'password'}
            value={form.password}
            onChange={e => setForm(f => ({ ...f, password: e.target.value }))}
            className="input pr-10" placeholder="••••••••" autoComplete="new-password"
          />
          <button type="button" onClick={() => setShowPw(s => !s)}
            className="absolute right-3 top-1/2 -translate-y-1/2 opacity-60 hover:opacity-100"
            style={{ color: 'var(--text-muted)' }}>
            {showPw ? <EyeOff size={14} /> : <Eye size={14} />}
          </button>
        </div>
      </Field>

      <Field label="From address">
        <input value={form.fromAddress} onChange={e => setForm(f => ({ ...f, fromAddress: e.target.value }))}
          className="input" placeholder="orkestra <no-reply@example.com>" />
      </Field>

      <div className="flex items-center justify-between p-4 rounded-lg border"
        style={{ borderColor: 'var(--border)', backgroundColor: 'var(--surface)' }}>
        <div>
          <p className="text-sm font-medium" style={{ color: 'var(--text)' }}>STARTTLS</p>
          <p className="text-xs" style={{ color: 'var(--text-muted)' }}>Recommended for port 587.</p>
        </div>
        <Toggle checked={form.starttls} onChange={v => setForm(f => ({ ...f, starttls: v }))} />
      </div>

      <button type="submit" disabled={busy}
        className="px-4 py-2 rounded text-sm font-medium disabled:opacity-50"
        style={{ backgroundColor: 'var(--accent)', color: '#0d1117' }}>
        {busy ? 'Saving…' : 'Save configuration'}
      </button>
    </form>
  )
}

// ─── Shared sub-components ────────────────────────────────────────────────────

function Field({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
  return (
    <div>
      <label className="block text-xs mb-1 font-medium" style={{ color: 'var(--text-muted)' }}>{label}</label>
      {children}
      {hint && <p className="text-xs mt-1" style={{ color: 'var(--text-muted)', opacity: 0.7 }}>{hint}</p>}
    </div>
  )
}

function Toggle({ checked, onChange }: { checked: boolean; onChange: (v: boolean) => void }) {
  return (
    <button
      type="button"
      onClick={() => onChange(!checked)}
      className="relative w-10 h-6 rounded-full transition-colors"
      style={{ backgroundColor: checked ? 'var(--accent)' : 'var(--border)' }}
    >
      <span
        className="absolute top-1 left-1 w-4 h-4 rounded-full bg-white transition-transform"
        style={{ transform: checked ? 'translateX(16px)' : 'translateX(0)' }}
      />
    </button>
  )
}

function Skeleton({ lines }: { lines: number }) {
  return (
    <div className="space-y-3">
      {Array.from({ length: lines }).map((_, i) => (
        <div key={i} className="h-9 rounded animate-pulse" style={{ backgroundColor: 'var(--surface-2)' }} />
      ))}
    </div>
  )
}
