import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  COMPACT_MEDIA,
  browserHistory,
  createCompactNav,
  isTextInput,
  resolveLayout,
  watchNarrow,
  type NavHistory,
  type NavState,
} from './layout'

/**
 * A history that records pushes and, like a browser, answers back()/go()
 * asynchronously — by handing the landed-on entry's state to `pop`, which
 * the test wires to nav.onPopState. Index 0 is the page's own entry.
 */
function fakeHistory() {
  const entries: unknown[] = [null]
  let index = 0
  let pop: (state: unknown) => void = () => {}
  const h: NavHistory & {
    entries: unknown[]
    readonly index: number
    setPop(fn: (state: unknown) => void): void
    /** Deliver the pending navigation, as the browser would after back(). */
    flush(): void
  } = {
    entries,
    get index() {
      return index
    },
    get state() {
      return entries[index]
    },
    pushState: vi.fn((state: unknown) => {
      entries.splice(index + 1)
      entries.push(state)
      index = entries.length - 1
    }),
    replaceState: vi.fn((state: unknown) => {
      entries[index] = state
    }),
    back: vi.fn(() => {
      pending.push(-1)
    }),
    go: vi.fn((delta: number) => {
      pending.push(delta)
    }),
    setPop(fn) {
      pop = fn
    },
    flush() {
      while (pending.length > 0) {
        const delta = pending.shift()!
        index = Math.max(0, Math.min(entries.length - 1, index + delta))
        pop(entries[index])
      }
    },
  }
  const pending: number[] = []
  return h
}

function setup() {
  const history = fakeHistory()
  const changes: NavState[] = []
  const nav = createCompactNav(history, (s) => changes.push(s))
  history.setPop((s) => nav.onPopState(s))
  return { history, nav, changes }
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('resolveLayout', () => {
  it('follows the width under auto and ignores it otherwise', () => {
    expect(resolveLayout('auto', true)).toBe('compact')
    expect(resolveLayout('auto', false)).toBe('wide')
    expect(resolveLayout('wide', true)).toBe('wide')
    expect(resolveLayout('wide', false)).toBe('wide')
    expect(resolveLayout('compact', true)).toBe('compact')
    expect(resolveLayout('compact', false)).toBe('compact')
  })
})

describe('watchNarrow', () => {
  it('reports not-narrow once when matchMedia is missing (jsdom)', () => {
    expect(typeof window.matchMedia).not.toBe('function')
    const cb = vi.fn()
    const stop = watchNarrow(cb)
    expect(cb).toHaveBeenCalledTimes(1)
    expect(cb).toHaveBeenCalledWith(false)
    stop()
  })

  /** A fake MediaQueryList whose `matches` the test flips at will. */
  function fakeMql(matches: boolean) {
    let listener: ((e: { matches: boolean }) => void) | null = null
    const mql = {
      matches,
      addEventListener: vi.fn((_: string, fn: (e: { matches: boolean }) => void) => {
        listener = fn
      }),
      removeEventListener: vi.fn(() => {
        listener = null
      }),
      /** Deliver the change event the way a browser would. */
      change(next: boolean) {
        mql.matches = next
        listener?.({ matches: next })
      },
      get listening() {
        return listener !== null
      },
    }
    vi.stubGlobal('matchMedia', vi.fn(() => mql))
    return mql
  }

  it('reports the current match and every change until unsubscribed', () => {
    const mql = fakeMql(true)
    const cb = vi.fn()
    const stop = watchNarrow(cb)
    expect(window.matchMedia).toHaveBeenCalledWith(COMPACT_MEDIA)
    expect(cb).toHaveBeenLastCalledWith(true)
    mql.change(false)
    expect(cb).toHaveBeenLastCalledWith(false)
    stop()
    expect(mql.removeEventListener).toHaveBeenCalled()
    expect(mql.listening).toBe(false)
  })

  it('re-reads the match on a window resize when the change event never comes', () => {
    // An embedded webview (the wails desktop shell) can resize the window
    // without delivering the MediaQueryList change event.
    const mql = fakeMql(false)
    const cb = vi.fn()
    const stop = watchNarrow(cb)
    expect(cb).toHaveBeenCalledTimes(1)
    mql.matches = true
    window.dispatchEvent(new Event('resize'))
    expect(cb).toHaveBeenCalledTimes(2)
    expect(cb).toHaveBeenLastCalledWith(true)
    // Same answer again: no duplicate report.
    window.dispatchEvent(new Event('resize'))
    expect(cb).toHaveBeenCalledTimes(2)
    // The change event after the resize already reported it is a no-op.
    mql.change(true)
    expect(cb).toHaveBeenCalledTimes(2)
    mql.matches = false
    window.dispatchEvent(new Event('resize'))
    expect(cb).toHaveBeenLastCalledWith(false)
    stop()
    mql.matches = true
    window.dispatchEvent(new Event('resize'))
    expect(cb).toHaveBeenCalledTimes(3)
  })

  it('re-reads the match when the root element changes size', () => {
    const mql = fakeMql(false)
    let observed: Element | null = null
    let fire: (() => void) | null = null
    const disconnect = vi.fn(() => {
      fire = null
    })
    class FakeResizeObserver {
      constructor(callback: () => void) {
        fire = callback
      }
      observe(el: Element) {
        observed = el
      }
      disconnect = disconnect
    }
    vi.stubGlobal('ResizeObserver', FakeResizeObserver)
    const cb = vi.fn()
    const stop = watchNarrow(cb)
    expect(observed).toBe(document.documentElement)
    mql.matches = true
    fire!()
    expect(cb).toHaveBeenLastCalledWith(true)
    expect(cb).toHaveBeenCalledTimes(2)
    stop()
    expect(disconnect).toHaveBeenCalled()
    expect(fire).toBeNull()
  })
})

describe('compact nav', () => {
  it('starts at the root with nothing pushed', () => {
    const { nav, history } = setup()
    expect(nav.state).toEqual({ screen: 'list', drawerOpen: false, depth: 0 })
    expect(history.pushState).not.toHaveBeenCalled()
    nav.back() // nothing to pop
    expect(history.back).not.toHaveBeenCalled()
  })

  it('pushes detail once and pops it back to the list via history', () => {
    const { nav, history, changes } = setup()
    nav.openDetail()
    nav.openDetail() // repeated: no second entry
    expect(history.pushState).toHaveBeenCalledTimes(1)
    expect(history.entries[1]).toEqual(expect.objectContaining({ snp: 1 }))
    expect(nav.state).toEqual({ screen: 'detail', drawerOpen: false, depth: 1 })
    nav.back()
    // State changes only once the browser reports the pop.
    expect(nav.state.screen).toBe('detail')
    history.flush()
    expect(nav.state).toEqual({ screen: 'list', drawerOpen: false, depth: 0 })
    expect(changes.at(-1)).toEqual(nav.state)
  })

  it('opens and closes the drawer as its own level', () => {
    const { nav, history } = setup()
    nav.openDrawer()
    nav.openDrawer()
    expect(history.pushState).toHaveBeenCalledTimes(1)
    expect(nav.state).toEqual({ screen: 'list', drawerOpen: true, depth: 1 })
    nav.closeDrawer()
    history.flush()
    expect(nav.state).toEqual({ screen: 'list', drawerOpen: false, depth: 0 })
    nav.closeDrawer() // already closed: no history traffic
    expect(history.back).toHaveBeenCalledTimes(1)
  })

  it('pops the drawer before the detail screen', () => {
    const { nav, history } = setup()
    nav.openDetail()
    nav.openDrawer()
    expect(nav.state).toEqual({ screen: 'detail', drawerOpen: true, depth: 2 })
    nav.back()
    history.flush()
    expect(nav.state).toEqual({ screen: 'detail', drawerOpen: false, depth: 1 })
    nav.back()
    history.flush()
    expect(nav.state.screen).toBe('list')
  })

  it('selecting from the drawer swaps it for detail at the same depth', () => {
    const { nav, history } = setup()
    nav.openDrawer()
    nav.openDetail()
    expect(history.pushState).toHaveBeenCalledTimes(1)
    expect(nav.state).toEqual({ screen: 'detail', drawerOpen: false, depth: 1 })
    nav.back()
    history.flush()
    expect(nav.state).toEqual({ screen: 'list', drawerOpen: false, depth: 0 })
  })

  it('ignores a popstate that is not the level below', () => {
    const { nav, history } = setup()
    nav.openDetail()
    nav.openDrawer()
    const ours = history.entries[1] as { snp: number; ses: number }
    nav.onPopState({ snp: 1, ses: ours.ses + 1 }) // another session's entry
    nav.onPopState({ snp: 5, ses: ours.ses }) // not the level below
    nav.onPopState(null) // two levels down: not the level below
    expect(nav.state.depth).toBe(2)
    nav.onPopState(ours)
    expect(nav.state).toEqual({ screen: 'detail', drawerOpen: false, depth: 1 })
    // Below depth 1 sits the page's own entry, whatever state it carries.
    nav.onPopState({ foreign: true })
    expect(nav.state).toEqual({ screen: 'list', drawerOpen: false, depth: 0 })
    nav.onPopState(ours) // forward into a stale entry at the root
    expect(nav.state.depth).toBe(0)
  })

  it('toRoot resets synchronously with one history move', () => {
    const { nav, history } = setup()
    nav.openDetail()
    nav.openDrawer()
    nav.toRoot()
    expect(nav.state).toEqual({ screen: 'list', drawerOpen: false, depth: 0 })
    expect(history.go).toHaveBeenCalledTimes(1)
    expect(history.go).toHaveBeenCalledWith(-2)
    history.flush() // the browser's popstate lands and is ignored
    expect(nav.state.depth).toBe(0)
    nav.toRoot()
    expect(history.go).toHaveBeenCalledTimes(1)
  })

  it('enter lands on detail when there is one, leave forgets the stack', () => {
    const { nav, history } = setup()
    nav.enter(true)
    expect(nav.state).toEqual({ screen: 'detail', drawerOpen: false, depth: 1 })
    nav.leave()
    expect(nav.state).toEqual({ screen: 'list', drawerOpen: false, depth: 0 })
    expect(history.back).not.toHaveBeenCalled()
    expect(history.go).not.toHaveBeenCalled()
    // The leftover entry pops harmlessly later.
    nav.onPopState(null)
    expect(nav.state.depth).toBe(0)
    nav.enter(false)
    expect(nav.state.screen).toBe('list')
    expect(history.pushState).toHaveBeenCalledTimes(1)
  })

  it('re-entering onto a stale detail entry reuses it, so Back still reaches the root', () => {
    // wide → compact (detail) → wide → compact: the first spell's entry is
    // still the current one when the second spell begins.
    const { nav, history } = setup()
    nav.enter(true)
    const first = history.state as { ses: number }
    nav.leave()
    nav.enter(true)
    expect(history.pushState).toHaveBeenCalledTimes(1)
    expect(history.replaceState).toHaveBeenCalledTimes(1)
    expect(history.entries).toHaveLength(2)
    expect((history.state as { ses: number }).ses).not.toBe(first.ses)
    expect(nav.state).toEqual({ screen: 'detail', drawerOpen: false, depth: 1 })
    nav.back()
    history.flush()
    expect(nav.state).toEqual({ screen: 'list', drawerOpen: false, depth: 0 })
    expect(history.index).toBe(0)
  })

  it('a stale entry under a new push is the root, not a level', () => {
    // A previous compact spell ended at depth 1 without popping; the next
    // spell starts at the root (nothing selected) and pushes on top.
    const { nav, history } = setup()
    nav.enter(true)
    nav.leave()
    nav.enter(false)
    nav.openDetail()
    expect(history.entries).toHaveLength(3)
    nav.back()
    history.flush() // lands on the stale entry
    expect(nav.state.depth).toBe(0)
    expect(nav.state.screen).toBe('list')
  })
})

describe('helpers', () => {
  it('browserHistory wraps window.history', () => {
    const h = browserHistory()
    expect(h).not.toBeNull()
    const spy = vi.spyOn(window.history, 'pushState')
    h!.pushState({ snp: 1 }, '')
    expect(spy).toHaveBeenCalledWith({ snp: 1 }, '')
    spy.mockRestore()
  })

  it('isTextInput recognises fields and editable regions', () => {
    expect(isTextInput(null)).toBe(false)
    expect(isTextInput(document.createElement('input'))).toBe(true)
    expect(isTextInput(document.createElement('textarea'))).toBe(true)
    expect(isTextInput(document.createElement('select'))).toBe(true)
    expect(isTextInput(document.createElement('button'))).toBe(false)
    const div = document.createElement('div')
    Object.defineProperty(div, 'isContentEditable', { value: true })
    expect(isTextInput(div)).toBe(true)
  })
})
