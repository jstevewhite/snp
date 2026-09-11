import { describe, expect, it } from 'vitest'
import { OnlineTracker } from './online'

describe('OnlineTracker', () => {
  it('starts online when navigator.onLine is true', () => {
    expect(new OnlineTracker().online).toBe(true)
  })

  it('starts offline when navigator.onLine is false', () => {
    const original = Object.getOwnPropertyDescriptor(
      Navigator.prototype,
      'onLine',
    )
    Object.defineProperty(navigator, 'onLine', {
      value: false,
      configurable: true,
    })
    try {
      expect(new OnlineTracker().online).toBe(false)
    } finally {
      if (original) Object.defineProperty(navigator, 'onLine', original)
    }
  })

  it('a failed fetch marks offline even when the browser is online', () => {
    const t = new OnlineTracker()
    t.fetchFailed()
    expect(t.online).toBe(false)
    t.fetchSucceeded()
    expect(t.online).toBe(true)
  })

  it('the offline event marks offline; the online event restores', () => {
    const t = new OnlineTracker()
    const unsub = t.trackBrowser()
    window.dispatchEvent(new Event('offline'))
    expect(t.online).toBe(false)
    window.dispatchEvent(new Event('online'))
    expect(t.online).toBe(true)
    unsub()
  })

  it('trackBrowser unsubscribe stops event handling', () => {
    const t = new OnlineTracker()
    const unsub = t.trackBrowser()
    unsub()
    window.dispatchEvent(new Event('offline'))
    expect(t.online).toBe(true)
  })

  it('subscribe fires on changes, with the current value first', () => {
    const t = new OnlineTracker()
    const seen: boolean[] = []
    const unsub = t.subscribe((v) => seen.push(v))
    t.fetchFailed()
    t.fetchSucceeded()
    unsub()
    expect(seen).toEqual([true, false, true])
  })

  it('does not emit when the state does not change', () => {
    const t = new OnlineTracker()
    const seen: boolean[] = []
    t.subscribe((v) => seen.push(v))
    t.fetchFailed()
    t.fetchFailed()
    expect(seen).toEqual([true, false])
  })

  it('unsubscribe stops further notifications', () => {
    const t = new OnlineTracker()
    const seen: boolean[] = []
    const unsub = t.subscribe((v) => seen.push(v))
    unsub()
    t.fetchFailed()
    expect(seen).toEqual([true])
  })
})
