import { describe, expect, it } from 'vitest'
import { SnippetIndex } from './search'
import type { Snippet } from './types'

function sn(p: Partial<Snippet> & { id: string }): Snippet {
  return {
    title: '',
    body: '',
    language: '',
    notes: '',
    folder_id: null,
    tags: [],
    is_sensitive: false,
    uses_variables: false,
    created_at: '2026-09-01T00:00:00Z',
    updated_at: '2026-09-01T00:00:00Z',
    ...p,
  }
}

const caddy = sn({
  id: 's1',
  title: 'restart caddy',
  body: 'sudo systemctl restart caddy',
  language: 'bash',
  tags: ['ops', 'caddy'],
  updated_at: '2026-09-02T10:00:00Z',
})

const deploy = sn({
  id: 's2',
  title: 'deploy app',
  body: 'rsync -av ./ /var/www/app',
  language: 'bash',
  tags: ['ops', 'deploy'],
  updated_at: '2026-09-02T11:00:00Z',
})

const gomod = sn({
  id: 's3',
  title: 'go mod tidy',
  body: 'go mod tidy',
  language: 'go',
  tags: ['dev'],
  updated_at: '2026-09-02T12:00:00Z',
})

// Sensitive: body is null locally, as it is in every sync response.
const dbBackup = sn({
  id: 's4',
  title: 'db backup',
  body: null,
  language: 'sql',
  tags: ['ops', 'db'],
  is_sensitive: true,
  updated_at: '2026-09-02T13:00:00Z',
})

const all = [caddy, deploy, gomod, dbBackup]

describe('SnippetIndex', () => {
  it('finds snippets by a term in title, body, or tags', () => {
    const index = new SnippetIndex(all)
    expect(index.search('caddy').map((s) => s.id)).toEqual(['s1'])
    expect(index.search('rsync').map((s) => s.id)).toEqual(['s2'])
    expect(index.search('dev').map((s) => s.id)).toEqual(['s3'])
  })

  it('prefix-matches the start of a token, like the server', () => {
    const index = new SnippetIndex(all)
    expect(index.search('rest').map((s) => s.id)).toEqual(['s1'])
    expect(index.search('depl').map((s) => s.id)).toEqual(['s2'])
    expect(index.search('back').map((s) => s.id)).toEqual(['s4'])
  })

  it('does not match the middle of a token', () => {
    const index = new SnippetIndex(all)
    expect(index.search('estart')).toEqual([])
  })

  it('ANDs multiple terms: every term must match, like FTS5', () => {
    // MiniSearch defaults to OR, so combineWith is set explicitly to match
    // the server. internal/store/search_test.go pins the same behaviour
    // against FTS5 (TestFTSMatch prefix and AND cases) — keep the two in
    // step when touching either engine.
    const index = new SnippetIndex(all)
    expect(index.search('rest caddy').map((s) => s.id)).toEqual(['s1'])
    // One unmatched term drops the row, even though 'caddy' alone matches.
    expect(index.search('caddy nonexistentterm')).toEqual([])
  })

  it('returns everything, most recently updated first, when no FTS terms remain', () => {
    const index = new SnippetIndex(all)
    expect(index.search('').map((s) => s.id)).toEqual(['s4', 's3', 's2', 's1'])
    expect(index.search('tag:ops tag:db').map((s) => s.id)).toEqual(['s4'])
  })

  it('ANDs tag filters: a snippet must carry every tag', () => {
    const index = new SnippetIndex(all)
    expect(index.search('tag:ops').map((s) => s.id).sort()).toEqual([
      's1',
      's2',
      's4',
    ])
    expect(index.search('tag:ops tag:db').map((s) => s.id)).toEqual(['s4'])
  })

  it('ORs language filters within the list, like the server IN (...)', () => {
    const index = new SnippetIndex(all)
    expect(index.search('lang:go').map((s) => s.id)).toEqual(['s3'])
    expect(index.search('lang:go lang:sql').map((s) => s.id)).toEqual([
      's4',
      's3',
    ])
  })

  it('combines FTS terms with filters', () => {
    const index = new SnippetIndex(all)
    expect(index.search('restart tag:ops').map((s) => s.id)).toEqual(['s1'])
    expect(index.search('restart tag:db')).toEqual([])
  })

  it('cannot match sensitive bodies, consistent with the server', () => {
    const index = new SnippetIndex(all)
    // 'sqlite' would be in dbBackup's body if it were cached; it is null.
    expect(index.search('sqlite tag:db')).toEqual([])
    // ...but its title still matches.
    expect(index.search('backup tag:db').map((s) => s.id)).toEqual(['s4'])
  })

  it('upsert updates a snippet in place', () => {
    const index = new SnippetIndex(all)
    index.upsert({ ...caddy, title: 'restart caddy service' })
    expect(index.search('caddy').map((s) => s.id)).toEqual(['s1'])
    expect(index.search('service').map((s) => s.id)).toEqual(['s1'])
    expect(index.size).toBe(4)
  })

  it('remove is idempotent and unknown ids are a no-op', () => {
    const index = new SnippetIndex(all)
    index.remove('s1')
    index.remove('s1')
    index.remove('nope')
    expect(index.size).toBe(3)
    expect(index.search('caddy')).toEqual([])
  })

  it('an empty index searches to nothing', () => {
    const index = new SnippetIndex()
    expect(index.search('anything')).toEqual([])
    expect(index.search('')).toEqual([])
  })
})
