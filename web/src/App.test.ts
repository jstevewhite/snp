import 'fake-indexeddb/auto'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/svelte'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { PANE_WIDTHS_STORAGE_KEY } from './lib/panes'
import { TWO_LINE_TITLES_STORAGE_KEY } from './lib/settings'
import App from './App.svelte'

const T0 = '2026-09-03T00:00:00Z'

function syncPayload() {
  return {
    server_time: '2026-09-03T10:00:00Z',
    folders: [
      { id: 'f1', parent_id: null, name: 'Ops', created_at: T0, updated_at: T0 },
    ],
    snippets: [
      {
        id: 's1',
        title: 'Caddyfile',
        body: 'http://localhost:8080',
        language: 'go',
        notes: '',
        folder_id: 'f1',
        tags: ['ops'],
        is_sensitive: false,
        uses_variables: false,
        created_at: T0,
        updated_at: '2026-09-03T01:00:00Z',
      },
      {
        id: 's2',
        title: 'Redis flush',
        body: null,
        language: 'bash',
        notes: '',
        folder_id: null,
        tags: ['ops'],
        is_sensitive: true,
        uses_variables: false,
        created_at: T0,
        updated_at: '2026-09-03T02:00:00Z',
      },
      {
        id: 's3',
        title: 'Deploy',
        body: 'kubectl -n {{ns}} get pods',
        language: 'bash',
        notes: '',
        folder_id: null,
        tags: ['ops'],
        is_sensitive: false,
        uses_variables: true,
        var_defaults: { ns: 'prod' },
        created_at: T0,
        updated_at: '2026-09-03T03:00:00Z',
      },
    ],
  }
}

function stubFetch(): ReturnType<typeof vi.fn> {
  const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input)
    if (url.includes('/api/sync')) {
      return new Response(JSON.stringify(syncPayload()), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    }
    if (url.includes('/api/version')) {
      return new Response(JSON.stringify({ version: 'v9.9.9-test' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    }
    return new Response('null', {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    })
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

/** Stubs navigator.clipboard and returns its writeText spy. */
function stubClipboard(): ReturnType<typeof vi.fn> {
  const writeText = vi.fn().mockResolvedValue(undefined)
  Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
  return writeText
}

describe('App', () => {
  beforeEach(async () => {
    await indexedDB.deleteDatabase('snp')
    localStorage.removeItem('snp.theme')
    localStorage.removeItem('snp.textScale')
    localStorage.removeItem('snp.version')
    localStorage.removeItem(PANE_WIDTHS_STORAGE_KEY)
    localStorage.removeItem(TWO_LINE_TITLES_STORAGE_KEY)
    document.documentElement.removeAttribute('data-theme')
    document.documentElement.style.removeProperty('--text-scale')
  })

  afterEach(() => {
    cleanup()
    Reflect.deleteProperty(navigator, 'clipboard')
    vi.unstubAllGlobals()
  })

  it('syncs from the server into the local cache and renders the panes', async () => {
    const fetchMock = stubFetch()
    const { unmount } = render(App)
    await waitFor(() => expect(screen.getByText('Caddyfile')).toBeDefined())
    expect(screen.getByText('Ops')).toBeDefined()
    expect(screen.getByText('Redis flush')).toBeDefined()
    expect(screen.getByText('online')).toBeDefined()
    expect(fetchMock).toHaveBeenCalled()
    unmount()
  })

  it('resizes the panes with the dividers and persists the widths', async () => {
    stubFetch()
    const { unmount } = render(App)
    await waitFor(() => expect(screen.getByText('Caddyfile')).toBeDefined())

    const dividers = screen.getAllByRole('separator')
    expect(dividers.length).toBe(2)
    const panes = document.querySelector('.panes') as HTMLElement
    // jsdom does no layout, so the component falls back to its documented
    // defaults (folders 230 / list 360) — the same baseline the first drag in
    // a browser would start from.
    panes.getBoundingClientRect = () => ({ width: 1200 }) as DOMRect

    // Dragging the folders divider right trades width with the list pane.
    await fireEvent.pointerDown(dividers[0], { clientX: 230, pointerId: 1 })
    await fireEvent.pointerMove(dividers[0], { clientX: 280, pointerId: 1 })
    await fireEvent.pointerUp(dividers[0], { clientX: 280, pointerId: 1 })

    await waitFor(() => expect(panes.getAttribute('style')).toContain('--folders-w: 280px'))
    expect(panes.getAttribute('style')).toContain('--list-w: 310px')
    expect(JSON.parse(localStorage.getItem(PANE_WIDTHS_STORAGE_KEY) ?? '')).toEqual({
      folders: 280,
      list: 310,
    })

    // The list divider is absorbed by the flexible detail pane.
    await fireEvent.pointerDown(dividers[1], { clientX: 600, pointerId: 2 })
    await fireEvent.pointerMove(dividers[1], { clientX: 660, pointerId: 2 })
    await fireEvent.pointerUp(dividers[1], { clientX: 660, pointerId: 2 })
    await waitFor(() => expect(panes.getAttribute('style')).toContain('--list-w: 370px'))
    expect(panes.getAttribute('style')).toContain('--folders-w: 280px')
    unmount()
  })

  it('nudges the panes from the keyboard and resets them on double-click', async () => {
    stubFetch()
    const { unmount } = render(App)
    await waitFor(() => expect(screen.getByText('Caddyfile')).toBeDefined())

    const dividers = screen.getAllByRole('separator')
    const panes = document.querySelector('.panes') as HTMLElement
    panes.getBoundingClientRect = () => ({ width: 1200 }) as DOMRect

    // A focused divider responds to the arrow keys (1px with Shift).
    await fireEvent.keyDown(dividers[0], { key: 'ArrowRight' })
    await waitFor(() => expect(panes.getAttribute('style')).toContain('--folders-w: 246px'))
    await fireEvent.keyDown(dividers[0], { key: 'ArrowLeft', shiftKey: true })
    await waitFor(() => expect(panes.getAttribute('style')).toContain('--folders-w: 245px'))

    // Double-click clears the stored widths and hands the layout back to the
    // stylesheet default (no custom properties at all).
    await fireEvent.dblClick(dividers[0])
    await waitFor(() => expect(panes.getAttribute('style')).not.toContain('--folders-w'))
    expect(localStorage.getItem(PANE_WIDTHS_STORAGE_KEY)).toBeNull()
    unmount()
  })

  it('shows the release version after the wordmark, and puts the sync time last', async () => {
    stubFetch()
    const { container, unmount } = render(App)
    await waitFor(() => expect(screen.getByText('Caddyfile')).toBeDefined())

    // The version arrives from GET /api/version and sits right after 'snp'.
    const brand = container.querySelector('.topbar .brand')!
    const version = await waitFor(() => {
      const v = container.querySelector('.topbar .version')
      expect(v?.textContent).toBe('v9.9.9-test')
      return v!
    })
    expect(brand.nextElementSibling).toBe(version)

    // The synced label is the last text in the bar, immediately left of
    // the Resync button, and reads as a relative age — the precise time
    // stays in the tooltip rather than in the bar.
    const synced = await waitFor(() => {
      const el = container.querySelector('.topbar .synced')
      expect(el?.textContent).toMatch(
        /Synced (?:just now|\d+ (?:minute|minutes|hour|hours) ago)/,
      )
      return el!
    })
    // The precise time is in the tooltip (a 4-digit year is enough to
    // prove it is a formatted absolute time, without pinning today's date).
    expect(synced.getAttribute('title')).toMatch(/\d{4}/)
    const resync = [...container.querySelectorAll('.topbar button')].find(
      (b) => b.textContent?.trim() === 'Resync',
    )!
    expect(synced.nextElementSibling).toBe(resync)
    unmount()
  })

  it('selecting a snippet shows its detail pane', async () => {
    stubFetch()
    const { unmount } = render(App)
    await waitFor(() => expect(screen.getByText('Caddyfile')).toBeDefined())
    await fireEvent.click(screen.getAllByText('Caddyfile')[0])
    await waitFor(() =>
      expect(screen.getByText('http://localhost:8080')).toBeDefined(),
    )
    unmount()
  })

  it('editing a sensitive snippet fetches its body and never saves an empty one', async () => {
    let putBody: unknown = null
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.includes('/api/sync')) {
        return new Response(JSON.stringify(syncPayload()), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        })
      }
      if (url.endsWith('/api/snippets/s2') && init?.method === 'GET') {
        // GET returns the decrypted body; it must not go to the editor as ''.
        const s2 = syncPayload().snippets[1] as Record<string, unknown>
        return new Response(
          JSON.stringify({ ...s2, body: 'SECRET', var_defaults: { k: 'v' } }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        )
      }
      if (url.endsWith('/api/snippets/s2') && init?.method === 'PUT') {
        putBody = JSON.parse(String(init.body))
        const s2 = syncPayload().snippets[1] as Record<string, unknown>
        // Sensitive rows come back with body null (spec §5).
        return new Response(
          JSON.stringify({ ...s2, body: null, updated_at: '2026-09-04T09:00:00Z' }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        )
      }
      return new Response('null', {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    })
    vi.stubGlobal('fetch', fetchMock)
    const { unmount } = render(App)
    await waitFor(() => expect(screen.getByText('Caddyfile')).toBeDefined())
    await fireEvent.click(screen.getAllByText('Redis flush')[0])
    await waitFor(() => expect(screen.getByText('Show body')).toBeDefined())
    // Edit is enabled online; the decrypted body is fetched for the form.
    await fireEvent.click(screen.getByText('Edit').closest('button') as HTMLButtonElement)
    await waitFor(() =>
      expect((screen.getByLabelText('Body') as HTMLTextAreaElement).value).toBe('SECRET'),
    )
    await fireEvent.click(screen.getByText('Save'))
    await waitFor(() => expect(putBody).not.toBeNull())
    // The full-replace PUT carries the real body — never '' — and keeps
    // the sensitive flag (regression: the old flow wiped the body).
    expect(putBody).toMatchObject({ title: 'Redis flush', body: 'SECRET', is_sensitive: true })
    unmount()
  })

  it('search filters the list locally', async () => {
    stubFetch()
    const { unmount } = render(App)
    await waitFor(() => expect(screen.getByText('Caddyfile')).toBeDefined())
    await fireEvent.input(screen.getByLabelText('Search snippets'), {
      target: { value: 'redis' },
    })
    await waitFor(() => expect(screen.queryByText('Caddyfile')).toBeNull())
    expect(screen.getByText('Redis flush')).toBeDefined()
    unmount()
  })

  it('shows the offline banner when sync fails', async () => {
    const fetchMock = vi.fn(async () => {
      throw new TypeError('fetch failed')
    })
    vi.stubGlobal('fetch', fetchMock)
    const { unmount } = render(App)
    await waitFor(() => expect(screen.getByText('offline')).toBeDefined())
    expect(fetchMock).toHaveBeenCalled()
    unmount()
  })

  it('disables writes and reveal when offline', async () => {
    stubFetch()
    const { unmount } = render(App)
    await waitFor(() => expect(screen.getByText('Caddyfile')).toBeDefined())
    window.dispatchEvent(new Event('offline'))
    await waitFor(() =>
      expect(
        screen.getByText(
          'Browsing cached snippets. Create, edit, and delete are disabled until the connection is back.',
        ),
      ).toBeDefined(),
    )
    // Create buttons (folders pane + snippet list) are disabled.
    const newBtns = [
      ...screen.getAllByText('New folder'),
      ...screen.getAllByText('New snippet'),
    ].map((el) => el.closest('button') as HTMLButtonElement)
    for (const b of newBtns) expect(b.disabled).toBe(true)
    // Folder actions are disabled.
    const subfolderBtn = screen
      .getByLabelText('New subfolder in Ops')
      .closest('button') as HTMLButtonElement
    expect(subfolderBtn.disabled).toBe(true)
    // Sensitive reveal is blocked with a hint.
    await fireEvent.click(screen.getAllByText('Redis flush')[0])
    await waitFor(() => expect(screen.getByText('Show body')).toBeDefined())
    const revealBtn = screen.getByText('Show body').closest('button') as HTMLButtonElement
    expect(revealBtn.disabled).toBe(true)
    expect(
      screen.getByText('Online connection required to show the body.'),
    ).toBeDefined()
    // Edit/delete are disabled.
    expect((screen.getByText('Edit').closest('button') as HTMLButtonElement).disabled).toBe(
      true,
    )
    expect(
      (screen.getByLabelText('Delete snippet') as HTMLButtonElement).disabled,
    ).toBe(true)
    // Saving variable defaults is also a server write.
    await fireEvent.click(screen.getAllByText('Deploy')[0])
    await waitFor(() => expect(screen.getByText('Variables')).toBeDefined())
    expect(
      (screen.getByText('Save defaults').closest('button') as HTMLButtonElement)
        .disabled,
    ).toBe(true)
    unmount()
  })

  it('saving defaults replaces the snippet with the new defaults map', async () => {
    let putBody: unknown = null
    let putUrl = ''
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.includes('/api/sync')) {
        return new Response(JSON.stringify(syncPayload()), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        })
      }
      if (url.includes('/api/snippets/s3') && init?.method === 'PUT') {
        putUrl = url
        putBody = JSON.parse(String(init.body))
        const payload = JSON.parse(String(init.body))
        const updated = {
          ...(syncPayload().snippets[2] as Record<string, unknown>),
          updated_at: '2026-09-04T09:00:00Z',
          var_defaults: payload.var_defaults,
        }
        return new Response(JSON.stringify(updated), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        })
      }
      return new Response('null', {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    })
    vi.stubGlobal('fetch', fetchMock)
    const { unmount } = render(App)
    await waitFor(() => expect(screen.getByText('Deploy')).toBeDefined())
    await fireEvent.click(screen.getAllByText('Deploy')[0])
    await waitFor(() => expect(screen.getByText('Variables')).toBeDefined())
    // The input is pre-filled from the saved default.
    expect((screen.getByLabelText('ns') as HTMLInputElement).value).toBe('prod')
    await fireEvent.input(screen.getByLabelText('ns'), {
      target: { value: 'staging' },
    })
    await fireEvent.click(screen.getByText('Save defaults'))
    await waitFor(() => expect(putBody).not.toBeNull())
    expect(putUrl).toContain('/api/snippets/s3')
    // The replace carries the snippet's current fields plus the new map,
    // pruned to the body's variables (spec §4/§5).
    expect(putBody).toEqual({
      title: 'Deploy',
      body: 'kubectl -n {{ns}} get pods',
      language: 'bash',
      notes: '',
      folder_id: null,
      tags: ['ops'],
      is_sensitive: false,
      uses_variables: true,
      var_defaults: { ns: 'staging' },
    })
    unmount()
  })

  it('creates root and subfolders through the in-app dialog', async () => {
    const posts: { url: string; body: { name: string; parent_id: string | null } }[] = []
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.includes('/api/sync')) {
        return new Response(JSON.stringify(syncPayload()), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        })
      }
      if (url.includes('/api/folders') && init?.method === 'POST') {
        const body = JSON.parse(String(init.body)) as { name: string; parent_id: string | null }
        posts.push({ url, body })
        return new Response(
          JSON.stringify({
            id: `fd${posts.length}`,
            parent_id: body.parent_id ?? null,
            name: body.name,
            created_at: T0,
            updated_at: T0,
          }),
          { status: 201, headers: { 'Content-Type': 'application/json' } },
        )
      }
      return new Response('null', {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    })
    vi.stubGlobal('fetch', fetchMock)
    const { unmount } = render(App)
    await waitFor(() => expect(screen.getByText('Caddyfile')).toBeDefined())

    // Root folder: the folder pane's New folder button opens the dialog.
    await fireEvent.click(screen.getByText('New folder'))
    await waitFor(() => expect(screen.getByLabelText('Folder name')).toBeDefined())
    await fireEvent.input(screen.getByLabelText('Folder name'), {
      target: { value: 'Ops2' },
    })
    await fireEvent.click(screen.getByText('Create'))
    await waitFor(() => expect(screen.getByText('Ops2')).toBeDefined())
    expect(posts[0]).toEqual({
      url: '/api/folders',
      body: { name: 'Ops2', parent_id: null },
    })

    // Subfolder: the row's "+" opens the same dialog with the parent id.
    await fireEvent.click(screen.getByLabelText('New subfolder in Ops2'))
    await waitFor(() => expect(screen.getByLabelText('Folder name')).toBeDefined())
    await fireEvent.input(screen.getByLabelText('Folder name'), {
      target: { value: 'deploy' },
    })
    await fireEvent.click(screen.getByText('Create'))
    await waitFor(() => expect(posts.length).toBe(2))
    expect(posts[1]).toEqual({
      url: '/api/folders',
      body: { name: 'deploy', parent_id: 'fd1' },
    })
    unmount()
  })

  it('choosing a theme applies and persists it', async () => {
    stubFetch()
    const { unmount } = render(App)
    await waitFor(() => expect(screen.getByText('Caddyfile')).toBeDefined())
    await fireEvent.click(screen.getByLabelText('Settings'))
    const select = screen.getByLabelText('Theme') as HTMLSelectElement
    expect(select.options.length).toBe(7)
    await fireEvent.change(select, { target: { value: 'tokyo-night' } })
    await waitFor(() =>
      expect(document.documentElement.getAttribute('data-theme')).toBe('tokyo-night'),
    )
    expect(localStorage.getItem('snp.theme')).toBe('tokyo-night')
    // Back to auto: the attribute is removed and the key cleared.
    await fireEvent.change(select, { target: { value: 'auto' } })
    await waitFor(() =>
      expect(document.documentElement.hasAttribute('data-theme')).toBe(false),
    )
    expect(localStorage.getItem('snp.theme')).toBeNull()
    unmount()
  })

  it('the text-size slider scales and persists the interface text size', async () => {
    stubFetch()
    const { unmount } = render(App)
    await waitFor(() => expect(screen.getByText('Caddyfile')).toBeDefined())
    await fireEvent.click(screen.getByLabelText('Settings'))
    const slider = screen.getByLabelText('Interface text size') as HTMLInputElement
    expect(slider.value).toBe('100')
    expect(screen.getByText('100%')).toBeDefined()
    await fireEvent.input(slider, { target: { value: '125' } })
    await waitFor(() =>
      expect(
        document.documentElement.style.getPropertyValue('--text-scale'),
      ).toBe('1.25'),
    )
    expect(localStorage.getItem('snp.textScale')).toBe('125')
    expect(screen.getByText('125%')).toBeDefined()
    unmount()
  })

  it('full resync clears the cache and re-syncs from scratch', async () => {
    let n = 0
    const fresh = {
      server_time: '2026-09-04T00:00:00Z',
      folders: [{ id: 'f1', deleted_at: '2026-09-04T00:00:00Z' }],
      snippets: [
        // Tombstone: the merge is additive, so a locally-cached row is only
        // removed when the server explicitly reports its deletion.
        { id: 's1', deleted_at: '2026-09-04T00:00:00Z' },
        {
          id: 's9',
          title: 'Fresh',
          body: 'fresh body',
          language: '',
          notes: '',
          folder_id: null,
          tags: [],
          is_sensitive: false,
          uses_variables: false,
          created_at: '2026-09-04T00:00:00Z',
          updated_at: '2026-09-04T00:00:00Z',
        },
      ],
    }
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes('/api/sync')) {
        n++
        const payload = n === 1 ? syncPayload() : fresh
        return new Response(JSON.stringify(payload), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        })
      }
      return new Response('null', {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    })
    vi.stubGlobal('fetch', fetchMock)
    const { unmount } = render(App)
    await waitFor(() => expect(screen.getByText('Caddyfile')).toBeDefined())
    // Full resync confirms through the in-app dialog (window.confirm is
    // unavailable in the desktop webview).
    await fireEvent.click(screen.getByLabelText('Settings'))
    await fireEvent.click(screen.getByText('Full resync'))
    await fireEvent.click(screen.getByText('Clear and resync'))
    await waitFor(() => expect(screen.getByText('Fresh')).toBeDefined())
    // The cache was cleared and re-populated from the server.
    expect(screen.queryByText('Caddyfile')).toBeNull()
    expect(screen.queryByText('Ops')).toBeNull()
    expect(n).toBe(2)
    unmount()
  })

  it('adds the starter snippets from the settings panel and re-syncs', async () => {
    let seeded = false
    const payload = () => {
      const p = syncPayload()
      if (!seeded) return p
      p.folders.push({ id: 'f9', parent_id: null, name: 'Starter', created_at: T0, updated_at: T0 })
      p.snippets.push({
        id: 's9',
        title: 'snp config.toml',
        body: 'state_dir = "~/.local/share/snp"',
        language: 'toml',
        notes: '',
        folder_id: 'f9',
        tags: ['snp', 'config'],
        is_sensitive: false,
        uses_variables: false,
        created_at: T0,
        updated_at: T0,
      })
      return p
    }
    let seedPosts = 0
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.includes('/api/sync')) {
        return new Response(JSON.stringify(payload()), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        })
      }
      if (url.endsWith('/api/seed') && init?.method === 'POST') {
        seedPosts++
        seeded = true
        return new Response(JSON.stringify({ created: 4, updated: 0 }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        })
      }
      return new Response('null', {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    })
    vi.stubGlobal('fetch', fetchMock)
    const { unmount } = render(App)
    await waitFor(() => expect(screen.getByText('Caddyfile')).toBeDefined())

    await fireEvent.click(screen.getByLabelText('Settings'))
    await fireEvent.click(screen.getByText('Add starter snippets'))

    await waitFor(() => expect(screen.getByText('Added 4 starter snippets.')).toBeDefined())
    expect(seedPosts).toBe(1)
    // Seeding writes through the import path, so the rows arrive on the
    // next sync like any other server-side change.
    await waitFor(() => expect(screen.getByText('snp config.toml')).toBeDefined())
    unmount()
  })

  it('deletes a snippet only after confirming in the dialog', async () => {
    const deletes: string[] = []
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.includes('/api/sync')) {
        return new Response(JSON.stringify(syncPayload()), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        })
      }
      if (init?.method === 'DELETE') {
        deletes.push(url)
        return new Response(null, { status: 204 })
      }
      return new Response('null', {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    })
    vi.stubGlobal('fetch', fetchMock)
    const { container, unmount } = render(App)
    await waitFor(() => expect(screen.getByText('Caddyfile')).toBeDefined())
    await fireEvent.click(screen.getAllByText('Caddyfile')[0])
    const trash = () => container.querySelector('button.trash') as HTMLButtonElement
    await waitFor(() => expect(trash()).not.toBeNull())

    // Cancel keeps the snippet.
    await fireEvent.click(trash())
    await waitFor(() => expect(screen.getByText('Delete snippet?')).toBeDefined())
    await fireEvent.click(screen.getByText('Cancel'))
    expect(deletes).toEqual([])
    expect(screen.getAllByText('Caddyfile').length).toBeGreaterThan(0)

    // Confirm issues the DELETE and drops it from the list.
    await fireEvent.click(trash())
    await waitFor(() => expect(screen.getByText('Delete snippet?')).toBeDefined())
    await fireEvent.click(screen.getByText('Delete'))
    await waitFor(() => expect(deletes).toEqual(['/api/snippets/s1']))
    await waitFor(() => expect(screen.queryByText('Caddyfile')).toBeNull())
    unmount()
  })

  it('tag clicks filter the list (AND) and combine with folders', async () => {
    const payload = {
      server_time: '2026-09-03T10:00:00Z',
      folders: [{ id: 'f1', parent_id: null, name: 'Ops', created_at: T0, updated_at: T0 }],
      snippets: [
        {
          id: 't1',
          title: 'Alpha',
          body: 'a',
          language: '',
          notes: '',
          folder_id: 'f1',
          tags: ['ops'],
          is_sensitive: false,
          uses_variables: false,
          created_at: T0,
          updated_at: '2026-09-03T01:00:00Z',
        },
        {
          id: 't2',
          title: 'Bravo',
          body: 'b',
          language: '',
          notes: '',
          folder_id: null,
          tags: ['db'],
          is_sensitive: false,
          uses_variables: false,
          created_at: T0,
          updated_at: '2026-09-03T02:00:00Z',
        },
        {
          id: 't3',
          title: 'Charlie',
          body: 'c',
          language: '',
          notes: '',
          folder_id: null,
          tags: ['ops', 'db'],
          is_sensitive: false,
          uses_variables: false,
          created_at: T0,
          updated_at: '2026-09-03T03:00:00Z',
        },
      ],
    }
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      if (String(input).includes('/api/sync')) {
        return new Response(JSON.stringify(payload), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        })
      }
      return new Response('null', {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    })
    vi.stubGlobal('fetch', fetchMock)
    const { unmount } = render(App)
    await waitFor(() => expect(screen.getByText('Alpha')).toBeDefined())
    expect(screen.getByText('Bravo')).toBeDefined()
    expect(screen.getByText('Charlie')).toBeDefined()
    // Tag row present with the count of snippets carrying it.
    expect(screen.getByRole('button', { name: 'Filter by tag ops' }).textContent).toContain('2')

    // Click ops: only snippets carrying ops remain.
    await fireEvent.click(screen.getByRole('button', { name: 'Filter by tag ops' }))
    expect(screen.getByText('Alpha')).toBeDefined()
    expect(screen.getByText('Charlie')).toBeDefined()
    expect(screen.queryByText('Bravo')).toBeNull()
    // Add db: AND — only the snippet with both remains.
    await fireEvent.click(screen.getByRole('button', { name: 'Filter by tag db' }))
    await waitFor(() => expect(screen.queryByText('Alpha')).toBeNull())
    expect(screen.getByText('Charlie')).toBeDefined()
    expect(screen.queryByText('Bravo')).toBeNull()
    // Clearing one tag widens again.
    await fireEvent.click(screen.getByRole('button', { name: 'Filter by tag ops' }))
    expect(screen.queryByText('Alpha')).toBeNull()
    expect(screen.getByText('Charlie')).toBeDefined()
    expect(screen.getByText('Bravo')).toBeDefined()

    // Combine with the folder: folder Ops has only Alpha (which is not db).
    await fireEvent.click(screen.getByText('Ops'))
    await waitFor(() => expect(screen.queryByText('Charlie')).toBeNull())
    expect(screen.queryByText('Alpha')).toBeNull()
    expect(screen.queryByText('Bravo')).toBeNull()
    // Back to "All snippets" and cleared tags: everything shows again.
    await fireEvent.click(screen.getByRole('button', { name: 'Filter by tag db' }))
    await fireEvent.click(screen.getByText('All snippets'))
    await waitFor(() => expect(screen.getByText('Alpha')).toBeDefined())
    expect(screen.getByText('Bravo')).toBeDefined()
    expect(screen.getByText('Charlie')).toBeDefined()
    unmount()
  })

  it('full resync cancel keeps the cache', async () => {
    const fetchMock = stubFetch()
    const { unmount } = render(App)
    await waitFor(() => expect(screen.getByText('Caddyfile')).toBeDefined())
    await fireEvent.click(screen.getByLabelText('Settings'))
    await fireEvent.click(screen.getByText('Full resync'))
    await fireEvent.click(screen.getByText('Cancel'))
    await new Promise((r) => setTimeout(r, 25))
    expect(screen.getByText('Caddyfile')).toBeDefined()
    expect(
      fetchMock.mock.calls.filter((c) => String(c[0]).includes('/api/sync')),
    ).toHaveLength(1)
    unmount()
  })

  it('focuses the search field on the search shortcut', async () => {
    stubFetch()
    const { unmount } = render(App)
    await waitFor(() => expect(screen.getByText('Caddyfile')).toBeDefined())
    const search = screen.getByLabelText('Search snippets') as HTMLInputElement
    expect(document.activeElement).not.toBe(search)

    await fireEvent.keyDown(window, { key: 'k', ctrlKey: true })
    expect(document.activeElement).toBe(search)

    // The current query is selected too, so typing replaces it.
    await fireEvent.input(search, { target: { value: 'caddy' } })
    await waitFor(() => expect(search.value).toBe('caddy'))
    await fireEvent.keyDown(window, { key: 'k', ctrlKey: true })
    expect(search.selectionStart).toBe(0)
    expect(search.selectionEnd).toBe('caddy'.length)
    unmount()
  })

  it('walks the list with the arrow keys and copies the selection on Enter', async () => {
    stubFetch()
    const writeText = stubClipboard()
    const { unmount } = render(App)
    await waitFor(() => expect(screen.getByText('Caddyfile')).toBeDefined())
    const search = screen.getByLabelText('Search snippets') as HTMLInputElement
    search.focus()

    // Newest first (updated_at DESC): Deploy, Redis flush, Caddyfile.
    await fireEvent.keyDown(window, { key: 'ArrowDown' })
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Deploy' })).toBeDefined())

    // Redis flush is sensitive, so its body stays behind the reveal button.
    await fireEvent.keyDown(window, { key: 'ArrowDown' })
    await waitFor(() => expect(screen.getByText('Show body')).toBeDefined())

    // Back to the template; Enter copies what its Copy button would write,
    // with {{ns}} resolved from the saved default.
    await fireEvent.keyDown(window, { key: 'ArrowUp' })
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Deploy' })).toBeDefined())
    await fireEvent.keyDown(window, { key: 'Enter' })
    await waitFor(() => expect(writeText).toHaveBeenCalledWith('kubectl -n prod get pods'))
    // The keyboard path has no button to flash, so it reports in the topbar.
    expect(screen.getByText('Copied.')).toBeDefined()
    unmount()
  })

  it('wraps the arrow-key selection at both ends', async () => {
    stubFetch()
    const { unmount } = render(App)
    await waitFor(() => expect(screen.getByText('Caddyfile')).toBeDefined())
    const search = screen.getByLabelText('Search snippets') as HTMLInputElement
    search.focus()

    // Nothing selected: ArrowUp takes the last (oldest) row.
    await fireEvent.keyDown(window, { key: 'ArrowUp' })
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Caddyfile' })).toBeDefined())

    // ArrowDown from the last row wraps to the first.
    await fireEvent.keyDown(window, { key: 'ArrowDown' })
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Deploy' })).toBeDefined())
    unmount()
  })

  it('clears the search query on Escape, then leaves the field', async () => {
    stubFetch()
    const { unmount } = render(App)
    await waitFor(() => expect(screen.getByText('Caddyfile')).toBeDefined())
    const search = screen.getByLabelText('Search snippets') as HTMLInputElement
    search.focus()
    await fireEvent.input(search, { target: { value: 'caddy' } })
    await waitFor(() => expect(search.value).toBe('caddy'))

    await fireEvent.keyDown(window, { key: 'Escape' })
    await waitFor(() => expect(search.value).toBe(''))

    await fireEvent.keyDown(window, { key: 'Escape' })
    expect(document.activeElement).not.toBe(search)
    unmount()
  })

  it('toggles two-line list titles from the settings panel', async () => {
    stubFetch()
    const { container, unmount } = render(App)
    await waitFor(() => expect(screen.getByText('Caddyfile')).toBeDefined())
    const list = (): HTMLElement => container.querySelector('.snippet-list') as HTMLElement
    expect(list().classList.contains('two-line')).toBe(false)

    await fireEvent.click(screen.getByLabelText('Settings'))
    await fireEvent.click(screen.getByLabelText('Two-line titles in the list'))

    await waitFor(() => expect(list().classList.contains('two-line')).toBe(true))
    expect(localStorage.getItem(TWO_LINE_TITLES_STORAGE_KEY)).toBe('true')
    unmount()
  })

  it('leaves Enter alone while the snippet editor has focus', async () => {
    stubFetch()
    const writeText = stubClipboard()
    const { unmount } = render(App)
    await waitFor(() => expect(screen.getByText('Caddyfile')).toBeDefined())
    await fireEvent.click(screen.getAllByText('Caddyfile')[0])
    await waitFor(() => expect(screen.getByText('Edit')).toBeDefined())
    await fireEvent.click(screen.getByText('Edit'))

    const body = (await waitFor(() => screen.getByLabelText('Body'))) as HTMLTextAreaElement
    body.focus()
    await fireEvent.keyDown(window, { key: 'Enter' })
    expect(writeText).not.toHaveBeenCalled()
    unmount()
  })
})
