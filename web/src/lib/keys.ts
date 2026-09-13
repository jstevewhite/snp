/**
 * Keyboard shortcut helpers.
 *
 * The same SPA runs in a browser, as an installed PWA and in the wails
 * desktop shell, on macOS and on Linux, so a shortcut hint has to name
 * the modifier the platform actually uses rather than hardcoding one.
 */

/** True on Apple platforms, where the command key is the modifier. */
export function isMac(): boolean {
  if (typeof navigator === 'undefined') return false
  const platform = navigator.platform
  if (platform !== undefined && platform !== '') return /mac|iphone|ipad|ipod/i.test(platform)
  return /mac os x/i.test(navigator.userAgent ?? '')
}

/** The search shortcut as it should be shown to the user. */
export function searchShortcutLabel(): string {
  return isMac() ? '⌘K' : 'Ctrl K'
}

/**
 * True when the event is the search shortcut (Cmd+K on macOS, Ctrl+K
 * elsewhere). Alt is excluded so AltGr combinations never match.
 */
export function isSearchShortcut(e: KeyboardEvent): boolean {
  return (e.metaKey || e.ctrlKey) && !e.altKey && e.key.toLowerCase() === 'k'
}

/** The command-palette shortcut as it should be shown to the user. */
export function paletteShortcutLabel(): string {
  return isMac() ? '⇧⌘P' : 'Ctrl Shift P'
}

/**
 * True when the event is the command-palette shortcut (Cmd+Shift+P on
 * macOS, Ctrl+Shift+P elsewhere). Matched on the physical key, since
 * Shift changes `key` on some layouts; Alt is excluded as for search.
 */
export function isPaletteShortcut(e: KeyboardEvent): boolean {
  return (e.metaKey || e.ctrlKey) && e.shiftKey && !e.altKey && e.code === 'KeyP'
}
