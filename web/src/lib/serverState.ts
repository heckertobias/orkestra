/**
 * The agent-reported container inventory of one server (StackService.GetServerState).
 *
 * The Connect JSON API is not pinned to a casing, so every field is read in both camelCase and
 * snake_case — the same defensive mapping the other pages use.
 */

export type BadgeVariant = 'online' | 'offline' | 'warn' | 'error' | 'default'

export interface ServiceError {
  serviceName: string
  error: string
}

export interface ContainerRow {
  id: string
  name: string
  image: string
  serviceName: string
  state: string
  status: string
  health: string
  restartCount: number
  startedAt: number
  /** Stack this container belongs to, denormalised for a flat table. */
  stackId: string
  stackName: string
}

export interface StackState {
  stackId: string
  stackName: string
  runningVersionId: string
  runningVersion: number
  driftDetected: boolean
  driftDescription: string
  error: string
  serviceErrors: ServiceError[]
  containers: ContainerRow[]
}

export interface ServerState {
  serverId: string
  /** Unix ms of the last report; 0 = the agent has never reported. */
  reportedAt: number
  stacks: StackState[]
}

type Raw = Record<string, unknown>

function asArray(v: unknown): Raw[] {
  return Array.isArray(v) ? (v as Raw[]) : []
}

function str(...vals: unknown[]): string {
  for (const v of vals) {
    if (v !== undefined && v !== null) return String(v)
  }
  return ''
}

function num(...vals: unknown[]): number {
  for (const v of vals) {
    if (v !== undefined && v !== null) return Number(v) || 0
  }
  return 0
}

function mapServiceErrors(v: unknown): ServiceError[] {
  return asArray(v).map(e => ({
    serviceName: str(e.serviceName, e.service_name),
    error: str(e.error),
  }))
}

/** Maps the GetServerState JSON response into the shape the page renders. */
export function mapServerState(json: unknown): ServerState {
  const data = (json ?? {}) as Raw
  const stacks = asArray(data.stacks).map((s): StackState => {
    const stackId = str(s.stackId, s.stack_id)
    const stackName = str(s.stackName, s.stack_name)
    return {
      stackId,
      stackName,
      runningVersionId: str(s.runningVersionId, s.running_version_id),
      runningVersion: num(s.runningVersion, s.running_version),
      driftDetected: Boolean(s.driftDetected ?? s.drift_detected ?? false),
      driftDescription: str(s.driftDescription, s.drift_description),
      error: str(s.error),
      serviceErrors: mapServiceErrors(s.serviceErrors ?? s.service_errors),
      containers: asArray(s.containers).map((c): ContainerRow => ({
        id: str(c.id),
        name: str(c.name),
        image: str(c.image),
        serviceName: str(c.serviceName, c.service_name),
        state: str(c.state),
        status: str(c.status),
        health: str(c.health),
        restartCount: num(c.restartCount, c.restart_count),
        startedAt: num(c.startedAt, c.started_at),
        stackId,
        stackName,
      })),
    }
  })

  return {
    serverId: str(data.serverId, data.server_id),
    reportedAt: num(data.reportedAt, data.reported_at),
    stacks,
  }
}

/**
 * Flattens the inventory into table rows, sorted by stack then service, so the table keeps a
 * stable order across refreshes.
 */
export function containerRows(state: ServerState | undefined): ContainerRow[] {
  if (!state) return []
  return state.stacks
    .flatMap(s => s.containers)
    .sort((a, b) =>
      (a.stackName || a.stackId).localeCompare(b.stackName || b.stackId) ||
      a.serviceName.localeCompare(b.serviceName),
    )
}

/** Stacks whose last reconcile failed — rendered above the table. */
export function failedStacks(state: ServerState | undefined): StackState[] {
  return (state?.stacks ?? []).filter(s => s.error !== '')
}

export function containerStateVariant(state: string): BadgeVariant {
  if (state === 'running') return 'online'
  if (state === 'exited' || state === 'created' || state === 'paused') return 'offline'
  return 'warn'
}

export function healthVariant(health: string): BadgeVariant {
  if (health === 'healthy') return 'online'
  if (health === 'unhealthy') return 'error'
  if (health === 'starting') return 'warn'
  return 'default'
}

/** Version label for a stack: the numeric version when the Master could resolve it. */
export function versionLabel(stack: StackState): string {
  if (stack.runningVersion > 0) return `v${stack.runningVersion}`
  if (stack.runningVersionId !== '') return stack.runningVersionId.slice(0, 8)
  return '—'
}
