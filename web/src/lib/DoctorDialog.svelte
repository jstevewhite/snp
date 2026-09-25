<script lang="ts">
  import { untrack } from 'svelte'
  import * as api from './api'
  import type { DoctorRepairResult, DoctorReport, DoctorStatus } from './api'
  import { modal } from './modal'

  // Library health (spec "Health check and index repair"). The checks run on
  // the server, so this is online-only; repair touches derived data only,
  // which is why it needs no second confirmation beyond naming what it does.
  let { online, busy = $bindable(false), saving, onclose }: {
    online: boolean
    /** Bound to the app, so an in-flight check or repair defers reloads. */
    busy?: boolean
    saving: boolean
    onclose: () => void
  } = $props()

  let report = $state<DoctorReport | null>(null)
  let before = $state<DoctorReport | null>(null)
  let repair = $state<DoctorRepairResult | null>(null)
  let loading = $state(false)
  let repairing = $state(false)
  let error = $state<string | null>(null)
  let epoch = 0
  let alive = true

  const locked = $derived(loading || repairing || saving)
  /** Failing checks a repair can actually fix. */
  const repairable = $derived(
    (report?.checks ?? [])
      .filter((c) => c.repairable && c.status !== 'ok')
      .map((c) => c.name),
  )

  // Keep the app's copy in step, so a reload waits for a repair in flight.
  $effect(() => {
    busy = loading || repairing
  })

  $effect(() => {
    if (online) void untrack(() => load())
  })

  $effect(() => () => {
    alive = false
    epoch++
  })

  async function load(): Promise<void> {
    if (!online) return
    const mine = ++epoch
    loading = true
    error = null
    try {
      const res = await api.doctor()
      if (!alive || mine !== epoch) return
      report = res.report
      before = null
      repair = null
    } catch (e) {
      if (alive && mine === epoch) error = message(e)
    } finally {
      if (alive && mine === epoch) loading = false
    }
  }

  async function runRepair(): Promise<void> {
    if (!online || locked) return
    const mine = ++epoch
    repairing = true
    error = null
    try {
      const res = await api.repairDoctor()
      if (!alive || mine !== epoch) return
      before = res.report
      repair = res.repair ?? null
      report = res.after ?? res.report
    } catch (e) {
      if (alive && mine === epoch) error = message(e)
    } finally {
      if (alive && mine === epoch) repairing = false
    }
  }

  function message(e: unknown): string {
    return e instanceof Error ? e.message : String(e)
  }
  function statusLabel(s: DoctorStatus): string {
    return s === 'error' ? 'Error' : s === 'warn' ? 'Warning' : 'OK'
  }
  function errors(rep: DoctorReport): number {
    return rep.checks.filter((c) => c.status === 'error').length
  }
  function summary(rep: DoctorReport): string {
    const c = rep.counts
    return `${c.snippets} snippets (${c.trashed} trashed) · ${c.folders} folders · ${c.tags} tags · ${c.revisions} revisions`
  }
  /** What a repair run changed, in plain words. */
  function changes(res: DoctorRepairResult): string[] {
    const out: string[] = []
    if (res.cleared_sensitive_mirrors > 0) {
      out.push(`cleared ${res.cleared_sensitive_mirrors} leaked sensitive value(s)`)
    }
    if (res.resynced_tag_mirrors > 0) {
      out.push(`resynced ${res.resynced_tag_mirrors} tag mirror(s)`)
    }
    if (res.rebuilt_fts) out.push('rebuilt the search index')
    return out
  }
</script>

<div class="doctor-backdrop">
  <div
    class="doctor"
    role="dialog"
    aria-modal="true"
    aria-label="Library health"
    tabindex="-1"
    use:modal
  >
    <header>
      <div>
        <h2>Library health</h2>
        <p>
          Checks the database, the search index that is rebuilt from it, and the key file.
          Repairing rewrites only derived data — the index and the plaintext columns it reads —
          never a snippet's title, body, notes, folder or tags.
        </p>
      </div>
      <button data-modal-initial disabled={busy} onclick={onclose} aria-label="Close health check">
        Close
      </button>
    </header>

    {#if !online}
      <p class="notice" role="status">
        Reconnect to check the library. The checks run on the server.
      </p>
    {/if}
    {#if error}
      <div class="notice error" role="alert">
        {error}
        <button disabled={!online || busy} onclick={() => load()}>Try again</button>
      </div>
    {/if}
    {#if loading && report === null}
      <p class="notice" role="status">Checking…</p>
    {/if}

    {#if report}
      <p class="verdict" role="status">
        <strong>{report.healthy ? 'No problems found.' : 'Problems found.'}</strong>
        Schema {report.schema_version} (this binary {report.binary_schema}) · {summary(report)}
      </p>

      {#if repair !== null || before !== null}
        <p class="outcome" role="status">
          {#if before !== null && repair !== null}
            {errors(before)} → {errors(report)} errors.
            {#if changes(repair).length > 0}
              Changed: {changes(repair).join(', ')}.
            {:else}
              Nothing needed changing.
            {/if}
          {/if}
        </p>
      {/if}

      <ul class="checks">
        {#each report.checks as c (c.name)}
          <li class:bad={c.status === 'error'} class:warn={c.status === 'warn'}>
            <span class="status">{statusLabel(c.status)}</span>
            <span class="name">{c.name}</span>
            <span class="detail">{c.detail ?? ''}</span>
          </li>
        {/each}
      </ul>

      {#if repairable.length > 0}
        <div class="repair">
          <button class="primary" disabled={!online || busy} onclick={runRepair}>
            {repairing ? 'Repairing…' : `Repair ${repairable.join(', ')}`}
          </button>
          <p>
            Repairs {repairable.join(' and ')}. Nothing your snippets contain changes, so no
            resync is needed.
          </p>
        </div>
      {:else if report.healthy}
        <div class="repair">
          <button disabled={!online || busy} onclick={() => load()}>
            {loading ? 'Checking…' : 'Check again'}
          </button>
        </div>
      {:else}
        <p class="notice">
          Nothing here can be repaired automatically. The remaining findings are reported only.
        </p>
      {/if}
    {/if}
  </div>
</div>

<style>
  .doctor-backdrop {
    position: fixed;
    inset: 0;
    z-index: 100;
    display: grid;
    place-items: center;
    background: #0008;
    padding: 1rem;
  }
  .doctor {
    width: min(720px, 100%);
    max-height: 90dvh;
    overflow: auto;
    border: 1px solid var(--border);
    border-radius: 12px;
    background: var(--bg);
    box-shadow: 0 24px 80px #0005;
  }
  header {
    display: flex;
    align-items: start;
    justify-content: space-between;
    gap: 1rem;
    padding: 1.2rem;
    border-bottom: 1px solid var(--border);
  }
  h2,
  p {
    margin: 0;
  }
  h2 {
    color: var(--text-h);
    font-size: 1.2rem;
  }
  header p {
    margin-top: 0.4rem;
    font-size: 0.85rem;
    opacity: 0.85;
  }
  button {
    flex-shrink: 0;
  }
  .notice {
    padding: 0.85rem 1.2rem;
    border-bottom: 1px solid var(--border);
    font-size: 0.9rem;
  }
  .error {
    color: var(--danger);
  }
  .verdict {
    padding: 0.85rem 1.2rem 0;
    font-size: 0.9rem;
  }
  .outcome {
    margin: 0.85rem 1.2rem 0;
    padding: 0.7rem 0.85rem;
    border: 1px solid var(--accent-border);
    border-radius: 8px;
    font-size: 0.85rem;
  }
  .checks {
    list-style: none;
    margin: 0.85rem 0 0;
    padding: 0 1.2rem;
  }
  .checks li {
    display: grid;
    grid-template-columns: 4.5rem 8rem minmax(0, 1fr);
    gap: 0.6rem;
    align-items: baseline;
    padding: 0.45rem 0;
    border-bottom: 1px solid var(--border);
    font-size: 0.85rem;
  }
  .status {
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.03em;
    font-size: 0.72rem;
  }
  .bad .status {
    color: var(--danger);
  }
  .warn .status {
    color: var(--warn, #b7791f);
  }
  .name {
    font-family: var(--mono);
  }
  .detail {
    min-width: 0;
    overflow-wrap: anywhere;
    opacity: 0.85;
  }
  .repair {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 0.85rem;
    padding: 1.2rem;
  }
  .repair p {
    flex: 1;
    min-width: 12rem;
    font-size: 0.8rem;
    opacity: 0.8;
  }
  .primary {
    color: var(--accent);
    border-color: var(--accent-border);
  }
  @media (max-width: 600px) {
    .doctor-backdrop {
      padding: 0.5rem;
    }
    .checks li {
      grid-template-columns: 4.5rem minmax(0, 1fr);
    }
    .detail {
      grid-column: 1 / -1;
    }
  }
</style>
