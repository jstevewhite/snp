/**
 * Compact layout (spec §6 "Compact layout"): which arrangement the window
 * gets, and the screen stack that drives the one-screen-at-a-time mode.
 *
 * Two independent pieces, both free of Svelte so they test without a DOM:
 *
 * - `resolveLayout` + `watchNarrow` turn the Layout setting (lib/settings)
 *   and the window width into 'wide' | 'compact'. Auto follows a
 *   matchMedia breakpoint live; the manual values ignore the width.
 * - `createCompactNav` is the stack: list (root) → detail, with the
 *   folders drawer as an overlay on either. Every push is one history
 *   entry tagged with its depth, so the browser or OS back gesture pops
 *   exactly one level and only leaves the app from the root. The in-app
 *   Back and close controls go through `history.back()` too, so the
 *   button and the gesture share one code path: the popstate handler is
 *   the only place state is popped.
 */

import type { Layout } from './settings'

export type LayoutMode = 'wide' | 'compact'

/** Widest viewport (CSS px) that gets the compact layout under Auto. */
export const COMPACT_MAX_WIDTH = 719
export const COMPACT_MEDIA = `(max-width: ${COMPACT_MAX_WIDTH}px)`

/** The mode a Layout setting resolves to at the given narrowness. */
export function resolveLayout(setting: Layout, narrow: boolean): LayoutMode {
  if (setting === 'wide' || setting === 'compact') return setting
  return narrow ? 'compact' : 'wide'
}

/**
 * Report whether the viewport is narrow, now and on every change.
 * Without matchMedia (jsdom, an old webview) the answer is "not narrow"
 * once, so the wide layout is the default and compact must be forced
 * through the setting. Returns the unsubscribe.
 */
export function watchNarrow(cb: (narrow: boolean) => void): () => void {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') {
    cb(false)
    return () => {}
  }
  const mql = window.matchMedia(COMPACT_MEDIA)
  cb(mql.matches)
  const onChange = (e: MediaQueryListEvent): void => cb(e.matches)
  mql.addEventListener('change', onChange)
  return () => mql.removeEventListener('change', onChange)
}

export type Screen = 'list' | 'detail'

export interface NavState {
  screen: Screen
  drawerOpen: boolean
  /** History entries this stack has pushed (0 at the root). */
  depth: number
}

/**
 * The slice of `window.history` the stack uses, so tests inject a fake
 * and the app passes the real one (see `browserHistory`).
 */
export interface NavHistory {
  pushState(state: unknown, unused: string): void
  back(): void
  go(delta: number): void
}

/** History state written by every push; anything else is not ours. */
interface NavEntry {
  snp: number
}

function isNavEntry(state: unknown): state is NavEntry {
  return (
    typeof state === 'object' &&
    state !== null &&
    typeof (state as { snp?: unknown }).snp === 'number'
  )
}

export interface CompactNav {
  /** Current state; a fresh object after every change. */
  readonly state: NavState
  /** Show the detail screen (closing the drawer first). No-op if shown. */
  openDetail(): void
  openDrawer(): void
  /** Close the drawer through history, so the gesture and the button agree. */
  closeDrawer(): void
  /** Pop one level through history: drawer → closed, else detail → list. */
  back(): void
  /** Straight to the root in one history move (the search shortcut). */
  toRoot(): void
  /** Entering compact mode: reset, and land on detail when there is one. */
  enter(hasDetail: boolean): void
  /** Leaving compact mode: forget the stack; leftover entries are ignored. */
  leave(): void
  /** Handle a popstate event's `state`. Foreign or stale entries are ignored. */
  onPopState(state: unknown): void
}

/**
 * Create the stack. `onChange` fires after every state change with the new
 * snapshot, so a component can mirror it into its own reactive state.
 */
export function createCompactNav(
  history: NavHistory,
  onChange: (state: NavState) => void = () => {},
): CompactNav {
  let state: NavState = { screen: 'list', drawerOpen: false, depth: 0 }

  function set(next: NavState): void {
    state = next
    onChange(state)
  }

  function push(next: Omit<NavState, 'depth'>): void {
    const depth = state.depth + 1
    history.pushState({ snp: depth } satisfies NavEntry, '')
    set({ ...next, depth })
  }

  /** The state one level below the current one. */
  function popped(): NavState {
    if (state.drawerOpen) return { ...state, drawerOpen: false, depth: state.depth - 1 }
    return { screen: 'list', drawerOpen: false, depth: state.depth - 1 }
  }

  return {
    get state() {
      return state
    },
    openDetail() {
      if (state.screen === 'detail' && !state.drawerOpen) return
      if (state.drawerOpen) {
        // Replace the drawer's entry rather than stacking detail on top of
        // an open drawer: the drawer closes and detail takes its level.
        set({ screen: 'detail', drawerOpen: false, depth: state.depth })
        return
      }
      push({ screen: 'detail', drawerOpen: false })
    },
    openDrawer() {
      if (state.drawerOpen) return
      push({ screen: state.screen, drawerOpen: true })
    },
    closeDrawer() {
      if (!state.drawerOpen) return
      history.back()
    },
    back() {
      if (state.depth === 0) return
      history.back()
    },
    toRoot() {
      const depth = state.depth
      if (depth === 0) return
      set({ screen: 'list', drawerOpen: false, depth: 0 })
      history.go(-depth)
    },
    enter(hasDetail) {
      set({ screen: 'list', drawerOpen: false, depth: 0 })
      if (hasDetail) push({ screen: 'detail', drawerOpen: false })
    },
    leave() {
      set({ screen: 'list', drawerOpen: false, depth: 0 })
    },
    onPopState(entry) {
      if (state.depth === 0) return
      // Ours only if it is exactly the level below; the root has no entry
      // of ours, so a null/foreign state counts as the root when at depth 1.
      const target = isNavEntry(entry) ? entry.snp : 0
      if (target !== state.depth - 1) return
      set(popped())
    },
  }
}

/** `window.history` as a NavHistory, or null outside a browser. */
export function browserHistory(): NavHistory | null {
  if (typeof window === 'undefined' || !window.history) return null
  const h = window.history
  return {
    pushState: (state, unused) => h.pushState(state, unused),
    back: () => h.back(),
    go: (delta) => h.go(delta),
  }
}

/**
 * True when keyboard input belongs to the element: a text field or an
 * editable region, where Escape must not navigate away.
 */
export function isTextInput(el: Element | null): boolean {
  if (el === null) return false
  const tag = el.tagName
  if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return true
  return (el as HTMLElement).isContentEditable === true
}
