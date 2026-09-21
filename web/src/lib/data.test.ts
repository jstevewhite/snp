import { afterEach, describe, expect, it, vi } from 'vitest'
import { downloadData, MAX_IMPORT_BYTES, parseImport } from './data'
import * as desktop from './desktop'

afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); vi.useRealTimers() })

describe('data files', () => {
  it('preserves snp fields, Unicode and a UTF-8 BOM', () => {
    const doc = { version: 1, folders: [], snippets: [{ id: 's1', title: '日本語', body: 'echo {{host}}', var_defaults: { host: 'host' }, pinned: true, is_sensitive: true }] }
    expect(parseImport('\uFEFF' + JSON.stringify(doc))).toEqual(doc)
  })
  it('rejects invalid, oversized and redacted documents', () => {
    expect(() => parseImport('not JSON')).toThrow('not valid JSON')
    expect(() => parseImport('{"version":1}')).toThrow('snippets array')
    expect(() => parseImport('{"version":2,"snippets":[]}')).toThrow('version-1')
    expect(() => parseImport('{"version":1,"snippets":[{"title":"hidden","body":null}]}')).toThrow('text body')
    expect(() => parseImport('x'.repeat(MAX_IMPORT_BYTES + 1))).toThrow('10 MiB')
  })
  it('downloads binary backups with the JSON guard header and releases the Blob URL', async () => {
    vi.useFakeTimers()
    const revoke = vi.fn()
    const create = vi.fn(() => 'blob:backup')
    vi.stubGlobal('URL', class extends URL { static createObjectURL = create; static revokeObjectURL = revoke })
    vi.stubGlobal('fetch', vi.fn(async () => new Response(new Uint8Array([80, 75, 3, 4]), { headers: { 'Content-Type': 'application/zip' } })))
    let filename = ''
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) { filename = this.download })
    await expect(downloadData('backup')).resolves.toBe('downloaded')
    expect(fetch).toHaveBeenCalledWith('/api/backup', { method: 'POST', headers: { 'Content-Type': 'application/json' }, cache: 'no-store' })
    expect(filename).toMatch(/^snp-backup-.*\.zip$/)
    expect(create).toHaveBeenCalledOnce()
    expect(document.querySelector('a[download]')).toBeNull()
    await vi.runAllTimersAsync()
    expect(revoke).toHaveBeenCalledWith('blob:backup')
  })
  it('uses native save for desktop downloads and handles cancellation', async () => {
    vi.spyOn(desktop, 'isDesktop').mockReturnValue(true)
    const save = vi.fn().mockResolvedValueOnce(false).mockResolvedValueOnce(true)
    vi.spyOn(desktop, 'resolveDesktopApp').mockResolvedValue({ CallAPI: vi.fn(), SaveData: save })
    vi.stubGlobal('fetch', vi.fn())
    await expect(downloadData('backup')).resolves.toBe('cancelled')
    await expect(downloadData('export')).resolves.toBe('saved')
    expect(fetch).not.toHaveBeenCalled()
    expect(save).toHaveBeenNthCalledWith(1, 'backup')
  })
  it('surfaces a download error without saving the error as a file', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response('{"error":"Backup unavailable"}', { status: 500 })))
    await expect(downloadData('backup')).rejects.toThrow('Backup unavailable')
  })
})


it('posts passwords in the body and gives protected downloads an age extension', async () => {
  vi.useFakeTimers()
  vi.stubGlobal('URL', class extends URL { static createObjectURL = vi.fn(() => 'blob:encrypted'); static revokeObjectURL = vi.fn() })
  vi.stubGlobal('fetch', vi.fn(async () => new Response('encrypted ciphertext')))
  let filename = ''
  vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) { filename = this.download })
  await downloadData('export', 'my protected file password')
  expect(fetch).toHaveBeenCalledWith('/api/export', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ encrypt: true, password: 'my protected file password' }), cache: 'no-store' })
  expect(filename).toMatch(/\.json\.age$/)
  await vi.runAllTimersAsync()
})
it('never falls back to plaintext when the desktop encryption bridge is missing', async () => {
  vi.spyOn(desktop, 'isDesktop').mockReturnValue(true)
  const save = vi.fn()
  vi.spyOn(desktop, 'resolveDesktopApp').mockResolvedValue({ CallAPI: vi.fn(), SaveData: save })
  await expect(downloadData('backup', 'my file password')).rejects.toThrow('Encrypted file saving is unavailable')
  expect(save).not.toHaveBeenCalled()
})
