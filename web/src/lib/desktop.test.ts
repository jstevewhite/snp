import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import * as api from './api'
import { ApiError } from './types'
import { desktopApp, isDesktop, resolveDesktopApp, type DesktopApp } from './desktop'

interface DesktopWindow {
  __SNP_DESKTOP__?: boolean
  go?: { desktop?: { App?: DesktopApp } }
}

function setDesktop(bridge: DesktopApp | undefined, marker = true): void {
  const w = window as unknown as DesktopWindow
  w.__SNP_DESKTOP__ = marker
  w.go = bridge ? { desktop: { App: bridge } } : undefined
}

function callRecorder() {
  const calls: { method: string; path: string; body: string }[] = []
  const bridge: DesktopApp = {
    CallAPI: vi.fn(async (method: string, path: string, body: string) => {
      calls.push({ method, path, body })
      const notFound = path.includes('/nope')
      if (notFound) return { status: 404, contentType: 'text/plain', body: '404 page not found' }
      return { status: 200, contentType: 'application/json', body: '{"ok":true}' }
    }),
  }
  return { bridge, calls }
}

describe('desktop bridge detection', () => {
  afterEach(() => {
    setDesktop(undefined, false)
  })

  it('is inert in a plain browser (no marker, no window.go)', () => {
    setDesktop(undefined, false)
    expect(isDesktop()).toBe(false)
    expect(desktopApp()).toBeUndefined()
  })

  it('detects the desktop shell from the injected marker', () => {
    setDesktop(undefined, true)
    expect(isDesktop()).toBe(true)
  })

  it('resolveDesktopApp resolves immediately when the bridge is bound', async () => {
    const { bridge } = callRecorder()
    setDesktop(bridge)
    await expect(resolveDesktopApp()).resolves.toBe(bridge)
  })

  it('resolveDesktopApp returns undefined in a browser without polling', async () => {
    setDesktop(undefined, false)
    await expect(resolveDesktopApp(50)).resolves.toBeUndefined()
  })
})

describe('api over the desktop bridge', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn(async () => {
      throw new Error('fetch must not be used in desktop mode')
    }))
  })

  afterEach(() => {
    setDesktop(undefined, false)
    vi.unstubAllGlobals()
  })

  it('routes a GET through CallAPI with the full /api path', async () => {
    const { bridge, calls } = callRecorder()
    setDesktop(bridge)
    await expect(api.me()).resolves.toEqual({ ok: true })
    expect(calls).toEqual([{ method: 'GET', path: '/api/me', body: '' }])
    expect(fetch).not.toHaveBeenCalled()
  })

  it('sends a JSON body on POST and none on body-less DELETE', async () => {
    const { bridge, calls } = callRecorder()
    setDesktop(bridge)
    await api.createSnippet({ title: 't', body: 'b' } as never)
    await api.deleteSnippet('abc')
    expect(calls[0]).toMatchObject({
      method: 'POST',
      path: '/api/snippets',
      body: JSON.stringify({ title: 't', body: 'b' }),
    })
    expect(calls[1]).toEqual({ method: 'DELETE', path: '/api/snippets/abc', body: '' })
  })

  it('carries the query string into the bridge path', async () => {
    const { bridge, calls } = callRecorder()
    setDesktop(bridge)
    await api.listSnippets({ q: 'restart caddy', tag: ['ops'] })
    // URLSearchParams form-encodes spaces as '+'; the server decodes it
    // the same way it would a browser fetch.
    expect(calls[0].path).toBe('/api/snippets?q=restart+caddy&tag=ops')
  })

  it('maps a 204 to undefined', async () => {
    const bridge: DesktopApp = {
      CallAPI: vi.fn(async () => ({
        status: 204,
        contentType: '',
        body: '',
      })),
    }
    setDesktop(bridge)
    await expect(api.deleteFolder('f')).resolves.toBeUndefined()
  })

  it('maps an error status to ApiError with the server message', async () => {
    const bridge: DesktopApp = {
      CallAPI: vi.fn(async () => ({
        status: 409,
        contentType: 'application/json',
        body: '{"error":"folder not empty"}',
      })),
    }
    setDesktop(bridge)
    await expect(api.deleteFolder('f1')).rejects.toMatchObject({
      status: 409,
      message: 'folder not empty',
    })
  })

  it('turns a bridge-level rejection into a network-style ApiError', async () => {
    const bridge: DesktopApp = {
      CallAPI: vi.fn(async () => {
        throw new Error('bridge boom')
      }),
    }
    setDesktop(bridge)
    await expect(api.listTags()).rejects.toMatchObject({ status: 0 })
  })

  it('getRaw returns the plain-text body', async () => {
    const bridge: DesktopApp = {
      CallAPI: vi.fn(async () => ({
        status: 200,
        contentType: 'text/plain; charset=utf-8',
        body: 'echo hi',
      })),
    }
    setDesktop(bridge)
    await expect(api.getRaw('abc')).resolves.toBe('echo hi')
  })

  it('getRaw surfaces non-2xx statuses', async () => {
    const { bridge } = callRecorder()
    setDesktop(bridge)
    await expect(api.getRaw('nope')).rejects.toBeInstanceOf(ApiError)
  })
})
