import { afterEach, describe, expect, it, vi } from 'vitest'
import { writeClipboard } from './clipboard'

/** Replaces navigator.clipboard for one test (jsdom has none by default). */
function stubClipboard(value: unknown): void {
  Object.defineProperty(navigator, 'clipboard', { value, configurable: true })
}

/** Replaces document.execCommand for one test (jsdom does not implement it). */
function stubExecCommand(value: unknown): void {
  Object.defineProperty(document, 'execCommand', { value, configurable: true })
}

afterEach(() => {
  Reflect.deleteProperty(navigator, 'clipboard')
  Reflect.deleteProperty(document, 'execCommand')
  vi.restoreAllMocks()
})

describe('writeClipboard', () => {
  it('writes through navigator.clipboard when available', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    stubClipboard({ writeText })
    await writeClipboard('hello')
    expect(writeText).toHaveBeenCalledWith('hello')
  })

  it('falls back to execCommand when the async API rejects', async () => {
    const writeText = vi.fn().mockRejectedValue(new Error('denied'))
    stubClipboard({ writeText })
    const exec = vi.fn().mockReturnValue(true)
    stubExecCommand(exec)
    await writeClipboard('hello')
    expect(writeText).toHaveBeenCalled()
    expect(exec).toHaveBeenCalledWith('copy')
  })

  it('rejects when neither path works', async () => {
    stubClipboard(undefined)
    await expect(writeClipboard('hello')).rejects.toThrow(/clipboard/i)
  })

  it('rejects when execCommand reports failure', async () => {
    stubClipboard({ writeText: vi.fn().mockRejectedValue(new Error('denied')) })
    stubExecCommand(vi.fn().mockReturnValue(false))
    await expect(writeClipboard('hello')).rejects.toThrow()
  })
})
