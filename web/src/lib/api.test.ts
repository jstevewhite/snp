import { afterEach, describe, expect, it, vi } from 'vitest'
import * as api from './api'
import { ApiError, type SnippetInput } from './types'

const input: SnippetInput = {
  title: 't',
  body: 'b',
  language: 'bash',
  notes: '',
  folder_id: null,
  tags: ['x'],
  is_sensitive: false,
  uses_variables: false,
}

/** The init object as api.ts actually sends it (a subset of RequestInit). */
type FetchInit = {
  method?: string
  body?: string
  headers?: Record<string, string>
}

function mockFetch(
  respond: (url: string, init?: FetchInit) => Response | Promise<Response>,
) {
  const fn = vi.fn(async (input: RequestInfo | URL, init?: FetchInit) =>
    respond(String(input), init),
  )
  vi.stubGlobal('fetch', fn)
  return fn
}

/**
 * The most recent call. `init` is asserted present — only use on calls that
 * actually send one (every call in this file that inspects init does).
 */
function lastCall(fn: ReturnType<typeof mockFetch>): {
  url: string
  init: FetchInit
} {
  const [url, init] = fn.mock.calls[0] as [string, FetchInit]
  return { url, init }
}

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('listSnippets', () => {
  it('requests /api/snippets with no query when params are empty', async () => {
    const fn = mockFetch(() => json(200, []))
    await api.listSnippets()
    expect(lastCall(fn).url).toBe('/api/snippets')
  })

  it('encodes q, repeated tag/lang, folder, limit and offset', async () => {
    const fn = mockFetch(() => json(200, []))
    await api.listSnippets({
      q: 'restart caddy',
      tag: ['ops', 'db'],
      lang: ['go'],
      folder: 'f1',
      limit: 10,
      offset: 20,
    })
    const url = new URL(lastCall(fn).url, 'http://x')
    expect(url.searchParams.get('q')).toBe('restart caddy')
    expect(url.searchParams.getAll('tag')).toEqual(['ops', 'db'])
    expect(url.searchParams.getAll('lang')).toEqual(['go'])
    expect(url.searchParams.get('folder')).toBe('f1')
    expect(url.searchParams.get('limit')).toBe('10')
    expect(url.searchParams.get('offset')).toBe('20')
  })

  it('omits undefined params', async () => {
    const fn = mockFetch(() => json(200, []))
    await api.listSnippets({ limit: 5 })
    const url = new URL(lastCall(fn).url, 'http://x')
    expect(url.searchParams.has('q')).toBe(false)
    expect(url.searchParams.has('tag')).toBe(false)
    expect(url.searchParams.get('limit')).toBe('5')
  })

  it('encodes snippet ids in paths', async () => {
    const fn = mockFetch(() => json(200, { id: 'a b' }))
    await api.getSnippet('a b')
    expect(lastCall(fn).url).toBe('/api/snippets/a%20b')
  })
})

describe('write endpoints', () => {
  it('POST /api/snippets sends the JSON content type and body', async () => {
    const fn = mockFetch(() => json(201, { id: '1' }))
    await api.createSnippet(input)
    const { url, init } = lastCall(fn)
    expect(url).toBe('/api/snippets')
    expect(init.method).toBe('POST')
    expect(init.headers?.['Content-Type']).toBe('application/json')
    expect(JSON.parse(init.body ?? '')).toMatchObject({
      title: 't',
      folder_id: null,
    })
  })

  it('PUT /api/snippets/{id} targets the right path and method', async () => {
    const fn = mockFetch(() => json(200, { id: 'abc' }))
    await api.updateSnippet('abc', input)
    const { url, init } = lastCall(fn)
    expect(url).toBe('/api/snippets/abc')
    expect(init.method).toBe('PUT')
  })

  it('DELETE /api/snippets/{id} resolves undefined on 204', async () => {
    const fn = mockFetch(() => new Response(null, { status: 204 }))
    await expect(api.deleteSnippet('abc')).resolves.toBeUndefined()
    // Bodyless deletes must still carry the JSON content type: the server
    // rejects state-changing requests without it (415, spec §3).
    const { url, init } = lastCall(fn)
    expect(url).toBe('/api/snippets/abc')
    expect(init.method).toBe('DELETE')
    expect(init.headers?.['Content-Type']).toBe('application/json')
    expect(init.body).toBeUndefined()
  })

  it('POST /api/folders sends name and parent_id', async () => {
    const fn = mockFetch(() => json(201, { id: 'f' }))
    await api.createFolder('Ops', null)
    const { init } = lastCall(fn)
    expect(JSON.parse(init.body ?? '')).toEqual({ name: 'Ops', parent_id: null })
  })

  it('PUT /api/folders/{id} sends only the patched fields', async () => {
    const fn = mockFetch(() => json(200, { id: 'f' }))
    await api.updateFolder('f', { name: 'New' })
    const { url, init } = lastCall(fn)
    expect(url).toBe('/api/folders/f')
    expect(init.method).toBe('PUT')
    expect(JSON.parse(init.body ?? '')).toEqual({ name: 'New' })
  })

  it('DELETE /api/folders/{id} uses the DELETE method and JSON content type', async () => {
    const fn = mockFetch(() => new Response(null, { status: 204 }))
    await api.deleteFolder('f')
    const { init } = lastCall(fn)
    expect(init.method).toBe('DELETE')
    expect(init.headers?.['Content-Type']).toBe('application/json')
  })
})

describe('sync and raw', () => {
  it('sync omits since when not provided', async () => {
    const fn = mockFetch(() =>
      json(200, { server_time: 't', folders: [], snippets: [] }),
    )
    await api.sync()
    expect(lastCall(fn).url).toBe('/api/sync')
  })

  it('sync passes since as a query param', async () => {
    const fn = mockFetch(() =>
      json(200, { server_time: 't', folders: [], snippets: [] }),
    )
    await api.sync('2026-09-01T00:00:00Z')
    expect(lastCall(fn).url).toBe('/api/sync?since=2026-09-01T00%3A00%3A00Z')
  })

  it('getRaw returns the plain-text body', async () => {
    mockFetch(() => new Response('echo hi', { status: 200 }))
    await expect(api.getRaw('abc')).resolves.toBe('echo hi')
  })

  it('getRaw throws ApiError on failure', async () => {
    mockFetch(() => new Response(null, { status: 404 }))
    await expect(api.getRaw('nope')).rejects.toMatchObject({ status: 404 })
  })
})

describe('errors', () => {
  it('throws ApiError carrying the server message', async () => {
    mockFetch(() => json(409, { error: 'folder not empty' }))
    await expect(api.deleteFolder('f1')).rejects.toMatchObject({
      status: 409,
      message: 'folder not empty',
    })
  })

  it('throws ApiError with status 0 on network failure', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => {
        throw new TypeError('failed to fetch')
      }),
    )
    await expect(api.me()).rejects.toBeInstanceOf(ApiError)
    await expect(api.me()).rejects.toMatchObject({ status: 0 })
  })

  it('falls back to HTTP <status> when the error body is not JSON', async () => {
    mockFetch(() => new Response('oops', { status: 500 }))
    await expect(api.listTags()).rejects.toMatchObject({
      status: 500,
      message: 'HTTP 500',
    })
  })
})

describe('seed', () => {
  it('seedStarter POSTs to /api/seed and returns the counts', async () => {
    const fn = mockFetch(() => json(200, { created: 4, updated: 0 }))
    const out = await api.seedStarter()
    const { url, init } = lastCall(fn)
    expect(url).toBe('/api/seed')
    expect(init.method).toBe('POST')
    expect(out).toEqual({ created: 4, updated: 0 })
  })
})

describe('ai', () => {
  it('aiStatus GETs /api/ai/status and parses the enabled flag', async () => {
    const fn = mockFetch(() => json(200, { enabled: true, model: 'm' }))
    await expect(api.aiStatus()).resolves.toEqual({ enabled: true, model: 'm' })
    const { url, init } = lastCall(fn)
    expect(url).toBe('/api/ai/status')
    expect(init.method).toBe('GET')
  })

  it('generateSnippet POSTs the prompt to /api/ai/generate', async () => {
    const fn = mockFetch(() =>
      json(200, {
        title: 'T',
        language: 'bash',
        body: 'cp {{file}} ~/',
        notes: 'Copies it home.',
        uses_variables: true,
      }),
    )
    const out = await api.generateSnippet({ prompt: 'copy a file home' })
    const { url, init } = lastCall(fn)
    expect(url).toBe('/api/ai/generate')
    expect(init.method).toBe('POST')
    expect(JSON.parse(init.body ?? '')).toEqual({ prompt: 'copy a file home' })
    expect(out.uses_variables).toBe(true)
    expect(out.notes).toBe('Copies it home.')
  })

  it('generateSnippet sends the requested kind', async () => {
    const fn = mockFetch(() =>
      json(200, {
        title: 'T',
        language: 'bash',
        body: '#!/usr/bin/env bash\necho hi',
        uses_variables: false,
      }),
    )
    await api.generateSnippet({ prompt: 'a greeting script', kind: 'script' })
    const { init } = lastCall(fn)
    expect(JSON.parse(init.body ?? '')).toEqual({
      prompt: 'a greeting script',
      kind: 'script',
    })
  })
})
describe('ai tags', () => {
  it('suggestTags POSTs body/title/language to /api/ai/tags', async () => {
    const fn = mockFetch(() => json(200, { tags: ['python', 'network'] }))
    const out = await api.suggestTags({ body: 'python -m http.server', language: 'python', is_sensitive: false })
    const { url, init } = lastCall(fn)
    expect(url).toBe('/api/ai/tags')
    expect(init.method).toBe('POST')
    expect(JSON.parse(init.body ?? '')).toEqual({ body: 'python -m http.server', language: 'python', is_sensitive: false })
    expect(out.tags).toEqual(['python', 'network'])
  })
})

describe('ai explain', () => {
  it('explainSnippet POSTs the body to /api/ai/explain', async () => {
    const fn = mockFetch(() => json(200, { notes: 'It copies files.' }))
    const out = await api.explainSnippet({ body: 'cp -rf src dst', is_sensitive: false })
    const { url, init } = lastCall(fn)
    expect(url).toBe('/api/ai/explain')
    expect(init.method).toBe('POST')
    expect(JSON.parse(init.body ?? '')).toEqual({ body: 'cp -rf src dst', is_sensitive: false })
    expect(out.notes).toBe('It copies files.')
  })
})
