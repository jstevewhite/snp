import { describe, expect, it } from 'vitest'
import { filterCommands, type Command } from './commands'

function cmd(label: string, extra: Partial<Command> = {}): Command {
  return { id: label.toLowerCase().replace(/\s+/g, '-'), label, group: 'app', run: () => {}, ...extra }
}

describe('filterCommands', () => {
  it('returns every command in its original order for an empty query', () => {
    const list = [cmd('New snippet'), cmd('Resync'), cmd('Show body')]
    expect(filterCommands(list, '').map((c) => c.label)).toEqual(['New snippet', 'Resync', 'Show body'])
    expect(filterCommands(list, '   ').map((c) => c.label)).toEqual(['New snippet', 'Resync', 'Show body'])
  })

  it('keeps commands whose label contains the query, case-insensitively', () => {
    const list = [cmd('New snippet'), cmd('Resync'), cmd('Full resync')]
    expect(filterCommands(list, 'SYNC').map((c) => c.label)).toEqual(['Resync', 'Full resync'])
  })

  it('ranks a word-prefix match ahead of a substring match', () => {
    const list = [cmd('Unfavorite'), cmd('Favorite')]
    expect(filterCommands(list, 'fav').map((c) => c.label)).toEqual(['Favorite', 'Unfavorite'])
  })

  it('requires every whitespace-separated token to match', () => {
    const list = [cmd('Resync'), cmd('Full resync')]
    expect(filterCommands(list, 'full re').map((c) => c.label)).toEqual(['Full resync'])
    expect(filterCommands(list, 'full nope')).toEqual([])
  })

  it('keeps disabled commands so the reason stays visible', () => {
    const list = [cmd('Edit snippet', { disabled: 'Select a snippet first' })]
    expect(filterCommands(list, 'edit')).toHaveLength(1)
  })
})
