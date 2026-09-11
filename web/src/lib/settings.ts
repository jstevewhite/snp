/**
 * Appearance settings (themes + interface text size), shared by the
 * settings panel (App.svelte) and the startup apply step (main.ts).
 *
 * Themes are applied by setting `data-theme` on <html>; app.css defines
 * one :root palette block per theme (see the [data-theme=…] rules).
 * The special "auto" theme is the absence of the attribute, so the
 * prefers-color-scheme media query drives it. Text size is a scale
 * factor on the --text-scale custom property, which every font-size
 * declaration multiplies (app.css).
 *
 * Both settings persist in localStorage and are re-applied on startup,
 * in the browser and in the wails desktop window alike.
 */

export interface ThemeOption {
  /** Value of the data-theme attribute; 'auto' is no attribute. */
  id: string
  label: string
}

export const THEMES: ThemeOption[] = [
  { id: 'auto', label: 'Auto (system)' },
  { id: 'light', label: 'Light' },
  { id: 'dark', label: 'Dark' },
  { id: 'solarized-light', label: 'Solarized Light' },
  { id: 'solarized-dark', label: 'Solarized Dark' },
  { id: 'kimbie-dark', label: 'Kimbie Dark' },
  { id: 'tokyo-night', label: 'Tokyo Night' },
]

/** Lower bound of the text-size slider, as a percentage of base. */
export const TEXT_SCALE_MIN = 75
/** Upper bound of the text-size slider, as a percentage of base. */
export const TEXT_SCALE_MAX = 150
export const TEXT_SCALE_STEP = 5

export const THEME_STORAGE_KEY = 'snp.theme'
export const TEXT_SCALE_STORAGE_KEY = 'snp.textScale'

const DEFAULT_THEME = 'auto'
const DEFAULT_TEXT_SCALE = 100

export interface AppearanceSettings {
  theme: string
  /** Interface text size as a percentage of the base size (100 = base). */
  textScale: number
}

export function defaultSettings(): AppearanceSettings {
  return { theme: DEFAULT_THEME, textScale: DEFAULT_TEXT_SCALE }
}

function storage(): Storage | undefined {
  try {
    return typeof window === 'undefined' ? undefined : window.localStorage
  } catch {
    // Privacy modes / blocked storage: fall back to defaults.
    return undefined
  }
}

/** The persisted appearance settings, or defaults when absent/unreadable. */
export function loadSettings(): AppearanceSettings {
  const s = storage()
  const out = defaultSettings()
  if (!s) return out
  try {
    const theme = s.getItem(THEME_STORAGE_KEY)
    if (theme !== null && THEMES.some((t) => t.id === theme)) out.theme = theme
    const raw = s.getItem(TEXT_SCALE_STORAGE_KEY)
    if (raw !== null && raw !== '') {
      const scale = Number(raw)
      if (Number.isFinite(scale)) out.textScale = clampTextScale(scale)
    }
  } catch {
    // Ignore malformed values; keep defaults.
  }
  return out
}

export function saveTheme(theme: string): void {
  const s = storage()
  if (!s) return
  try {
    if (theme === DEFAULT_THEME) s.removeItem(THEME_STORAGE_KEY)
    else s.setItem(THEME_STORAGE_KEY, theme)
  } catch {
    // Persistence is best-effort.
  }
}

export function saveTextScale(percent: number): void {
  const s = storage()
  if (!s) return
  try {
    s.setItem(TEXT_SCALE_STORAGE_KEY, String(clampTextScale(percent)))
  } catch {
    // Persistence is best-effort.
  }
}

export function clampTextScale(percent: number): number {
  if (!Number.isFinite(percent)) return DEFAULT_TEXT_SCALE
  return Math.min(TEXT_SCALE_MAX, Math.max(TEXT_SCALE_MIN, Math.round(percent)))
}

/**
 * Apply a theme id to the document: 'auto' removes the attribute so the
 * prefers-color-scheme media query wins; anything else sets
 * data-theme=<id>, which app.css maps to a palette.
 */
export function applyTheme(theme: string): void {
  if (typeof document === 'undefined') return
  if (theme === DEFAULT_THEME || !THEMES.some((t) => t.id === theme)) {
    document.documentElement.removeAttribute('data-theme')
    return
  }
  document.documentElement.setAttribute('data-theme', theme)
}

/** Apply a text-size percentage to the --text-scale custom property. */
export function applyTextScale(percent: number): void {
  if (typeof document === 'undefined') return
  const pct = clampTextScale(percent)
  document.documentElement.style.setProperty('--text-scale', String(pct / 100))
}
