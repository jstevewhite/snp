import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'
import SnippetList from './SnippetList.svelte'
import type { Snippet } from './types'
import { LIST_SIZE_STORAGE_KEY } from './settings'

function snippet(id: string, title: string, p: Partial<Snippet> = {}): Snippet {
  return {
    id,
    title,
    body: null,
    language: '',
    notes: '',
    folder_id: null,
    tags: [],
    is_sensitive: false,
    uses_variables: false,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-02T00:00:00Z',
    ...p,
  }
}

afterEach(() => {
  cleanup()
  localStorage.removeItem(LIST_SIZE_STORAGE_KEY)
})

describe('SnippetList', () => {
  it('renders items with title, language and tags', () => {
    render(SnippetList, {
      snippets: [
        snippet('s1', 'Caddyfile', { language: 'go', tags: ['ops', 'caddy'] }),
        snippet('s2', 'Reset redis', { is_sensitive: true }),
      ],
      selectedId: null,
      query: '',
      onselect: () => {},
      onsearch: () => {},
      oncreate: () => {},
    })
    expect(screen.getByText('Caddyfile')).toBeDefined()
    expect(screen.getByText('go')).toBeDefined()
    expect(screen.getByText('#ops')).toBeDefined()
    expect(screen.getByText('#caddy')).toBeDefined()
    expect(screen.getByText('Reset redis')).toBeDefined()
  })

  it('keeps the precise timestamp in the when tooltip', () => {
    render(SnippetList, {
      snippets: [snippet('s1', 'Caddyfile')],
      selectedId: null,
      query: '',
      onselect: () => {},
      onsearch: () => {},
      oncreate: () => {},
    })
    const when = document.querySelector('.snippet-list .when') as HTMLElement
    // The row shows a short date, not the raw stored value...
    expect(when.textContent).not.toContain('2026-01-02T00:00:00Z')
    // ...which stays available on hover.
    expect(when.getAttribute('title')).toContain('2026')
  })

  it('puts the full title in the tooltip for a truncated row', () => {
    render(SnippetList, {
      snippets: [snippet('s1', 'A very long operational command title')],
      selectedId: null,
      query: '',
      onselect: () => {},
      onsearch: () => {},
      oncreate: () => {},
    })
    const title = document.querySelector('.snippet-list .title') as HTMLElement
    expect(title.textContent).toBe('A very long operational command title')
    expect(title.getAttribute('title')).toBe('A very long operational command title')
  })

  it('keeps one-line titles by default', () => {
    const { container } = render(SnippetList, {
      snippets: [],
      selectedId: null,
      query: '',
      onselect: () => {},
      onsearch: () => {},
      oncreate: () => {},
    })
    const list = container.querySelector('.snippet-list') as HTMLElement
    expect(list.classList.contains('two-line')).toBe(false)
  })

  it('switches to two-line titles when asked', () => {
    const { container } = render(SnippetList, {
      snippets: [],
      selectedId: null,
      query: '',
      twoLine: true,
      onselect: () => {},
      onsearch: () => {},
      oncreate: () => {},
    })
    const list = container.querySelector('.snippet-list') as HTMLElement
    expect(list.classList.contains('two-line')).toBe(true)
  })

  it('emits select on item click', async () => {
    const onselect = vi.fn()
    render(SnippetList, {
      snippets: [snippet('s1', 'Caddyfile')],
      selectedId: null,
      query: '',
      onselect,
      onsearch: () => {},
      oncreate: () => {},
    })
    await fireEvent.click(screen.getByText('Caddyfile'))
    expect(onselect).toHaveBeenCalledWith('s1')
  })

  it('shows the search shortcut hint inside the field', () => {
    render(SnippetList, {
      snippets: [],
      selectedId: null,
      query: '',
      onselect: () => {},
      onsearch: () => {},
      oncreate: () => {},
    })
    const hint = document.querySelector('.snippet-list .hint')
    // The modifier depends on the platform, so either label is correct.
    expect(hint?.textContent?.trim()).toMatch(/^(Ctrl K|⌘K)$/)
    // Decorative: the field's aria-label is its accessible name.
    expect(hint?.getAttribute('aria-hidden')).toBe('true')
  })

  it('emits search as the user types', async () => {
    const onsearch = vi.fn()
    render(SnippetList, {
      snippets: [],
      selectedId: null,
      query: '',
      onselect: () => {},
      onsearch,
      oncreate: () => {},
    })
    const input = screen.getByLabelText('Search snippets')
    await fireEvent.input(input, { target: { value: 'tag:ops' } })
    expect(onsearch).toHaveBeenCalledWith('tag:ops')
  })

  it('shows an empty state and emits create', async () => {
    const oncreate = vi.fn()
    render(SnippetList, {
      snippets: [],
      selectedId: null,
      query: '',
      onselect: () => {},
      onsearch: () => {},
      oncreate,
    })
    expect(screen.getByText('No snippets yet')).toBeDefined()
    await fireEvent.click(screen.getByText('New snippet'))
    expect(oncreate).toHaveBeenCalled()
  })

  it('shows "No matches" when a query has no results', () => {
    render(SnippetList, {
      snippets: [],
      selectedId: null,
      query: 'zzz',
      onselect: () => {},
      onsearch: () => {},
      oncreate: () => {},
    })
    expect(screen.getByText('No matches')).toBeDefined()
  })
})


it('remembers list size and previews only non-sensitive bodies in large cards', async () => {
  const props = {
    snippets: [
      snippet('public', 'Public', { body: 'public preview' }),
      snippet('secret', 'Secret', { body: 'never preview this', is_sensitive: true }),
    ],
    onselect: vi.fn(), onsearch: vi.fn(), oncreate: vi.fn(),
  }
  localStorage.setItem(LIST_SIZE_STORAGE_KEY, 'invalid')
  const view = render(SnippetList, props)
  expect(screen.getByRole('button', { name: 'Regular' }).getAttribute('aria-pressed')).toBe('true')
  await fireEvent.click(screen.getByRole('button', { name: 'Large' }))
  expect(screen.getByText('public preview')).toBeDefined()
  expect(screen.queryByText('never preview this')).toBeNull()
  view.unmount()
  render(SnippetList, props)
  expect(screen.getByRole('button', { name: 'Large' }).getAttribute('aria-pressed')).toBe('true')
  await fireEvent.click(screen.getByRole('button', { name: 'Compact' }))
  expect(screen.queryByText('public preview')).toBeNull()
  await fireEvent.click(screen.getByText('Public'))
  expect(props.onselect).toHaveBeenCalledWith('public')
})
