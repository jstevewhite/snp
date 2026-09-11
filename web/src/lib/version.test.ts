import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import * as api from './api'
import { VERSION_STORAGE_KEY, loadCachedVersion, resolveVersion } from './version'

vi.mock('./api', () => ({ version: vi.fn() }))

describe('version', () => {
  beforeEach(() => {
    localStorage.removeItem(VERSION_STORAGE_KEY)
    vi.mocked(api.version).mockReset()
  })

  afterEach(() => {
    localStorage.clear()
  })

  it('returns and caches the fetched version', async () => {
    vi.mocked(api.version).mockResolvedValue({ version: 'v1.2.3' })
    await expect(resolveVersion()).resolves.toBe('v1.2.3')
    expect(loadCachedVersion()).toBe('v1.2.3')
  })

  it('falls back to the cached version when the request fails', async () => {
    localStorage.setItem(VERSION_STORAGE_KEY, 'v1.0.0')
    vi.mocked(api.version).mockRejectedValue(new Error('offline'))
    await expect(resolveVersion()).resolves.toBe('v1.0.0')
  })

  it('returns null when there is no cache and the request fails', async () => {
    vi.mocked(api.version).mockRejectedValue(new Error('offline'))
    await expect(resolveVersion()).resolves.toBeNull()
  })

  it('ignores an empty version and keeps the cached one', async () => {
    localStorage.setItem(VERSION_STORAGE_KEY, 'v1.0.0')
    vi.mocked(api.version).mockResolvedValue({ version: '  ' })
    await expect(resolveVersion()).resolves.toBe('v1.0.0')
  })
})
