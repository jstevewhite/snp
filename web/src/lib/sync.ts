import { sync as apiSync } from './api'
import { cacheSnippet, getServerTime, META_SERVER_TIME, type SnpDB } from './db'
import type { SnippetIndex } from './search'
import type { Folder, Snippet, SyncResponse, Tombstone } from './types'

/**
 * Sync + idempotent merge into the local cache (spec §6).
 *
 * The merge is idempotent: the server may return the same row in two
 * consecutive syncs (spec §5), and a crash mid-merge just re-fetches
 * from the stored since on the next sync, because server_time is
 * written in the same transaction as the rows.
 */

/** A sync item is a tombstone when it carries no live-row fields. */
export function isFolderTombstone(item: Folder | Tombstone): item is Tombstone {
  return !('name' in item)
}

export function isSnippetTombstone(item: Snippet | Tombstone): item is Tombstone {
  return !('title' in item)
}

/**
 * Merge a sync response into IndexedDB and, optionally, the search
 * index. Live rows upsert; tombstones delete. The in-memory index is
 * updated after the transaction commits, so it never reflects rows the
 * database rejected.
 */
export async function mergeSyncResponse(
  db: SnpDB,
  resp: SyncResponse,
  index?: SnippetIndex,
): Promise<void> {
  const tx = db.transaction(['folders', 'snippets', 'meta'], 'readwrite')
  const folders = tx.objectStore('folders')
  const snippets = tx.objectStore('snippets')
  const meta = tx.objectStore('meta')

  const added: Snippet[] = []
  const removed: string[] = []

  for (const item of resp.folders) {
    if (isFolderTombstone(item)) {
      folders.delete(item.id)
    } else {
      folders.put(item)
    }
  }
  for (const item of resp.snippets) {
    if (isSnippetTombstone(item)) {
      snippets.delete(item.id)
      removed.push(item.id)
    } else {
      snippets.put(cacheSnippet(item))
      added.push(item)
    }
  }
  meta.put(resp.server_time, META_SERVER_TIME)
  await tx.done

  for (const s of added) index?.upsert(s)
  for (const id of removed) index?.remove(id)
}

/**
 * One sync cycle: read the stored since, fetch /api/sync, merge into
 * the local cache. Throws ApiError on network/HTTP failure; the caller
 * decides when to retry (spec §8: failed sync is retried on the next
 * interval).
 */
export async function syncLocal(
  db: SnpDB,
  index?: SnippetIndex,
): Promise<SyncResponse> {
  const since = await getServerTime(db)
  const resp = await apiSync(since)
  await mergeSyncResponse(db, resp, index)
  return resp
}
