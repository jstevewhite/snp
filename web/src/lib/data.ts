import type { ImportDocument } from './api'
import { isDesktop, resolveDesktopApp } from './desktop'
import { ApiError } from './types'

export const MAX_IMPORT_BYTES = 10 * 1024 * 1024
export const MAX_SELECTED_BYTES = 16 * 1024 * 1024
export function isEncryptedFile(text: string): boolean { return text.trimStart().startsWith('-----BEGIN AGE ENCRYPTED FILE-----') }
export type DownloadKind = 'export' | 'backup'

/** Retain the original fields; the server performs the authoritative validation. */
export function parseImport(text: string): ImportDocument {
  if (new TextEncoder().encode(text).byteLength > MAX_IMPORT_BYTES) {
    throw new Error('Import files must be 10 MiB or smaller. Use snp import for larger files.')
  }
  let value: unknown
  try { value = JSON.parse(text.replace(/^\uFEFF/, '')) }
  catch { throw new Error('This file is not valid JSON.') }
  if (!record(value) || value.version !== 1 || !Array.isArray(value.snippets)) {
    throw new Error('Choose a snp version-1 JSON export with a snippets array. Backup ZIP files must be restored separately.')
  }
  if (value.folders !== undefined && !Array.isArray(value.folders)) throw new Error('folders must be an array.')
  for (const [i, s] of value.snippets.entries()) {
    if (!record(s) || typeof s.title !== 'string' || !s.title.trim() || typeof s.body !== 'string') {
      throw new Error(`Snippet ${i + 1} needs a title and a text body. Use an export, not a sync response with hidden bodies.`)
    }
  }
  return value as ImportDocument
}
function record(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

export async function chooseDesktopImport(): Promise<{ name: string; text: string } | null> {
  const app = await resolveDesktopApp()
  if (!app?.OpenImport) throw new Error('Native file import is unavailable. Update the desktop app.')
  return app.OpenImport()
}

/** No file contents enter IndexedDB, localStorage or the diagnostic log. */
export async function downloadData(kind: DownloadKind, password?: string): Promise<'saved' | 'downloaded' | 'cancelled'> {
  if (isDesktop()) {
    const app = await resolveDesktopApp()
    if (password !== undefined) {
      if (!app?.SaveEncryptedData) throw new Error('Encrypted file saving is unavailable. Update the desktop app.')
      return await app.SaveEncryptedData(kind, password) ? 'saved' : 'cancelled'
    }
    if (!app?.SaveData) throw new Error('Native file saving is unavailable. Update the desktop app.')
    return await app.SaveData(kind) ? 'saved' : 'cancelled'
  }
  const response = await fetch(`/api/${kind}`, kind === 'backup' || password !== undefined
    ? { method: 'POST', headers: { 'Content-Type': 'application/json' }, ...(password !== undefined ? { body: JSON.stringify({ encrypt: true, password }) } : {}), cache: 'no-store' }
    : { cache: 'no-store' })
  if (!response.ok) {
    let message = `Download failed (HTTP ${response.status}).`
    try { const failure = await response.json(); if (typeof failure.error === 'string') message = failure.error } catch { /* Keep HTTP status. */ }
    throw new ApiError(response.status, message)
  }
  const blob = await response.blob()
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = `snp-${kind}-${new Date().toISOString().replace(/[:.]/g, '-')}.${kind === 'backup' ? 'zip' : 'json'}${password !== undefined ? '.age' : ''}`
  document.body.appendChild(link)
  try { link.click() } finally {
    link.remove()
    // Give the browser time to accept the download before releasing its bytes.
    setTimeout(() => URL.revokeObjectURL(url), 1000)
  }
  return 'downloaded'
}
