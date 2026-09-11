<script lang="ts">
  import type { Snippet } from './types'

  let {
    snippets,
    selectedId = null,
    query = '',
    offline = false,
    onselect,
    onsearch,
    oncreate,
  }: {
    snippets: Snippet[]
    selectedId?: string | null
    query?: string
    /** Offline: create is unavailable (spec §6). */
    offline?: boolean
    onselect: (id: string) => void
    onsearch: (q: string) => void
    oncreate: () => void
  } = $props()

  function fmtWhen(iso: string): string {
    return new Date(iso).toLocaleDateString(undefined, {
      month: 'short',
      day: 'numeric',
    })
  }
</script>

<div class="snippet-list">
  <div class="toolbar">
    <input
      type="search"
      class="search"
      placeholder="Search: caddy tag:ops lang:go"
      value={query}
      oninput={(e) => onsearch(e.currentTarget.value)}
      aria-label="Search snippets"
    />
    <button class="new" disabled={offline} onclick={oncreate}>New</button>
  </div>
  <ul class="items">
    {#each snippets as s (s.id)}
      <li>
        <button
          class="item"
          class:selected={s.id === selectedId}
          onclick={() => onselect(s.id)}
        >
          <span class="title">{s.title}</span>
          <span class="meta">
            {#if s.language !== ''}<span class="lang">{s.language}</span>{/if}
            {#each s.tags.slice(0, 3) as t (t)}
              <span class="tag">#{t}</span>
            {/each}
            {#if s.is_sensitive}<span class="lock" title="Sensitive">🔒</span>{/if}
            <span class="when">{fmtWhen(s.updated_at)}</span>
          </span>
        </button>
      </li>
    {:else}
      <li class="empty">{query.trim() !== '' ? 'No matches' : 'No snippets yet'}</li>
    {/each}
  </ul>
</div>
