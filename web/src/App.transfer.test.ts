import 'fake-indexeddb/auto'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/svelte'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import App from './App.svelte'
import { allSnippets, openLocalDB } from './lib/db'
import type { Snippet, SnippetInput } from './lib/types'

const time = '2026-09-17T00:00:00Z'
function fixture(sensitive = true): Snippet {
  return { id: 's1', title: 'Deploy', body: 'echo {{target}}', language: '',
    notes: '', folder_id: null, tags: ['ops'], is_sensitive: sensitive,
    uses_variables: true, var_defaults: { target: 'production' },
    created_at: time, updated_at: time }
}
function setup(sensitive = true) {
  let saved = fixture(sensitive)
  const writes: SnippetInput[] = []
  const redacted = () => saved.is_sensitive ? { ...saved, body: null, var_defaults: null } : saved
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input)
    let result: unknown = { enabled: false }
    if (url.includes('/api/sync')) result = { server_time: time,
      folders: [{ id: 'f1', name: 'Ops', parent_id: null, created_at: time, updated_at: time }],
      snippets: [redacted()] }
    else if (url.endsWith('/api/snippets/s1')) {
      if (init?.method === 'PUT') {
        const body = JSON.parse(String(init.body)) as SnippetInput
        writes.push(body)
        saved = { ...saved, ...body }
        result = redacted()
      } else result = saved
    }
    return new Response(JSON.stringify(result), { status: 200 })
  })
  vi.stubGlobal('fetch', fetchMock)
  return { writes, fetchMock, update: (patch: Partial<Snippet>) => { saved = { ...saved, ...patch } } }
}
async function select() {
  await waitFor(() => expect(screen.getByText('Deploy')).toBeDefined())
  await fireEvent.click(screen.getByText('Deploy'))
}
async function edit() {
  await fireEvent.click(screen.getByText('Edit'))
  await waitFor(() => expect(screen.getByLabelText('Body')).toBeDefined())
}
beforeEach(async () => { await indexedDB.deleteDatabase('snp'); localStorage.clear(); localStorage.setItem('snp.layout', 'wide') })
afterEach(() => { cleanup(); vi.unstubAllGlobals() })

describe('sensitive detail lifecycle', () => {
  it('reveals defaults, saves the new body, and never restores an old body via defaults', async () => {
    const { writes } = setup()
    const copy = vi.fn(async () => {})
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: copy } })
    render(App); await select()
    await fireEvent.click(screen.getByText('Show body'))
    await waitFor(() => expect((screen.getByLabelText('target') as HTMLInputElement).value).toBe('production'))
    await edit()
    await fireEvent.input(screen.getByLabelText('Body'), { target: { value: 'deploy {{target}}' } })
    await fireEvent.click(screen.getByText('Save'))
    await waitFor(() => expect(screen.queryByLabelText('Body')).toBeNull())
    expect(document.querySelector('pre.body')?.textContent).toBe('deploy {{target}}')
    await fireEvent.click(screen.getByText('Copy rendered'))
    expect(copy).toHaveBeenCalledWith('deploy production')
    await fireEvent.input(screen.getByLabelText('target'), { target: { value: 'staging' } })
    await fireEvent.click(screen.getByText('Save defaults'))
    await waitFor(() => expect(writes).toHaveLength(2))
    expect(writes[1]).toMatchObject({ body: 'deploy {{target}}', var_defaults: { target: 'staging' } })
    const db = await openLocalDB()
    expect(await allSnippets(db)).toEqual([expect.objectContaining({ body: null, var_defaults: null })])
    db.close()
  })

  it('invalidates revealed content on sync without changing an open editor', async () => {
    setup(); render(App); await select(); await edit()
    await fireEvent.input(screen.getByLabelText('Body'), { target: { value: 'draft' } })
    await fireEvent.click(screen.getByRole('button', { name: 'Resync' }))
    await waitFor(() => expect((screen.getByRole('button', { name: 'Resync' }) as HTMLButtonElement).disabled).toBe(false))
    expect((screen.getByLabelText('Body') as HTMLTextAreaElement).value).toBe('draft')
  })

  it('reveals again after leaving the view and after sync', async () => {
    setup(); render(App); await select()
    await fireEvent.click(screen.getByText('Show body'))
    await waitFor(() => expect(screen.getByLabelText('target')).toBeDefined())
    await fireEvent.click(screen.getByRole('button', { name: 'Resync' }))
    await waitFor(() => expect(screen.getByText('Show body')).toBeDefined())
    await fireEvent.click(screen.getByText('Show body'))
    await waitFor(() => expect(screen.getByLabelText('target')).toBeDefined())
    await fireEvent.click(document.querySelector('.snippet-list .new')!)
    await fireEvent.click(screen.getByText('Cancel'))
    expect(screen.getByText('Show body')).toBeDefined()
  })
})

describe('draft protection and dialogs', () => {
  it('keeps edits until discard is confirmed and only warns on unload while dirty', async () => {
    setup(false); render(App); await select(); await edit()
    const clean = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(clean)
    expect(clean.defaultPrevented).toBe(false)
    await fireEvent.input(screen.getByLabelText('Body'), { target: { value: 'UNSAVED' } })
    const dirtyEvent = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(dirtyEvent)
    expect(dirtyEvent.defaultPrevented).toBe(true)
    await fireEvent.click(document.querySelector('.snippet-list .item')!)
    expect(screen.getByRole('dialog', { name: 'Unsaved changes' })).toBeDefined()
    expect(document.activeElement).toBe(screen.getByText('Keep editing'))
    await fireEvent.click(screen.getByText('Keep editing'))
    expect((screen.getByLabelText('Body') as HTMLTextAreaElement).value).toBe('UNSAVED')
    await fireEvent.click(screen.getByText('Cancel'))
    await fireEvent.click(screen.getByText('Discard changes'))
    expect(screen.queryByLabelText('Body')).toBeNull()
    const discarded = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(discarded)
    expect(discarded.defaultPrevented).toBe(false)
  })

  it('guards New and native-window close with the same draft dialog', async () => {
    setup(false)
    const confirmClose = vi.fn(async () => {})
    const win = window as unknown as { go?: unknown }
    win.go = { main: { CloseGuard: { ConfirmClose: confirmClose } } }
    try {
      render(App); await select(); await edit()
      await fireEvent.input(screen.getByLabelText('Title'), { target: { value: 'Draft' } })
      await fireEvent.click(document.querySelector('.snippet-list .new')!)
      await fireEvent.click(screen.getByText('Keep editing'))
      await fireEvent(window, new Event('snp:close-request'))
      expect(confirmClose).not.toHaveBeenCalled()
      await fireEvent.click(screen.getByText('Discard changes'))
      expect(confirmClose).toHaveBeenCalledOnce()
    } finally { delete win.go }
  })

  it('contains dialog focus, makes the background inert, and restores the opener', async () => {
    setup(); render(App); await select()
    const opener = document.querySelector('.folders .pane-head button') as HTMLButtonElement
    opener.focus()
    await fireEvent.click(opener)
    const input = screen.getByLabelText('Folder name')
    expect(document.activeElement).toBe(input)
    expect(document.querySelector('main')?.inert).toBe(true)
    await fireEvent.keyDown(input, { key: 'Tab', shiftKey: true })
    expect(document.activeElement).toBe(screen.getByText('Cancel'))
    await fireEvent.keyDown(document.activeElement!, { key: 'Tab' })
    expect(document.activeElement).toBe(input)
    await fireEvent.keyDown(input, { key: 'Escape' })
    await waitFor(() => expect(document.activeElement).toBe(opener))
    expect(screen.queryByRole('dialog')).toBeNull()
  })
})

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((r) => { resolve = r })
  return { promise, resolve }
}

describe('save lifecycle', () => {
  it('serializes create, blocks navigation during save, and preserves fields after failure', async () => {
    const { fetchMock } = setup(false)
    const original = fetchMock.getMockImplementation()!
    let response = deferred<Response>()
    let posts = 0
    fetchMock.mockImplementation((input, init) => {
      if (String(input).endsWith('/api/snippets') && init?.method === 'POST') {
        posts++
        return response.promise
      }
      return original(input, init)
    })
    render(App); await select()
    await fireEvent.click(document.querySelector('.snippet-list .new')!)
    await fireEvent.input(screen.getByLabelText('Title'), { target: { value: 'Draft' } })
    const form = document.querySelector('form')!
    await fireEvent.submit(form)
    await fireEvent.submit(form)
    await waitFor(() => expect(posts).toBe(1))
    await fireEvent.click(document.querySelector('.snippet-list .item')!)
    expect((screen.getByLabelText('Title') as HTMLInputElement).value).toBe('Draft')
    expect((screen.getByText('Saving…') as HTMLButtonElement).disabled).toBe(true)
    response.resolve(new Response(JSON.stringify({ error: 'Try again' }), { status: 500 }))
    await waitFor(() => expect(screen.getByRole('alert').textContent).toBe('Try again'))
    expect((screen.getByLabelText('Title') as HTMLInputElement).value).toBe('Draft')
    response = deferred<Response>()
    await waitFor(() => expect(screen.getByText('Create')).toBeDefined())
    await fireEvent.click(screen.getByText('Create'))
    await waitFor(() => expect(posts).toBe(2))
    response.resolve(new Response(JSON.stringify({ ...fixture(false), title: 'Draft' })))
    await waitFor(() => expect(screen.queryByLabelText('Title')).toBeNull())
  })

  it('disables an open form offline and allows saving after reconnect', async () => {
    setup(false); render(App); await select(); await edit()
    await fireEvent.input(screen.getByLabelText('Body'), { target: { value: 'draft' } })
    await fireEvent(window, new Event('offline'))
    expect((screen.getByText('Save') as HTMLButtonElement).disabled).toBe(true)
    await fireEvent(window, new Event('online'))
    await waitFor(() => expect((screen.getByText('Save') as HTMLButtonElement).disabled).toBe(false))
    expect((screen.getByLabelText('Body') as HTMLTextAreaElement).value).toBe('draft')
  })

  it('marks mutation network failures offline and allows an explicit sync retry', async () => {
    const { fetchMock } = setup(false)
    const original = fetchMock.getMockImplementation()!
    fetchMock.mockImplementation((input, init) => init?.method === 'PUT'
      ? Promise.reject(new TypeError('connection lost')) : original(input, init))
    render(App); await select(); await edit()
    await fireEvent.input(screen.getByLabelText('Body'), { target: { value: 'draft' } })
    await fireEvent.click(screen.getByText('Save'))
    await waitFor(() => expect(screen.getByText('offline')).toBeDefined())
    await waitFor(() => expect((screen.getByText('Save') as HTMLButtonElement).disabled).toBe(true))
    await fireEvent.click(screen.getByRole('button', { name: 'Resync' }))
    await waitFor(() => expect((screen.getByText('Save') as HTMLButtonElement).disabled).toBe(false))
    expect((screen.getByLabelText('Body') as HTMLTextAreaElement).value).toBe('draft')
  })
})

describe('reactive search', () => {
  it('refreshes an untouched query after remote changes, including empty results', async () => {
    const { update } = setup(false); render(App); await select()
    await fireEvent.input(screen.getByLabelText('Search snippets'), { target: { value: 'Deploy' } })
    update({ title: 'Renamed', body: 'echo something' })
    await fireEvent.click(screen.getByRole('button', { name: 'Resync' }))
    await waitFor(() => expect(document.querySelectorAll('.snippet-list .item')).toHaveLength(0))
    update({ title: 'Deploy again' })
    await fireEvent.click(screen.getByRole('button', { name: 'Resync' }))
    await waitFor(() => expect(document.querySelector('.snippet-list .title')?.textContent).toBe('Deploy again'))
    expect((screen.getByLabelText('Search snippets') as HTMLInputElement).value).toBe('Deploy')
  })

  it('refreshes matching results on local edit, create, and delete', async () => {
    const { fetchMock } = setup(false)
    const original = fetchMock.getMockImplementation()!
    fetchMock.mockImplementation((input, init) => {
      if (String(input).endsWith('/api/snippets') && init?.method === 'POST') {
        return Promise.resolve(new Response(JSON.stringify({ ...fixture(false), id: 's2', ...JSON.parse(String(init.body)) })))
      }
      if (init?.method === 'DELETE') return Promise.resolve(new Response(null, { status: 204 }))
      return original(input, init)
    })
    render(App); await select()
    await fireEvent.input(screen.getByLabelText('Search snippets'), { target: { value: 'Deploy' } })
    await edit()
    await fireEvent.input(screen.getByLabelText('Title'), { target: { value: 'Renamed' } })
    await fireEvent.click(screen.getByText('Save'))
    await waitFor(() => expect(document.querySelectorAll('.snippet-list .item')).toHaveLength(0))
    await fireEvent.click(document.querySelector('.snippet-list .new')!)
    await fireEvent.input(screen.getByLabelText('Title'), { target: { value: 'Deploy new' } })
    await fireEvent.click(screen.getByText('Create'))
    await waitFor(() => expect(document.querySelector('.snippet-list .title')?.textContent).toBe('Deploy new'))
    await fireEvent.click(screen.getByLabelText('Delete snippet'))
    await fireEvent.click(screen.getByRole('button', { name: 'Delete' }))
    await waitFor(() => expect(document.querySelectorAll('.snippet-list .item')).toHaveLength(0))
  })
})

describe('folder assignments', () => {
  it('preserves null when editing with a folder filter and shows the saved unfiled result', async () => {
    const { writes } = setup(false); render(App); await select()
    await fireEvent.click(screen.getByRole('button', { name: 'Ops' }))
    await edit()
    expect((screen.getByLabelText('Folder') as HTMLSelectElement).value).toBe('')
    await fireEvent.click(screen.getByText('Save'))
    await waitFor(() => expect(writes).toHaveLength(1))
    expect(writes[0].folder_id).toBeNull()
    await waitFor(() => expect(document.querySelector('.snippet-list .item')).not.toBeNull())
    expect(document.querySelector('.folder-tree .all')?.classList.contains('selected')).toBe(true)
  })

  it('uses the selected folder for new snippets and supports moving into and out of it', async () => {
    const { writes } = setup(false); render(App); await select()
    await fireEvent.click(screen.getByRole('button', { name: 'Ops' }))
    await fireEvent.click(document.querySelector('.snippet-list .new')!)
    expect((screen.getByLabelText('Folder') as HTMLSelectElement).value).toBe('f1')
    await fireEvent.click(screen.getByText('Cancel'))
    await edit()
    await fireEvent.change(screen.getByLabelText('Folder'), { target: { value: 'f1' } })
    await fireEvent.click(screen.getByText('Save'))
    await waitFor(() => expect(screen.queryByLabelText('Folder')).toBeNull())
    expect(writes[0].folder_id).toBe('f1')
    await edit()
    await fireEvent.change(screen.getByLabelText('Folder'), { target: { value: '' } })
    await fireEvent.click(screen.getByText('Save'))
    await waitFor(() => expect(screen.queryByLabelText('Folder')).toBeNull())
    expect(writes[1].folder_id).toBeNull()
    expect(document.querySelector('.snippet-list .item')).not.toBeNull()
  })
})


describe('newer navigation and data paths', () => {
  it('protects edits on compact browser Back and allows a confirmed Back', async () => {
    localStorage.setItem('snp.layout', 'compact')
    setup(false); render(App); await select(); await edit()
    await fireEvent.input(screen.getByLabelText('Body'), { target: { value: 'draft' } })
    window.history.back()
    await waitFor(() => expect(screen.getByRole('dialog', { name: 'Unsaved changes' })).toBeDefined())
    await fireEvent.click(screen.getByText('Keep editing'))
    expect((screen.getByLabelText('Body') as HTMLTextAreaElement).value).toBe('draft')
    expect((document.querySelector('.pane.detail') as HTMLElement).hidden).toBe(false)
    await fireEvent.click(screen.getByRole('button', { name: 'Back' }))
    await waitFor(() => expect(screen.getByText('Discard changes')).toBeDefined())
    await fireEvent.click(screen.getByText('Discard changes'))
    await waitFor(() => expect((document.querySelector('.pane.list') as HTMLElement).hidden).toBe(false))
    expect(screen.queryByLabelText('Body')).toBeNull()
  })

  it('guards compact Focus search from the palette and traps palette focus', async () => {
    localStorage.setItem('snp.layout', 'compact')
    setup(false); render(App); await select(); await edit()
    await fireEvent.input(screen.getByLabelText('Body'), { target: { value: 'draft' } })
    await fireEvent.keyDown(window, { key: 'P', code: 'KeyP', ctrlKey: true, shiftKey: true })
    const input = screen.getByRole('combobox', { name: 'Command' })
    await fireEvent.keyDown(input, { key: 'Tab' })
    expect(document.activeElement).toBe(input)
    await fireEvent.input(input, { target: { value: 'Focus search' } })
    await fireEvent.keyDown(input, { key: 'Enter' })
    expect(screen.getByRole('dialog', { name: 'Unsaved changes' })).toBeDefined()
    await fireEvent.click(screen.getByText('Keep editing'))
    expect((screen.getByLabelText('Body') as HTMLTextAreaElement).value).toBe('draft')
  })

  it('guards Favorites selection and preserves pinned on edit/defaults saves', async () => {
    const { writes, update } = setup()
    update({ pinned: true })
    render(App)
    await waitFor(() => expect(document.querySelector('.snippet-list .item')).not.toBeNull())
    await fireEvent.click(document.querySelector('.snippet-list .item')!)
    await edit()
    await fireEvent.input(screen.getByLabelText('Body'), { target: { value: 'new {{target}}' } })
    await fireEvent.click(document.querySelector('.favorites .name')!)
    expect(screen.getByRole('dialog', { name: 'Unsaved changes' })).toBeDefined()
    await fireEvent.click(screen.getByText('Keep editing'))
    await fireEvent.click(screen.getByText('Save'))
    await waitFor(() => expect(screen.queryByLabelText('Body')).toBeNull())
    expect(writes[0].pinned).toBe(true)
    await fireEvent.input(screen.getByLabelText('target'), { target: { value: 'staging' } })
    await fireEvent.click(screen.getByText('Save defaults'))
    await waitFor(() => expect(writes).toHaveLength(2))
    expect(writes[1]).toMatchObject({ pinned: true, body: 'new {{target}}', var_defaults: { target: 'staging' } })
  })

  it('defers a service-worker reload during a dirty edit', async () => {
    setup(false); render(App); await select(); await edit()
    await fireEvent.input(screen.getByLabelText('Body'), { target: { value: 'draft' } })
    const event = new Event('snp:before-reload', { cancelable: true })
    window.dispatchEvent(event)
    expect(event.defaultPrevented).toBe(true)
    await fireEvent.click(screen.getByText('Cancel'))
    await fireEvent.click(screen.getByText('Discard changes'))
    const clean = new Event('snp:before-reload', { cancelable: true })
    window.dispatchEvent(clean)
    expect(clean.defaultPrevented).toBe(false)
  })

  it('ignores a late sensitive reveal after selection changes', async () => {
    const { fetchMock } = setup()
    const original = fetchMock.getMockImplementation()!
    const pending = deferred<Response>()
    fetchMock.mockImplementation((input, init) => String(input).endsWith('/api/snippets/s1') && init?.method === 'GET'
      ? pending.promise : original(input, init))
    render(App); await select()
    await fireEvent.click(screen.getByText('Show body'))
    await fireEvent.click(document.querySelector('.snippet-list .new')!)
    pending.resolve(new Response(JSON.stringify(fixture())))
    await fireEvent.click(screen.getByText('Cancel'))
    expect(screen.getByText('Show body')).toBeDefined()
    expect(screen.queryByLabelText('target')).toBeNull()
  })

  it('waits for an in-flight sync before saving the draft', async () => {
    const { fetchMock, writes } = setup(false)
    render(App); await select(); await edit()
    const original = fetchMock.getMockImplementation()!
    const pending = deferred<Response>()
    fetchMock.mockImplementation((input, init) => String(input).includes('/api/sync')
      ? pending.promise : original(input, init))
    await fireEvent.click(screen.getByRole('button', { name: 'Resync' }))
    await fireEvent.input(screen.getByLabelText('Body'), { target: { value: 'new body' } })
    await fireEvent.click(screen.getByText('Save'))
    expect(writes).toHaveLength(0)
    pending.resolve(new Response(JSON.stringify({ server_time: time, folders: [], snippets: [fixture(false)] })))
    await waitFor(() => expect(screen.queryByLabelText('Body')).toBeNull())
    expect(writes).toHaveLength(1)
    expect(document.querySelector('pre.body')?.textContent).toBe('new body')
  })
})
