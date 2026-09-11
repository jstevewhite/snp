<script lang="ts">
  import * as api from './api'
  import type { AIKind } from './api'
  import type { Folder, Snippet, SnippetInput } from './types'
  import { extractTemplateVars, hasTemplateVars } from './templates'

  let {
    initial = null,
    folders,
    defaultFolderId = null,
    onsave,
    oncancel,
  }: {
    /** null → create mode; a snippet → edit mode. */
    initial?: Snippet | null
    folders: Folder[]
    defaultFolderId?: string | null
    onsave: (input: SnippetInput) => void
    oncancel: () => void
  } = $props()

  // The form is remounted for each snippet (App.svelte passes `key`), so the
  // fields are seeded from `initial` exactly once, at mount; afterwards the
  // form owns its own state. The one-time capture of the prop is intentional.
  // svelte-ignore state_referenced_locally
  const seed = {
    title: initial?.title ?? '',
    language: initial?.language ?? '',
    tags: initial?.tags.join(', ') ?? '',
    folder: initial?.folder_id ?? defaultFolderId ?? '',
    body: initial?.body ?? '',
    notes: initial?.notes ?? '',
    sensitive: initial?.is_sensitive ?? false,
    usesVariables: initial?.uses_variables ?? false,
    varDefaults: initial?.var_defaults ?? {},
  }

  let title = $state(seed.title)
  let language = $state(seed.language)
  let tagsText = $state(seed.tags)
  let folderId = $state<string>(seed.folder)
  let body = $state(seed.body)
  let notes = $state(seed.notes)
  let isSensitive = $state(seed.sensitive)
  let usesVariables = $state(seed.usesVariables)

  // A body containing {{var}} placeholders is a template (spec §4). Keep the
  // flag in sync with the body so it can't drift from the content; a manual
  // toggle holds until the next body edit.
  $effect(() => {
    usesVariables = hasTemplateVars(body)
  })

  const isEdit = $derived(initial !== null)
  // '{{' in markup would be parsed as an expression, so these are strings.
  const templateLabel = 'Template — body contains {{var}} placeholders'
  const noVarsHint = 'No {{var}} placeholders detected in the body.'
  /** Variable names currently detected in the body, for the hint below
   * the Template checkbox. */
  const detectedVars = $derived(extractTemplateVars(body).map((v) => v.name))

  // Ask-AI (spec §13): one-shot generation that fills the form for
  // review; saving still goes through the normal snippet endpoints, so
  // AI output never lands in the store un-reviewed. Hidden entirely
  // when the server reports the feature unconfigured.
  let aiEnabled = $state(false)
  let aiKnown = $state(false)
  let aiPrompt = $state('')
  // What the model should produce (spec §13): one command (default), a
  // multi-line script, or a function definition. Picking the shape
  // swaps the server-side system prompt; the reply still lands in the
  // form for review.
  let aiKind = $state<AIKind>('command')
  let aiBusy = $state(false)
  let aiError: string | null = $state(null)
  let aiDone: string | null = $state(null)

  // Suggest tags (spec §13): 2-3 relevant tags from the model, merged
  // into the Tags field without clobbering what is already typed.
  let tagsBusy = $state(false)
  let tagsError: string | null = $state(null)

  // Explain (spec §13): one-shot explanation of the body, appended to
  // the Notes field.
  let explainBusy = $state(false)
  let explainError: string | null = $state(null)

  $effect(() => {
    let cancelled = false
    api
      .aiStatus()
      .then((st) => {
        if (!cancelled) {
          aiKnown = true
          aiEnabled = st.enabled
        }
      })
      .catch(() => {
        if (!cancelled) aiKnown = true
      })
    return () => {
      cancelled = true
    }
  })

  async function askAI(): Promise<void> {
    const prompt = aiPrompt.trim()
    if (prompt === '' || aiBusy) return
    aiBusy = true
    aiError = null
    aiDone = null
    try {
      const res = await api.generateSnippet({
        prompt,
        language: language.trim() === '' ? undefined : language.trim(),
        kind: aiKind,
      })
      if (res.body === '') throw new Error('AI returned an empty snippet')
      if (res.title !== '') title = res.title
      if (res.language !== '') language = res.language
      body = res.body
      // The model's explanation of the command goes into Notes (spec
      // §13): bare command in Body, explanation alongside.
      if (res.notes !== undefined && res.notes.trim() !== '') notes = res.notes
      aiDone = 'Generated — review and save.'
    } catch (e) {
      aiError = e instanceof Error ? e.message : String(e)
    } finally {
      aiBusy = false
    }
  }

  async function suggestTags(): Promise<void> {
    if (tagsBusy || body.trim() === '') return
    tagsBusy = true
    tagsError = null
    try {
      const res = await api.suggestTags({
        body,
        title: title.trim() === '' ? undefined : title.trim(),
        language: language.trim() === '' ? undefined : language.trim(),
      })
      const current = tagsText
        .split(',')
        .map((t) => t.trim().toLowerCase())
        .filter((t) => t !== '')
      for (const t of res.tags) {
        const tl = t.toLowerCase()
        if (!current.includes(tl)) current.push(tl)
      }
      tagsText = current.join(', ')
    } catch (e) {
      tagsError = e instanceof Error ? e.message : String(e)
    } finally {
      tagsBusy = false
    }
  }

  async function explain(): Promise<void> {
    if (explainBusy || body.trim() === '') return
    explainBusy = true
    explainError = null
    try {
      const res = await api.explainSnippet(body)
      if (res.notes.trim() === '') throw new Error('AI returned an empty explanation')
      const existing = notes.trim()
      notes = existing === '' ? res.notes : existing + '\n\n' + res.notes
    } catch (e) {
      explainError = e instanceof Error ? e.message : String(e)
    } finally {
      explainBusy = false
    }
  }

  function submit(): void {
    if (title.trim() === '') return
    // A full replace wipes var_defaults (spec §5), so carry the saved
    // defaults forward, pruned to the variables the body still uses (spec
    // §4): editing the body must not silently drop them, and a body with
    // no variables keeps an empty map.
    const names = new Set(extractTemplateVars(body).map((v) => v.name))
    const defaults: Record<string, string> = {}
    for (const [name, value] of Object.entries(seed.varDefaults)) {
      if (names.has(name) && value !== '') defaults[name] = value
    }
    onsave({
      title: title.trim(),
      body,
      language: language.trim(),
      notes: notes.trim(),
      folder_id: folderId === '' ? null : folderId,
      tags: tagsText
        .split(',')
        .map((t) => t.trim())
        .filter((t) => t !== ''),
      is_sensitive: isSensitive,
      uses_variables: usesVariables,
      var_defaults: defaults,
    })
  }
</script>

<form class="form" onsubmit={(e) => { e.preventDefault(); submit() }}>
  {#if aiKnown && aiEnabled}
    <details class="ai">
      <summary>Ask AI…</summary>
      <textarea
        bind:value={aiPrompt}
        rows="2"
        placeholder="e.g. give me a command to copy a file to my home dir"
        aria-label="AI prompt"
      ></textarea>
      <label>
        <span>Output</span>
        <select bind:value={aiKind} aria-label="AI output type">
          <option value="command">Command — one line</option>
          <option value="script">Script — multi-line, runnable</option>
          <option value="function">Function — one definition</option>
        </select>
      </label>
      <div class="ai-actions">
        <button type="button" disabled={aiBusy || aiPrompt.trim() === ''} onclick={() => void askAI()}>
          {aiBusy ? 'Generating…' : 'Generate'}
        </button>
      </div>
      {#if aiError}<p class="ai-error">{aiError}</p>{/if}
      {#if aiDone}<p class="ai-done">{aiDone}</p>{/if}
    </details>
  {/if}
  <label>
    <span>Title</span>
    <input bind:value={title} required />
  </label>
  <div class="row">
    <label>
      <span>Language</span>
      <input bind:value={language} placeholder="go" />
    </label>
    <label>
      <span>Folder</span>
      <select bind:value={folderId}>
        <option value="">(none)</option>
        {#each folders as f (f.id)}
          <option value={f.id}>{f.name}</option>
        {/each}
      </select>
    </label>
  </div>
  <label>
    <span>Tags</span>
    <input bind:value={tagsText} placeholder="ops, caddy" />
  </label>
  {#if aiKnown && aiEnabled}
    <div class="tags-ai">
      <button type="button" disabled={tagsBusy || body.trim() === ''} onclick={() => void suggestTags()}>
        {tagsBusy ? 'Suggesting…' : 'Suggest tags'}
      </button>
      {#if tagsError}<span class="ai-error">{tagsError}</span>{/if}
    </div>
  {/if}
  <label>
    <span>Body</span>
    <textarea rows="14" bind:value={body} class="code"></textarea>
  </label>
  <label>
    <span>Notes</span>
    <textarea rows="3" bind:value={notes}></textarea>
  </label>
  {#if aiKnown && aiEnabled}
    <div class="tags-ai">
      <button type="button" disabled={explainBusy || body.trim() === ''} onclick={() => void explain()}>
        {explainBusy ? 'Explaining…' : 'Explain'}
      </button>
      {#if explainError}<span class="ai-error">{explainError}</span>{/if}
    </div>
  {/if}
  <label class="check">
    <input type="checkbox" bind:checked={isSensitive} />
    <span>Sensitive — body is never cached offline</span>
  </label>
  <label class="check">
    <input type="checkbox" bind:checked={usesVariables} />
    <span>{templateLabel}</span>
  </label>
  {#if usesVariables}
    <p class="template-hint">
      {#if detectedVars.length > 0}
        Detected variables: {detectedVars.join(', ')}
      {:else}
        {noVarsHint}
      {/if}
    </p>
  {/if}
  <div class="actions">
    <button type="submit">{isEdit ? 'Save' : 'Create'}</button>
    <button type="button" onclick={oncancel}>Cancel</button>
  </div>
</form>
