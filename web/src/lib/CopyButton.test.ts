import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'
import CopyButton, { COPY_FLASH_MS } from './CopyButton.svelte'

afterEach(() => {
  cleanup()
  // Transient-label tests opt into fake timers; always restore.
  vi.useRealTimers()
})

describe('CopyButton', () => {
  it('shows the resting label and hands oncopy the text', async () => {
    const oncopy = vi.fn()
    render(CopyButton, { text: 'payload', oncopy })
    await fireEvent.click(screen.getByText('Copy'))
    expect(oncopy).toHaveBeenCalledWith('payload')
  })

  it('uses the supplied label', () => {
    render(CopyButton, { text: 'x', label: 'Copy template', oncopy: vi.fn() })
    expect(screen.getByText('Copy template')).toBeDefined()
  })

  it('adds the small variant class', () => {
    render(CopyButton, { text: 'x', small: true, oncopy: vi.fn() })
    expect(screen.getByText('Copy').classList.contains('small')).toBe(true)
  })

  it('briefly shows Copied. then reverts to the label', async () => {
    vi.useFakeTimers()
    const oncopy = vi.fn().mockResolvedValue(undefined)
    render(CopyButton, { text: 'payload', oncopy })
    await fireEvent.click(screen.getByText('Copy'))
    await vi.advanceTimersByTimeAsync(0)
    expect(screen.getByText('Copied.')).toBeDefined()
    await vi.advanceTimersByTimeAsync(COPY_FLASH_MS)
    expect(screen.getByText('Copy')).toBeDefined()
  })

  it('reports a rejected write instead of claiming success', async () => {
    vi.useFakeTimers()
    const oncopy = vi.fn().mockRejectedValue(new Error('denied'))
    render(CopyButton, { text: 'payload', oncopy })
    await fireEvent.click(screen.getByText('Copy'))
    await vi.advanceTimersByTimeAsync(0)
    expect(screen.getByText('Copy failed')).toBeDefined()
    await vi.advanceTimersByTimeAsync(COPY_FLASH_MS)
    expect(screen.getByText('Copy')).toBeDefined()
  })

  it('does not write while disabled', async () => {
    const oncopy = vi.fn()
    render(CopyButton, { text: 'x', disabled: true, oncopy })
    await fireEvent.click(screen.getByText('Copy'))
    expect(oncopy).not.toHaveBeenCalled()
  })
})
