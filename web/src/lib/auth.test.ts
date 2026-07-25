import { describe, it, expect, vi, afterEach } from 'vitest'
import {
  fetchCurrentUser,
  isAdmin,
  canManageSecrets,
  hasAnyOperator,
  canOperateOn,
  canViewOn,
  canViewServer,
  type AuthUser,
  type RoleBinding,
} from './auth'

// These gates mirror internal/master/auth/rbac.go. The backend is authoritative — the
// frontend copy exists only to hide controls the API would reject. The cases below are
// kept aligned with rbac_test.go so drift between the two shows up as a failing test
// rather than as a button that 403s.

let nextId = 0
function binding(role: string, serverId = '', stackId = ''): RoleBinding {
  return { id: `b${nextId++}`, role, serverId, stackId }
}

function user(...bindings: RoleBinding[]): AuthUser {
  return {
    id: 'u1',
    username: 'test',
    displayName: 'Test',
    roles: bindings.map(b => b.role),
    bindings,
    hasPassword: true,
    hasOidc: false,
  }
}

describe('RBAC gates — unauthenticated', () => {
  it('denies everything for a null user', () => {
    expect(isAdmin(null)).toBe(false)
    expect(canManageSecrets(null)).toBe(false)
    expect(hasAnyOperator(null)).toBe(false)
    expect(canOperateOn(null, 's1')).toBe(false)
    expect(canViewOn(null, 's1')).toBe(false)
    expect(canViewServer(null, 's1')).toBe(false)
  })

  it('denies everything for a user with no bindings', () => {
    const u = user()
    expect(isAdmin(u)).toBe(false)
    expect(canManageSecrets(u)).toBe(false)
    expect(hasAnyOperator(u)).toBe(false)
    expect(canOperateOn(u, 's1')).toBe(false)
    expect(canViewOn(u, 's1')).toBe(false)
    expect(canViewServer(u, 's1')).toBe(false)
  })
})

describe('RBAC gates — admin', () => {
  const admin = user(binding('admin'))

  it('grants every gate regardless of scope', () => {
    expect(isAdmin(admin)).toBe(true)
    expect(canManageSecrets(admin)).toBe(true)
    expect(hasAnyOperator(admin)).toBe(true)
    expect(canOperateOn(admin, 'any-server', 'any-stack')).toBe(true)
    expect(canViewOn(admin, 'any-server', 'any-stack')).toBe(true)
    expect(canViewServer(admin, 'any-server')).toBe(true)
  })
})

describe('RBAC gates — secrets-manager', () => {
  const sm = user(binding('secrets-manager'))

  it('grants secret management only', () => {
    expect(canManageSecrets(sm)).toBe(true)
    expect(isAdmin(sm)).toBe(false)
    expect(hasAnyOperator(sm)).toBe(false)
  })

  it('carries no view or operate rights', () => {
    // secrets-manager has no role weight, so it never wins bestRole().
    expect(canViewOn(sm, 's1')).toBe(false)
    expect(canOperateOn(sm, 's1')).toBe(false)
    expect(canViewServer(sm, 's1')).toBe(false)
  })
})

describe('RBAC gates — global roles', () => {
  it('lets a global viewer view any server but not operate', () => {
    const u = user(binding('viewer'))
    expect(canViewOn(u, 's1')).toBe(true)
    expect(canViewOn(u, 's2', 'stack-x')).toBe(true)
    expect(canViewServer(u, 's1')).toBe(true)
    expect(canOperateOn(u, 's1')).toBe(false)
    expect(hasAnyOperator(u)).toBe(false)
  })

  it('lets a global operator both view and operate', () => {
    const u = user(binding('operator'))
    expect(canViewOn(u, 's1')).toBe(true)
    expect(canOperateOn(u, 's1')).toBe(true)
    expect(hasAnyOperator(u)).toBe(true)
    expect(isAdmin(u)).toBe(false)
  })
})

describe('RBAC gates — server-scoped roles', () => {
  const u = user(binding('operator', 's1'))

  it('applies only to the bound server', () => {
    expect(canOperateOn(u, 's1')).toBe(true)
    expect(canViewOn(u, 's1')).toBe(true)
    expect(canViewServer(u, 's1')).toBe(true)
  })

  it('does not leak to other servers', () => {
    expect(canOperateOn(u, 's2')).toBe(false)
    expect(canViewOn(u, 's2')).toBe(false)
    expect(canViewServer(u, 's2')).toBe(false)
  })

  it('covers every stack on that server', () => {
    expect(canOperateOn(u, 's1', 'any-stack')).toBe(true)
  })
})

describe('RBAC gates — stack-scoped roles', () => {
  const u = user(binding('operator', 's1', 'stack-a'))

  it('grants access to the bound stack', () => {
    expect(canOperateOn(u, 's1', 'stack-a')).toBe(true)
    expect(canViewOn(u, 's1', 'stack-a')).toBe(true)
  })

  it('denies access to other stacks on the same server', () => {
    expect(canOperateOn(u, 's1', 'stack-b')).toBe(false)
    expect(canViewOn(u, 's1', 'stack-b')).toBe(false)
  })

  it('grants no blanket server-wide access', () => {
    // Querying without a stack must not match a stack-scoped binding.
    expect(canOperateOn(u, 's1')).toBe(false)
  })

  it('still lets the server appear in the server list', () => {
    // canViewServer is deliberately looser so the server stays navigable.
    expect(canViewServer(u, 's1')).toBe(true)
    expect(canViewServer(u, 's2')).toBe(false)
  })
})

describe('RBAC gates — role precedence', () => {
  it('takes the strongest matching binding', () => {
    const u = user(binding('viewer', 's1'), binding('operator', 's1'))
    expect(canOperateOn(u, 's1')).toBe(true)
  })

  it('is order-independent', () => {
    const u = user(binding('operator', 's1'), binding('viewer', 's1'))
    expect(canOperateOn(u, 's1')).toBe(true)
  })

  it('ignores unknown role names', () => {
    const u = user(binding('wizard', 's1'))
    expect(canViewOn(u, 's1')).toBe(false)
    expect(canOperateOn(u, 's1')).toBe(false)
  })

  it('combines a global viewer with a server-scoped operator', () => {
    const u = user(binding('viewer'), binding('operator', 's1'))
    expect(canOperateOn(u, 's1')).toBe(true)
    expect(canOperateOn(u, 's2')).toBe(false)
    expect(canViewOn(u, 's2')).toBe(true)
  })
})

describe('fetchCurrentUser', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  function mockResponse(ok: boolean, body: unknown) {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok,
      json: async () => body,
    }))
  }

  it('returns null when unauthenticated', async () => {
    mockResponse(false, {})
    expect(await fetchCurrentUser()).toBeNull()
  })

  it('reads a camelCase payload', async () => {
    mockResponse(true, {
      id: 'u1',
      username: 'alice',
      displayName: 'Alice',
      roles: ['admin'],
      bindings: [{ id: 'b1', role: 'admin', serverId: 's1', stackId: 'st1' }],
      hasPassword: true,
      hasOidc: false,
    })
    expect(await fetchCurrentUser()).toEqual({
      id: 'u1',
      username: 'alice',
      displayName: 'Alice',
      roles: ['admin'],
      bindings: [{ id: 'b1', role: 'admin', serverId: 's1', stackId: 'st1' }],
      hasPassword: true,
      hasOidc: false,
    })
  })

  it('reads an equivalent snake_case payload identically', async () => {
    mockResponse(true, {
      id: 'u1',
      username: 'alice',
      display_name: 'Alice',
      roles: ['admin'],
      bindings: [{ id: 'b1', role: 'admin', server_id: 's1', stack_id: 'st1' }],
      has_password: true,
      has_oidc: true,
    })
    const u = await fetchCurrentUser()
    expect(u?.displayName).toBe('Alice')
    expect(u?.bindings).toEqual([{ id: 'b1', role: 'admin', serverId: 's1', stackId: 'st1' }])
    expect(u?.hasPassword).toBe(true)
    expect(u?.hasOidc).toBe(true)
  })

  it('fills in defaults for a minimal payload', async () => {
    mockResponse(true, {})
    expect(await fetchCurrentUser()).toEqual({
      id: '',
      username: '',
      displayName: '',
      roles: [],
      bindings: [],
      hasPassword: false,
      hasOidc: false,
    })
  })

  it('tolerates a non-array bindings field', async () => {
    mockResponse(true, { id: 'u1', bindings: null })
    const u = await fetchCurrentUser()
    expect(u?.bindings).toEqual([])
  })

  it('produces a user the RBAC gates accept', async () => {
    mockResponse(true, {
      id: 'u1',
      bindings: [{ id: 'b1', role: 'operator', server_id: 's1', stack_id: '' }],
    })
    const u = await fetchCurrentUser()
    expect(canOperateOn(u, 's1')).toBe(true)
    expect(canOperateOn(u, 's2')).toBe(false)
  })
})
