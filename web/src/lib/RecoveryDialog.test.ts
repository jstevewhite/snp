import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'
import RecoveryDialog from './RecoveryDialog.svelte'
import * as api from './api'
import type { Revision, Snippet, TrashEntry } from './types'

const current: Snippet = { id: 's1', title: 'Current title', body: 'current body', language: 'bash', notes: 'current notes', folder_id: null, tags: [], is_sensitive: false, uses_variables: false, created_at: '2026-09-01T00:00:00Z', updated_at: '2026-09-02T00:00:00Z' }
const previous: Snippet = { ...current, title: 'Old title', body: 'previous body', notes: 'old notes', folder_id: 'deleted', uses_variables: true, var_defaults: { host: 'example' } }
const revision: Revision = { id: 1, saved_at: current.updated_at, version_at: current.created_at, protected: false }
function setup(snippet: Snippet | null = current) {
  const onrestore = vi.fn(async () => {})
  const onclose = vi.fn()
  const view = render(RecoveryDialog, { snippet, folders: [], online: true, saving: false, onrestore, onclose })
  return { ...view, onrestore, onclose }
}
afterEach(() => { cleanup(); vi.restoreAllMocks() })

describe('Recovery', () => {
  it('compares full version content and restores the selected revision', async () => {
    vi.spyOn(api, 'listRevisions').mockResolvedValue([revision])
    vi.spyOn(api, 'getRevision').mockResolvedValue({ ...revision, snippet: previous })
    const { onrestore } = setup()
    await fireEvent.click(await screen.findByRole('button', { name: /Previous version/ }))
    const old = within(await screen.findByRole('article', { name: 'Selected version' }))
    expect(old.getByText('previous body')).toBeDefined()
    expect(old.getByText('old notes')).toBeDefined()
    expect(old.getByText('Deleted folder → Unfiled on restore')).toBeDefined()
    expect(old.getByText(/"host": "example"/)).toBeDefined()
    expect(within(screen.getByRole('article', { name: 'Current version' })).getByText('current body')).toBeDefined()
    await fireEvent.click(screen.getByRole('button', { name: 'Restore this version' }))
    expect(onrestore).toHaveBeenCalledWith('s1', 1)
  })

  it('requires reveal for protected comparisons and forgets them offline', async () => {
    vi.spyOn(api, 'listRevisions').mockResolvedValue([{ ...revision, protected: true }])
    const preview = vi.spyOn(api, 'getRevision').mockResolvedValue({ ...revision, protected: true, snippet: { ...previous, body: 'private historical body' } })
    const reveal = vi.spyOn(api, 'getSnippet').mockResolvedValue({ ...current, body: 'private current body', is_sensitive: true })
    const view = setup({ ...current, body: null, is_sensitive: true })
    await fireEvent.click(await screen.findByRole('button', { name: /Previous version/ }))
    expect(preview).not.toHaveBeenCalled()
    expect(reveal).not.toHaveBeenCalled()
    await fireEvent.click(screen.getByRole('button', { name: 'Reveal comparison' }))
    await screen.findByText('private historical body')
    expect(preview).toHaveBeenCalledWith('s1', 1, true)
    expect(screen.getByText('This version will restore as sensitive.')).toBeDefined()
    await view.rerender({ online: false })
    await waitFor(() => expect(screen.queryByText('private historical body')).toBeNull())
    expect(screen.queryByText('private current body')).toBeNull()
    expect((screen.getByRole('button', { name: 'Reveal comparison' }) as HTMLButtonElement).disabled).toBe(true)
  })

  it('ignores a stale preview after selecting another version', async () => {
    vi.spyOn(api, 'listRevisions').mockResolvedValue([revision, { ...revision, id: 2 }])
    let resolve!: (value: apiReturn) => void
    type apiReturn = Awaited<ReturnType<typeof api.getRevision>>
    vi.spyOn(api, 'getRevision').mockImplementation((_id, rid) => rid === 1 ? new Promise((done) => { resolve = done }) : Promise.resolve({ ...revision, id: 2, snippet: { ...previous, body: 'selected second version' } }))
    setup()
    await fireEvent.click(await screen.findByRole('button', { name: /Previous version/ }))
    await fireEvent.click(screen.getByRole('button', { name: /Earlier version 2/ }))
    await screen.findByText('selected second version')
    resolve({ ...revision, snippet: { ...previous, body: 'stale secret' } })
    await Promise.resolve()
    expect(screen.queryByText('stale secret')).toBeNull()
    expect(screen.getByText('selected second version')).toBeDefined()
  })

  it('discards a pending sensitive reveal on disconnect', async () => {
    vi.spyOn(api, 'listRevisions').mockResolvedValue([{ ...revision, protected: true }])
    let resolve!: (value: Awaited<ReturnType<typeof api.getRevision>>) => void
    vi.spyOn(api, 'getRevision').mockReturnValue(new Promise((done) => { resolve = done }))
    const view = setup()
    await fireEvent.click(await screen.findByRole('button', { name: /Previous version/ }))
    await fireEvent.click(screen.getByRole('button', { name: 'Reveal comparison' }))
    await view.rerender({ online: false })
    resolve({ ...revision, protected: true, snippet: { ...previous, body: 'late secret' } })
    await Promise.resolve()
    expect(screen.queryByText('late secret')).toBeNull()
  })

  it('shows restore failures without losing the preview', async () => {
    vi.spyOn(api, 'listRevisions').mockResolvedValue([revision])
    vi.spyOn(api, 'getRevision').mockResolvedValue({ ...revision, snippet: previous })
    const { onrestore } = setup()
    onrestore.mockRejectedValue(new Error('Restore failed'))
    await fireEvent.click(await screen.findByRole('button', { name: /Previous version/ }))
    await fireEvent.click(await screen.findByRole('button', { name: 'Restore this version' }))
    expect((await screen.findByRole('alert')).textContent).toContain('Restore failed')
    expect(screen.getByText('previous body')).toBeDefined()
  })

  it('pages trash and restores by id', async () => {
    const entries: TrashEntry[] = Array.from({ length: 100 }, (_, i) => ({ id: `s${i}`, title: `Deleted ${i}`, language: 'bash', is_sensitive: false, deleted_at: current.updated_at }))
    const list = vi.spyOn(api, 'listTrash').mockResolvedValueOnce(entries).mockResolvedValueOnce([{ ...entries[0], id: 'last', title: 'Last deleted' }])
    const { onrestore } = setup(null)
    await fireEvent.click(await screen.findByRole('button', { name: 'Load more' }))
    expect(list).toHaveBeenLastCalledWith(100)
    await fireEvent.click(await screen.findByRole('button', { name: 'Restore Last deleted' }))
    expect(onrestore).toHaveBeenCalledWith('last', undefined)
    expect(screen.queryByRole('button', { name: 'Load more' })).toBeNull()
  })
})
