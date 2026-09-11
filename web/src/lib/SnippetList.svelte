<script lang="ts">
  import { searchShortcutLabel } from './keys'
  import { formatAbsolute, formatDate } from './time'
  import type { Snippet } from './types'

  let {
    snippets,
    selectedId = null,
    query = '',
    offline = false,
    twoLine = false,
    searchEl = $bindable<HTMLInputElement | undefined>(undefined),
    onselect,
    onsearch,
    oncreate,
  }: {
    snippets: Snippet[]
    selectedId?: string | null
    query?: string
    /** Offline: create is unavailable (spec §6). */
    offline?: boolean
    /** Wrap titles onto a second line instead of truncating at one. */
    twoLine?: boolean
    /**
     * The search input itself, exposed so the app can focus it from the
     * global shortcut (spec §6 keyboard discipline).
     */
    searchEl?: HTMLInputElement | undefined
    onselect: (id: string) => void
    onsearch: (q: string) => void
    oncreate: () => void
  } = $props()

  let listEl = $state<HTMLElement | undefined>()

  // Arrow-key selection happens while focus stays in the search field, so
  // the selected row can be off-screen. 'nearest' makes this a no-op for a
  // mouse click on a row that is already visible.
  $effect(() => {
    const id = selectedId
    if (id === null) return
    const row = listEl?.querySelector(`[data-id="${id}"]`)
    row?.scrollIntoView?.({ block: 'nearest' })
  })
</script>

<div class="snippet-list" class:two-line={twoLine}>
  <div class="toolbar">
    <div class="search-wrap">
      <input
        bind:this={searchEl}
        type="search"
        class="search"
        placeholder="Search: caddy tag:ops lang:go"
        value={query}
        oninput={(e) => onsearch(e.currentTarget.value)}
        aria-label="Search snippets"
      />
      <!-- Decorative: the field's aria-label is its accessible name, and the
           hint only repeats what the keydown handler accepts. -->
      <kbd class="hint" aria-hidden="true">{searchShortcutLabel()}</kbd>
    </div>
    <button class="new" disabled={offline} onclick={oncreate}>New snippet</button>
  </div>
  <ul class="items" bind:this={listEl}>
    {#each snippets as s (s.id)}
      <li>
        <button
          class="item"
          class:selected={s.id === selectedId}
          data-id={s.id}
          onclick={() => onselect(s.id)}
        >
          <!-- The full title is on hover however it is clamped, since a
               truncated title is otherwise distinguishable only by opening
               the snippet. -->
          <span class="title" title={s.title}>{s.title}</span>
          <span class="meta">
            {#if s.language !== ''}<span class="lang">{s.language}</span>{/if}
            {#each s.tags.slice(0, 3) as t (t)}
              <span class="tag">#{t}</span>
            {/each}
            {#if s.is_sensitive}<span class="lock" title="Sensitive">🔒</span>{/if}
            <span class="when" title={formatAbsolute(s.updated_at)}
              >{formatDate(s.updated_at)}</span
            >
          </span>
        </button>
      </li>
    {:else}
      <li class="empty">{query.trim() !== '' ? 'No matches' : 'No snippets yet'}</li>
    {/each}
  </ul>
</div>
