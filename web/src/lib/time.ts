/**
 * Timestamp formatting for the UI.
 *
 * Every stored and serialized time is RFC3339 UTC, second precision,
 * always 'Z' (spec §4). Formatting here is display-only — it never feeds
 * a value back into the store or the sync cursor — and any simplified
 * label keeps the exact timestamp available as a tooltip via
 * `formatAbsolute`.
 *
 * Functions accept a string, a number of epoch milliseconds, or a Date.
 * Unparseable input renders as the input itself (for a string) rather
 * than an empty label, so a bad value is visible instead of invisible.
 */

export type TimeInput = string | number | Date

const MINUTE_MS = 60_000
const HOUR_MS = 60 * MINUTE_MS
const DAY_MS = 24 * HOUR_MS

function toDate(input: TimeInput): Date | null {
  const d = input instanceof Date ? input : new Date(input)
  return Number.isNaN(d.getTime()) ? null : d
}

function unparseable(input: TimeInput): string {
  return typeof input === 'string' ? input : ''
}

function ago(n: number, unit: string): string {
  return `${n} ${unit}${n === 1 ? '' : 's'} ago`
}

/**
 * A coarse "how long ago" label for recent times, falling back to the
 * calendar date once it is older than yesterday:
 *
 *   < 1 min      "just now"
 *   < 1 hour     "1 minute ago" / "12 minutes ago"
 *   < 24 hours   "1 hour ago" / "5 hours ago"
 *   < 48 hours   "yesterday"
 *   older        "Sep 11" (see formatDate)
 *
 * A timestamp in the future reads "just now" rather than "-1 minutes
 * ago": this labels the last sync, and a client clock behind the
 * server's must not produce a nonsense age.
 */
export function formatRelative(input: TimeInput, now: number = Date.now()): string {
  const d = toDate(input)
  if (d === null) return unparseable(input)
  const diff = now - d.getTime()
  if (diff < MINUTE_MS) return 'just now'
  if (diff < HOUR_MS) return ago(Math.floor(diff / MINUTE_MS), 'minute')
  if (diff < DAY_MS) return ago(Math.floor(diff / HOUR_MS), 'hour')
  if (diff < 2 * DAY_MS) return 'yesterday'
  return formatDate(d, now)
}

/**
 * Short calendar date: "Sep 11", plus the year when it is not the
 * current one ("Sep 11, 2025"). Locale-aware, so the month name follows
 * the user's locale rather than a hardcoded abbreviation.
 */
export function formatDate(input: TimeInput, now: number = Date.now()): string {
  const d = toDate(input)
  if (d === null) return unparseable(input)
  const sameYear = d.getFullYear() === new Date(now).getFullYear()
  return d.toLocaleDateString(undefined, {
    month: 'short',
    day: 'numeric',
    ...(sameYear ? {} : { year: 'numeric' }),
  })
}

/**
 * The precise local date and time, for the tooltip behind a simplified
 * label ("Sep 11, 2026, 1:32:00 PM").
 */
export function formatAbsolute(input: TimeInput): string {
  const d = toDate(input)
  if (d === null) return unparseable(input)
  return d.toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'medium' })
}
