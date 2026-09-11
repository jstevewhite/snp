/**
 * The running release version, shown in the header next to the wordmark
 * (spec §5, `GET /api/version`).
 *
 * The server is authoritative — in the browser the request goes over HTTP,
 * in the desktop window over the in-process bridge — but the value is a
 * property of the build, so it is cached in localStorage and still shown
 * when the PWA is opened offline or the request fails.
 */

import * as api from './api'
import type { VersionInfo } from './types'

export const VERSION_STORAGE_KEY = 'snp.version'

function storage(): Storage | undefined {
  try {
    return typeof window === 'undefined' ? undefined : window.localStorage
  } catch {
    // Privacy modes / blocked storage: the header just shows nothing.
    return undefined
  }
}

/** The version last fetched on this device, or null when never seen. */
export function loadCachedVersion(): string | null {
  const s = storage()
  if (!s) return null
  try {
    const v = s.getItem(VERSION_STORAGE_KEY)
    return v !== null && v !== '' ? v : null
  } catch {
    return null
  }
}

/** Persist a version for later (offline) loads. */
export function cacheVersion(version: string): void {
  const s = storage()
  if (!s) return
  try {
    s.setItem(VERSION_STORAGE_KEY, version)
  } catch {
    // Full/blocked storage: the header still shows the live value.
  }
}

/**
 * Fetch the server's version, caching it. On any failure this falls back
 * to the cached value (null when there is none) and never throws: the
 * header chip is decoration, so a failed lookup must not surface as an
 * app error.
 */
export async function resolveVersion(): Promise<string | null> {
  try {
    const info = (await api.version()) as VersionInfo | null
    const v = info !== null && typeof info.version === 'string' ? info.version.trim() : ''
    if (v !== '') {
      cacheVersion(v)
      return v
    }
  } catch {
    // Offline, or a server older than /api/version.
  }
  return loadCachedVersion()
}
