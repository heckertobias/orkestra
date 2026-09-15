import { describe, it, expect, vi, afterEach } from 'vitest'
import { copyToClipboard } from './clipboard'

/**
 * These run in the `node` environment (see vitest.config.ts), so both `navigator` and
 * `document` are stubbed. The fake document is deliberately minimal — just enough of the
 * textarea dance for the fallback to be exercised, and to record what it did.
 */
interface FakeDoc {
  execCommandResult: boolean
  execCommandCalls: string[]
  copiedValue: string | null
  attached: number
  detached: number
}

function stubDocument(execCommandResult: boolean): FakeDoc {
  const state: FakeDoc = {
    execCommandResult,
    execCommandCalls: [],
    copiedValue: null,
    attached: 0,
    detached: 0,
  }
  const doc = {
    createElement: () => ({
      value: '',
      style: {} as Record<string, string>,
      setAttribute: () => {},
      select() {
        state.copiedValue = (this as { value: string }).value
      },
      setSelectionRange: () => {},
      remove: () => { state.detached++ },
    }),
    body: { appendChild: () => { state.attached++ } },
    execCommand: (cmd: string) => {
      state.execCommandCalls.push(cmd)
      return state.execCommandResult
    },
  }
  vi.stubGlobal('document', doc)
  return state
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('copyToClipboard', () => {
  it('uses the async Clipboard API when it is available', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { clipboard: { writeText } })
    const doc = stubDocument(true)

    expect(await copyToClipboard('s3cret')).toBe(true)
    expect(writeText).toHaveBeenCalledWith('s3cret')
    // The fallback must not run as well — it would clobber the user's selection.
    expect(doc.execCommandCalls).toEqual([])
  })

  it('falls back to execCommand outside a secure context', async () => {
    // Plain HTTP: navigator.clipboard is undefined. This is the #103 case.
    vi.stubGlobal('navigator', {})
    const doc = stubDocument(true)

    expect(await copyToClipboard('s3cret')).toBe(true)
    expect(doc.execCommandCalls).toEqual(['copy'])
    expect(doc.copiedValue).toBe('s3cret')
  })

  it('falls back when the Clipboard API rejects', async () => {
    const writeText = vi.fn().mockRejectedValue(new Error('permission denied'))
    vi.stubGlobal('navigator', { clipboard: { writeText } })
    const doc = stubDocument(true)

    expect(await copyToClipboard('s3cret')).toBe(true)
    expect(doc.execCommandCalls).toEqual(['copy'])
  })

  it('reports failure instead of swallowing it when both paths fail', async () => {
    vi.stubGlobal('navigator', {})
    stubDocument(false)

    expect(await copyToClipboard('s3cret')).toBe(false)
  })

  it('always removes the scratch textarea', async () => {
    vi.stubGlobal('navigator', {})
    const doc = stubDocument(false)

    await copyToClipboard('s3cret')
    expect(doc.attached).toBe(1)
    expect(doc.detached).toBe(1)
  })
})
