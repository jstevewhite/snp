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
    pushState: vi.fn((state: unknown) => {
      entries.splice(index + 1)
      entries.push(state)
      index = entries.length - 1
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

  it('reports the current match and every change until unsubscribed', () => {
    let listener: ((e: { matches: boolean }) => void) | null = null
    const mql = {
      matches: true,
      addEventListener: vi.fn((_: string, fn: (e: { matches: boolean }) => void) => {
        listener = fn
      }),
      removeEventListener: vi.fn(() => {
        listener = null
      }),
    }
    const matchMedia = vi.fn(() => mql)
    vi.stubGlobal('matchMedia', matchMedia)
    const cb = vi.fn()
    const stop = watchNarrow(cb)
    expect(matchMedia).toHaveBeenCalledWith(COMPACT_MEDIA)
    expect(cb).toHaveBeenLastCalledWith(true)
    listener!({ matches: false })
    expect(cb).toHaveBeenLastCalledWith(false)
    stop()
    expect(mql.removeEventListener).toHaveBeenCalled()
    expect(listener).toBeNull()
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
    expect(history.entries[1]).toEqual({ snp: 1 })
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
    const { nav } = setup()
    nav.openDetail()
    nav.openDrawer()
    nav.onPopState({ snp: 5 }) // stale entry from another page load
    nav.onPopState(null) // two levels down: not the level below
    expect(nav.state.depth).toBe(2)
    nav.onPopState({ snp: 1 })
    expect(nav.state).toEqual({ screen: 'detail', drawerOpen: false, depth: 1 })
    // Below depth 1 sits the page's own entry, whatever state it carries.
    nav.onPopState({ foreign: true })
    expect(nav.state).toEqual({ screen: 'list', drawerOpen: false, depth: 0 })
    nav.onPopState({ snp: 1 }) // forward into a stale entry at the root
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
