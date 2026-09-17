import { describe, it, expect, vi, afterEach } from 'vitest'
import { copyToClipboard } from './clipboard'

// These run in the `node` environment (see vitest.config.ts), so `navigator` is stubbed.
afterEach(() => {
  vi.unstubAllGlobals()
})

describe('copyToClipboard', () => {
  it('copies through the Clipboard API when it is available', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { clipboard: { writeText } })

    expect(await copyToClipboard('s3cret')).toBe(true)
    expect(writeText).toHaveBeenCalledWith('s3cret')
  })

  it('reports failure outside a secure context', async () => {
    // Plain HTTP: navigator.clipboard is undefined whatever the browser version. The copy
    // cannot happen, so the caller has to be told rather than left to assume it worked.
    vi.stubGlobal('navigator', {})

    expect(await copyToClipboard('s3cret')).toBe(false)
  })

  it('reports failure when the Clipboard API rejects', async () => {
    const writeText = vi.fn().mockRejectedValue(new Error('permission denied'))
    vi.stubGlobal('navigator', { clipboard: { writeText } })

    expect(await copyToClipboard('s3cret')).toBe(false)
  })
})
