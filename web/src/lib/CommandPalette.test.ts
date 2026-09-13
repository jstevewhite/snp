import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'
import CommandPalette from './CommandPalette.svelte'
import type { Command } from './commands'

afterEach(cleanup)

function setup(overrides: Partial<Command>[] = []) {
  const runs: string[] = []
  const base: Command[] = [
    { id: 'new', label: 'New snippet', group: 'snippet', shortcut: 'N', run: () => runs.push('new') },
    { id: 'copy', label: 'Copy snippet', group: 'snippet', run: () => runs.push('copy') },
    { id: 'resync', label: 'Resync', group: 'app', run: () => runs.push('resync') },
  ]
  overrides.forEach((o, i) => Object.assign(base[i], o))
  const onclose = vi.fn()
  render(CommandPalette, { commands: base, onclose })
  const input = screen.getByRole('combobox', { name: 'Command' }) as HTMLInputElement
  return { runs, onclose, input }
}

function highlighted(): string | null {
  const el = document.querySelector('[role="option"][aria-selected="true"]')
  return el?.textContent?.trim() ?? null
}

describe('CommandPalette', () => {
  it('lists every command, shows shortcut hints, and focuses the filter', () => {
    const { input } = setup()
    expect(screen.getAllByRole('option')).toHaveLength(3)
    expect(screen.getByText('N')).toBeDefined()
    expect(document.activeElement).toBe(input)
    expect(highlighted()).toContain('New snippet')
  })

  it('filters as the user types and highlights the first match', async () => {
    const { input } = setup()
    await fireEvent.input(input, { target: { value: 'sync' } })
    expect(screen.getAllByRole('option')).toHaveLength(1)
    expect(highlighted()).toContain('Resync')
  })

  it('moves with the arrows, wrapping at both ends', async () => {
    const { input } = setup()
    await fireEvent.keyDown(input, { key: 'ArrowUp' })
    expect(highlighted()).toContain('Resync')
    await fireEvent.keyDown(input, { key: 'ArrowDown' })
    expect(highlighted()).toContain('New snippet')
    await fireEvent.keyDown(input, { key: 'ArrowDown' })
    expect(highlighted()).toContain('Copy snippet')
  })

  it('runs the highlighted command on Enter and closes', async () => {
    const { input, runs, onclose } = setup()
    await fireEvent.keyDown(input, { key: 'ArrowDown' })
    await fireEvent.keyDown(input, { key: 'Enter' })
    expect(runs).toEqual(['copy'])
    expect(onclose).toHaveBeenCalledTimes(1)
  })

  it('shows the reason for a disabled command and refuses to run it', async () => {
    const { input, runs, onclose } = setup([{ disabled: 'Offline' }])
    expect(screen.getByText('Offline')).toBeDefined()
    expect(screen.getAllByRole('option')[0].getAttribute('aria-disabled')).toBe('true')
    await fireEvent.keyDown(input, { key: 'Enter' })
    expect(runs).toEqual([])
    expect(onclose).not.toHaveBeenCalled()
  })

  it('runs a command on click', async () => {
    const { runs, onclose } = setup()
    await fireEvent.click(screen.getByText('Resync'))
    expect(runs).toEqual(['resync'])
    expect(onclose).toHaveBeenCalledTimes(1)
  })

  it('closes on Escape and on a backdrop click without running anything', async () => {
    const { input, runs, onclose } = setup()
    await fireEvent.keyDown(input, { key: 'Escape' })
    expect(onclose).toHaveBeenCalledTimes(1)
    await fireEvent.click(screen.getByTestId('palette-backdrop'))
    expect(onclose).toHaveBeenCalledTimes(2)
    await fireEvent.click(screen.getByRole('dialog'))
    expect(onclose).toHaveBeenCalledTimes(2)
    expect(runs).toEqual([])
  })

  it('says so when nothing matches', async () => {
    const { input } = setup()
    await fireEvent.input(input, { target: { value: 'zzz' } })
    expect(screen.queryAllByRole('option')).toHaveLength(0)
    expect(screen.getByText('No matching commands')).toBeDefined()
  })
})
