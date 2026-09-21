<script lang="ts">
  import { onDestroy } from 'svelte'
  import { modal } from './modal'
  import { isDesktop } from './desktop'
  import { chooseDesktopImport, downloadData, MAX_SELECTED_BYTES, isEncryptedFile, parseImport, type DownloadKind } from './data'
  import { decryptImport, importSnippets, type ImportDocument, type ImportMode, type ImportResult } from './api'

  let { online, busy = $bindable(false), onimport, onclose }: {
    online: boolean
    busy?: boolean
    onimport: (doc: ImportDocument, mode: ImportMode) => Promise<ImportResult & { refreshWarning?: string }>
    onclose: () => void
  } = $props()
  let fileInput: HTMLInputElement
  let document: ImportDocument | null = $state(null)
  let filename = $state('')
  let mode = $state<ImportMode>('merge')
  let preview = $state<ImportResult | null>(null)
  let acknowledge = $state(false)
  let error = $state<string | null>(null)
  let status = $state<string | null>(null)
  let encrypt = $state(true)
  let password = $state('')
  let confirmPassword = $state('')
  let encryptedFile = $state<string | null>(null)
  let importPassword = $state('')
  let alive = true
  onDestroy(() => { alive = false; document = null; encryptedFile = null; password = ''; confirmPassword = ''; importPassword = '' })

  function resetPreview(): void { preview = null; acknowledge = false; error = null; status = null }
  function read(text: string, name: string): void {
    resetPreview(); document = null; encryptedFile = null; importPassword = ''; filename = ''
    if (isEncryptedFile(text)) { encryptedFile = text; filename = name; return }
    document = parseImport(text); filename = name
  }
  async function choose(): Promise<void> {
    if (!online || busy) return
    if (!isDesktop()) { fileInput.click(); return }
    busy = true; error = null
    try { const file = await chooseDesktopImport(); if (alive && file) read(file.text, file.name) }
    catch (e) { if (alive) error = message(e) }
    finally { if (alive) busy = false }
  }
  async function selectedFile(event: Event): Promise<void> {
    const input = event.currentTarget as HTMLInputElement
    const file = input.files?.[0]
    input.value = ''
    if (!file || busy || !online) return
    busy = true; error = null; document = null; encryptedFile = null; importPassword = ''; filename = ''; resetPreview()
    try {
      if (file.size > MAX_SELECTED_BYTES) throw new Error('Selected files must be 16 MiB or smaller. Use snp decrypt and snp import for larger files.')
      const text = await file.text()
      if (alive) read(text, file.name)
    } catch (e) { if (alive) error = message(e) }
    finally { if (alive) busy = false }
  }
  async function unlock(): Promise<void> {
    if (!encryptedFile || !online || busy) return
    if (!importPassword) { error = 'Enter the file password.'; return }
    busy = true; error = null; status = null
    try {
      const result = await decryptImport(encryptedFile, importPassword)
      if (alive) {
        document = parseImport(JSON.stringify(result)); encryptedFile = null
        status = 'File unlocked. Preview the changes before importing.'
      }
    } catch (e) { if (alive) error = message(e) }
    finally { importPassword = ''; if (alive) busy = false }
  }
  function toggleEncryption(): void { password = ''; confirmPassword = ''; error = null }
  async function checkImport(): Promise<void> {
    if (!document || !online || busy) return
    busy = true; resetPreview()
    try { const out = await importSnippets(document, mode, true); if (alive) preview = out }
    catch (e) { if (alive) error = message(e) }
    finally { if (alive) busy = false }
  }
  async function applyImport(): Promise<void> {
    if (!document || !preview || !online || busy || (mode === 'replace' && !acknowledge)) return
    busy = true; error = null; status = null
    try {
      const result = await onimport(document, mode)
      if (alive) {
        status = `Import complete: ${result.created} new, ${result.updated} existing, ${result.trashed} moved to Trash.${result.refreshWarning ? ` ${result.refreshWarning}` : ''}`
        document = null; filename = ''; preview = null; acknowledge = false
      }
    } catch (e) { if (alive) { error = message(e); preview = null; acknowledge = false } }
    finally { if (alive) busy = false }
  }
  async function download(kind: DownloadKind): Promise<void> {
    if (!online || busy) return
    error = null; status = null
    if (encrypt && (Array.from(password).length < 12 || new TextEncoder().encode(password).byteLength > 1024)) {
      error = 'Use a password of at least 12 characters (at most 1024 bytes).'; return
    }
    if (encrypt && password !== confirmPassword) { error = 'Passwords do not match.'; return }
    busy = true
    try {
      const result = await downloadData(kind, encrypt ? password : undefined)
      if (alive) status = result === 'cancelled' ? 'Save cancelled.' : result === 'saved' ? 'File saved.' : 'Download started. Check your browser’s downloads.'
    } catch (e) { if (alive) error = message(e) }
    finally { password = ''; confirmPassword = ''; if (alive) busy = false }
  }
  function message(e: unknown): string { return e instanceof Error ? e.message : String(e) }
</script>

<div class="data-backdrop">
  <div class="data-dialog" role="dialog" aria-modal="true" aria-label="Import, export & backup" tabindex="-1" use:modal>
    <header><h2>Import, export & backup</h2><button data-modal-initial aria-label="Close data management" disabled={busy} onclick={onclose}>Close</button></header>
    {#if !online}<p class="notice" role="status">Reconnect to import, export or back up your snippets.</p>{/if}
    {#if error}<p class="notice error" role="alert">{error}</p>{/if}
    {#if status}<p class="notice" role="status">{status}</p>{/if}
    {#if busy}<p class="notice" role="status">Working…</p>{/if}
    <section aria-labelledby="data-import-heading">
      <h3 id="data-import-heading">Import JSON</h3>
      <p>Bring in a snp JSON export, including folders, tags, templates and favorites. Preview changes before importing. Accepts JSON or a password-protected .json.age export. Maximum JSON size: 10 MiB (16 MiB encrypted).</p>
      <input bind:this={fileInput} type="file" accept=".json,.age,application/json" aria-label="Import JSON file" hidden onchange={selectedFile} />
      <button disabled={!online || busy} onclick={choose}>Choose JSON file</button>
      {#if encryptedFile}
        <p class="file-summary"><strong>{filename}</strong> · Password-protected export</p>
        <label class="password-field">File password<input type="password" autocomplete="off" bind:value={importPassword} disabled={!online || busy} /></label>
        <button disabled={!online || busy} onclick={unlock}>Unlock file</button>
      {/if}
      {#if document}
        <p class="file-summary"><strong>{filename}</strong> · {document.snippets.length} snippets · {document.folders?.length ?? 0} folders</p>
        <label>Import mode
          <select bind:value={mode} disabled={!online || busy} onchange={resetPreview}>
            <option value="merge">Merge into my library</option>
            <option value="replace">Replace my library</option>
          </select>
        </label>
        <p class="hint">{mode === 'merge' ? 'Matching IDs are overwritten; other snippets stay. Changed content is kept in revision history.' : 'Matching IDs are overwritten. Snippets missing from this file move to Trash. Folder structure is replaced.'} Snippets without IDs are added as new.</p>
        <button disabled={!online || busy} onclick={checkImport}>Preview import</button>
        {#if preview}
          <div class="import-preview" aria-label="Import preview">
            <strong>{preview.created} new · {preview.updated} existing · {preview.trashed} to Trash</strong>
            <p class="hint">Existing includes unchanged snippets and restored items. Counts may change if another client edits the library before import.</p>
            {#if mode === 'replace'}
              <label class="acknowledge"><input type="checkbox" bind:checked={acknowledge} disabled={!online || busy} />Replace my library with this file and move missing snippets to Trash.</label>
            {/if}
            <button class="primary" disabled={!online || busy || (mode === 'replace' && !acknowledge)} onclick={applyImport}>Import now</button>
          </div>
        {/if}
      {/if}
    </section>
    <section aria-labelledby="data-protection-heading">
      <h3 id="data-protection-heading">Protect downloaded files</h3>
      <label><input type="checkbox" bind:checked={encrypt} onchange={toggleEncryption} disabled={!online || busy} />Encrypt exports and backups with a password</label>
      {#if encrypt}
        <p>Protects the entire file, including the key inside a backup. Use a long, unique passphrase, separate from your snp encryption key.</p>
        <label class="password-field">Download password<input type="password" autocomplete="new-password" bind:value={password} disabled={!online || busy} /></label>
        <label class="password-field">Confirm password<input type="password" autocomplete="new-password" bind:value={confirmPassword} disabled={!online || busy} /></label>
        <p class="hint">At least 12 characters. snp never saves this password.</p>
        <p class="password-warning"><strong>If you forget this password, you're toast.</strong> There is no reset or recovery for the encrypted file. Save the password somewhere safe.</p>
      {:else}
        <p class="password-warning"><strong>These downloads will not be password-protected.</strong> Exported sensitive content is readable, and the backup contains its decryption key. Keep the files private.</p>
      {/if}
    </section>
    <section aria-labelledby="data-export-heading">
      <h3 id="data-export-heading">Export JSON</h3>
      <p>A portable copy of all current snippets and folders. Includes sensitive bodies. Trash and revision history are excluded. Password-protected exports download as .json.age files.</p>
      <button disabled={!online || busy} onclick={() => download('export')}>Download JSON</button>
    </section>
    <section aria-labelledby="data-backup-heading">
      <h3 id="data-backup-heading">Full backup</h3>
      <p>A ZIP containing the database, encryption key and restore instructions. Includes Trash and revision history. Settings and Tailscale configuration are excluded.</p>
      <p>Password-protected backups download as .zip.age files. The password protects everything in the archive.</p>
      <button disabled={!online || busy} onclick={() => download('backup')}>Download backup</button>
      <details><summary>How do I restore a backup?</summary><p>For an encrypted backup, run <code>snp decrypt backup.zip.age restored.zip</code> in a terminal and enter its password when prompted. Then stop snp. Keep your existing state directory as a rollback copy, then extract <code>snp.db</code> and <code>key</code> into a new, empty state directory. Start snp using that directory and choose Settings → Full resync on each client. The ZIP contains detailed instructions. JSON Import does not restore backup ZIPs.</p></details>
    </section>
  </div>
</div>

<style>
  .data-backdrop { position: fixed; inset: 0; z-index: 100; display: grid; place-items: center; padding: 1rem; background: #0008; }
  .data-dialog { width: min(700px, 100%); max-height: 90dvh; overflow: auto; background: var(--bg); border: 1px solid var(--border); border-radius: 12px; box-shadow: 0 24px 80px #0005; }
  header { padding: 1rem 1.2rem; display: flex; align-items: center; justify-content: space-between; gap: 1rem; border-bottom: 1px solid var(--border); }
  h2, h3 { margin: 0; color: var(--text-h); } h2 { font-size: 1.2rem; } h3 { font-size: 1.05rem; }
  section { padding: 1.1rem 1.2rem; border-bottom: 1px solid var(--border); } section:last-child { border: 0; }
  p { margin: .6rem 0 .9rem; } .notice { padding: .7rem 1.2rem; margin: 0; border-bottom: 1px solid var(--border); } .error { color: var(--danger); }
  label { display: flex; align-items: center; flex-wrap: wrap; gap: .5rem; margin: .75rem 0; } select { max-width: 100%; }
  .hint { font-size: .85rem; } .file-summary { overflow-wrap: anywhere; }
  .import-preview { padding: .9rem; border: 1px solid var(--accent-border); border-radius: 6px; background: var(--selected-bg); margin-top: .9rem; }
  .acknowledge { flex-wrap: nowrap; align-items: start; } .acknowledge input { margin-top: .25rem; }
  .password-field { display: grid; max-width: 24rem; }
  input[type="password"] { min-width: 0; width: 100%; box-sizing: border-box; padding: 6px 8px; border: 1px solid var(--border); border-radius: 6px; background: var(--bg); color: inherit; font: inherit; }
  input[type="password"]:focus { outline: 2px solid var(--accent); outline-offset: 2px; }
  .password-warning { padding: .75rem; border: 1px solid var(--accent-border); border-radius: 6px; }
  .primary { color: var(--accent); border-color: var(--accent-border); }
  details { margin-top: 1rem; font-size: .85rem; } summary { cursor: pointer; } details p { margin-bottom: 0; }
  @media(max-width: 500px) { .data-backdrop { padding: .5rem; } .data-dialog { max-height: 95dvh; } }
</style>
