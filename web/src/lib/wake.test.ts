import { describe, expect, it, vi } from 'vitest'
import { onWake } from './wake'

function setVisibility(state: 'visible' | 'hidden'): void {
  Object.defineProperty(document, 'visibilityState', {
    value: state,
    configurable: true,
  })
}

describe('onWake', () => {
  it('fires when the page becomes visible', () => {
    const fn = vi.fn()
    const off = onWake(fn)
    setVisibility('hidden')
    document.dispatchEvent(new Event('visibilitychange'))
    expect(fn).not.toHaveBeenCalled()
    setVisibility('visible')
    document.dispatchEvent(new Event('visibilitychange'))
    expect(fn).toHaveBeenCalledTimes(1)
    off()
  })

  it('fires when the window regains focus', () => {
    const fn = vi.fn()
    const off = onWake(fn)
    window.dispatchEvent(new Event('focus'))
    expect(fn).toHaveBeenCalledTimes(1)
    off()
  })

  it('stops firing after unsubscribe', () => {
    const fn = vi.fn()
    const off = onWake(fn)
    off()
    window.dispatchEvent(new Event('focus'))
    setVisibility('visible')
    document.dispatchEvent(new Event('visibilitychange'))
    expect(fn).not.toHaveBeenCalled()
  })
})
