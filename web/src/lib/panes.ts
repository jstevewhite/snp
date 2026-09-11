/**
 * Resizable pane layout (spec §6 "Layout"): folders+tags | snippet list |
 * detail, side by side with draggable dividers.
 *
 * The detail pane is the flexible one — it always takes whatever the two
 * fixed panes leave, so only two widths are tracked. Until the user drags a
 * divider the widths are left unset and the stylesheet's own
 * `minmax(280px, 1fr) / minmax(0, 2fr)` default drives the layout; the mount
 * measurement (measurePaneWidths) then pins the same pixel values so a drag
 * has a baseline. That keeps the pre-existing proportions on wide screens
 * instead of freezing the list at some arbitrary default width.
 *
 * Widths persist in localStorage and are re-applied on startup, browser and
 * wails desktop window alike — same pattern as lib/settings.ts.
 */

export interface PaneWidths {
  /** Folders + tag list pane, in CSS pixels. */
  folders: number
  /** Snippet list pane, in CSS pixels. The detail pane takes the rest. */
  list: number
}

/** Which divider is being dragged: the pane to its *left* is named. */
export type SplitterId = 'folders' | 'list'

export const PANE_WIDTHS_STORAGE_KEY = 'snp.paneWidths'

/**
 * Divider hit area in CSS pixels — must match `.splitter` width in app.css
 * (the room calculation below has to subtract both dividers).
 */
export const SPLITTER_WIDTH = 6

export const FOLDERS_MIN = 150
export const FOLDERS_MAX = 480
export const LIST_MIN = 220
export const LIST_MAX = 860
/** The flexible detail pane never shrinks below this while dragging. */
export const DETAIL_MIN = 280

/** Keyboard nudge: arrows move a divider this far (1px with Shift held). */
export const NUDGE_PX = 16

/** Used when the DOM cannot be measured (jsdom, or a hidden container). */
export function fallbackPaneWidths(): PaneWidths {
  return { folders: 230, list: 360 }
}

function clampRange(value: number, min: number, max: number): number {
  if (!Number.isFinite(value)) return min
  return Math.min(max, Math.max(min, value))
}

/**
 * Space the two fixed panes may occupy, i.e. the container minus both
 * dividers and the detail pane's minimum. `null` when the container width is
 * unknown (jsdom reports 0), which disables the container-aware limits.
 */
function roomFor(containerWidth: number): number | null {
  if (!Number.isFinite(containerWidth) || containerWidth <= 0) return null
  return containerWidth - SPLITTER_WIDTH * 2 - DETAIL_MIN
}

/** Clamp a width pair into range, keeping the detail pane usable. */
export function clampPaneWidths(widths: PaneWidths, containerWidth: number): PaneWidths {
  const room = roomFor(containerWidth)
  let folders = clampRange(widths.folders, FOLDERS_MIN, FOLDERS_MAX)
  let list = clampRange(widths.list, LIST_MIN, LIST_MAX)
  if (room !== null && folders + list > room) {
    // Give up list width first, then folders — both stay at their minimums
    // even when that no longer fits (a very narrow window overflows rather
    // than collapsing a pane to nothing).
    list = Math.max(LIST_MIN, room - folders)
    if (folders + list > room) folders = Math.max(FOLDERS_MIN, room - list)
  }
  return { folders: Math.round(folders), list: Math.round(list) }
}

/**
 * Apply a drag (or keyboard) delta to one divider.
 *
 * The folders divider trades width with the list pane, so the detail pane
 * stays put; the list divider is absorbed by the flexible detail pane.
 * `delta` is the pointer movement in CSS pixels (right is positive).
 */
export function resizePane(
  widths: PaneWidths,
  which: SplitterId,
  delta: number,
  containerWidth: number,
): PaneWidths {
  const room = roomFor(containerWidth)
  if (which === 'folders') {
    const total = clampRange(
      widths.folders + widths.list,
      FOLDERS_MIN + LIST_MIN,
      room ?? Number.POSITIVE_INFINITY,
    )
    const minFolders = Math.max(FOLDERS_MIN, total - LIST_MAX)
    const maxFolders = Math.min(FOLDERS_MAX, total - LIST_MIN)
    const folders = clampRange(widths.folders + delta, minFolders, maxFolders)
    return { folders: Math.round(folders), list: Math.round(total - folders) }
  }
  const maxList = Math.min(LIST_MAX, (room ?? Number.POSITIVE_INFINITY) - widths.folders)
  const list = clampRange(widths.list + delta, LIST_MIN, Math.max(LIST_MIN, maxList))
  return { folders: widths.folders, list: Math.round(list) }
}

function storage(): Storage | undefined {
  try {
    return typeof window === 'undefined' ? undefined : window.localStorage
  } catch {
    // Privacy modes / blocked storage: fall back to the stylesheet default.
    return undefined
  }
}

function valid(widths: PaneWidths): boolean {
  return Number.isFinite(widths.folders) && Number.isFinite(widths.list)
}

/** Persisted widths, or `null` when absent/unreadable (use the CSS default). */
export function loadPaneWidths(): PaneWidths | null {
  const s = storage()
  if (!s) return null
  try {
    const raw = s.getItem(PANE_WIDTHS_STORAGE_KEY)
    if (raw === null || raw === '') return null
    const parsed: unknown = JSON.parse(raw)
    if (typeof parsed !== 'object' || parsed === null) return null
    const { folders, list } = parsed as Partial<PaneWidths>
    if (typeof folders !== 'number' || typeof list !== 'number') return null
    const widths = { folders, list }
    return valid(widths) ? clampPaneWidths(widths, 0) : null
  } catch {
    // Ignore malformed values; keep the flexible default.
    return null
  }
}

/** Persist widths; `null` clears the stored value (back to the default). */
export function savePaneWidths(widths: PaneWidths | null): void {
  const s = storage()
  if (!s) return
  try {
    if (widths === null) s.removeItem(PANE_WIDTHS_STORAGE_KEY)
    else s.setItem(PANE_WIDTHS_STORAGE_KEY, JSON.stringify(widths))
  } catch {
    // Persistence is best-effort.
  }
}

/**
 * The inline custom properties for the `.panes` element. Empty string (no
 * vars) leaves the stylesheet's flexible default in charge.
 */
export function paneStyleVars(widths: PaneWidths | null): string {
  if (widths === null) return ''
  return `--folders-w: ${widths.folders}px; --list-w: ${widths.list}px`
}

/**
 * Measure the rendered folders and list panes. Returns `null` when the
 * container is not laid out (jsdom reports zero rects, and so would a hidden
 * window), so callers keep the stylesheet default instead of pinning 0px.
 */
export function measurePaneWidths(root: HTMLElement | null | undefined): PaneWidths | null {
  if (!root) return null
  const folders = root.querySelector<HTMLElement>('.pane.folders')
  const list = root.querySelector<HTMLElement>('.pane.list')
  if (!folders || !list) return null
  const foldersWidth = folders.getBoundingClientRect().width
  const listWidth = list.getBoundingClientRect().width
  if (foldersWidth <= 0 || listWidth <= 0) return null
  return { folders: Math.round(foldersWidth), list: Math.round(listWidth) }
}
