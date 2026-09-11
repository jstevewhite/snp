import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'
import Favorites from './Favorites.svelte'
import type { Snippet } from './types'

function snippet(id: string, title: string): Snippet {
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
    pinned: true,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-02T00:00:00Z',
  }
}

afterEach(cleanup)

describe('Favorites', () => {
  it('lists pinned snippets and emits select', async () => {
    const onselect = vi.fn()
    render(Favorites, {
      snippets: [snippet('s1', 'Deploy'), snippet('s2', 'Tail logs')],
      selectedId: null,
      onselect,
      onunpin: () => {},
    })
    expect(screen.getByText('Favorites')).toBeDefined()
    await fireEvent.click(screen.getByText('Deploy'))
    expect(onselect).toHaveBeenCalledWith('s1')
  })

  it('marks the selected row', () => {
    const { container } = render(Favorites, {
      snippets: [snippet('s1', 'Deploy')],
      selectedId: 's1',
      onselect: () => {},
      onunpin: () => {},
    })
    const name = container.querySelector('.favorites .name') as HTMLElement
    expect(name.classList.contains('selected')).toBe(true)
  })

  it('puts the full title in a tooltip', () => {
    const { container } = render(Favorites, {
      snippets: [snippet('s1', 'A very long pinned operational command')],
      selectedId: null,
      onselect: () => {},
      onunpin: () => {},
    })
    const name = container.querySelector('.favorites .name') as HTMLElement
    expect(name.getAttribute('title')).toBe('A very long pinned operational command')
  })

  it('emits unpin for a row, disabled offline', async () => {
    const onunpin = vi.fn()
    render(Favorites, {
      snippets: [snippet('s1', 'Deploy')],
      selectedId: null,
      onselect: () => {},
      onunpin,
    })
    await fireEvent.click(screen.getByLabelText('Remove Deploy from favorites'))
    expect(onunpin).toHaveBeenCalledWith('s1')

    cleanup()
    render(Favorites, {
      snippets: [snippet('s1', 'Deploy')],
      selectedId: null,
      offline: true,
      onselect: () => {},
      onunpin: () => {},
    })
    const btn = screen.getByLabelText('Remove Deploy from favorites') as HTMLButtonElement
    expect(btn.disabled).toBe(true)
  })

  it('says how to pin when there is nothing pinned', () => {
    render(Favorites, {
      snippets: [],
      selectedId: null,
      onselect: () => {},
      onunpin: () => {},
    })
    expect(screen.getByText('Pin a snippet from its page to keep it here.')).toBeDefined()
  })
})
