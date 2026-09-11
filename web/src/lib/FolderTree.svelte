<script lang="ts">
  import type { Folder } from './types'

  let {
    folders,
    selectedId = null,
    offline = false,
    onselect,
    oncreate,
    onrename,
    onremove,
  }: {
    folders: Folder[]
    selectedId?: string | null
    /** Offline: folder writes are unavailable (spec §6). */
    offline?: boolean
    onselect: (id: string | null) => void
    oncreate: (parentId: string | null) => void
    onrename: (id: string, name: string) => void
    onremove: (id: string) => void
  } = $props()

  let renamingId: string | null = $state(null)
  let renameValue = $state('')
  let collapsed = $state<Record<string, boolean>>({})

  /** Flat, depth-annotated rows in tree order (collapsed branches skipped). */
  const rows = $derived.by(() => {
    const byParent = new Map<string | null, Folder[]>()
    for (const f of folders) {
      const list = byParent.get(f.parent_id)
      if (list) list.push(f)
      else byParent.set(f.parent_id, [f])
    }
    const out: { folder: Folder; depth: number }[] = []
    const walk = (parentId: string | null, depth: number): void => {
      for (const f of byParent.get(parentId) ?? []) {
        out.push({ folder: f, depth })
        if (!collapsed[f.id]) walk(f.id, depth + 1)
      }
    }
    walk(null, 0)
    return out
  })

  const childCount = $derived.by(() => {
    const m = new Map<string, number>()
    for (const f of folders) {
      if (f.parent_id !== null) m.set(f.parent_id, (m.get(f.parent_id) ?? 0) + 1)
    }
    return m
  })

  function startRename(f: Folder): void {
    if (offline) return
    renamingId = f.id
    renameValue = f.name
  }

  function commitRename(): void {
    if (renamingId !== null && renameValue.trim() !== '') {
      onrename(renamingId, renameValue.trim())
    }
    renamingId = null
  }

  function toggle(id: string): void {
    collapsed[id] = !collapsed[id]
  }
</script>

<div class="folder-tree">
  <button class="all" class:selected={selectedId === null} onclick={() => onselect(null)}>
    All snippets
  </button>
  {#if folders.length === 0}
    <p class="hint">No folders yet</p>
  {/if}
  {#each rows as { folder, depth } (folder.id)}
    <div class="row" style="padding-left: {8 + depth * 14}px">
      {#if (childCount.get(folder.id) ?? 0) > 0}
        <button
          class="caret"
          aria-label={collapsed[folder.id]
            ? 'Expand ' + folder.name
            : 'Collapse ' + folder.name}
          onclick={() => toggle(folder.id)}
        >
          {collapsed[folder.id] ? '▸' : '▾'}
        </button>
      {:else}
        <span class="caret" aria-hidden="true">·</span>
      {/if}
      {#if renamingId === folder.id}
        <input
          class="rename"
          value={renameValue}
          oninput={(e) => (renameValue = e.currentTarget.value)}
          onkeydown={(e) => {
            if (e.key === 'Enter') commitRename()
            else if (e.key === 'Escape') renamingId = null
          }}
          onblur={commitRename}
        />
      {:else}
        <button
          class="name"
          class:selected={selectedId === folder.id}
          onclick={() => onselect(folder.id)}
        >
          {folder.name}
        </button>
      {/if}
      <span class="actions">
        <button
          title="New subfolder"
          aria-label="New subfolder in {folder.name}"
          disabled={offline}
          onclick={() => oncreate(folder.id)}
        >+</button>
        <button
          title="Rename"
          aria-label="Rename {folder.name}"
          disabled={offline}
          onclick={() => startRename(folder)}
        >✎</button>
        <button
          title="Delete"
          aria-label="Delete {folder.name}"
          disabled={offline}
          onclick={() => onremove(folder.id)}
        >✕</button>
      </span>
    </div>
  {/each}
</div>
