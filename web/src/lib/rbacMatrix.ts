/**
 * Conversion between a user's flat role-binding list and the two-dimensional
 * permission matrix rendered by the Users page.
 *
 * `buildMatrix` folds bindings into per-server rows; `matrixToBindings` folds a
 * matrix back into the binding list sent to the API. The two are inverses for
 * every state the UI can produce, so a bug here silently grants or revokes
 * permissions — hence they live outside the component and are unit-tested.
 */

import type { RoleBinding } from './auth'

/** A server as far as the matrix is concerned: its identity plus assigned stacks. */
export interface MatrixServer {
  id: string
  name: string
  assignments: { stackId: string }[]
}

/** A stack as far as the matrix is concerned. */
export interface MatrixStack {
  id: string
  name: string
}

export interface MatrixRow {
  serverId: string
  serverName: string
  role: '' | 'viewer' | 'operator'
  stackIds: string[]  // empty = no restriction (all stacks)
  expanded: boolean
  availableStacks: { id: string; name: string }[]
}

export interface MatrixState {
  admin: boolean
  secretsManager: boolean
  rows: MatrixRow[]  // rows[0] is always the global row (serverId='')
}

/** A binding to be created; the server assigns the id. */
export type DesiredBinding = Omit<RoleBinding, 'id'>

/** Label for the synthetic first row representing global (all-server) grants. */
export const GLOBAL_ROW_LABEL = 'Global (all servers)'

/** Build the matrix view from a user's existing bindings. */
export function buildMatrix(
  bindings: RoleBinding[],
  servers: MatrixServer[],
  stacks: MatrixStack[],
): MatrixState {
  const stackById = new Map(stacks.map(s => [s.id, s]))

  const admin = bindings.some(b => b.role === 'admin')
  const secretsManager = bindings.some(b => b.role === 'secrets-manager')

  function serverRole(serverId: string): '' | 'viewer' | 'operator' {
    const relevant = bindings.filter(b =>
      b.serverId === serverId && (b.role === 'viewer' || b.role === 'operator')
    )
    if (relevant.some(b => b.role === 'operator')) return 'operator'
    if (relevant.some(b => b.role === 'viewer')) return 'viewer'
    return ''
  }

  function stackIds(serverId: string, role: string): string[] {
    if (!role) return []
    if (bindings.some(b => b.serverId === serverId && b.role === role && b.stackId === ''))
      return []
    return bindings
      .filter(b => b.serverId === serverId && b.role === role && b.stackId !== '')
      .map(b => b.stackId)
  }

  const globalRole = serverRole('')
  const rows: MatrixRow[] = [
    {
      serverId: '',
      serverName: GLOBAL_ROW_LABEL,
      role: globalRole,
      stackIds: [],
      expanded: false,
      availableStacks: [],
    },
  ]

  for (const server of servers) {
    const role = serverRole(server.id)
    const ids = stackIds(server.id, role)
    const availableStacks = server.assignments
      .map(a => stackById.get(a.stackId))
      .filter((s): s is MatrixStack => s !== undefined)
      .map(s => ({ id: s.id, name: s.name }))

    rows.push({
      serverId: server.id,
      serverName: server.name,
      role,
      stackIds: ids,
      expanded: ids.length > 0,
      availableStacks,
    })
  }

  return { admin, secretsManager, rows }
}

/** Fold the matrix back into the binding list to persist. */
export function matrixToBindings(matrix: MatrixState): DesiredBinding[] {
  // When admin is on, only the admin binding matters — all other settings are
  // hidden and should not be persisted.
  if (matrix.admin) return [{ role: 'admin', serverId: '', stackId: '' }]

  const result: DesiredBinding[] = []
  if (matrix.secretsManager) result.push({ role: 'secrets-manager', serverId: '', stackId: '' })

  for (const row of matrix.rows) {
    if (!row.role) continue
    if (row.stackIds.length === 0) {
      result.push({ role: row.role, serverId: row.serverId, stackId: '' })
    } else {
      for (const stackId of row.stackIds) {
        result.push({ role: row.role, serverId: row.serverId, stackId })
      }
    }
  }
  return result
}
