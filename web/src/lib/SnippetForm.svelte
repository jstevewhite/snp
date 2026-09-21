<script lang="ts">
  import { folderOptions } from './folders'
  import * as api from './api'
  import type { AIKind } from './api'
  import type { Folder, Snippet, SnippetInput } from './types'
  import { extractTemplateVars, hasTemplateVars } from './templates'

  let {
    initial = null,
    draft = null,
    folders,
    defaultFolderId = null,
    onsave,
    oncancel,
    ondirty = () => {},
    saving = false,
    offline = false,
    error = null,
  }: {
    /** null → create mode; a snippet → edit mode. */
    initial?: Snippet | null
    /** Seed a new unsaved copy, without giving it an existing identity. */
    draft?: SnippetInput | null
    folders: Folder[]
    defaultFolderId?: string | null
    onsave: (input: SnippetInput) => void | Promise<void>
    oncancel: () => void
    ondirty?: (dirty: boolean) => void
    saving?: boolean
    offline?: boolean
    error?: string | null
  } = $props()

  // The form is remounted for each snippet (App.svelte passes `key`), so the
  // fields are seeded from `initial` exactly once, at mount; afterwards the
  // form owns its own state. The one-time capture of the prop is intentional.
  // svelte-ignore state_referenced_locally
  const source = initial ?? draft
  // svelte-ignore state_referenced_locally
  const isDuplicate = draft !== null
  // svelte-ignore state_referenced_locally
  const seed = {
    title: source?.title ?? '',
    language: source?.language ?? '',
    tags: source?.tags.join(', ') ?? '',
    folder: source !== null ? source.folder_id ?? '' : defaultFolderId ?? '',
    body: source?.body ?? '',
    notes: source?.notes ?? '',
    sensitive: source?.is_sensitive ?? false,
    usesVariables: source?.uses_variables ?? false,
    varDefaults: source?.var_defaults ?? {},
    // Pinning is toggled from the read view, not the form, but a PUT is a
    // full replace — so editing a snippet has to carry the flag through or
    // the save would silently unpin it.
    pinned: source?.pinned ?? false,
  }

  let title = $state(seed.title)
  let language = $state(seed.language)
  let tagsText = $state(seed.tags)
  let folderId = $state<string>(seed.folder)
  let body = $state(seed.body)
  let notes = $state(seed.notes)
  let isSensitive = $state(seed.sensitive)
  // Invalidate pending body-AI results even if Sensitive is toggled back
  // off before the response arrives. Requests already sent cannot be recalled.
  let sensitivityVersion = 0
  let usesVariables = $state(seed.usesVariables)

  // A body containing {{var}} placeholders is a template (spec §4). Keep the
  // flag in sync with the body so it can't drift from the content; a manual
  // toggle holds until the next body edit.
  $effect(() => {
    usesVariables = hasTemplateVars(body)
  })

  const dirty = $derived(
    isDuplicate ||
    title !== seed.title || language !== seed.language || tagsText !== seed.tags ||
    folderId !== seed.folder || body !== seed.body || notes !== seed.notes ||
    isSensitive !== seed.sensitive || usesVariables !== hasTemplateVars(seed.body)
  )
  $effect(() => { ondirty(dirty) })

  const folderChoices = $derived(folderOptions(folders))
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

  // Explain (spec §13): one-shot explanation of the body that *replaces*
  // the Notes field, rather than appending to it as it first did — with
  // text already there, appending meant hand-deleting the old explanation
  // before every re-run. The replaced text is kept so a result the user
  // does not want can be undone from the button beside Explain; typing in
  // Notes by hand drops that snapshot, so Undo can never silently discard
  // something the user wrote after the overwrite.
  let explainBusy = $state(false)
  let explainError: string | null = $state(null)
  /** Notes as they stood before the last Explain; null = nothing to undo. */
  let explainUndo = $state<string | null>(null)

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
    if (prompt === '' || aiBusy || tagsBusy || explainBusy || saving || offline) return
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
    if (isSensitive || tagsBusy || aiBusy || explainBusy || saving || offline || body.trim() === '') return
    const version = sensitivityVersion
    tagsBusy = true
    tagsError = null
    try {
      const res = await api.suggestTags({
        body,
        is_sensitive: isSensitive,
        title: title.trim() === '' ? undefined : title.trim(),
        language: language.trim() === '' ? undefined : language.trim(),
      })
      if (version !== sensitivityVersion) return
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
      if (version === sensitivityVersion) tagsError = e instanceof Error ? e.message : String(e)
    } finally {
      tagsBusy = false
    }
  }

  async function explain(): Promise<void> {
    if (isSensitive || explainBusy || aiBusy || tagsBusy || saving || offline || body.trim() === '') return
    const version = sensitivityVersion
    explainBusy = true
    explainError = null
    try {
      const res = await api.explainSnippet({ body, is_sensitive: isSensitive })
      if (version !== sensitivityVersion) return
      if (res.notes.trim() === '') throw new Error('AI returned an empty explanation')
      // Replace, keeping what was there so the overwrite is reversible.
      // The snapshot is taken only on success, so a failed run leaves both
      // the notes and any earlier undo point intact.
      explainUndo = notes
      notes = res.notes
    } catch (e) {
      if (version === sensitivityVersion) explainError = e instanceof Error ? e.message : String(e)
    } finally {
      explainBusy = false
    }
  }

  /** Put back the Notes the last Explain replaced (spec §13). */
  function undoExplain(): void {
    if (explainUndo === null) return
    notes = explainUndo
    explainUndo = null
  }

  let submitting = $state(false)
  const aiPending = $derived(aiBusy || tagsBusy || explainBusy)

  async function submit(): Promise<void> {
    if (title.trim() === '' || saving || submitting || offline || aiPending) return
    submitting = true
    // A full replace wipes var_defaults (spec §5), so carry the saved
    // defaults forward, pruned to the variables the body still uses (spec
    // §4): editing the body must not silently drop them, and a body with
    // no variables keeps an empty map.
    const names = new Set(extractTemplateVars(body).map((v) => v.name))
    const defaults: Record<string, string> = {}
    for (const [name, value] of Object.entries(seed.varDefaults)) {
      if (names.has(name) && value !== '') defaults[name] = value
    }
    try {
      await onsave({
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
        pinned: seed.pinned,
        var_defaults: defaults,
      })
    } finally {
      submitting = false
    }
  }
</script>

<form aria-busy={saving || submitting} class="form" onsubmit={(e) => { e.preventDefault(); void submit() }}>
  <fieldset disabled={saving || submitting}>
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
        <button type="button" disabled={offline || aiPending || aiPrompt.trim() === ''} onclick={() => void askAI()}>
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
        {#each folderChoices as f (f.id)}
          <option value={f.id}>{f.path}</option>
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
      <button type="button" disabled={isSensitive || offline || aiPending || body.trim() === ''} onclick={() => void suggestTags()}>
        {tagsBusy ? 'Suggesting…' : 'Suggest tags'}
      </button>
      {#if isSensitive}<span>Unavailable for sensitive snippets.</span>{/if}
      {#if tagsError}<span class="ai-error">{tagsError}</span>{/if}
    </div>
  {/if}
  <label>
    <span>Body</span>
    <textarea rows="14" bind:value={body} class="code"></textarea>
  </label>
  <label>
    <span>Notes</span>
    <!-- Hand-editing drops the undo snapshot: Undo reverts the Explain
         overwrite only, never typing that came after it. -->
    <textarea rows="3" bind:value={notes} oninput={() => (explainUndo = null)}></textarea>
  </label>
  {#if aiKnown && aiEnabled}
    <div class="tags-ai">
      <button type="button" disabled={isSensitive || offline || aiPending || body.trim() === ''} onclick={() => void explain()}>
        {explainBusy ? 'Explaining…' : 'Explain'}
      </button>
      {#if isSensitive}<span>Unavailable for sensitive snippets.</span>{/if}
      {#if explainUndo !== null}
        <button
          type="button"
          disabled={explainBusy}
          title="Put back the notes Explain replaced"
          onclick={undoExplain}
        >
          Undo
        </button>
      {/if}
      {#if explainError}<span class="ai-error">{explainError}</span>{/if}
    </div>
  {/if}
  <label class="check">
    <input type="checkbox" bind:checked={isSensitive} onchange={() => {
      sensitivityVersion++
      tagsError = null
      explainError = null
    }} />
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
  </fieldset>
  {#if error}<p class="form-error" role="alert">{error}</p>{/if}
  {#if offline}<p role="status">Offline — reconnect before saving. Your edits remain here.</p>{/if}
  <div class="actions">
    <button type="submit" disabled={saving || submitting || offline || aiPending}>{saving || submitting ? 'Saving…' : isEdit ? 'Save' : 'Create'}</button>
    <button type="button" disabled={saving || submitting} onclick={oncancel}>Cancel</button>
  </div>
</form>
