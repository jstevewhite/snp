import { afterEach, describe, expect, it } from 'vitest'
import {
  DETAIL_MIN,
  FOLDERS_MAX,
  FOLDERS_MIN,
  LIST_MAX,
  LIST_MIN,
  PANE_WIDTHS_STORAGE_KEY,
  SPLITTER_WIDTH,
  clampPaneWidths,
  fallbackPaneWidths,
  loadPaneWidths,
  measurePaneWidths,
  paneStyleVars,
  resizePane,
  savePaneWidths,
} from './panes'

afterEach(() => {
  localStorage.removeItem(PANE_WIDTHS_STORAGE_KEY)
})

describe('pane widths', () => {
  it('offers a usable fallback for unmeasurable containers', () => {
    const widths = fallbackPaneWidths()
    expect(widths.folders).toBeGreaterThanOrEqual(FOLDERS_MIN)
    expect(widths.list).toBeGreaterThanOrEqual(LIST_MIN)
  })

  it('clamps each pane into range when the container is unknown', () => {
    expect(clampPaneWidths({ folders: 1, list: 1 }, 0)).toEqual({
      folders: FOLDERS_MIN,
      list: LIST_MIN,
    })
    expect(clampPaneWidths({ folders: 9999, list: 9999 }, 0)).toEqual({
      folders: FOLDERS_MAX,
      list: LIST_MAX,
    })
  })

  it('keeps the detail pane above its minimum, shrinking the list first', () => {
    // 1000 - 2 dividers - 280 detail = 708 available for folders + list.
    const widths = clampPaneWidths({ folders: 300, list: 600 }, 1000)
    expect(widths.folders).toBe(300)
    expect(widths.list).toBe(708 - 300)
    expect(1000 - SPLITTER_WIDTH * 2 - widths.folders - widths.list).toBe(DETAIL_MIN)
  })

  it('shrinks folders too when the list is already at its minimum', () => {
    // 950 - 2 dividers - 280 detail = 658 available, which is less than
    // folders' max (480) plus the list minimum (220) — so folders must give.
    const room = 950 - SPLITTER_WIDTH * 2 - DETAIL_MIN
    const widths = clampPaneWidths({ folders: FOLDERS_MAX, list: 400 }, 950)
    expect(widths.list).toBe(LIST_MIN)
    expect(widths.folders).toBe(room - LIST_MIN)
    expect(950 - SPLITTER_WIDTH * 2 - widths.folders - widths.list).toBe(DETAIL_MIN)
  })

  it('trades folders width with the list, leaving the detail pane alone', () => {
    const start = { folders: 230, list: 360 }
    const grown = resizePane(start, 'folders', 50, 1440)
    expect(grown).toEqual({ folders: 280, list: 310 })
    expect(grown.folders + grown.list).toBe(start.folders + start.list)

    // Clamped at the list minimum rather than at the folders maximum.
    expect(resizePane(start, 'folders', 200, 1440)).toEqual({ folders: 370, list: 220 })
    // And at the folders minimum when dragged the other way.
    expect(resizePane(start, 'folders', -500, 1440)).toEqual({ folders: 150, list: 440 })
  })

  it('lets the list divider eat into the flexible detail pane', () => {
    const start = { folders: 230, list: 360 }
    expect(resizePane(start, 'list', 100, 1440)).toEqual({ folders: 230, list: 460 })
    // Folders is untouched; the detail pane takes the difference.
    expect(resizePane(start, 'list', 100, 1440).folders).toBe(start.folders)
    // Never past the list maximum…
    expect(resizePane(start, 'list', 5000, 1440).list).toBe(LIST_MAX)
    // …and never close enough to squeeze the detail pane below its minimum.
    const narrow = resizePane({ folders: 230, list: 360 }, 'list', 5000, 800)
    expect(narrow.list).toBe(800 - SPLITTER_WIDTH * 2 - 230 - DETAIL_MIN)
    expect(800 - SPLITTER_WIDTH * 2 - narrow.folders - narrow.list).toBe(DETAIL_MIN)
  })

  it('never goes below the list minimum when the window is too narrow', () => {
    const squeezed = resizePane({ folders: 230, list: 360 }, 'list', -5000, 1440)
    expect(squeezed.list).toBe(LIST_MIN)
  })

  it('round-trips widths through storage', () => {
    expect(loadPaneWidths()).toBeNull()
    savePaneWidths({ folders: 260, list: 420 })
    expect(loadPaneWidths()).toEqual({ folders: 260, list: 420 })
  })

  it('clears stored widths on reset', () => {
    savePaneWidths({ folders: 260, list: 420 })
    savePaneWidths(null)
    expect(loadPaneWidths()).toBeNull()
  })

  it('ignores malformed stored values', () => {
    localStorage.setItem(PANE_WIDTHS_STORAGE_KEY, 'not json')
    expect(loadPaneWidths()).toBeNull()
    localStorage.setItem(PANE_WIDTHS_STORAGE_KEY, '{"folders":"wide"}')
    expect(loadPaneWidths()).toBeNull()
    localStorage.setItem(PANE_WIDTHS_STORAGE_KEY, '{"folders":null,"list":null}')
    expect(loadPaneWidths()).toBeNull()
  })

  it('clamps stored values into range', () => {
    localStorage.setItem(PANE_WIDTHS_STORAGE_KEY, '{"folders":-50,"list":99999}')
    expect(loadPaneWidths()).toEqual({ folders: FOLDERS_MIN, list: LIST_MAX })
  })

  it('omits the custom properties when unset so the stylesheet default wins', () => {
    expect(paneStyleVars(null)).toBe('')
    expect(paneStyleVars({ folders: 230, list: 360 })).toBe(
      '--folders-w: 230px; --list-w: 360px',
    )
  })

  it('measures the rendered panes', () => {
    const root = document.createElement('div')
    const folders = document.createElement('div')
    folders.className = 'pane folders'
    const list = document.createElement('div')
    list.className = 'pane list'
    root.append(folders, list)
    folders.getBoundingClientRect = () => ({ width: 241.6 }) as DOMRect
    list.getBoundingClientRect = () => ({ width: 380.2 }) as DOMRect

    expect(measurePaneWidths(root)).toEqual({ folders: 242, list: 380 })
  })

  it('declines to measure an unlaid-out container', () => {
    const root = document.createElement('div')
    const folders = document.createElement('div')
    folders.className = 'pane folders'
    const list = document.createElement('div')
    list.className = 'pane list'
    root.append(folders, list)

    // jsdom returns all-zero rects — pinning 0px would collapse the panes.
    expect(measurePaneWidths(root)).toBeNull()
    expect(measurePaneWidths(null)).toBeNull()
    expect(measurePaneWidths(document.createElement('div'))).toBeNull()
  })
})
