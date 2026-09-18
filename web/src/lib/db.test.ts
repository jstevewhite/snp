import 'fake-indexeddb/auto'
import { beforeEach, describe, expect, it } from 'vitest'
import {
  allFolders,
  allSnippets,
  clearLocalData,
  createSnpDB,
  getServerTime,
  putFolder,
  putSnippet,
  removeFolder,
  removeSnippet,
  setServerTime,
  type SnpDB,
} from './db'
import type { Folder, Snippet } from './types'

const folderA: Folder = {
  id: 'f1',
  parent_id: null,
  name: 'Ops',
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
  tags: ['ops'],
  is_sensitive: false,
  uses_variables: false,
  created_at: '2026-09-01T00:00:00Z',
  updated_at: '2026-09-01T00:00:00Z',
}

const sensitiveSnippet: Snippet = {
  ...snippetA,
  id: 's2',
  title: 'db password',
  body: null,
  language: 'sql',
  tags: ['db'],
  is_sensitive: true,
}

let db: SnpDB

beforeEach(async () => {
  // A fresh in-memory database per test (fake-indexeddb is per-name).
  db = await createSnpDB(`snp-test-${Math.random().toString(36).slice(2)}`)
})

describe('folders', () => {
  it('round-trips a folder', async () => {
    await putFolder(db, folderA)
    expect(await allFolders(db)).toEqual([folderA])
  })

  it('put is an upsert', async () => {
    await putFolder(db, folderA)
    await putFolder(db, { ...folderA, name: 'Renamed' })
    expect((await allFolders(db)).map((f) => f.name)).toEqual(['Renamed'])
  })

  it('removeFolder deletes the row', async () => {
    await putFolder(db, folderA)
    await removeFolder(db, 'f1')
    expect(await allFolders(db)).toEqual([])
  })
})

describe('snippets', () => {
  it('round-trips a snippet', async () => {
    await putSnippet(db, snippetA)
    expect(await allSnippets(db)).toEqual([snippetA])
  })

  it('keeps sensitive bodies null', async () => {
    await putSnippet(db, sensitiveSnippet)
    const [s] = await allSnippets(db)
    expect(s.body).toBeNull()
    expect(s.is_sensitive).toBe(true)
  })

  it('redacts decrypted sensitive fields at the persistence boundary', async () => {
    await putSnippet(db, { ...sensitiveSnippet, body: 'secret', var_defaults: { token: 'secret' } })
    expect((await allSnippets(db))[0]).toMatchObject({ body: null, var_defaults: null })
  })

  it('removeSnippet deletes the row', async () => {
    await putSnippet(db, snippetA)
    await removeSnippet(db, 's1')
    expect(await allSnippets(db)).toEqual([])
  })
})

describe('meta', () => {
  it('has no server_time on a fresh db', async () => {
    expect(await getServerTime(db)).toBeUndefined()
  })

  it('stores and returns server_time', async () => {
    await setServerTime(db, '2026-09-02T10:00:00Z')
    expect(await getServerTime(db)).toBe('2026-09-02T10:00:00Z')
  })
})

describe('clearLocalData', () => {
  it('empties every store', async () => {
    await putFolder(db, folderA)
    await putSnippet(db, snippetA)
    await setServerTime(db, '2026-09-02T10:00:00Z')
    await clearLocalData(db)
    expect(await allFolders(db)).toEqual([])
    expect(await allSnippets(db)).toEqual([])
    expect(await getServerTime(db)).toBeUndefined()
  })
})
