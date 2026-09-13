<script lang="ts">
  import { filterCommands, type Command } from './commands'

  let {
    commands,
    onclose,
  }: {
    /** Every command, in display order; disabled ones stay listed. */
    commands: Command[]
    /** Called when the palette should go away: Escape, backdrop, or after a command ran. */
    onclose: () => void
  } = $props()

  let query = $state('')
  let index = $state(0)
  let input: HTMLInputElement | undefined = $state()

  const visible = $derived(filterCommands(commands, query))

  // Focus on open; the filter field is the palette's only input.
  $effect(() => {
    input?.focus()
  })

  // A new query invalidates the highlight (the row it pointed at may be gone).
  $effect(() => {
    void query
    index = 0
  })

  function move(delta: 1 | -1): void {
    const n = visible.length
    if (n === 0) return
    index = (index + delta + n) % n
  }

  function run(c: Command): void {
    if (c.disabled !== undefined) return
    onclose()
    c.run()
  }

  function onkeydown(e: KeyboardEvent): void {
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      move(1)
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      move(-1)
    } else if (e.key === 'Enter') {
      e.preventDefault()
      const c = visible[index]
      if (c !== undefined) run(c)
    } else if (e.key === 'Escape') {
      e.preventDefault()
      onclose()
    }
  }
</script>

<!-- Command palette (spec §6 keyboard): Cmd/Ctrl+Shift+P. Reuses the dialog
     shell; unlike the confirm dialogs it has no Cancel button, so a scrim
     button behind the panel closes it (the compact drawer's pattern). -->
<div class="modal-backdrop palette-backdrop">
  <button
    class="palette-scrim"
    data-testid="palette-backdrop"
    aria-label="Close commands"
    tabindex="-1"
    onclick={onclose}
  ></button>
  <div class="modal palette" role="dialog" aria-modal="true" aria-label="Commands" tabindex="-1">
    <input
      bind:this={input}
      bind:value={query}
      type="text"
      role="combobox"
      aria-label="Command"
      aria-expanded="true"
      aria-controls="palette-list"
      aria-activedescendant={visible[index] ? `palette-${visible[index].id}` : undefined}
      aria-autocomplete="list"
      placeholder="Type a command…"
      autocomplete="off"
      spellcheck="false"
      onkeydown={onkeydown}
    />
    {#if visible.length === 0}
      <p class="palette-empty">No matching commands</p>
    {:else}
      <ul id="palette-list" class="palette-list" role="listbox" aria-label="Commands">
        {#each visible as c, i (c.id)}
          <!-- svelte-ignore a11y_click_events_have_key_events -->
          <li
            id="palette-{c.id}"
            role="option"
            aria-selected={i === index}
            aria-disabled={c.disabled !== undefined ? 'true' : undefined}
            class:active={i === index}
            class:disabled={c.disabled !== undefined}
            onmousemove={() => (index = i)}
            onclick={() => run(c)}
          >
            <span class="label">{c.label}</span>
            {#if c.disabled !== undefined}
              <span class="reason">{c.disabled}</span>
            {:else if c.shortcut !== undefined}
              <kbd>{c.shortcut}</kbd>
            {/if}
          </li>
        {/each}
      </ul>
    {/if}
  </div>
</div>
