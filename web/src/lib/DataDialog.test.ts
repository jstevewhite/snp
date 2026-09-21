import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'
import DataDialog from './DataDialog.svelte'
import * as api from './api'
import * as data from './data'
import * as desktop from './desktop'

const source = { version: 1, snippets: [{ title: 'Imported', body: 'echo hello' }], folders: [] }
async function choose(value: unknown = source): Promise<void> {
  const text = JSON.stringify(value)
  const file = new File([text], 'snippets.json', { type: 'application/json' })
  Object.defineProperty(file, 'text', { value: async () => text })
  await fireEvent.change(screen.getByLabelText('Import JSON file'), { target: { files: [file] } })
}
function setup() {
  const onimport = vi.fn(async () => ({ created: 1, updated: 0, trashed: 0 }))
  return { ...render(DataDialog, { online: true, onimport, onclose: vi.fn() }), onimport }
}
afterEach(() => { cleanup(); vi.restoreAllMocks() })

describe('Data management dialog', () => {
  it('previews merge before applying and discards file contents after success', async () => {
    const preview = vi.spyOn(api, 'importSnippets').mockResolvedValue({ created: 1, updated: 0, trashed: 0 })
    const { onimport } = setup()
    await choose()
    expect(screen.queryByRole('button', { name: 'Import now' })).toBeNull()
    await fireEvent.click(screen.getByRole('button', { name: 'Preview import' }))
    await screen.findByText('1 new · 0 existing · 0 to Trash')
    expect(preview).toHaveBeenCalledWith(source, 'merge', true)
    expect(onimport).not.toHaveBeenCalled()
    await fireEvent.click(screen.getByRole('button', { name: 'Import now' }))
    await screen.findByText(/Import complete/)
    expect(onimport).toHaveBeenCalledWith(source, 'merge')
    expect(screen.queryByText('snippets.json')).toBeNull()
  })
  it('invalidates preview when mode changes and requires acknowledgement for replace', async () => {
    vi.spyOn(api, 'importSnippets').mockResolvedValue({ created: 1, updated: 0, trashed: 3 })
    const { onimport } = setup()
    await choose()
    await fireEvent.click(screen.getByRole('button', { name: 'Preview import' }))
    await screen.findByRole('button', { name: 'Import now' })
    await fireEvent.change(screen.getByLabelText('Import mode'), { target: { value: 'replace' } })
    expect(screen.queryByRole('button', { name: 'Import now' })).toBeNull()
    await fireEvent.click(screen.getByRole('button', { name: 'Preview import' }))
    const apply = await screen.findByRole('button', { name: 'Import now' }) as HTMLButtonElement
    expect(apply.disabled).toBe(true)
    await fireEvent.click(screen.getByRole('checkbox', { name: /Replace my library with/ }))
    expect(apply.disabled).toBe(false)
    await fireEvent.click(apply)
    await waitFor(() => expect(onimport).toHaveBeenCalledWith(source, 'replace'))
  })
  it('rejects a bad replacement file without retaining the previous preview', async () => {
    vi.spyOn(api, 'importSnippets').mockResolvedValue({ created: 1, updated: 0, trashed: 0 })
    setup(); await choose()
    await fireEvent.click(screen.getByRole('button', { name: 'Preview import' }))
    await screen.findByRole('button', { name: 'Import now' })
    await choose({ version: 1 })
    await screen.findByRole('alert')
    expect(screen.queryByRole('button', { name: 'Import now' })).toBeNull()
    expect(screen.queryByText('snippets.json')).toBeNull()
  })
  it('handles native file-picker cancellation and download errors', async () => {
    vi.spyOn(desktop, 'isDesktop').mockReturnValue(true)
    vi.spyOn(data, 'chooseDesktopImport').mockResolvedValue(null)
    vi.spyOn(data, 'downloadData').mockRejectedValue(new Error('Disk full'))
    setup()
    await fireEvent.click(screen.getByRole('button', { name: 'Choose JSON file' }))
    await waitFor(() => expect((screen.getByRole('button', { name: 'Download backup' }) as HTMLButtonElement).disabled).toBe(false))
    expect(screen.queryByRole('alert')).toBeNull()
    await fireEvent.click(screen.getByRole('checkbox', { name: /Encrypt exports/ }))
    await fireEvent.click(screen.getByRole('button', { name: 'Download backup' }))
    expect((await screen.findByRole('alert')).textContent).toBe('Disk full')
  })
  it('disables apply and downloads while offline', async () => {
    vi.spyOn(api, 'importSnippets').mockResolvedValue({ created: 1, updated: 0, trashed: 0 })
    const view = setup(); await choose()
    await fireEvent.click(screen.getByRole('button', { name: 'Preview import' }))
    await screen.findByRole('button', { name: 'Import now' })
    await view.rerender({ online: false })
    for (const name of ['Import now', 'Download backup', 'Download JSON']) expect((screen.getByRole('button', { name }) as HTMLButtonElement).disabled).toBe(true)
  })
  it('reports committed imports with refresh failure as success and prevents accidental retry', async () => {
    vi.spyOn(api, 'importSnippets').mockResolvedValue({ created: 1, updated: 0, trashed: 0 })
    render(DataDialog, { online: true, onclose: vi.fn(), onimport: async () => ({ created: 1, updated: 0, trashed: 0, refreshWarning: 'Use Resync after reconnecting.' }) })
    await choose()
    await fireEvent.click(screen.getByRole('button', { name: 'Preview import' }))
    await fireEvent.click(await screen.findByRole('button', { name: 'Import now' }))
    await screen.findByText(/Import complete.*Use Resync/)
    expect(screen.queryByRole('button', { name: 'Import now' })).toBeNull()
  })
})


describe('password-protected files', () => {
  it('defaults encryption on and requires matching passwords before either download', async () => {
    const download = vi.spyOn(data, 'downloadData').mockResolvedValue('downloaded')
    setup()
    expect((screen.getByRole('checkbox', { name: /Encrypt exports/ }) as HTMLInputElement).checked).toBe(true)
    expect(screen.getByText(/If you forget this password/)).toBeTruthy()
    await fireEvent.click(screen.getByRole('button', { name: 'Download JSON' }))
    expect(download).not.toHaveBeenCalled()
    await fireEvent.input(screen.getByLabelText('Download password'), { target: { value: 'my long download password' } })
    await fireEvent.input(screen.getByLabelText('Confirm password'), { target: { value: 'does not match' } })
    await fireEvent.click(screen.getByRole('button', { name: 'Download backup' }))
    expect(screen.getByRole('alert').textContent).toBe('Passwords do not match.')
    expect(download).not.toHaveBeenCalled()
    await fireEvent.input(screen.getByLabelText('Confirm password'), { target: { value: 'my long download password' } })
    await fireEvent.click(screen.getByRole('button', { name: 'Download backup' }))
    await waitFor(() => expect(download).toHaveBeenCalledWith('backup', 'my long download password'))
    expect((screen.getByLabelText('Download password') as HTMLInputElement).value).toBe('')
    expect((screen.getByLabelText('Confirm password') as HTMLInputElement).value).toBe('')
  })
  it('requires explicitly disabling protection for a plaintext export', async () => {
    const download = vi.spyOn(data, 'downloadData').mockResolvedValue('saved')
    setup()
    await fireEvent.click(screen.getByRole('checkbox', { name: /Encrypt exports/ }))
    expect(screen.getByText(/These downloads will not be password-protected/)).toBeTruthy()
    await fireEvent.click(screen.getByRole('button', { name: 'Download JSON' }))
    await waitFor(() => expect(download).toHaveBeenCalledWith('export', undefined))
  })
  it('unlocks an encrypted export before preview and clears passwords even on failure', async () => {
    const decrypt = vi.spyOn(api, 'decryptImport').mockRejectedValueOnce(new Error('Wrong password')).mockResolvedValueOnce(source as api.ImportDocument)
    const preview = vi.spyOn(api, 'importSnippets').mockResolvedValue({ created: 1, updated: 0, trashed: 0 })
    const { onimport } = setup()
    const text = '-----BEGIN AGE ENCRYPTED FILE-----\nfake test ciphertext'
    const file = new File([text], 'snippets.json.age')
    Object.defineProperty(file, 'text', { value: async () => text })
    await fireEvent.change(screen.getByLabelText('Import JSON file'), { target: { files: [file] } })
    await screen.findByLabelText('File password')
    expect(screen.queryByRole('button', { name: 'Preview import' })).toBeNull()
    await fireEvent.input(screen.getByLabelText('File password'), { target: { value: 'wrong' } })
    await fireEvent.click(screen.getByRole('button', { name: 'Unlock file' }))
    await screen.findByText('Wrong password')
    expect((screen.getByLabelText('File password') as HTMLInputElement).value).toBe('')
    await fireEvent.input(screen.getByLabelText('File password'), { target: { value: 'correct password' } })
    await fireEvent.click(screen.getByRole('button', { name: 'Unlock file' }))
    await screen.findByRole('button', { name: 'Preview import' })
    expect(decrypt).toHaveBeenLastCalledWith(text, 'correct password')
    expect(screen.queryByLabelText('File password')).toBeNull()
    expect(onimport).not.toHaveBeenCalled()
    expect(preview).not.toHaveBeenCalled()
    await fireEvent.click(screen.getByRole('button', { name: 'Preview import' }))
    await waitFor(() => expect(preview).toHaveBeenCalledWith(source, 'merge', true))
  })
})
