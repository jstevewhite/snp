<script lang="ts">
  import { onDestroy, untrack } from 'svelte'
  import * as api from './api'
  import { modal } from './modal'
  import { formatAbsolute } from './time'
  import type { Folder, Revision, RevisionDetail, Snippet, TrashEntry } from './types'

  let { snippet = null, folders, online, saving, onrestore, onclose }: {
    snippet?: Snippet | null
    folders: Folder[]
    online: boolean
    saving: boolean
    onrestore: (id: string, revision?: number) => Promise<void>
    onclose: () => void
  } = $props()

  let trash = $state<TrashEntry[]>([])
  let revisions = $state<Revision[]>([])
  let chosen = $state<Revision | null>(null)
  let detail = $state<RevisionDetail | null>(null)
  let current = $state<Snippet | null>(null)
  let loading = $state(false)
  let previewing = $state(false)
  let restoring = $state(false)
  let more = $state(false)
  let error = $state<string | null>(null)
  let listEpoch = 0
  let previewEpoch = 0
  let alive = true
  const locked = $derived(saving || restoring)

  onDestroy(() => { alive = false; listEpoch++; previewEpoch++; detail = null; current = null })
  $effect(() => {
    if (online) void untrack(() => load())
    else {
      listEpoch++; previewEpoch++; detail = null; current = null
      loading = false; previewing = false
    }
  })

  async function load(append = false): Promise<void> {
    if (!online || locked) return
    const epoch = ++listEpoch
    loading = true; error = null
    if (!append) { previewEpoch++; chosen = null; detail = null; current = null; previewing = false }
    try {
      if (snippet) {
        const rows = await api.listRevisions(snippet.id)
        if (alive && epoch === listEpoch) revisions = rows
      } else {
        const rows = await api.listTrash(append ? trash.length : 0)
        if (alive && epoch === listEpoch) { trash = append ? [...trash, ...rows] : rows; more = rows.length === 100 }
      }
    } catch (e) { if (alive && epoch === listEpoch) error = message(e) }
    finally { if (alive && epoch === listEpoch) loading = false }
  }

  function message(e: unknown): string { return e instanceof Error ? e.message : String(e) }
  function folderName(id: string | null): string {
    if (!id) return 'Unfiled'
    return folders.find((f) => f.id === id)?.name ?? 'Deleted folder → Unfiled on restore'
  }

  async function preview(r: Revision, reveal = false): Promise<void> {
    if (!snippet || !online || locked) return
    const epoch = ++previewEpoch
    chosen = r; detail = null; current = null; error = null; previewing = false
    if ((r.protected || snippet.is_sensitive) && !reveal) return
    previewing = true
    try {
      const version = await api.getRevision(snippet.id, r.id, reveal)
      if (!alive || epoch !== previewEpoch || !online) return
      const now = snippet.is_sensitive ? await api.getSnippet(snippet.id) : snippet
      if (alive && epoch === previewEpoch) { detail = version; current = now }
    } catch (e) { if (alive && epoch === previewEpoch) error = message(e) }
    finally { if (alive && epoch === previewEpoch) previewing = false }
  }

  async function restore(id: string, revision?: number): Promise<void> {
    if (!online || locked) return
    restoring = true; error = null
    try { await onrestore(id, revision) }
    catch (e) { if (alive) error = message(e) }
    finally { if (alive) restoring = false }
  }
</script>

<div class="recovery-backdrop">
  <div class="recovery" role="dialog" aria-modal="true" aria-label={snippet ? 'Revision history' : 'Trash'} tabindex="-1" use:modal>
    <header>
      <div>
        <h2>{snippet ? 'Revision history' : 'Trash'}</h2>
        <p>{snippet ? snippet.title : 'Deleted snippets stay here for 30 days before automatic removal.'}</p>
      </div>
      <button data-modal-initial disabled={locked} onclick={onclose} aria-label="Close recovery">Close</button>
    </header>
    {#if !online}<p class="notice" role="status">Reconnect to view or restore snippets.</p>{/if}
    {#if error}<div class="notice error" role="alert">{error} <button disabled={!online || locked} onclick={() => load()}>Refresh</button></div>{/if}
    {#if snippet}
      <p class="notice">The last 50 changed versions are kept. Restoring saves your current version first and keeps your favorite setting.</p>
      <div class="history-layout">
        <nav aria-label="Saved versions">
          {#if loading}<p role="status">Loading history…</p>
          {:else if revisions.length === 0}<p>No previous versions yet. History starts with your next edit.</p>{/if}
          {#each revisions as r, i (r.id)}
            <button class:active={chosen?.id === r.id} aria-pressed={chosen?.id === r.id} disabled={!online || locked} onclick={() => preview(r)}>
              <strong>{i === 0 ? 'Previous version' : `Earlier version ${i + 1}`}</strong>
              <span>{formatAbsolute(r.version_at)}</span>
              <small>Saved over {formatAbsolute(r.saved_at)}{r.protected ? ' · Protected' : ''}</small>
            </button>
          {/each}
        </nav>
        <div class="preview">
          {#if previewing}<p role="status">Loading version…</p>
          {:else if detail && current}
            <div class="restore-bar">
              <p>{detail.protected || snippet.is_sensitive ? 'This version will restore as sensitive.' : 'Restore all content and metadata shown below.'}</p>
              <button class="primary" disabled={!online || locked} onclick={() => restore(snippet!.id, detail!.id)}>{locked ? 'Restoring…' : 'Restore this version'}</button>
            </div>
            <div class="comparison">
              <article aria-label="Selected version"><h3>Selected version</h3>{@render snapshot(detail.snippet)}</article>
              <article aria-label="Current version"><h3>Current version</h3>{@render snapshot(current)}</article>
            </div>
          {:else if chosen && (chosen.protected || snippet.is_sensitive)}
            <p>This comparison contains protected content. It stays only in memory while this dialog is open.</p>
            <button disabled={!online || locked} onclick={() => preview(chosen!, true)}>Reveal comparison</button>
          {:else}<p>Select a version to compare it with the current snippet.</p>{/if}
        </div>
      </div>
    {:else}
      <div class="trash-list">
        {#if !loading && trash.length === 0}<p>Trash is empty.</p>{/if}
        {#each trash as entry (entry.id)}
          <div class="trash-row">
            <div><strong>{entry.title}</strong><span>{entry.language || 'Plain text'}{entry.is_sensitive ? ' · Sensitive' : ''} · Deleted {formatAbsolute(entry.deleted_at)}</span></div>
            <button disabled={!online || locked} aria-label={`Restore ${entry.title}`} onclick={() => restore(entry.id)}>{locked ? 'Restoring…' : 'Restore'}</button>
          </div>
        {/each}
        {#if loading}<p role="status">Loading trash…</p>{/if}
        {#if more}<button disabled={!online || locked || loading} onclick={() => load(true)}>Load more</button>{/if}
      </div>
    {/if}
  </div>
</div>

{#snippet snapshot(s: Snippet)}
  <h4>{s.title}</h4>
  <dl>
    <dt>Language</dt><dd>{s.language || 'Plain text'}</dd>
    <dt>Folder</dt><dd>{folderName(s.folder_id)}</dd>
    <dt>Tags</dt><dd>{s.tags?.join(', ') || 'None'}</dd>
    <dt>Template</dt><dd>{s.uses_variables ? 'Yes' : 'No'}</dd>
  </dl>
  <h4>Body</h4><pre>{s.body ?? ''}</pre>
  <h4>Notes</h4><pre>{s.notes || 'No notes'}</pre>
  {#if Object.keys(s.var_defaults ?? {}).length > 0}
    <h4>Variable defaults</h4><pre>{JSON.stringify(s.var_defaults, null, 2)}</pre>
  {/if}
{/snippet}

<style>
  .recovery-backdrop { position: fixed; inset: 0; z-index: 100; display: grid; place-items: center; background: #0008; padding: 1rem; }
  .recovery { width: min(1100px, 100%); max-height: 90dvh; overflow: auto; border: 1px solid var(--border); border-radius: 12px; background: var(--bg); box-shadow: 0 24px 80px #0005; }
  header { display: flex; align-items: start; justify-content: space-between; gap: 1rem; padding: 1.2rem; border-bottom: 1px solid var(--border); }
  h2, h3, h4, p { margin: 0; } h2 { color: var(--text-h); font-size: 1.2rem; } header p { margin-top: .4rem; overflow-wrap: anywhere; }
  button { flex-shrink: 0; } .notice { padding: .85rem 1.2rem; border-bottom: 1px solid var(--border); font-size: .9rem; } .error { color: var(--danger); }
  .history-layout { display: grid; grid-template-columns: 230px minmax(0, 1fr); min-height: 300px; }
  nav { padding: .75rem; border-right: 1px solid var(--border); overflow: auto; max-height: 60dvh; }
  nav button { width: 100%; text-align: left; margin-bottom: .5rem; padding: .7rem; white-space: normal; }
  nav span, nav small, .trash-row span { display: block; margin-top: .3rem; font-size: .8rem; } nav small { opacity: .8; }
  .active { border-color: var(--accent); background: var(--selected-bg); }
  .preview { padding: 1rem; min-width: 0; } .restore-bar { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: .75rem; margin-bottom: 1rem; font-size: .85rem; }
  .primary { color: var(--accent); border-color: var(--accent-border); }
  .comparison { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 1rem; }
  article { min-width: 0; } h3 { color: var(--accent); font-size: .95rem; border-bottom: 1px solid var(--border); padding-bottom: .5rem; } h4 { margin: .8rem 0 .35rem; color: var(--text-h); overflow-wrap: anywhere; }
  dl { display: grid; grid-template-columns: auto minmax(0, 1fr); gap: .3rem .7rem; font-size: .85rem; } dd { margin: 0; overflow-wrap: anywhere; }
  pre { white-space: pre-wrap; overflow-wrap: anywhere; background: var(--code-bg); padding: .75rem; border-radius: 6px; margin: 0; font: .85rem/1.6 var(--mono); max-height: 35dvh; overflow: auto; }
  .trash-list { padding: 0 1.2rem 1rem; } .trash-list > p { padding-top: 1rem; }
  .trash-row { display: flex; align-items: center; justify-content: space-between; gap: 1rem; padding: 1rem 0; border-bottom: 1px solid var(--border); } .trash-row > div { min-width: 0; overflow-wrap: anywhere; }
  @media(max-width: 700px) { .recovery-backdrop { padding: .5rem; } .recovery { max-height: 95dvh; } .history-layout { grid-template-columns: 1fr; } nav { max-height: 180px; border-right: 0; border-bottom: 1px solid var(--border); } .comparison { grid-template-columns: 1fr; } }
</style>
