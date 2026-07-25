import { describe, it, expect } from 'vitest'
import {
  buildMatrix,
  matrixToBindings,
  GLOBAL_ROW_LABEL,
  type MatrixServer,
  type MatrixStack,
  type MatrixState,
} from './rbacMatrix'
import type { RoleBinding } from './auth'

let nextId = 0
function binding(role: string, serverId = '', stackId = ''): RoleBinding {
  return { id: `b${nextId++}`, role, serverId, stackId }
}

const stacks: MatrixStack[] = [
  { id: 'st-a', name: 'stack-a' },
  { id: 'st-b', name: 'stack-b' },
]

const servers: MatrixServer[] = [
  { id: 's1', name: 'server-one', assignments: [{ stackId: 'st-a' }, { stackId: 'st-b' }] },
  { id: 's2', name: 'server-two', assignments: [{ stackId: 'st-a' }] },
]

describe('buildMatrix — shape', () => {
  it('always starts with the global row followed by one row per server', () => {
    const m = buildMatrix([], servers, stacks)
    expect(m.rows).toHaveLength(3)
    expect(m.rows[0]).toMatchObject({ serverId: '', serverName: GLOBAL_ROW_LABEL, role: '' })
    expect(m.rows[1]).toMatchObject({ serverId: 's1', serverName: 'server-one' })
    expect(m.rows[2]).toMatchObject({ serverId: 's2', serverName: 'server-two' })
  })

  it('reports no roles for a user without bindings', () => {
    const m = buildMatrix([], servers, stacks)
    expect(m.admin).toBe(false)
    expect(m.secretsManager).toBe(false)
    expect(m.rows.every(r => r.role === '')).toBe(true)
  })

  it('resolves assigned stacks to names and drops unknown ids', () => {
    const withGhost: MatrixServer[] = [
      { id: 's1', name: 'server-one', assignments: [{ stackId: 'st-a' }, { stackId: 'deleted' }] },
    ]
    const m = buildMatrix([], withGhost, stacks)
    expect(m.rows[1].availableStacks).toEqual([{ id: 'st-a', name: 'stack-a' }])
  })

  it('leaves the global row without selectable stacks', () => {
    const m = buildMatrix([], servers, stacks)
    expect(m.rows[0].availableStacks).toEqual([])
    expect(m.rows[0].stackIds).toEqual([])
  })
})

describe('buildMatrix — role resolution', () => {
  it('detects the admin flag', () => {
    expect(buildMatrix([binding('admin')], servers, stacks).admin).toBe(true)
  })

  it('detects the secrets-manager flag', () => {
    const m = buildMatrix([binding('secrets-manager')], servers, stacks)
    expect(m.secretsManager).toBe(true)
    expect(m.admin).toBe(false)
  })

  it('places a global viewer on the global row', () => {
    const m = buildMatrix([binding('viewer')], servers, stacks)
    expect(m.rows[0].role).toBe('viewer')
    expect(m.rows[1].role).toBe('')
  })

  it('places a server-scoped role on that server row only', () => {
    const m = buildMatrix([binding('operator', 's1')], servers, stacks)
    expect(m.rows[0].role).toBe('')
    expect(m.rows[1].role).toBe('operator')
    expect(m.rows[2].role).toBe('')
  })

  it('prefers operator over viewer on the same server', () => {
    const m = buildMatrix([binding('viewer', 's1'), binding('operator', 's1')], servers, stacks)
    expect(m.rows[1].role).toBe('operator')
  })
})

describe('buildMatrix — stack scoping', () => {
  it('collects stack-scoped bindings and expands the row', () => {
    const m = buildMatrix([binding('operator', 's1', 'st-a')], servers, stacks)
    expect(m.rows[1].role).toBe('operator')
    expect(m.rows[1].stackIds).toEqual(['st-a'])
    expect(m.rows[1].expanded).toBe(true)
  })

  it('treats a server-wide binding as "no stack restriction"', () => {
    const m = buildMatrix([binding('operator', 's1')], servers, stacks)
    expect(m.rows[1].stackIds).toEqual([])
    expect(m.rows[1].expanded).toBe(false)
  })

  it('lets a server-wide binding absorb redundant stack-scoped ones', () => {
    const m = buildMatrix(
      [binding('operator', 's1'), binding('operator', 's1', 'st-a')],
      servers,
      stacks,
    )
    // The blanket grant already covers st-a, so no restriction is shown.
    expect(m.rows[1].stackIds).toEqual([])
  })

  it('keeps multiple stack-scoped bindings for the same server', () => {
    const m = buildMatrix(
      [binding('viewer', 's1', 'st-a'), binding('viewer', 's1', 'st-b')],
      servers,
      stacks,
    )
    expect(m.rows[1].role).toBe('viewer')
    expect(m.rows[1].stackIds).toEqual(['st-a', 'st-b'])
  })
})

describe('matrixToBindings', () => {
  function emptyMatrix(): MatrixState {
    return buildMatrix([], servers, stacks)
  }

  it('collapses to a single binding when admin is set', () => {
    const m = emptyMatrix()
    m.admin = true
    m.secretsManager = true
    m.rows[1].role = 'operator'
    expect(matrixToBindings(m)).toEqual([{ role: 'admin', serverId: '', stackId: '' }])
  })

  it('emits the secrets-manager binding globally', () => {
    const m = emptyMatrix()
    m.secretsManager = true
    expect(matrixToBindings(m)).toEqual([
      { role: 'secrets-manager', serverId: '', stackId: '' },
    ])
  })

  it('skips rows without a role', () => {
    expect(matrixToBindings(emptyMatrix())).toEqual([])
  })

  it('emits one server-wide binding for an unrestricted row', () => {
    const m = emptyMatrix()
    m.rows[1].role = 'operator'
    expect(matrixToBindings(m)).toEqual([{ role: 'operator', serverId: 's1', stackId: '' }])
  })

  it('emits one binding per selected stack', () => {
    const m = emptyMatrix()
    m.rows[1].role = 'viewer'
    m.rows[1].stackIds = ['st-a', 'st-b']
    expect(matrixToBindings(m)).toEqual([
      { role: 'viewer', serverId: 's1', stackId: 'st-a' },
      { role: 'viewer', serverId: 's1', stackId: 'st-b' },
    ])
  })

  it('emits the global row with an empty serverId', () => {
    const m = emptyMatrix()
    m.rows[0].role = 'viewer'
    expect(matrixToBindings(m)).toEqual([{ role: 'viewer', serverId: '', stackId: '' }])
  })
})

describe('round trip', () => {
  // The dialog seeds its state with buildMatrix and saves matrixToBindings. Any binding
  // set the UI can display must therefore survive a save that changes nothing.
  function roundTrip(bindings: RoleBinding[]) {
    return matrixToBindings(buildMatrix(bindings, servers, stacks))
  }

  it('preserves an admin binding', () => {
    expect(roundTrip([binding('admin')])).toEqual([{ role: 'admin', serverId: '', stackId: '' }])
  })

  it('preserves a global viewer', () => {
    expect(roundTrip([binding('viewer')])).toEqual([
      { role: 'viewer', serverId: '', stackId: '' },
    ])
  })

  it('preserves a server-scoped operator', () => {
    expect(roundTrip([binding('operator', 's1')])).toEqual([
      { role: 'operator', serverId: 's1', stackId: '' },
    ])
  })

  it('preserves stack-scoped bindings', () => {
    expect(roundTrip([binding('viewer', 's1', 'st-a'), binding('viewer', 's1', 'st-b')])).toEqual([
      { role: 'viewer', serverId: 's1', stackId: 'st-a' },
      { role: 'viewer', serverId: 's1', stackId: 'st-b' },
    ])
  })

  it('preserves secrets-manager combined with a server role', () => {
    expect(roundTrip([binding('secrets-manager'), binding('operator', 's2')])).toEqual([
      { role: 'secrets-manager', serverId: '', stackId: '' },
      { role: 'operator', serverId: 's2', stackId: '' },
    ])
  })

  it('is idempotent when applied twice', () => {
    const start = [binding('viewer'), binding('operator', 's1', 'st-a')]
    const once = roundTrip(start)
    const twice = matrixToBindings(
      buildMatrix(once.map((b, i) => ({ ...b, id: `r${i}` })), servers, stacks),
    )
    expect(twice).toEqual(once)
  })

  it('drops a redundant stack binding already covered server-wide', () => {
    // Lossy but semantically safe: the blanket grant is a superset.
    expect(roundTrip([binding('operator', 's1'), binding('operator', 's1', 'st-a')])).toEqual([
      { role: 'operator', serverId: 's1', stackId: '' },
    ])
  })

  it('DOWNGRADE: a server-wide viewer is lost when a stack-scoped operator exists', () => {
    // The matrix models exactly one role per server, so "viewer everywhere on s1 plus
    // operator on st-a" cannot be represented: the row becomes operator/[st-a] and the
    // server-wide viewer grant disappears on save. The user silently loses view access
    // to st-b. Asserted here to pin the behaviour, not to endorse it — see issue #50.
    expect(roundTrip([binding('viewer', 's1'), binding('operator', 's1', 'st-a')])).toEqual([
      { role: 'operator', serverId: 's1', stackId: 'st-a' },
    ])
  })
})
