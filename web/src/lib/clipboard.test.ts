import { afterEach, describe, expect, it, vi } from 'vitest'
import { writeClipboard, writeClipboardDeferred } from './clipboard'

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

describe('writeClipboardDeferred', () => {
  afterEach(() => {
    Reflect.deleteProperty(globalThis, 'ClipboardItem')
  })

  it('starts a ClipboardItem write before the text resolves', async () => {
    class FakeItem {
      constructor(public data: Record<string, Promise<Blob>>) {}
    }
    Object.defineProperty(globalThis, 'ClipboardItem', { value: FakeItem, configurable: true })
    let item: FakeItem | undefined
    const write = vi.fn(async (items: FakeItem[]) => {
      item = items[0]
    })
    stubClipboard({ write, writeText: vi.fn() })
    let resolve!: (t: string) => void
    const done = writeClipboardDeferred(() => new Promise((r) => (resolve = r)))
    // The write was issued synchronously, inside the would-be gesture.
    expect(write).toHaveBeenCalledTimes(1)
    resolve('secret')
    await done
    const blob = await item!.data['text/plain']
    expect(await blob.text()).toBe('secret')
  })

  it('falls back to writeText once the text arrives', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    stubClipboard({ writeText })
    const produce = vi.fn().mockResolvedValue('secret')
    await writeClipboardDeferred(produce)
    expect(produce).toHaveBeenCalledTimes(1)
    expect(writeText).toHaveBeenCalledWith('secret')
  })

  it('rejects when the text cannot be produced', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    stubClipboard({ writeText })
    await expect(writeClipboardDeferred(() => Promise.reject(new Error('offline')))).rejects.toThrow('offline')
    expect(writeText).not.toHaveBeenCalled()
  })
})
