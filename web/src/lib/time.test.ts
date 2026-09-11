import { describe, expect, it } from 'vitest'
import { formatAbsolute, formatDate, formatRelative } from './time'

// A fixed instant so the thresholds are deterministic: 2026-09-11 12:00 UTC.
const NOW = Date.UTC(2026, 8, 11, 12, 0, 0)
const MINUTE = 60_000
const HOUR = 60 * MINUTE
const DAY = 24 * HOUR

/** An ISO timestamp `ms` before NOW. */
function agoIso(ms: number): string {
  return new Date(NOW - ms).toISOString()
}

describe('formatRelative', () => {
  it('reads just now under a minute', () => {
    expect(formatRelative(agoIso(0), NOW)).toBe('just now')
    expect(formatRelative(agoIso(MINUTE - 1), NOW)).toBe('just now')
  })

  it('counts whole minutes, singular at one', () => {
    expect(formatRelative(agoIso(MINUTE), NOW)).toBe('1 minute ago')
    expect(formatRelative(agoIso(2 * MINUTE), NOW)).toBe('2 minutes ago')
    expect(formatRelative(agoIso(59 * MINUTE), NOW)).toBe('59 minutes ago')
  })

  it('counts whole hours, singular at one', () => {
    expect(formatRelative(agoIso(HOUR), NOW)).toBe('1 hour ago')
    expect(formatRelative(agoIso(5 * HOUR), NOW)).toBe('5 hours ago')
    expect(formatRelative(agoIso(DAY - 1), NOW)).toBe('23 hours ago')
  })

  it('says yesterday between one and two days', () => {
    expect(formatRelative(agoIso(DAY), NOW)).toBe('yesterday')
    expect(formatRelative(agoIso(2 * DAY - 1), NOW)).toBe('yesterday')
  })

  it('falls back to the calendar date beyond yesterday', () => {
    const label = formatRelative(agoIso(2 * DAY), NOW)
    // Locale-dependent month name, so assert the parts, not the spelling.
    expect(label).toContain('9')
    expect(label).not.toContain('2026')
  })

  it('treats a future timestamp as just now', () => {
    // A client clock behind the server must not render "-1 minutes ago".
    expect(formatRelative(new Date(NOW + 5 * MINUTE).toISOString(), NOW)).toBe('just now')
  })

  it('accepts epoch milliseconds and Date objects', () => {
    expect(formatRelative(NOW - 2 * MINUTE, NOW)).toBe('2 minutes ago')
    expect(formatRelative(new Date(NOW - HOUR), NOW)).toBe('1 hour ago')
  })

  it('returns the input when it cannot be parsed', () => {
    expect(formatRelative('not a date', NOW)).toBe('not a date')
    expect(formatRelative(Number.NaN, NOW)).toBe('')
  })
})

describe('formatDate', () => {
  it('omits the year within the current year', () => {
    const label = formatDate(agoIso(0), NOW)
    expect(label).toContain('11')
    expect(label).not.toContain('2026')
  })

  it('includes the year for another year', () => {
    expect(formatDate('2025-09-11T12:00:00Z', NOW)).toContain('2025')
  })

  it('accepts epoch milliseconds', () => {
    expect(formatDate(NOW, NOW)).not.toContain('2026')
  })

  it('returns the input when it cannot be parsed', () => {
    expect(formatDate('nope', NOW)).toBe('nope')
  })
})

describe('formatAbsolute', () => {
  it('carries the precise date and time', () => {
    const label = formatAbsolute('2026-09-11T12:00:00Z')
    expect(label).toContain('2026')
    expect(label).toContain('11')
  })

  it('returns the input when it cannot be parsed', () => {
    expect(formatAbsolute('nope')).toBe('nope')
  })
})
