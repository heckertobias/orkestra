import { describe, it, expect } from 'vitest'
import {
  mapServerState,
  containerRows,
  failedStacks,
  containerStateVariant,
  displayName,
  healthVariant,
  versionLabel,
} from './serverState'

const camelCase = {
  serverId: 'srv-1',
  reportedAt: 1700000000000,
  stacks: [
    {
      stackId: 'st-a',
      stackName: 'web-stack',
      runningVersionId: 'ver-a1',
      runningVersion: 3,
      containers: [
        {
          id: 'c1',
          name: 'st-a-web-1234abcd',
          image: 'nginx:1.27',
          serviceName: 'web',
          state: 'running',
          status: 'Up 3 hours',
          health: 'healthy',
          restartCount: 2,
          startedAt: 1699999999000,
        },
      ],
    },
  ],
}

const snakeCase = {
  server_id: 'srv-1',
  reported_at: 1700000000000,
  stacks: [
    {
      stack_id: 'st-a',
      stack_name: 'web-stack',
      running_version_id: 'ver-a1',
      running_version: 3,
      containers: [
        {
          id: 'c1',
          name: 'st-a-web-1234abcd',
          image: 'nginx:1.27',
          service_name: 'web',
          state: 'running',
          status: 'Up 3 hours',
          health: 'healthy',
          restart_count: 2,
          started_at: 1699999999000,
        },
      ],
    },
  ],
}

describe('mapServerState', () => {
  it('maps a camelCase response', () => {
    const state = mapServerState(camelCase)
    expect(state.serverId).toBe('srv-1')
    expect(state.reportedAt).toBe(1700000000000)
    expect(state.stacks[0].runningVersion).toBe(3)
    expect(state.stacks[0].containers[0].serviceName).toBe('web')
    expect(state.stacks[0].containers[0].restartCount).toBe(2)
  })

  it('maps a snake_case response identically', () => {
    expect(mapServerState(snakeCase)).toEqual(mapServerState(camelCase))
  })

  it('denormalises the stack onto each container row', () => {
    const c = mapServerState(camelCase).stacks[0].containers[0]
    expect(c.stackId).toBe('st-a')
    expect(c.stackName).toBe('web-stack')
  })

  it('maps per-service errors in both casings', () => {
    const a = mapServerState({ stacks: [{ serviceErrors: [{ serviceName: 'api', error: 'boom' }] }] })
    const b = mapServerState({ stacks: [{ service_errors: [{ service_name: 'api', error: 'boom' }] }] })
    expect(a.stacks[0].serviceErrors).toEqual([{ serviceName: 'api', error: 'boom' }])
    expect(b).toEqual(a)
  })

  it('survives an empty or missing payload', () => {
    expect(mapServerState({})).toEqual({ serverId: '', reportedAt: 0, stacks: [] })
    expect(mapServerState(undefined)).toEqual({ serverId: '', reportedAt: 0, stacks: [] })
    expect(mapServerState({ stacks: null })).toEqual({ serverId: '', reportedAt: 0, stacks: [] })
  })

  it('defaults missing container fields instead of rendering undefined', () => {
    const c = mapServerState({ stacks: [{ stack_id: 'st-a', containers: [{ id: 'c1' }] }] })
      .stacks[0].containers[0]
    expect(c).toMatchObject({ name: '', image: '', health: '', restartCount: 0, startedAt: 0 })
  })
})

describe('containerRows', () => {
  it('flattens stacks and sorts by stack then service', () => {
    const state = mapServerState({
      stacks: [
        { stack_id: 'st-b', stack_name: 'beta', containers: [{ id: 'c3', service_name: 'api' }] },
        {
          stack_id: 'st-a',
          stack_name: 'alpha',
          containers: [
            { id: 'c2', service_name: 'web' },
            { id: 'c1', service_name: 'db' },
          ],
        },
      ],
    })
    expect(containerRows(state).map(c => c.id)).toEqual(['c1', 'c2', 'c3'])
  })

  it('returns nothing for an undefined state', () => {
    expect(containerRows(undefined)).toEqual([])
  })
})

describe('failedStacks', () => {
  it('returns only stacks with a reconcile error', () => {
    const state = mapServerState({
      stacks: [{ stack_id: 'st-a' }, { stack_id: 'st-b', error: 'converge failed' }],
    })
    expect(failedStacks(state).map(s => s.stackId)).toEqual(['st-b'])
  })
})

describe('displayName', () => {
  const row = (name: string, stackId = 'st-a', id = 'abcdef1234567890') =>
    mapServerState({ stacks: [{ stack_id: stackId, containers: [{ id, name }] }] })
      .stacks[0].containers[0]

  it('drops the repeated stack-id prefix', () => {
    expect(displayName(row('st-a-web-1234abcd'))).toBe('web-1234abcd')
  })

  it('leaves a name that does not carry the prefix untouched', () => {
    expect(displayName(row('my-container'))).toBe('my-container')
  })

  it('falls back to a short container id when there is no name', () => {
    expect(displayName(row(''))).toBe('abcdef123456')
  })
})

describe('containerStateVariant', () => {
  it('maps docker states to badge variants', () => {
    expect(containerStateVariant('running')).toBe('online')
    expect(containerStateVariant('exited')).toBe('offline')
    expect(containerStateVariant('restarting')).toBe('warn')
    expect(containerStateVariant('')).toBe('warn')
  })
})

describe('healthVariant', () => {
  it('maps health to badge variants, with no healthcheck as default', () => {
    expect(healthVariant('healthy')).toBe('online')
    expect(healthVariant('unhealthy')).toBe('error')
    expect(healthVariant('starting')).toBe('warn')
    expect(healthVariant('')).toBe('default')
  })
})

describe('versionLabel', () => {
  it('prefers the resolved numeric version', () => {
    const [stack] = mapServerState(camelCase).stacks
    expect(versionLabel(stack)).toBe('v3')
  })

  it('falls back to a short version id when the Master could not resolve it', () => {
    const [stack] = mapServerState({ stacks: [{ running_version_id: 'abcdef1234567890' }] }).stacks
    expect(versionLabel(stack)).toBe('abcdef12')
  })

  it('shows a dash for a stack with no unanimous version', () => {
    const [stack] = mapServerState({ stacks: [{ stack_id: 'st-a' }] }).stacks
    expect(versionLabel(stack)).toBe('—')
  })
})
