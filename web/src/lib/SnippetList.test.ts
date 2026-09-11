import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'
import SnippetList from './SnippetList.svelte'
import type { Snippet } from './types'

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

afterEach(cleanup)

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
    await fireEvent.click(screen.getByText('New'))
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
