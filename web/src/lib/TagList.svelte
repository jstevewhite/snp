<script lang="ts">
  /** One clickable tag with its snippet count. */
  export interface TagItem {
    name: string
    count: number
  }

  let {
    tags,
    active = [],
    onselect,
  }: {
    /** All tags with live-snippet counts, most-used first. */
    tags: TagItem[]
    /** Currently active (filtering) tag names. */
    active: string[]
    /** Called with the tag name when it is clicked (toggle). */
    onselect: (name: string) => void
  } = $props()

  const activeSet = $derived(new Set(active))
</script>

<div class="tag-list">
  <div class="tag-head">
    <h2>Tags</h2>
  </div>
  {#if tags.length === 0}
    <p class="hint">No tags yet</p>
  {:else}
    {#each tags as t (t.name)}
      <button
        class="tag"
        class:active={activeSet.has(t.name)}
        aria-pressed={activeSet.has(t.name)}
        aria-label={`Filter by tag ${t.name}`}
        title={activeSet.has(t.name)
          ? `${t.name}: active — click to clear`
          : `Filter by ${t.name}`}
        onclick={() => onselect(t.name)}
      >
        <span class="tag-name">{t.name}</span>
        <span class="tag-count">{t.count}</span>
      </button>
    {/each}
    {#if active.length > 0}
      <p class="tag-active-hint">
        Showing snippets with {active.join(', ')} — click a tag to clear it.
      </p>
    {/if}
  {/if}
</div>
