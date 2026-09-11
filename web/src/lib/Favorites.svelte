<script lang="ts">
  import type { Snippet } from './types'

  let {
    snippets,
    selectedId = null,
    offline = false,
    onselect,
    onunpin,
  }: {
    /** The pinned snippets, already ordered newest first. */
    snippets: Snippet[]
    selectedId?: string | null
    /** Offline: unpinning is a server write (spec §6). */
    offline?: boolean
    onselect: (id: string) => void
    onunpin: (id: string) => void
  } = $props()
</script>

<!-- Pinned snippets above the folder tree (spec §4). The list stays visible
     when empty: it is where the pin button in the detail pane leads, so
     hiding it would leave the feature undiscoverable. -->
<div class="favorites">
  <h3>Favorites</h3>
  {#if snippets.length === 0}
    <p class="hint">Pin a snippet from its page to keep it here.</p>
  {:else}
    <ul>
      {#each snippets as s (s.id)}
        <li>
          <button
            class="name"
            class:selected={s.id === selectedId}
            title={s.title}
            onclick={() => onselect(s.id)}
          >
            {s.title}
          </button>
          <button
            class="unpin"
            title="Remove from favorites"
            aria-label="Remove {s.title} from favorites"
            disabled={offline}
            onclick={() => onunpin(s.id)}
          >
            ✕
          </button>
        </li>
      {/each}
    </ul>
  {/if}
</div>
