import type { Folder } from './types'

/** Full paths disambiguate equal folder names in different branches. */
export function folderOptions(folders: Folder[]): { id: string; path: string }[] {
  const byId = new Map(folders.map((folder) => [folder.id, folder]))
  return folders.map((folder) => {
    const names: string[] = []
    const seen = new Set<string>()
    let current: Folder | undefined = folder
    while (current && !seen.has(current.id)) {
      seen.add(current.id)
      names.unshift(current.name)
      current = current.parent_id ? byId.get(current.parent_id) : undefined
    }
    return { id: folder.id, path: names.join(' / ') }
  }).sort((a, b) => a.path.localeCompare(b.path))
}
