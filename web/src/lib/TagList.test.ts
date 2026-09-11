import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'
import TagList, { type TagItem } from './TagList.svelte'

function tags(...pairs: [string, number][]): TagItem[] {
  return pairs.map(([name, count]) => ({ name, count }))
}

afterEach(cleanup)

describe('TagList', () => {
  it('renders every tag with its count in the given order', () => {
    // The parent (App) passes tags sorted most-used-first.
    render(TagList, {
      tags: tags(['db', 5], ['ops', 3], ['caddy', 1]),
      active: [],
      onselect: () => {},
    })
    expect(screen.getByText('Tags')).toBeDefined()
    expect(screen.getByText('ops')).toBeDefined()
    expect(screen.getByText('5')).toBeDefined()
    expect(screen.getByText('caddy')).toBeDefined()
    const names = screen.getAllByRole('button').map((b) => b.textContent)
    expect(names).toEqual(['db 5', 'ops 3', 'caddy 1'])
  })

  it('shows the empty hint when there are no tags', () => {
    render(TagList, { tags: [], active: [], onselect: () => {} })
    expect(screen.getByText('No tags yet')).toBeDefined()
  })

  it('marks active tags as pressed and reports clicks for toggling', async () => {
    const onselect = vi.fn()
    render(TagList, {
      tags: tags(['ops', 3], ['db', 5]),
      active: ['ops'],
      onselect,
    })
    const ops = screen.getByRole('button', { name: 'Filter by tag ops' })
    expect(ops.getAttribute('aria-pressed')).toBe('true')
    expect(screen.getByRole('button', { name: 'Filter by tag db' }).getAttribute('aria-pressed')).toBe(
      'false',
    )
    await fireEvent.click(ops)
    expect(onselect).toHaveBeenCalledWith('ops')
    await fireEvent.click(screen.getByRole('button', { name: 'Filter by tag db' }))
    expect(onselect).toHaveBeenLastCalledWith('db')
  })
})
