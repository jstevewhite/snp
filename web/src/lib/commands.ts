/**
 * Command palette model (spec §6 "Keyboard").
 *
 * A Command is one action the palette can run. The list is assembled by
 * App.svelte from the actions it already has; this module only defines
 * the shape and the pure filtering/ranking, so it can be tested without
 * a component.
 */

export type CommandGroup = 'snippet' | 'folder' | 'app'

export interface Command {
  id: string
  label: string
  group: CommandGroup
  /** Shortcut hint shown on the right, already platform-labelled. */
  shortcut?: string
  /**
   * Why the command cannot run right now, e.g. "Offline" or "Select a
   * snippet first". A disabled command stays listed, greyed, with this
   * reason beside it; undefined means it can run.
   */
  disabled?: string
  run: () => void
}

/**
 * Commands whose label matches every whitespace-separated token of the
 * query, case-insensitively. A command whose label has a word starting
 * with the first token ranks ahead of one that merely contains it; ties
 * keep the input order. An empty query returns the list unchanged.
 */
export function filterCommands(commands: Command[], query: string): Command[] {
  const tokens = query.toLowerCase().split(/\s+/).filter((t) => t !== '')
  if (tokens.length === 0) return commands.slice()
  const scored: { c: Command; rank: number; i: number }[] = []
  commands.forEach((c, i) => {
    const label = c.label.toLowerCase()
    if (!tokens.every((t) => label.includes(t))) return
    const words = label.split(/\s+/)
    const rank = words.some((w) => w.startsWith(tokens[0])) ? 0 : 1
    scored.push({ c, rank, i })
  })
  scored.sort((a, b) => a.rank - b.rank || a.i - b.i)
  return scored.map((s) => s.c)
}
