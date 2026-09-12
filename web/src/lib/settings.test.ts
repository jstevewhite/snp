import { afterEach, describe, expect, it } from 'vitest'
import {
  LAYOUTS,
  LAYOUT_STORAGE_KEY,
  TEXT_SCALE_MAX,
  TEXT_SCALE_MIN,
  THEMES,
  TWO_LINE_TITLES_STORAGE_KEY,
  applyTextScale,
  applyTheme,
  clampTextScale,
  defaultSettings,
  loadSettings,
  saveLayout,
  saveTextScale,
  saveTheme,
  saveTwoLineTitles,
  TEXT_SCALE_STORAGE_KEY,
  THEME_STORAGE_KEY,
} from './settings'

const keys = [
  THEME_STORAGE_KEY,
  TEXT_SCALE_STORAGE_KEY,
  TWO_LINE_TITLES_STORAGE_KEY,
  LAYOUT_STORAGE_KEY,
]

afterEach(() => {
  for (const k of keys) localStorage.removeItem(k)
  document.documentElement.removeAttribute('data-theme')
  document.documentElement.style.removeProperty('--text-scale')
})

describe('settings', () => {
  it('offers the full theme list including the requested ones', () => {
    const ids = THEMES.map((t) => t.id)
    expect(ids).toContain('auto')
    expect(ids).toContain('solarized-light')
    expect(ids).toContain('solarized-dark')
    expect(ids).toContain('kimbie-dark')
    expect(ids).toContain('tokyo-night')
  })

  it('defaults to auto theme at 100% and loads those when storage is empty', () => {
    const d = { theme: 'auto', textScale: 100, twoLineTitles: false, layout: 'auto' }
    expect(defaultSettings()).toEqual(d)
    expect(loadSettings()).toEqual(d)
  })

  it('round-trips saved values through storage', () => {
    saveTheme('tokyo-night')
    saveTextScale(125)
    saveTwoLineTitles(true)
    saveLayout('compact')
    expect(loadSettings()).toEqual({
      theme: 'tokyo-night',
      textScale: 125,
      twoLineTitles: true,
      layout: 'compact',
    })
    // Auto and "off" persist as the absence of their keys.
    saveTheme('auto')
    saveTwoLineTitles(false)
    saveLayout('auto')
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBeNull()
    expect(localStorage.getItem(TWO_LINE_TITLES_STORAGE_KEY)).toBeNull()
    expect(localStorage.getItem(LAYOUT_STORAGE_KEY)).toBeNull()
  })

  it('offers the three layouts and ignores an unknown stored one', () => {
    expect(LAYOUTS.map((l) => l.id)).toEqual(['auto', 'wide', 'compact'])
    localStorage.setItem(LAYOUT_STORAGE_KEY, 'stacked')
    expect(loadSettings().layout).toBe('auto')
    saveLayout('wide')
    expect(loadSettings().layout).toBe('wide')
  })

  it('ignores unknown stored values and falls back to defaults', () => {
    localStorage.setItem(THEME_STORAGE_KEY, 'not-a-theme')
    localStorage.setItem(TEXT_SCALE_STORAGE_KEY, 'nope')
    localStorage.setItem(TWO_LINE_TITLES_STORAGE_KEY, 'maybe')
    expect(loadSettings()).toEqual({
      theme: 'auto',
      textScale: 100,
      twoLineTitles: false,
      layout: 'auto',
    })
  })

  it('applies a theme via the data-theme attribute and auto removes it', () => {
    applyTheme('tokyo-night')
    expect(document.documentElement.getAttribute('data-theme')).toBe('tokyo-night')
    applyTheme('auto')
    expect(document.documentElement.hasAttribute('data-theme')).toBe(false)
    // Unknown ids behave like auto (never an unstyled state).
    applyTheme('bogus')
    expect(document.documentElement.hasAttribute('data-theme')).toBe(false)
  })

  it('applies the text scale as the --text-scale custom property', () => {
    applyTextScale(125)
    expect(document.documentElement.style.getPropertyValue('--text-scale')).toBe('1.25')
  })

  it('clamps text scale to the slider bounds', () => {
    expect(clampTextScale(50)).toBe(TEXT_SCALE_MIN)
    expect(clampTextScale(500)).toBe(TEXT_SCALE_MAX)
    expect(clampTextScale(Number.NaN)).toBe(100)
    applyTextScale(10)
    expect(document.documentElement.style.getPropertyValue('--text-scale')).toBe('0.75')
  })
})
