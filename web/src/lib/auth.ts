import { createContext, useContext } from 'react'

export interface RoleBinding {
  id: string
  role: string      // admin | operator | viewer | secrets-manager
  serverId: string  // '' = global
  stackId: string   // '' = all stacks on that server
}

export interface AuthUser {
  id: string
  username: string
  displayName: string
  roles: string[]        // global role names (display only)
  bindings: RoleBinding[] // full binding list for RBAC gating
  hasPassword: boolean
  hasOidc: boolean
}

export interface AuthCtx {
  user: AuthUser | null
  loading: boolean
  login: (username: string, password: string) => Promise<void>
  // Returns the IdP RP-initiated logout URL for SSO sessions, or null for local ones.
  logout: () => Promise<string | null>
  refresh: () => Promise<void>
}

export const Ctx = createContext<AuthCtx>({
  user: null,
  loading: true,
  login: async () => {},
  logout: async () => null,
  refresh: async () => {},
})

export function useAuth() {
  return useContext(Ctx)
}

// Fetches the current user; returns null when unauthenticated (non-2xx) so the
// react-query query resolves with data rather than erroring. Used by AuthProvider.
export async function fetchCurrentUser(): Promise<AuthUser | null> {
  const res = await fetch('/orkestra.v1.AuthService/GetCurrentUser', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({}),
  })
  if (!res.ok) return null
  const data = await res.json()
  const rawBindings = Array.isArray(data.bindings) ? data.bindings : []
  return {
    id:          String(data.id ?? ''),
    username:    String(data.username ?? ''),
    displayName: String(data.displayName ?? data.display_name ?? ''),
    roles:       Array.isArray(data.roles) ? data.roles.map(String) : [],
    bindings:    rawBindings.map((b: Record<string, unknown>) => ({
      id:       String(b.id ?? ''),
      role:     String(b.role ?? ''),
      serverId: String(b.serverId ?? b.server_id ?? ''),
      stackId:  String(b.stackId ?? b.stack_id ?? ''),
    })),
    hasPassword: Boolean(data.hasPassword ?? data.has_password ?? false),
    hasOidc:     Boolean(data.hasOidc ?? data.has_oidc ?? false),
  }
}

const ROLE_WEIGHT: Record<string, number> = { viewer: 1, operator: 2, admin: 3 }

function bestRole(user: AuthUser, serverId: string, stackId: string): string {
  let best = ''
  let bestW = 0
  for (const b of user.bindings) {
    if (b.serverId !== '' && b.serverId !== serverId) continue
    if (b.stackId  !== '' && b.stackId  !== stackId)  continue
    const w = ROLE_WEIGHT[b.role] ?? 0
    if (w > bestW) { best = b.role; bestW = w }
  }
  return best
}

export function isAdmin(user: AuthUser | null): boolean {
  return user?.bindings.some(b => b.role === 'admin') ?? false
}

export function canManageSecrets(user: AuthUser | null): boolean {
  if (!user) return false
  return user.bindings.some(b => b.role === 'admin' || b.role === 'secrets-manager')
}

export function hasAnyOperator(user: AuthUser | null): boolean {
  if (!user) return false
  return user.bindings.some(b => b.role === 'admin' || b.role === 'operator')
}

export function canOperateOn(user: AuthUser | null, serverId: string, stackId = ''): boolean {
  if (!user) return false
  if (isAdmin(user)) return true
  return (ROLE_WEIGHT[bestRole(user, serverId, stackId)] ?? 0) >= ROLE_WEIGHT['operator']
}

export function canViewOn(user: AuthUser | null, serverId: string, stackId = ''): boolean {
  if (!user) return false
  if (isAdmin(user)) return true
  return (ROLE_WEIGHT[bestRole(user, serverId, stackId)] ?? 0) >= ROLE_WEIGHT['viewer']
}

export function canViewServer(user: AuthUser | null, serverId: string): boolean {
  if (!user) return false
  if (isAdmin(user)) return true
  return user.bindings.some(b =>
    b.role !== 'secrets-manager' &&
    (b.serverId === '' || b.serverId === serverId) &&
    (ROLE_WEIGHT[b.role] ?? 0) > 0
  )
}
