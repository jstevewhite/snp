<script lang="ts">
  import CopyButton from './CopyButton.svelte'
  import { highlightBody } from './highlight'
  import { renderMarkdown } from './markdown'
  import { formatAbsolute, formatDate } from './time'
  import type { Snippet } from './types'
  import {
    extractTemplateVars,
    previewTemplate,
    renderTemplate,
    type TemplateVar,
  } from './templates'

  let {
    snippet,
    body = null,
    defaults = snippet.var_defaults ?? {},
    folderName = null,
    offline = false,
    saving = false,
    oncopy,
    onedit,
    onremove,
    onreveal,
    onpin,
    onsavedefaults,
    oncopytext,
  }: {
    snippet: Snippet
    /** Revealed body for sensitive snippets (fetched on demand, never cached). */
    body?: string | null
    defaults?: Record<string, string>
    folderName?: string | null
    /** Offline: server writes and sensitive reveals are unavailable (spec §6). */
    offline?: boolean
    saving?: boolean
    /**
     * Writes the given text to the clipboard (the rendered body). A
     * returned promise lets the copy buttons report a failed write
     * instead of claiming success.
     */
    oncopy: (text: string) => void | Promise<void>
    onedit: () => void
    onremove: () => void
    onreveal: () => void
    /** Toggles the snippet's pinned (favorite) flag (spec §4). */
    onpin: () => void
    /**
     * Persists per-variable defaults (spec §6); keys are client-owned.
     * Resolves true only when the server accepted them, so the button
     * reports success for a write that actually happened.
     */
    onsavedefaults: (defaults: Record<string, string>) => Promise<boolean>
    /**
     * Publishes the text the Copy buttons would write, so the app-level
     * copy shortcut writes exactly the same thing — variable values typed
     * into the panel included (spec §4).
     */
    oncopytext?: (id: string, text: string | null) => void
  } = $props()

  /** True when the body is not available locally (sensitive, not revealed). */
  const hidden = $derived(snippet.body === null && body === null)
  const shown = $derived(snippet.body ?? body ?? '')

  /** A body longer than this many lines collapses to a scrollable box. */
  const MAX_BODY_LINES = 10
  /** Line count, ignoring a single trailing newline. */
  function lineCount(text: string): number {
    return text === '' ? 0 : text.replace(/\n$/, '').split('\n').length
  }
  // Session-only: expands the read view to the body's full height. The
  // component is keyed by snippet id in App.svelte, so this resets on each
  // selection and is never persisted.
  let bodyExpanded = $state(false)
  const bodyLines = $derived(lineCount(shown))
  /** Long enough to offer the Show all / Show less control. */
  const bodyLong = $derived(!hidden && bodyLines > MAX_BODY_LINES)
  /** True while the code box is height-capped. */
  const bodyCapped = $derived(bodyLong && !bodyExpanded)

  /** Highlighted body HTML, or null while unknown/loading/unsupported
   * (the plain body renders in that case). */
  let highlighted = $state<string | null>(null)
  $effect(() => {
    const code = shown
    const lang = snippet.language
    const isHidden = snippet.body === null && body === null
    let cancelled = false
    highlighted = null
    if (code === '' || isHidden) return
    void highlightBody(code, lang).then((html) => {
      if (!cancelled) highlighted = html
    })
    return () => {
      cancelled = true
    }
  })

  // Template variables in the body, in order of first appearance (spec §4).
  const vars = $derived<TemplateVar[]>(extractTemplateVars(shown))
  /** Show the panel when the snippet is a template and its body is shown. */
  const showVars = $derived(snippet.uses_variables && !hidden && vars.length > 0)

  // Reveal can deliver saved defaults after mount. Only explicit typing
  // overrides them; untouched fields follow the current server values.
  let overrides = $state<Record<string, string>>({})
  const values = $derived(Object.fromEntries(vars.map((v) => [
    v.name, overrides[v.name] ?? defaults[v.name] ?? '',
  ])))
  $effect(() => { if (hidden) overrides = {} })

  /** Live preview: unfilled vars without a default stay visible. */
  const preview = $derived(showVars ? previewTemplate(shown, values) : shown)
  /** What the copy buttons write for the rendered form: blank/missing
   * values fall back to the default, and a var without a default renders
   * as an empty string (spec §4). The preview above deliberately differs —
   * it keeps unfilled placeholders visible — so the copied text is the
   * filled-in command, never a half-substituted one. */
  const copyText = $derived(showVars ? renderTemplate(shown, values) : shown)

  // Publish what Copy writes, so the app's copy shortcut agrees with the
  // button instead of re-deriving the text from the cached row. The id
  // rides along because the app keys this component by snippet: without it
  // the app could not tell a stale publication from the current one.
  $effect(() => {
    oncopytext?.(snippet.id, hidden ? null : copyText)
  })

  // The Rendered preview collapses on the same rule, but independently: a
  // short template can render long (a variable holding many lines), and an
  // expanded body must not force the preview open (or the reverse). Also
  // session-only and reset per selection.
  let renderedExpanded = $state(false)
  const renderedLong = $derived(showVars && lineCount(preview) > MAX_BODY_LINES)
  const renderedCapped = $derived(renderedLong && !renderedExpanded)

  function setValue(name: string, value: string): void {
    overrides[name] = value
  }

  /**
   * The map saveDefaults would persist (spec §4/§6): a blank input clears
   * that default, and keys for variables no longer in the body are dropped.
   * The server treats the map opaquely.
   */
  function cleanedValues(): Record<string, string> {
    const names = new Set(vars.map((v) => v.name))
    const out: Record<string, string> = {}
    for (const [name, value] of Object.entries(values)) {
      if (names.has(name) && value !== '') out[name] = value
    }
    return out
  }

  function sameDefaults(a: Record<string, string>, b: Record<string, string>): boolean {
    const keys = Object.keys(a)
    return keys.length === Object.keys(b).length && keys.every((k) => a[k] === b[k])
  }

  /**
   * What the server holds, in the same cleaned shape. Follows refreshed
   * defaults and advances on accepted saves; a failure keeps the old baseline.
   */
  let savedBaseline = $state<Record<string, string>>({})
  $effect(() => {
    savedBaseline = Object.fromEntries(vars
      .filter((v) => defaults[v.name] !== undefined && defaults[v.name] !== '')
      .map((v) => [v.name, defaults[v.name]]))
  })
  let defaultsSaving = $state(false)

  /** Inert until the inputs differ from what is stored. */
  const defaultsDirty = $derived(!sameDefaults(cleanedValues(), savedBaseline))

  /** Transient result shown next to the button. */
  type DefaultsStatus = 'idle' | 'saved' | 'failed'
  let defaultsStatus = $state<DefaultsStatus>('idle')
  let defaultsTimer: ReturnType<typeof setTimeout> | undefined
  const SAVED_FLASH_MS = 1500

  /** Persist the current inputs as the snippet's saved defaults (spec §4/§6). */
  async function saveDefaults(): Promise<void> {
    // The button is disabled in these cases; the guard keeps a
    // programmatic click from writing anyway.
    if (offline || saving || !defaultsDirty || defaultsSaving) return
    defaultsSaving = true
    const next = cleanedValues()
    let ok = false
    try { ok = await onsavedefaults(next) } catch { ok = false } finally { defaultsSaving = false }
    if (ok) {
      savedBaseline = next
      defaultsStatus = 'saved'
    } else {
      defaultsStatus = 'failed'
    }
    clearTimeout(defaultsTimer)
    defaultsTimer = setTimeout(() => (defaultsStatus = 'idle'), SAVED_FLASH_MS)
  }

  // Drop a pending revert if the component unmounts.
  $effect(() => () => clearTimeout(defaultsTimer))
</script>

<div class="detail">
  <header>
    <div class="title-row">
      <h1>{snippet.title}</h1>
      <!-- Pin: the Favorites list above the folder tree reads this flag
           (spec §4). aria-pressed carries the state, so the label stays a
           description of the action. -->
      <button
        class="pin"
        class:pinned={snippet.pinned === true}
        type="button"
        aria-pressed={snippet.pinned === true}
        aria-label={snippet.pinned === true ? 'Remove from favorites' : 'Add to favorites'}
        title={snippet.pinned === true ? 'Remove from favorites' : 'Add to favorites'}
        disabled={offline || saving}
        onclick={onpin}
      >
        <svg class="icon" viewBox="0 0 24 24" aria-hidden="true">
          <path d="M12 17v5" />
          <path
            d="M9 10.76a2 2 0 0 1-1.11 1.79l-1.78.9A2 2 0 0 0 5 15.24V16a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1v-.76a2 2 0 0 0-1.11-1.79l-1.78-.9A2 2 0 0 1 15 10.76V7a1 1 0 0 1 1-1 2 2 0 0 0 0-4H8a2 2 0 0 0 0 4 1 1 0 0 1 1 1z"
          />
        </svg>
      </button>
      <button
        class="trash"
        type="button"
        aria-label="Delete snippet"
        title="Delete snippet"
        disabled={offline || saving}
        onclick={onremove}
      >
        <svg class="icon" viewBox="0 0 24 24" aria-hidden="true">
          <path d="M3 6h18" />
          <path d="M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" />
          <path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6" />
          <path d="M10 11v6" />
          <path d="M14 11v6" />
        </svg>
      </button>
    </div>
    <!-- Flat SVG icons: language is the "</>" code glyph (Lucide code-xml
         paths), folder is Feather's folder outline. Both 24x24 with 2-unit
         strokes; app.css styles them with currentColor so they inherit the
         theme text color. Decorative: aria-hidden, the value follows. -->
    <div class="meta">
      {#if snippet.language !== ''}
        <span class="lang">
          <svg class="icon" viewBox="0 0 24 24" aria-hidden="true">
            <path d="m18 16 4-4-4-4" />
            <path d="m6 8-4 4 4 4" />
            <path d="m14.5 4-5 16" />
          </svg>
          {snippet.language}
        </span>
      {/if}
      {#if folderName !== null}
        <span class="folder">
          <svg class="icon" viewBox="0 0 24 24" aria-hidden="true">
            <path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z" />
          </svg>
          {folderName}
        </span>
      {/if}
      {#if snippet.tags.length > 0}
        <span class="tags" class:divided={snippet.language !== '' || folderName !== null}>
          {#each snippet.tags as t (t)}
            <span class="tag">#{t}</span>
          {/each}
        </span>
      {/if}
    </div>
  </header>

  {#if snippet.is_sensitive}
    <p class="sensitive">
      <svg class="icon" viewBox="0 0 24 24" aria-hidden="true">
        <rect x="3" y="11" width="18" height="11" rx="2" ry="2" />
        <path d="M7 11V7a5 5 0 0 1 10 0v4" />
      </svg>
      Sensitive — the body is not cached locally.
    </p>
  {/if}

  <div class="box">
    {#if showVars}
      <!-- The original and the copy of it sit together: Copy template
           reuses the placeholders, while Copy rendered (below) fills them. -->
      <div class="box-head">
        <span class="box-label">Template</span>
        <CopyButton small label="Copy template" text={shown} {oncopy} />
      </div>
    {:else}
      <!-- A non-template body still carries its own copy action, so the
           pane needs no footer Copy button. Inert while a sensitive body
           is hidden. -->
      <div class="box-head">
        <CopyButton small label="Copy snippet" text={copyText} disabled={hidden} {oncopy} />
      </div>
    {/if}
    {#if hidden}
      <div class="reveal-block">
        <button class="reveal" disabled={offline || saving} onclick={onreveal}>Show body</button>
        {#if offline}
          <p class="reveal-hint">Online connection required to show the body.</p>
        {/if}
      </div>
    {:else if highlighted !== null}
      <pre class="body hljs" class:clamped={bodyCapped}><code>{@html highlighted}</code></pre>
    {:else}
      <pre class="body" class:clamped={bodyCapped}>{shown}</pre>
    {/if}
    {#if bodyLong}
      <button
        class="show-all"
        type="button"
        aria-expanded={bodyExpanded}
        onclick={() => (bodyExpanded = !bodyExpanded)}
      >
        {bodyExpanded ? 'Show less' : 'Show all'}
      </button>
    {/if}
  </div>

  {#if showVars}
    <section class="vars">
      <h2>Variables</h2>
      {#each vars as v (v.name)}
        <label class="var">
          <span>{v.name}</span>
          <input
            value={values[v.name] ?? ''}
            placeholder={v.defaultValue ?? undefined}
            oninput={(e) => setValue(v.name, (e.currentTarget as HTMLInputElement).value)}
          />
        </label>
      {/each}
      <div class="box">
        <div class="box-head">
          <span class="box-label">Rendered</span>
          <CopyButton small label="Copy rendered" text={copyText} {oncopy} />
        </div>
        <pre class="preview" class:clamped={renderedCapped}>{preview}</pre>
        {#if renderedLong}
          <button
            class="show-all"
            type="button"
            aria-expanded={renderedExpanded}
            onclick={() => (renderedExpanded = !renderedExpanded)}
          >
            {renderedExpanded ? 'Show less' : 'Show all'}
          </button>
        {/if}
      </div>
      <div class="vars-actions">
        <button
          class="save-defaults"
          disabled={offline || saving || !defaultsDirty || defaultsSaving}
          onclick={() => void saveDefaults()}
        >
          Save defaults
        </button>
        {#if defaultsStatus !== 'idle'}
          <span
            class="defaults-note"
            class:failed={defaultsStatus === 'failed'}
            role="status"
          >
            {defaultsStatus === 'saved' ? 'Defaults saved' : 'Save failed'}
          </span>
        {/if}
      </div>
    </section>
  {/if}

  <footer class="actions">
    <button class="edit" disabled={offline || saving} onclick={onedit}>Edit</button>
  </footer>

  {#if snippet.notes !== ''}
    <section class="notes">
      <h2>Notes</h2>
      <div class="notes-body">
        {@html renderMarkdown(snippet.notes)}
      </div>
    </section>
  {/if}

  <div class="when" title={formatAbsolute(snippet.updated_at)}>
    Updated {formatDate(snippet.updated_at)}
  </div>
</div>
