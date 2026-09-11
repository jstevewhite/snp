import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'
import FolderTree from './FolderTree.svelte'
import type { Folder } from './types'

function folder(id: string, name: string, parentId: string | null): Folder {
  return {
    id,
    name,
    parent_id: parentId,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  }
}

const noop = (): void => {}

afterEach(cleanup)

describe('FolderTree', () => {
  it('renders the all-snippets row and every folder', () => {
    render(FolderTree, {
      folders: [folder('f1', 'Ops', null), folder('f2', 'Sub', 'f1')],
      selectedId: null,
      onselect: noop,
      oncreate: noop,
      onrename: noop,
      onremove: noop,
    })
    expect(screen.getByText('All snippets')).toBeDefined()
    expect(screen.getByText('Ops')).toBeDefined()
    expect(screen.getByText('Sub')).toBeDefined()
  })

  it('shows a hint when there are no folders', () => {
    render(FolderTree, {
      folders: [],
      selectedId: null,
      onselect: noop,
      oncreate: noop,
      onrename: noop,
      onremove: noop,
    })
    expect(screen.getByText('No folders yet')).toBeDefined()
  })

  it('emits select with the folder id, or null for all snippets', async () => {
    const onselect = vi.fn()
    render(FolderTree, {
      folders: [folder('f1', 'Ops', null)],
      selectedId: null,
      onselect,
      oncreate: noop,
      onrename: noop,
      onremove: noop,
    })
    await fireEvent.click(screen.getByText('Ops'))
    expect(onselect).toHaveBeenCalledWith('f1')
    await fireEvent.click(screen.getByText('All snippets'))
    expect(onselect).toHaveBeenLastCalledWith(null)
  })

  it('emits create for the given parent', async () => {
    const oncreate = vi.fn()
    render(FolderTree, {
      folders: [folder('f1', 'Ops', null)],
      selectedId: null,
      onselect: noop,
      oncreate,
      onrename: noop,
      onremove: noop,
    })
    await fireEvent.click(screen.getByLabelText('New subfolder in Ops'))
    expect(oncreate).toHaveBeenCalledWith('f1')
  })

  it('emits remove for the folder', async () => {
    const onremove = vi.fn()
    render(FolderTree, {
      folders: [folder('f1', 'Ops', null)],
      selectedId: null,
      onselect: noop,
      oncreate: noop,
      onrename: noop,
      onremove,
    })
    await fireEvent.click(screen.getByLabelText('Delete Ops'))
    expect(onremove).toHaveBeenCalledWith('f1')
  })

  it('renames inline and emits rename on Enter', async () => {
    const onrename = vi.fn()
    render(FolderTree, {
      folders: [folder('f1', 'Ops', null)],
      selectedId: null,
      onselect: noop,
      oncreate: noop,
      onrename,
      onremove: noop,
    })
    await fireEvent.click(screen.getByLabelText('Rename Ops'))
    const input = screen.getByDisplayValue('Ops')
    await fireEvent.input(input, { target: { value: 'Ops & Infra' } })
    await fireEvent.keyDown(input, { key: 'Enter' })
    expect(onrename).toHaveBeenCalledWith('f1', 'Ops & Infra')
  })

  it('collapses a branch and hides its children', async () => {
    render(FolderTree, {
      folders: [folder('f1', 'Ops', null), folder('f2', 'Sub', 'f1')],
      selectedId: null,
      onselect: noop,
      oncreate: noop,
      onrename: noop,
      onremove: noop,
    })
    expect(screen.getByText('Sub')).toBeDefined()
    await fireEvent.click(screen.getByLabelText('Collapse Ops'))
    await waitFor(() => expect(screen.queryByText('Sub')).toBeNull())
    await fireEvent.click(screen.getByLabelText('Expand Ops'))
    await waitFor(() => expect(screen.getByText('Sub')).toBeDefined())
  })
})
