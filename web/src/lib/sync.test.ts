import 'fake-indexeddb/auto'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  allFolders,
  allSnippets,
  createSnpDB,
  getServerTime,
  type SnpDB,
} from './db'
import { SnippetIndex } from './search'
import { mergeSyncResponse, syncLocal } from './sync'
import type { Folder, Snippet, SyncResponse } from './types'

const folderA: Folder = {
  id: 'f1',
  parent_id: null,
  name: 'Ops',
  created_at: '2026-09-01T00:00:00Z',
  updated_at: '2026-09-01T00:00:00Z',
}

const folderB: Folder = {
  id: 'f2',
  parent_id: 'f1',
  name: 'Deploy',
  created_at: '2026-09-01T00:00:00Z',
  updated_at: '2026-09-01T00:00:00Z',
}

const snippetA: Snippet = {
  id: 's1',
  title: 'restart caddy',
  body: 'sudo systemctl restart caddy',
  language: 'bash',
  notes: '',
  folder_id: 'f1',
  tags: ['ops', 'caddy'],
  is_sensitive: false,
  uses_variables: false,
  created_at: '2026-09-01T00:00:00Z',
  updated_at: '2026-09-01T00:00:00Z',
}

const snippetB: Snippet = {
  id: 's2',
  title: 'db password',
  body: null,
  language: 'sql',
  notes: '',
  folder_id: 'f2',
  tags: ['db'],
  is_sensitive: true,
  uses_variables: false,
  created_at: '2026-09-01T00:00:00Z',
  updated_at: '2026-09-01T00:00:00Z',
}

function resp(over: Partial<SyncResponse> = {}): SyncResponse {
  return {
    server_time: '2026-09-02T12:00:00Z',
    folders: [folderA, folderB],
    snippets: [snippetA, snippetB],
    ...over,
  }
}

let db: SnpDB
let index: SnippetIndex

beforeEach(async () => {
  db = await createSnpDB(`snp-sync-${Math.random().toString(36).slice(2)}`)
  index = new SnippetIndex()
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('mergeSyncResponse', () => {
  it('upserts live rows and stores server_time', async () => {
    await mergeSyncResponse(db, resp(), index)
    expect(await allFolders(db)).toHaveLength(2)
    expect(await allSnippets(db)).toHaveLength(2)
    expect(await getServerTime(db)).toBe('2026-09-02T12:00:00Z')
    expect(index.size).toBe(2)
  })

  it('keeps sensitive bodies null in the local cache', async () => {
    await mergeSyncResponse(db, resp())
    const s = (await allSnippets(db)).find((x) => x.id === snippetB.id)!
    expect(s.body).toBeNull()
    expect(s.is_sensitive).toBe(true)
  })

  it('deletes rows named by tombstones', async () => {
    await mergeSyncResponse(db, resp(), index)
    await mergeSyncResponse(
      db,
      resp({
        server_time: '2026-09-02T13:00:00Z',
        folders: [{ id: folderA.id, deleted_at: '2026-09-02T12:30:00Z' }],
        snippets: [
          snippetA,
          { id: snippetB.id, deleted_at: '2026-09-02T12:40:00Z' },
        ],
      }),
      index,
    )
    expect((await allFolders(db)).map((f) => f.id)).toEqual([folderB.id])
    expect((await allSnippets(db)).map((s) => s.id)).toEqual([snippetA.id])
    expect(index.size).toBe(1)
    expect(index.search('').map((s) => s.id)).toEqual([snippetA.id])
  })

  it('is idempotent: the same response twice leaves the same state', async () => {
    await mergeSyncResponse(db, resp(), index)
    await mergeSyncResponse(db, resp(), index)
    expect(await allFolders(db)).toHaveLength(2)
    expect(await allSnippets(db)).toHaveLength(2)
    expect(index.size).toBe(2)
    expect(await getServerTime(db)).toBe('2026-09-02T12:00:00Z')
  })

  it('overwrites a row with a newer version', async () => {
    await mergeSyncResponse(db, resp())
    await mergeSyncResponse(
      db,
      resp({
        server_time: '2026-09-02T14:00:00Z',
        folders: [],
        snippets: [{ ...snippetA, title: 'restart caddy (v2)' }],
      }),
    )
    const s = (await allSnippets(db)).find((x) => x.id === snippetA.id)!
    expect(s.title).toBe('restart caddy (v2)')
  })

  it('updates the search index incrementally', async () => {
    expect(index.search('caddy')).toEqual([])
    await mergeSyncResponse(db, resp(), index)
    expect(index.search('caddy').map((s) => s.id)).toEqual([snippetA.id])
  })
})

describe('syncLocal', () => {
  it('does a full sync when no since is stored, then passes since', async () => {
    const urls: string[] = []
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        urls.push(String(input))
        return new Response(JSON.stringify(resp()), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        })
      }),
    )

    await syncLocal(db, index)
    await syncLocal(db, index)

    expect(urls[0]).toBe('/api/sync')
    expect(urls[1]).toBe('/api/sync?since=2026-09-02T12%3A00%3A00Z')
  })
})
