import { afterEach, describe, expect, it } from 'vitest'
import {
  isMac,
  isPaletteShortcut,
  isSearchShortcut,
  paletteShortcutLabel,
  searchShortcutLabel,
} from './keys'

function stubPlatform(value: string): void {
  Object.defineProperty(navigator, 'platform', { value, configurable: true })
}

function key(init: KeyboardEventInit): KeyboardEvent {
  return new KeyboardEvent('keydown', init)
}

afterEach(() => {
  Reflect.deleteProperty(navigator, 'platform')
})

describe('isMac', () => {
  it('is false for a non-Apple platform', () => {
    stubPlatform('Linux x86_64')
    expect(isMac()).toBe(false)
  })

  it('is true for an Apple platform', () => {
    stubPlatform('MacIntel')
    expect(isMac()).toBe(true)
  })
})

describe('searchShortcutLabel', () => {
  it('names Ctrl off macOS', () => {
    stubPlatform('Linux x86_64')
    expect(searchShortcutLabel()).toBe('Ctrl K')
  })

  it('names the command key on macOS', () => {
    stubPlatform('MacIntel')
    expect(searchShortcutLabel()).toBe('⌘K')
  })
})

describe('isSearchShortcut', () => {
  it('matches Ctrl+K and Cmd+K, either case', () => {
    expect(isSearchShortcut(key({ key: 'k', ctrlKey: true }))).toBe(true)
    expect(isSearchShortcut(key({ key: 'K', metaKey: true }))).toBe(true)
  })

  it('ignores a bare k, other keys, and AltGr combinations', () => {
    expect(isSearchShortcut(key({ key: 'k' }))).toBe(false)
    expect(isSearchShortcut(key({ key: 'j', ctrlKey: true }))).toBe(false)
    expect(isSearchShortcut(key({ key: 'k', ctrlKey: true, altKey: true }))).toBe(false)
  })
})

describe('isPaletteShortcut', () => {
  it('matches Cmd+Shift+P and Ctrl+Shift+P by physical key', () => {
    expect(isPaletteShortcut(key({ metaKey: true, shiftKey: true, code: 'KeyP', key: 'P' }))).toBe(true)
    expect(isPaletteShortcut(key({ ctrlKey: true, shiftKey: true, code: 'KeyP', key: 'p' }))).toBe(true)
  })

  it('needs Shift and rejects Alt and other keys', () => {
    expect(isPaletteShortcut(key({ ctrlKey: true, code: 'KeyP', key: 'p' }))).toBe(false)
    expect(isPaletteShortcut(key({ ctrlKey: true, shiftKey: true, altKey: true, code: 'KeyP', key: 'P' }))).toBe(false)
    expect(isPaletteShortcut(key({ ctrlKey: true, shiftKey: true, code: 'KeyK', key: 'K' }))).toBe(false)
  })
})

describe('paletteShortcutLabel', () => {
  it('names the platform modifier', () => {
    stubPlatform('Linux x86_64')
    expect(paletteShortcutLabel()).toBe('Ctrl Shift P')
    stubPlatform('MacIntel')
    expect(paletteShortcutLabel()).toBe('⇧⌘P')
  })
})
