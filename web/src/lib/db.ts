import { openDB, type IDBPDatabase } from 'idb'
import type { Folder, Snippet } from './types'

/**
 * Local IndexedDB cache (spec §6): folders, snippets, and meta.
 * Sensitive snippet bodies are stored as null — the body is only
 * fetched on reveal and held in memory (spec §6: "Sensitive bodies are
 * never written to IndexedDB").
 */

export const DB_NAME = 'snp'
export const DB_VERSION = 1

/** The meta key holding the last server_time (the next `since`). */
export const META_SERVER_TIME = 'server_time'

/** idb schema for type-safe store access. */
export interface SnpDBSchema {
  folders: { key: string; value: Folder }
  snippets: { key: string; value: Snippet }
  /** String key–value pairs; currently only META_SERVER_TIME. */
  meta: { key: string; value: string }
}

export type SnpDB = IDBPDatabase<SnpDBSchema>

/**
 * Open (or create) a snp cache database under the given name. The name
 * is a parameter so tests get isolated databases; the app uses
 * openLocalDB.
 */
export function createSnpDB(name: string): Promise<SnpDB> {
  return openDB<SnpDBSchema>(name, DB_VERSION, {
    upgrade(db) {
      db.createObjectStore('folders', { keyPath: 'id' })
      db.createObjectStore('snippets', { keyPath: 'id' })
      db.createObjectStore('meta')
    },
  })
}

export function openLocalDB(): Promise<SnpDB> {
  return createSnpDB(DB_NAME)
}

export async function putFolder(db: SnpDB, folder: Folder): Promise<void> {
  await db.put('folders', folder)
}

export async function removeFolder(db: SnpDB, id: string): Promise<void> {
  await db.delete('folders', id)
}

export async function allFolders(db: SnpDB): Promise<Folder[]> {
  return db.getAll('folders')
}

export async function putSnippet(db: SnpDB, snippet: Snippet): Promise<void> {
  await db.put('snippets', cacheSnippet(snippet))
}

export async function removeSnippet(db: SnpDB, id: string): Promise<void> {
  await db.delete('snippets', id)
}

export async function allSnippets(db: SnpDB): Promise<Snippet[]> {
  return db.getAll('snippets')
}

/** The stored server_time (the next `since`); undefined on first run. */
export async function getServerTime(db: SnpDB): Promise<string | undefined> {
  return db.get('meta', META_SERVER_TIME)
}

export async function setServerTime(db: SnpDB, t: string): Promise<void> {
  await db.put('meta', t, META_SERVER_TIME)
}

/** Clears all local data — the "full resync" action (spec §6). */
export async function clearLocalData(db: SnpDB): Promise<void> {
  const tx = db.transaction(['folders', 'snippets', 'meta'], 'readwrite')
  await Promise.all([
    tx.objectStore('folders').clear(),
    tx.objectStore('snippets').clear(),
    tx.objectStore('meta').clear(),
  ])
  await tx.done
}

/** Strip decrypted fields at the persistence boundary, regardless of caller. */
export function cacheSnippet(snippet: Snippet): Snippet {
  return snippet.is_sensitive ? { ...snippet, body: null, var_defaults: null } : snippet
}
