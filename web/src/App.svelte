<script lang="ts">
  import { tick as settle, untrack } from 'svelte'
  import * as api from './lib/api'
  import { closeDesktop, registerDesktopClose } from './lib/desktop'
  import { modal } from './lib/modal'
  import { folderOptions } from './lib/folders'
  import { writeClipboard } from './lib/clipboard'
  import { ApiError } from './lib/types'
  import {
    allFolders,
    allSnippets,
    cacheSnippet,
    clearLocalData,
    openLocalDB,
    putFolder,
    putSnippet,
    removeFolder,
    removeSnippet,
    type SnpDB,
  } from './lib/db'
  import type { Command } from './lib/commands'
  import { isPaletteShortcut, isSearchShortcut, paletteShortcutLabel, searchShortcutLabel } from './lib/keys'
  import {
    browserHistory,
    createCompactNav,
    isTextInput,
    resolveLayout,
    watchNarrow,
    type NavHistory,
    type NavState,
  } from './lib/layout'
  import { OnlineTracker } from './lib/online'
  import {
    FOLDERS_MAX,
    FOLDERS_MIN,
    LIST_MAX,
    LIST_MIN,
    NUDGE_PX,
    fallbackPaneWidths,
    loadPaneWidths,
    measurePaneWidths,
    paneStyleVars,
    resizePane,
    savePaneWidths,
    type PaneWidths,
    type SplitterId,
  } from './lib/panes'
  import { SnippetIndex } from './lib/search'
  import {
    LAYOUTS,
    THEMES,
    TEXT_SCALE_MAX,
    TEXT_SCALE_MIN,
    TEXT_SCALE_STEP,
    applyTextScale,
    applyTheme,
    loadSettings,
    saveLayout,
    saveTextScale,
    saveTheme,
    saveTwoLineTitles,
  } from './lib/settings'
  import { syncLocal } from './lib/sync'
  import { renderTemplate } from './lib/templates'
  import { formatAbsolute, formatRelative } from './lib/time'
  import { loadCachedVersion, resolveVersion } from './lib/version'
  import { onWake } from './lib/wake'
  import type { Folder, Snippet, SnippetInput } from './lib/types'
  import FolderTree from './lib/FolderTree.svelte'
  import Favorites from './lib/Favorites.svelte'
  import OfflineBanner from './lib/OfflineBanner.svelte'
  import SnippetDetail from './lib/SnippetDetail.svelte'
  import SnippetForm from './lib/SnippetForm.svelte'
  import SnippetList from './lib/SnippetList.svelte'
  import TagList from './lib/TagList.svelte'
  import CommandPalette from './lib/CommandPalette.svelte'

  /** Sync cadence (spec §6): every 5 minutes while open. */
  const SYNC_INTERVAL_MS = 300_000

  // Reactive UI state
  let folders = $state<Folder[]>([])
  let snippets = $state<Snippet[]>([])
  let query = $state('')
  let selectedFolderId: string | null = $state(null)
  /** Tags active in the list filter (AND: all must be present). */
  let activeTags = $state<string[]>([])
  let selectedSnippetId: string | null = $state(null)
  let editing = $state(false)
  let editingSnippet: Snippet | null = $state(null)
  /** Revealed bodies for sensitive snippets (in memory only, never cached). */
  let revealed = $state<Record<string, { body: string; var_defaults: Record<string, string> }>>({})
  let detailEpoch = 0
  let dirty = $state(false)
  let saving = $state(false)
  let saveError = $state<string | null>(null)
  let editorKey = $state(0)

  function clearRevealed(): void {
    detailEpoch++
    revealed = {}
    detailCopy = null
  }

  function leaveEditor(proceed: () => void): void {
    if (saving) return
    if (editing && dirty) dialog = { kind: 'discard', proceed }
    else proceed()
  }

  $effect(() => registerDesktopClose(() => leaveEditor(() => void closeDesktop())))
  $effect(() => {
    const beforeReload = (e: Event): void => {
      if (dirty || saving) e.preventDefault()
    }
    window.addEventListener('snp:before-reload', beforeReload)
    return () => window.removeEventListener('snp:before-reload', beforeReload)
  })
  $effect(() => {
    if (!dirty && !saving) window.dispatchEvent(new Event('snp:reload-ready'))
  })
  let online = $state(true)
  /**
   * When this client last completed a sync (epoch ms). Deliberately a
   * local reading rather than the response's server_time: the label reads
   * "2 minutes ago", which is only true against the clock that observed
   * the sync. server_time stays the sync cursor (lib/sync.ts) and is not
   * a local wall-clock value.
   */
  let syncedAt: number | null = $state(null)
  /** Ticks so the relative "Synced …" label ages without a reload. */
  const CLOCK_TICK_MS = 30_000
  let now = $state(Date.now())
  let busy = $state(false)
  let error: string | null = $state(null)
  /** Transient topbar confirmation for an action with no button to flash
   * (the keyboard copy shortcut); failures reuse `error` instead. */
  const NOTICE_MS = 1500
  let notice = $state<string | null>(null)
  let noticeTimer: ReturnType<typeof setTimeout> | undefined
  /** The list pane's search input, bound so the shortcut can focus it. */
  let searchEl = $state<HTMLInputElement | undefined>()
  /**
   * The selected snippet's copy text as the detail pane computed it, with
   * the id it belongs to. Deliberately non-reactive: it is read only from
   * the keydown handler, and a reactive read would re-render the app on
   * every keystroke in the variables panel.
   */
  let detailCopy: { id: string; text: string | null } | null = null
  let ready = $state(false)
  let settingsOpen = $state(false)
  let settingsEl = $state<HTMLDivElement | undefined>()
  $effect(() => {
    if (!settingsOpen || !settingsEl) return
    const container = settingsEl
    const dismissSettings = (e: MouseEvent): void => {
      if (!e.composedPath().includes(container)) settingsOpen = false
    }
    // Capture also sees clicks that other controls stop from bubbling.
    // Include the toggle in the boundary so its own click closes normally.
    document.addEventListener('click', dismissSettings, true)
    return () => document.removeEventListener('click', dismissSettings, true)
  })
  /** Release version, shown next to the wordmark (spec §5). Starts from
   * the cached value so the header renders immediately — offline too. */
  let version: string | null = $state(loadCachedVersion())

  // Appearance: theme + interface text size. main.ts applies the saved
  // values before first paint; from here on the component owns them —
  // every change is applied to the document and persisted (browser and
  // wails desktop window alike).
  const savedAppearance = loadSettings()
  let theme = $state(savedAppearance.theme)
  let textScale = $state(savedAppearance.textScale)
  let twoLineTitles = $state(savedAppearance.twoLineTitles)
  let layout = $state(savedAppearance.layout)
  $effect(() => {
    applyTheme(theme)
    saveTheme(theme)
  })
  $effect(() => {
    applyTextScale(textScale)
    saveTextScale(textScale)
  })
  $effect(() => {
    saveTwoLineTitles(twoLineTitles)
  })
  $effect(() => {
    saveLayout(layout)
  })

  // Compact layout (spec §6 "Compact layout"): the Layout setting plus the
  // live width resolve to wide or compact. In compact mode the panes become
  // a screen stack — list (root) → detail — with the folders pane as a
  // drawer, driven by lib/layout.ts through one history entry per level so
  // the OS back gesture and the in-app Back share the popstate path. The
  // stack's snapshot is mirrored into `navState` so the markup can react.
  let narrow = $state(false)
  $effect(() => watchNarrow((n) => (narrow = n)))
  const mode = $derived(resolveLayout(layout, narrow))
  const compact = $derived(mode === 'compact')
  let navState = $state<NavState>({ screen: 'list', drawerOpen: false, depth: 0 })
  const inertHistory: NavHistory = {
    state: null,
    pushState() {},
    replaceState() {},
    back() {},
    go() {},
  }
  const nav = createCompactNav(browserHistory() ?? inertHistory, (s) => (navState = s))
  $effect(() => {
    const onPop = (e: PopStateEvent): void => {
      nav.onPopState(e.state)
      if (compact && editing && nav.state.screen === 'list' && (dirty || saving)) {
        // The browser already moved back. Restore the detail entry before
        // asking, so Keep editing leaves both the form and history intact.
        nav.openDetail()
        leaveEditor(() => {
          editing = false
          editingSnippet = null
          clearRevealed()
          nav.back()
        })
      }
    }
    window.addEventListener('popstate', onPop)
    return () => window.removeEventListener('popstate', onPop)
  })
  // Switching modes: entering compact lands on detail when something is
  // open (a window dragged narrower keeps what was being read); leaving it
  // forgets the stack. Reads of the selection are untracked so only the
  // mode change re-runs this.
  $effect(() => {
    if (compact) nav.enter(untrack(() => editing || selectedSnippetId !== null))
    else nav.leave()
  })
  // Landing on the list while the editor is open means Back was pressed in
  // the editor (button, gesture or Escape): that is Cancel (spec §6).
  $effect(() => {
    if (compact && navState.screen === 'list' && editing) cancelEdit()
  })
  // A detail screen with nothing to show (the snippet was deleted, or the
  // cache was cleared) falls back to the list.
  $effect(() => {
    if (compact && navState.screen === 'detail' && !editing && selectedSnippet === null) {
      nav.back()
    }
  })

  // Pane layout (spec §6): the folders and list panes are sized by inline
  // custom properties and the detail pane stays flexible, so only two widths
  // are tracked. They start unset — the stylesheet's own proportions — and
  // are measured after mount so a drag has a baseline without changing what
  // the first paint looked like. Only a real drag persists a pair, so a
  // window the user never resized keeps adapting to its size.
  let paneWidths = $state<PaneWidths | null>(loadPaneWidths())
  let panesEl = $state<HTMLElement | undefined>()
  /** Divider being held, for the drag styling. */
  let dragging = $state<SplitterId | null>(null)
  /** Pointer anchor for the in-flight drag; handlers only, so not reactive. */
  let dragOrigin: {
    which: SplitterId
    x: number
    widths: PaneWidths
    container: number
    moved: boolean
  } | null = null

  // Measure once the panes are in the DOM — and again after a reset clears
  // the widths, since reading a rect forces a synchronous layout and so sees
  // the default proportions rather than the pixels just discarded.
  $effect(() => {
    if (paneWidths !== null) return
    const measured = measurePaneWidths(panesEl)
    if (measured !== null) paneWidths = measured
  })

  function currentPaneWidths(): PaneWidths {
    return paneWidths ?? measurePaneWidths(panesEl) ?? fallbackPaneWidths()
  }

  function paneContainerWidth(): number {
    return panesEl?.getBoundingClientRect().width ?? 0
  }

  function startPaneDrag(which: SplitterId, event: PointerEvent): void {
    dragOrigin = {
      which,
      x: event.clientX,
      widths: currentPaneWidths(),
      container: paneContainerWidth(),
      moved: false,
    }
    dragging = which
    // Capture keeps the drag alive once the pointer leaves the 6px divider.
    // jsdom has no implementation, hence the guard.
    try {
      ;(event.currentTarget as HTMLElement | null)?.setPointerCapture?.(event.pointerId)
    } catch {
      // Capture is an enhancement; the drag works without it.
    }
    event.preventDefault()
  }

  function movePaneDrag(event: PointerEvent): void {
    if (dragOrigin === null) return
    dragOrigin.moved = true
    paneWidths = resizePane(
      dragOrigin.widths,
      dragOrigin.which,
      event.clientX - dragOrigin.x,
      dragOrigin.container,
    )
  }

  function endPaneDrag(event: PointerEvent): void {
    const origin = dragOrigin
    if (origin === null) return
    dragOrigin = null
    dragging = null
    try {
      ;(event.currentTarget as HTMLElement | null)?.releasePointerCapture?.(event.pointerId)
    } catch {
      // Nothing was captured (or jsdom): nothing to release.
    }
    if (origin.moved && paneWidths !== null) savePaneWidths(paneWidths)
  }

  /** Keyboard equivalent of a drag: arrows, 1px at a time with Shift held. */
  function nudgePane(which: SplitterId, event: KeyboardEvent, direction: 1 | -1): void {
    const step = event.shiftKey ? 1 : NUDGE_PX
    paneWidths = resizePane(currentPaneWidths(), which, direction * step, paneContainerWidth())
    savePaneWidths(paneWidths)
  }

  function onSplitterKeydown(which: SplitterId, event: KeyboardEvent): void {
    if (event.key === 'ArrowLeft') nudgePane(which, event, -1)
    else if (event.key === 'ArrowRight') nudgePane(which, event, 1)
    else if (event.key === 'Home') resetPaneWidths()
    else return
    event.preventDefault()
  }

  /** Double-click, or Home on a focused divider: back to the default layout. */
  function resetPaneWidths(): void {
    savePaneWidths(null)
    paneWidths = null
  }

  /**
   * Non-reactive "sync in progress" mutex. Deliberately NOT $state: tick()
   * runs inside the $effect body (via the tracker's immediate subscribe
   * callback), and a reactive read there would make the effect depend on
   * the flag — every sync would then re-run the effect and re-trigger a
   * sync, forever.
   */
  let syncing = false
  let syncDone: Promise<void> = Promise.resolve()

  // Non-reactive handles
  let db: SnpDB | undefined
  const index = $derived(new SnippetIndex(snippets))
  const tracker = new OnlineTracker()
  const dbPromise = openLocalDB()

  /** Snippets visible in the current folder, filtered by the current
   * query and the active tags (spec §6). Tag clicks, folder selection,
   * and the search box combine with AND, mirroring how tag: tokens and
   * the separate tag=/folder= params already interact server-side. */
  const visibleSnippets = $derived.by(() => {
    if (query.trim() !== '') {
      // index.search applies the tag:/lang: filters + FTS and keeps its
      // relevance order; folder + active tags narrow it further.
      return index.search(query).filter(matchesFolder).filter(matchesActiveTags)
    }
    return snippets
      .filter(matchesFolder)
      .filter(matchesActiveTags)
      .sort(byUpdatedThenIdDesc)
  })

  /** All tags with live-snippet counts (from the local cache, so the
   * pane works offline), most-used first, then alphabetically. */
  const tagItems = $derived.by(() => {
    const m = new Map<string, number>()
    for (const sn of snippets) {
      for (const t of sn.tags) m.set(t, (m.get(t) ?? 0) + 1)
    }
    return [...m.entries()]
      .map(([name, count]) => ({ name, count }))
      .sort((a, b) => b.count - a.count || a.name.localeCompare(b.name))
  })

  /**
   * Pinned snippets for the Favorites list (spec §4), newest first — the
   * same order the list pane uses. Built from the local cache, so the list
   * works offline; filter() returns a fresh array, so this never reorders
   * `snippets` itself.
   */
  const favoriteSnippets = $derived(
    snippets.filter((s) => s.pinned === true).sort(byUpdatedThenIdDesc),
  )

  const selectedSnippet = $derived(
    selectedSnippetId === null
      ? null
      : snippets.find((s) => s.id === selectedSnippetId) ?? null,
  )

  const selectedFolderName = $derived.by(() => {
    const fid = selectedSnippet?.folder_id
    if (fid === undefined || fid === null) return null
    return folderOptions(folders).find((f) => f.id === fid)?.path ?? null
  })

  function matchesFolder(s: Snippet): boolean {
    return selectedFolderId === null || s.folder_id === selectedFolderId
  }

  /** True when the snippet carries every active tag (AND semantics). */
  function matchesActiveTags(s: Snippet): boolean {
    return activeTags.every((t) => s.tags.includes(t))
  }

  /** Toggle a tag in the active filter (re-clicking clears it). */
  function toggleTag(name: string): void {
    activeTags = activeTags.includes(name)
      ? activeTags.filter((t) => t !== name)
      : [...activeTags, name]
  }

  function byUpdatedThenIdDesc(a: Snippet, b: Snippet): number {
    if (a.updated_at !== b.updated_at) return a.updated_at < b.updated_at ? 1 : -1
    return a.id < b.id ? 1 : -1
  }

  async function loadLocal(): Promise<void> {
    if (!db) return
    const [fs, ss] = await Promise.all([allFolders(db), allSnippets(db)])
    folders = fs
    snippets = ss
  }

  /** One sync cycle; failures are surfaced and retried on the next tick. */
  async function doSync(): Promise<void> {
    if (!db || !ready || untrack(() => saving) || syncing) return
    let finishSync!: () => void
    syncDone = new Promise<void>((resolve) => { finishSync = resolve })
    syncing = true
    busy = true
    try {
      await syncLocal(db)
      tracker.fetchSucceeded()
      syncedAt = Date.now()
      error = null
      await loadLocal()
      clearRevealed()
    } catch (e) {
      if (e instanceof ApiError && e.isNetworkError) tracker.fetchFailed()
      error = e instanceof Error ? e.message : String(e)
    } finally {
      busy = false
      syncing = false
      finishSync()
    }
  }

  /** Fire-and-forget sync (timer, reconnect, wake). */
  function tick(): void {
    if (syncing) return
    void doSync()
  }

  function fail(e: unknown): void {
    if (e instanceof ApiError && e.isNetworkError) tracker.fetchFailed()
    error = e instanceof Error ? e.message : String(e)
  }

  // Open the DB, load the cache, then start syncing.
  $effect(() => {
    let handle: SnpDB | undefined
    let cancelled = false
    dbPromise.then(async (h) => {
      if (cancelled) {
        h.close()
        return
      }
      handle = h
      db = h
      await loadLocal()
      ready = true
      void doSync()
    })
    return () => {
      cancelled = true
      handle?.close()
    }
  })

  // Release version for the header: refresh from the server once (the
  // cached value already rendered), and keep the cache for offline loads.
  // Reads nothing reactive, so it runs once and never re-triggers a sync.
  $effect(() => {
    void resolveVersion().then((v) => {
      if (v !== null) version = v
    })
  })

  // Age the relative "Synced …" label. Kept out of the sync effect on
  // purpose: a reactive read of `now` there would re-run the effect on
  // every tick and re-trigger a sync (the same hazard the non-reactive
  // `syncing` mutex exists to avoid).
  $effect(() => {
    const timer = setInterval(() => (now = Date.now()), CLOCK_TICK_MS)
    return () => clearInterval(timer)
  })

  // Connectivity, sync interval and wake-up triggers, active once ready.
  // Spec §6: sync on mount, on reconnect, on visibility/focus, and every
  // 5 minutes — mobile OSes suspend background apps, so the timer alone
  // is not enough.
  $effect(() => {
    if (!ready) return
    const timer = setInterval(tick, SYNC_INTERVAL_MS)
    const untrack = tracker.trackBrowser()
    const unwake = onWake(tick)
    const unsub = tracker.subscribe((o) => {
      online = o
      if (o) tick()
    })
    return () => {
      clearInterval(timer)
      untrack()
      unwake()
      unsub()
    }
  })

  // --- Folders (server writes: online only, spec §6) ---

  /** The open modal, if any. Native window.prompt/confirm dialogs are
   * not available in the wails desktop webview (WKWebView does not
   * implement them), so folder creation, full resync, and snippet
   * deletion confirmations are in-app dialogs that work in browsers and
   * in the window alike. */
  type Dialog =
    | { kind: 'folder'; parentId: string | null }
    | { kind: 'resync' }
    | { kind: 'discard'; proceed: () => void }
    | { kind: 'snippetDelete'; id: string }
    | null
  let dialog = $state<Dialog>(null)
  let folderName = $state('')
  function promptFolder(parentId: string | null): void {
    if (!online) return
    folderName = ''
    dialog = { kind: 'folder', parentId }
  }

  function promptResync(): void {
    if (!db || syncing || saving) return
    settingsOpen = false
    leaveEditor(() => {
      editing = false
      editingSnippet = null
      dialog = { kind: 'resync' }
    })
  }

  // Starter snippets (spec §5): applied on request, never automatically.
  // Seeding writes through the import path, so the new rows arrive in the
  // local cache on the next sync, like any other server-side change.
  let seedBusy = $state(false)
  let seedNote: string | null = $state(null)

  async function addStarterSnippets(): Promise<void> {
    if (seedBusy || !online) return
    seedBusy = true
    seedNote = null
    try {
      const res = await api.seedStarter()
      await doSync()
      seedNote =
        res.created > 0
          ? `Added ${res.created} starter snippets.`
          : 'Starter snippets were already there — refreshed.'
    } catch (e) {
      seedNote = e instanceof Error ? e.message : String(e)
    } finally {
      seedBusy = false
    }
  }

  function cancelDialog(): void {
    dialog = null
  }

  /** Label for the delete confirmation, naming the snippet when known. */
  function deleteTargetLabel(): string {
    const d = dialog
    if (d === null || d.kind !== 'snippetDelete') return 'this snippet'
    const title = snippets.find((s) => s.id === d.id)?.title
    return title === undefined || title === '' ? 'this snippet' : `“${title}”`
  }

  function confirmDialog(): void {
    const d = dialog
    if (d === null) return
    dialog = null
    if (d.kind === 'discard') {
      dirty = false
      d.proceed()
    } else if (d.kind === 'folder') {
      const name = folderName.trim()
      if (name === '') return
      void createFolder(name, d.parentId)
    } else if (d.kind === 'snippetDelete') {
      void deleteSnippet(d.id)
    } else {
      void fullResync()
    }
  }

  async function createFolder(name: string, parentId: string | null): Promise<void> {
    if (!db || !online) return
    try {
      const f = await api.createFolder(name, parentId)
      await putFolder(db, f)
      folders = [...folders, f]
      selectedFolderId = f.id
    } catch (e) {
      fail(e)
    }
  }

  async function renameFolder(id: string, name: string): Promise<void> {
    if (!db || !online) return
    try {
      const f = await api.updateFolder(id, { name })
      await putFolder(db, f)
      folders = folders.map((x) => (x.id === id ? f : x))
    } catch (e) {
      fail(e)
    }
  }

  async function deleteFolder(id: string): Promise<void> {
    if (!db || !online) return
    try {
      await api.deleteFolder(id)
      await removeFolder(db, id)
      folders = folders.filter((f) => f.id !== id)
      if (selectedFolderId === id) selectedFolderId = null
    } catch (e) {
      fail(e)
    }
  }

  // --- Snippets (server writes: online only, spec §6) ---

  function startCreate(): void {
    if (!online || !ready) return
    leaveEditor(() => {
      clearRevealed()
      editorKey++
      saveError = null
      dirty = false
      editing = true
      editingSnippet = null
      if (compact) nav.openDetail()
    })
  }

  /**
   * Enter edit mode for the selected snippet.
   *
   * Sensitive snippets are cached with body null (never written to
   * IndexedDB, spec §6), so the editor cannot be seeded from the local
   * row: a form seeded with '' would full-replace the snippet with an
   * empty body on save. Fetch the decrypted body first and refuse to
   * open the editor when the fetch fails. The revealed text is also
   * kept in `revealed` so the rest of the UI agrees.
   */
  async function startEdit(): Promise<void> {
    if (!online || !selectedSnippet || saving) return
    let s = selectedSnippet
    const epoch = ++detailEpoch
    if (s.body === null) {
      try {
        s = await api.getSnippet(s.id)
        if (s.body === null) return // server has no body for it either
      } catch (e) {
        fail(e)
        return
      }
      if (epoch !== detailEpoch || selectedSnippetId !== s.id) return
      if (s.is_sensitive) revealed = { [s.id]: { body: s.body, var_defaults: s.var_defaults ?? {} } }
    }
    editorKey++
    saveError = null
    dirty = false
    detailCopy = null
    editing = true
    editingSnippet = s
    if (compact) nav.openDetail()
  }

  /**
   * Leave the editor. In compact mode a cancelled *create* returns to the
   * list it was opened from; a cancelled edit stays on the snippet.
   */
  function cancelEdit(): void {
    leaveEditor(() => {
      const wasCreate = editing && editingSnippet === null
      editing = false
      editingSnippet = null
      dirty = false
      clearRevealed()
      if (compact && wasCreate && navState.screen === 'detail') nav.back()
    })
  }

  async function saveSnippet(input: SnippetInput): Promise<void> {
    if (!db || !online || saving) return
    const target = editingSnippet?.id
    saving = true
    saveError = null
    try {
      await syncDone
      if (!online) throw new Error('Offline — reconnect before saving.')
      const response = target
        ? await api.updateSnippet(target, input)
        : await api.createSnippet(input)
      const s = cacheSnippet(response)
      await putSnippet(db, s)
      clearRevealed()
      if (s.is_sensitive) revealed = { [s.id]: { body: input.body, var_defaults: input.var_defaults ?? {} } }
      snippets = snippets.some((x) => x.id === s.id)
        ? snippets.map((x) => (x.id === s.id ? s : x))
        : [...snippets, s]
      dirty = false
      editing = false
      editingSnippet = null
      selectedSnippetId = s.id
      selectedFolderId = input.folder_id
      error = null
    } catch (e) {
      fail(e)
      saveError = e instanceof Error ? e.message : String(e)
    } finally {
      saving = false
    }
  }

  /**
   * Write a snippet through the API's full-replace endpoint, rebuilding the
   * complete payload from the cached row and merging `patch` over it.
   *
   * The PUT replaces every field, so a caller that changes one thing must
   * still send the rest. Rebuilding the payload here once is what keeps a
   * pin toggle from dropping var_defaults, and a defaults save from
   * clearing the pin. Sensitive rows are fetched first: list/sync responses
   * carry neither their body nor their defaults (spec §5), so a payload
   * built from one would blank both.
   */
  async function replaceSnippet(
    id: string,
    patch: Partial<SnippetInput>,
  ): Promise<boolean> {
    if (!db || !online || saving) return false
    saving = true
    const epoch = detailEpoch
    try {
      await syncDone
      if (!online) throw new Error('Offline — reconnect before saving.')
      let s = snippets.find((x) => x.id === id)
      if (s === undefined) return false
      if (s.body === null) s = await api.getSnippet(id)
      if (s.body === null) throw new Error('Snippet body is unavailable. Try again.')
      const input: SnippetInput = {
        title: s.title,
        body: s.body,
        language: s.language,
        notes: s.notes,
        folder_id: s.folder_id,
        tags: s.tags,
        is_sensitive: s.is_sensitive,
        uses_variables: s.uses_variables,
        pinned: s.pinned ?? false,
        var_defaults: s.var_defaults ?? {},
        ...patch,
      }
      const updated = cacheSnippet(await api.updateSnippet(id, input))
      await putSnippet(db, updated)
      if (epoch === detailEpoch && selectedSnippetId === id && revealed[id] !== undefined) {
        detailCopy = null
        revealed = updated.is_sensitive ? { [id]: { body: input.body, var_defaults: input.var_defaults ?? {} } } : {}
      }
      snippets = snippets.map((x) => (x.id === id ? updated : x))
      return true
    } catch (e) {
      fail(e)
      return false
    } finally {
      saving = false
    }
  }

  /**
   * Persist per-variable defaults (spec §4/§6). The panel only shows when
   * the body is available locally, so this never blanks the body.
   */
  function saveDefaults(id: string, defaults: Record<string, string>): Promise<boolean> {
    return replaceSnippet(id, { var_defaults: defaults })
  }

  /** Pin or unpin a snippet (spec §4) — the Favorites list reads the flag. */
  function setPinned(id: string, pinned: boolean): Promise<boolean> {
    return replaceSnippet(id, { pinned })
  }

  async function deleteSnippet(id: string): Promise<void> {
    if (!db || !online || saving) return
    saving = true
    try {
      await syncDone
      if (!online) throw new Error('Offline — reconnect before deleting.')
      await api.deleteSnippet(id)
      await removeSnippet(db, id)
      snippets = snippets.filter((s) => s.id !== id)
      if (selectedSnippetId === id) {
        selectedSnippetId = null
        clearRevealed()
      }
    } catch (e) {
      fail(e)
    } finally {
      saving = false
    }
  }

  /**
   * Reveal a sensitive body (spec §6): the body is never cached, so it
   * requires a live connection.
   */
  async function reveal(id: string): Promise<void> {
    if (!online || saving) return
    const epoch = ++detailEpoch
    try {
      const s = await api.getSnippet(id)
      if (epoch !== detailEpoch || selectedSnippetId !== id) return
      if (s.body !== null) revealed = { [id]: { body: s.body, var_defaults: s.var_defaults ?? {} } }
    } catch (e) {
      fail(e)
    }
  }

  /**
   * Copy the rendered body to the clipboard. SnippetDetail computes the
   * text — filling in template variables (spec §4) — and passes it here.
   * The copy buttons are disabled while the body is hidden, so the text is
   * always available when this runs. lib/clipboard.ts performs the write
   * (with a webview fallback) and rejects when the text did not reach the
   * clipboard, which the button reports as "Copy failed".
   */
  async function copySelected(text: string): Promise<void> {
    await writeClipboard(text)
  }

  /**
   * Select a snippet from the list, by click or by the arrow keys.
   * Selecting always leaves edit mode — the list's click handler did this
   * inline until the keyboard path needed the same behavior.
   */
  function selectSnippet(id: string): void {
    setSelection(id, true)
  }

  /**
   * Change the selection without navigating: the arrow keys in the search
   * box walk the list in place, so in compact mode they must not push the
   * detail screen (spec §6).
   */
  function setSelection(id: string, openDetail = false): void {
    leaveEditor(() => {
      clearRevealed()
      selectedSnippetId = id
      editing = false
      editingSnippet = null
      dirty = false
      if (compact && openDetail) nav.openDetail()
    })
  }

  /** Focus the search field and select its text, so typing replaces it. */
  function focusSearch(): void {
    const el = searchEl
    if (el === undefined) return
    el.focus()
    el.select()
  }

  /**
   * Move the selection through the visible list (spec §6 keyboard
   * discipline). The ends wrap; with nothing selected, ArrowDown picks the
   * first row and ArrowUp the last.
   */
  function moveSelection(delta: 1 | -1): void {
    const items = visibleSnippets
    if (items.length === 0) return
    const at = items.findIndex((s) => s.id === selectedSnippetId)
    const next =
      at === -1
        ? delta === 1
          ? 0
          : items.length - 1
        : (at + delta + items.length) % items.length
    setSelection(items[next].id)
  }

  /**
   * The copy text for the current selection: what the detail pane last
   * published, or — while it is not mounted, i.e. while editing — the
   * cached body rendered with its saved defaults. Null means there is
   * nothing to copy, which is the case for a sensitive body that has not
   * been revealed.
   */
  function copyTextForSelection(): string | null {
    const s = selectedSnippet
    if (s === null) return null
    if (detailCopy !== null && detailCopy.id === s.id) return detailCopy.text
    const body = s.body ?? revealed[s.id]?.body ?? null
    if (body === null) return null
    return s.uses_variables ? renderTemplate(body, revealed[s.id]?.var_defaults ?? s.var_defaults ?? {}) : body
  }

  /**
   * Copy the selected snippet from the keyboard. Failures go to the usual
   * error report; success flashes a topbar confirmation, because unlike the
   * copy buttons there is no control under the cursor to change its label.
   */
  async function copyCurrentSelection(): Promise<void> {
    const text = copyTextForSelection()
    if (text === null) return
    try {
      await copySelected(text)
      flashNotice('Copied.')
    } catch (e) {
      fail(e)
    }
  }

  /** Show a short topbar confirmation (see `notice`). */
  function flashNotice(text: string): void {
    notice = text
    clearTimeout(noticeTimer)
    noticeTimer = setTimeout(() => (notice = null), NOTICE_MS)
  }

  // Drop a pending notice revert if the app unmounts.
  $effect(() => () => clearTimeout(noticeTimer))

  /**
   * Jump to the search field from anywhere. In compact mode that means
   * back to the list in one history move first, then focus once the list
   * is visible again (a hidden field cannot take focus).
   */
  function jumpToSearch(): void {
    if (compact && navState.depth > 0) {
      leaveEditor(() => {
        editing = false
        editingSnippet = null
        dirty = false
        clearRevealed()
        nav.toRoot()
        void settle().then(focusSearch)
      })
    } else {
      focusSearch()
    }
  }

  // --- Command palette (spec §6 keyboard) ---

  let paletteOpen = $state(false)

  /**
   * The palette's commands, built from the actions above. Every command
   * stays listed; the ones that cannot run right now carry the reason
   * (offline, nothing selected), so the palette also answers "why is this
   * greyed out". Snippet commands are dropped while the editor is open so
   * nothing can clobber an unsaved edit from the keyboard.
   */
  const commands = $derived.by((): Command[] => {
    const s = selectedSnippet
    const offline = saving ? 'Saving…' : online ? undefined : 'Offline'
    const needSelection = s === null ? 'Select a snippet first' : undefined
    const list: Command[] = []
    if (!editing) {
      list.push(
        { id: 'new', label: 'New snippet', group: 'snippet', disabled: offline, run: startCreate },
        {
          id: 'edit',
          label: 'Edit snippet',
          group: 'snippet',
          disabled: needSelection ?? offline,
          run: () => void startEdit(),
        },
        {
          id: 'copy',
          label: 'Copy snippet',
          group: 'snippet',
          disabled:
            needSelection ?? (copyTextForSelection() === null ? 'Body hidden' : undefined),
          run: () => void copyCurrentSelection(),
        },
        {
          id: 'pin',
          label: s?.pinned === true ? 'Unfavorite snippet' : 'Favorite snippet',
          group: 'snippet',
          disabled: needSelection ?? offline,
          run: () => {
            if (s !== null) void setPinned(s.id, s.pinned !== true)
          },
        },
        {
          id: 'delete',
          label: 'Delete snippet',
          group: 'snippet',
          disabled: needSelection ?? offline,
          run: () => {
            if (s !== null) dialog = { kind: 'snippetDelete', id: s.id }
          },
        },
      )
      if (s !== null && s.is_sensitive && revealed[s.id] === undefined) {
        list.push({
          id: 'reveal',
          label: 'Show body',
          group: 'snippet',
          disabled: offline,
          run: () => void reveal(s.id),
        })
      }
    }
    list.push(
      {
        id: 'folder',
        label: 'New folder',
        group: 'folder',
        disabled: offline,
        run: () => promptFolder(selectedFolderId),
      },
      {
        id: 'search',
        label: 'Focus search',
        group: 'app',
        shortcut: searchShortcutLabel(),
        run: jumpToSearch,
      },
      {
        id: 'resync',
        label: 'Resync',
        group: 'app',
        disabled: busy || saving ? 'Sync or save in progress' : undefined,
        run: tick,
      },
      { id: 'full-resync', label: 'Full resync', group: 'app', run: promptResync },
      {
        id: 'starter',
        label: 'Add starter snippets',
        group: 'app',
        disabled: offline ?? (seedBusy ? 'Adding…' : undefined),
        run: () => void addStarterSnippets(),
      },
    )
    for (const l of LAYOUTS) {
      list.push({
        id: `layout-${l.id}`,
        label: `Layout: ${l.label}`,
        group: 'app',
        disabled: layout === l.id ? 'Current' : undefined,
        run: () => (layout = l.id),
      })
    }
    return list
  })

  /**
   * Global keydown: the dialog shortcuts while a dialog is open, otherwise
   * the search workflow. Arrows and Enter are handled only while the search
   * field holds focus, so Enter in the snippet editor is never hijacked
   * (spec §6 keyboard discipline).
   */
  function onGlobalKeydown(e: KeyboardEvent): void {
    if (dialog !== null) {
      if (e.key === 'Escape') cancelDialog()
      return
    }
    if (isPaletteShortcut(e)) {
      e.preventDefault()
      paletteOpen = !paletteOpen
      return
    }
    // The palette owns the keyboard while open (its own handler answers
    // the arrows, Enter and Escape); nothing below may see those keys.
    if (paletteOpen) return
    if (isSearchShortcut(e)) {
      // Also stops Firefox focusing its own search bar on Ctrl+K.
      e.preventDefault()
      jumpToSearch()
      return
    }
    if (compact && e.key === 'Escape' && document.activeElement !== searchEl) {
      // Escape pops a level: the drawer, else detail → list — but never
      // out of the editor or a text field, where Cancel is explicit.
      if (navState.drawerOpen) {
        e.preventDefault()
        nav.back()
      } else if (navState.screen === 'detail' && !editing && !isTextInput(document.activeElement)) {
        e.preventDefault()
        nav.back()
      }
      return
    }
    if (document.activeElement !== searchEl) return
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      moveSelection(1)
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      moveSelection(-1)
    } else if (e.key === 'Enter') {
      e.preventDefault()
      void copyCurrentSelection()
    } else if (e.key === 'Escape') {
      // The first Escape clears the query, a second leaves the field.
      if (query !== '') {
        e.preventDefault()
        query = ''
      } else {
        searchEl?.blur()
      }
    }
  }

  /**
   * Full resync (spec §6 recovery path): clear the local cache, then sync
   * from scratch. The stored since is gone, so the next sync is a full
   * sync. Confirmation happens in the in-app dialog (promptResync) — the
   * native confirm() is unavailable in the desktop webview.
   */
  async function fullResync(): Promise<void> {
    if (!db || syncing || saving) return
    try {
      await clearLocalData(db)
      clearRevealed()
      syncedAt = null
      snippets = []
      folders = []
      await doSync()
    } catch (e) {
      fail(e)
    }
  }
</script>

<svelte:head>
  <title>snp</title>
</svelte:head>

<!-- Global keys: Escape cancels dialogs; Enter activates the focused
     control (the folder input handles its own Enter), otherwise the search workflow: ⌘/Ctrl+K focuses the
     field, arrows move the selection, Enter copies, Escape clears. Every
     handler but the focus shortcut is scoped to the search field so the
     snippet editor keeps its own keys. -->
<svelte:window onkeydown={onGlobalKeydown} onbeforeunload={(e) => {
  if (!dirty && !saving) return
  e.preventDefault()
  e.returnValue = ''
}} />

<div class="app" class:compact>
  <div class="chrome" inert={dialog !== null || paletteOpen}>
    <header class="topbar">
      {#if compact}
        <!-- One control at the left: Back whenever there is a level to
             pop (the drawer, or the detail screen), else the drawer toggle.
             Both go through the stack so the OS gesture and this button
             agree (lib/layout.ts). -->
        {#if navState.depth > 0}
          <button class="nav" aria-label="Back" onclick={() => nav.back()}>←</button>
        {:else}
          <button
            class="nav"
            aria-label="Folders"
            aria-expanded={navState.drawerOpen}
            onclick={() => nav.openDrawer()}
          >
            <svg class="control-icon" viewBox="0 0 24 24" aria-hidden="true">
              <path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z" />
            </svg>
          </button>
        {/if}
      {/if}
      <span class="brand">snp</span>
      {#if !compact}
        {#if version !== null}<span class="version">{version}</span>{/if}
        <span class="conn" class:offline={!online}>{online ? 'online' : 'offline'}</span>
      {/if}
      <span class="spacer"></span>
      {#if error !== null}<span class="error" title={error}>{error}</span>{/if}
      {#if notice !== null}<span class="notice" role="status">{notice}</span>{/if}
      {#if !compact}
        {@render syncStatus()}
      {/if}
      <div class="settings" bind:this={settingsEl}>
        <button
          aria-label="Settings"
          aria-expanded={settingsOpen}
          class:open={settingsOpen}
          disabled={!ready}
          onclick={() => (settingsOpen = !settingsOpen)}
        >
          <svg class="control-icon" viewBox="0 0 24 24" aria-hidden="true">
            <path d="M9.5 3h5l.6 2.5 1.7 1 2.5-.7 2.5 4.4-1.9 1.8v2l1.9 1.8-2.5 4.4-2.5-.7-1.7 1-.6 2.5h-5l-.6-2.5-1.7-1-2.5.7-2.5-4.4L4.1 14v-2l-1.9-1.8 2.5-4.4 2.5.7 1.7-1z" transform="translate(0 -1)" />
            <circle cx="12" cy="12" r="3" />
          </svg>
        </button>
        {#if settingsOpen}
          <div class="panel">
            {#if compact}
              <!-- The bar has no room for these in compact mode, so the
                   status line and Resync live here instead. -->
              <div class="status">
                {#if version !== null}<span class="version">{version}</span>{/if}
                <span class="conn" class:offline={!online}>{online ? 'online' : 'offline'}</span>
                {@render syncStatus()}
              </div>
              <hr />
            {/if}
            <label class="field">
              <span>Theme</span>
              <select aria-label="Theme" bind:value={theme}>
                {#each THEMES as t (t.id)}
                  <option value={t.id}>{t.label}</option>
                {/each}
              </select>
            </label>
            <label class="field">
              <span>Layout</span>
              <select aria-label="Layout" bind:value={layout}>
                {#each LAYOUTS as l (l.id)}
                  <option value={l.id}>{l.label}</option>
                {/each}
              </select>
            </label>
            <label class="field">
              <span>
                Interface text size
                <em>{textScale}%</em>
              </span>
              <input
                type="range"
                aria-label="Interface text size"
                min={TEXT_SCALE_MIN}
                max={TEXT_SCALE_MAX}
                step={TEXT_SCALE_STEP}
                bind:value={textScale}
              />
            </label>
            <label class="field checkbox">
              <input type="checkbox" bind:checked={twoLineTitles} />
              <span>Two-line titles in the list</span>
            </label>
            <hr />
            <p class="note keys">
              <kbd>{searchShortcutLabel()}</kbd> search · <kbd>{paletteShortcutLabel()}</kbd> commands
            </p>
            <div class="actions">
              <button onclick={() => void addStarterSnippets()} disabled={!online || seedBusy}>
                {seedBusy ? 'Adding…' : 'Add starter snippets'}
              </button>
              <button class="danger" onclick={promptResync}>Full resync</button>
            </div>
            {#if seedNote !== null}
              <p class="note">{seedNote}</p>
            {/if}
          </div>
        {/if}
      </div>
    </header>

    {#if ready && !online}
      <OfflineBanner />
    {/if}
  </div>

  <!-- The sync age and the Resync button: in the bar in wide mode, in the
       settings sheet in compact mode. -->
  {#snippet syncStatus()}
    {#if syncedAt !== null}
      <span class="synced" title={formatAbsolute(syncedAt)}>
        Synced {formatRelative(syncedAt, now)}
      </span>
    {/if}
    <button onclick={() => tick()} disabled={busy || saving || !ready}>Resync</button>
  {/snippet}

  <!-- Drag handle between two panes. The folders divider trades width with
       the list pane; the list divider is absorbed by the flexible detail
       pane (lib/panes.ts). Kept as a snippet so both dividers share the
       ARIA wiring and the keyboard path. -->
  {#snippet paneSplitter(
    which: SplitterId,
    label: string,
    value: number | null,
    min: number,
    max: number,
  )}
    <!-- Focusable "separator" is the ARIA window-splitter pattern: tabindex
         plus aria-value* make it operable while the role stays structural.
         The a11y linter does not model that, hence the ignores. -->
    <!-- svelte-ignore a11y_no_noninteractive_element_interactions, a11y_no_noninteractive_tabindex -->
    <div
      class="splitter"
      class:dragging={dragging === which}
      role="separator"
      aria-orientation="vertical"
      aria-label={label}
      aria-valuenow={value ?? undefined}
      aria-valuemin={min}
      aria-valuemax={max}
      tabindex="0"
      title="Drag to resize · double-click to reset"
      onpointerdown={(e) => startPaneDrag(which, e)}
      onpointermove={movePaneDrag}
      onpointerup={endPaneDrag}
      onpointercancel={endPaneDrag}
      onlostpointercapture={endPaneDrag}
      onkeydown={(e) => onSplitterKeydown(which, e)}
      ondblclick={resetPaneWidths}
    ></div>
  {/snippet}

  <main
    class="panes"
    inert={dialog !== null || paletteOpen}
    class:resizing={dragging !== null}
    bind:this={panesEl}
    style={compact ? '' : paneStyleVars(paneWidths)}
  >
    {#if compact && navState.drawerOpen}
      <!-- Covers the screen behind the drawer; a tap closes it, the same
           way as Back, through history. -->
      <button class="scrim" aria-label="Close folders" onclick={() => nav.closeDrawer()}
      ></button>
    {/if}
    <aside
      class="pane folders"
      use:modal={compact && navState.drawerOpen && dialog === null && !paletteOpen}
      class:open={navState.drawerOpen}
      aria-hidden={compact && !navState.drawerOpen ? 'true' : undefined}
    >
      <div class="pane-head">
        <h2>Folders</h2>
        <span class="head-actions">
          <button onclick={() => promptFolder(null)} disabled={!online}>New folder</button>
          {#if compact}
            <button class="close" aria-label="Close folders" onclick={() => nav.closeDrawer()}>
              ×
            </button>
          {/if}
        </span>
      </div>
      <Favorites
        snippets={favoriteSnippets}
        selectedId={selectedSnippetId}
        offline={!online || saving}
        onselect={selectSnippet}
        onunpin={(id) => void setPinned(id, false)}
      />
      <FolderTree
        {folders}
        selectedId={selectedFolderId}
        offline={!online}
        onselect={(id) => {
          selectedFolderId = id
          if (compact) nav.closeDrawer()
        }}
        oncreate={(pid) => promptFolder(pid)}
        onrename={(id, name) => void renameFolder(id, name)}
        onremove={(id) => void deleteFolder(id)}
      />
      <TagList tags={tagItems} active={activeTags} onselect={toggleTag} />
    </aside>

    {#if !compact}
      {@render paneSplitter(
        'folders',
        'Resize folders pane',
        paneWidths?.folders ?? null,
        FOLDERS_MIN,
        FOLDERS_MAX,
      )}
    {/if}

    <section class="pane list" inert={compact && navState.drawerOpen} hidden={compact && navState.screen === 'detail'}>
      <SnippetList
        snippets={visibleSnippets}
        selectedId={selectedSnippetId}
        {query}
        offline={!online || !ready || saving}
        twoLine={twoLineTitles}
        bind:searchEl
        onselect={selectSnippet}
        onsearch={(q) => (query = q)}
        oncreate={startCreate}
        onfolders={compact ? () => nav.openDrawer() : undefined}
        visible={!compact || navState.screen === 'list'}
      />
    </section>

    {#if !compact}
      {@render paneSplitter(
        'list',
        'Resize snippet list pane',
        paneWidths?.list ?? null,
        LIST_MIN,
        LIST_MAX,
      )}
    {/if}

    <section class="pane detail-pane" inert={compact && navState.drawerOpen} hidden={compact && navState.screen === 'list'}>
      {#if editing}
        {#key editorKey}
          <SnippetForm
            initial={editingSnippet}
            {folders}
            defaultFolderId={selectedFolderId}
            onsave={saveSnippet}
            {saving}
            offline={!online}
            error={saveError}
            ondirty={(value) => (dirty = value)}
            oncancel={cancelEdit}
          />
        {/key}
      {:else if selectedSnippet !== null}
        {#key selectedSnippet.id}
        <SnippetDetail
          snippet={selectedSnippet}
          body={revealed[selectedSnippet.id]?.body ?? null}
          defaults={revealed[selectedSnippet.id]?.var_defaults ?? selectedSnippet.var_defaults ?? {}}
          folderName={selectedFolderName}
          offline={!online}
          {saving}
          oncopy={(text) => copySelected(text)}
          onedit={startEdit}
          onremove={() => (dialog = { kind: 'snippetDelete', id: selectedSnippet.id })}
          onreveal={() => void reveal(selectedSnippet.id)}
          onsavedefaults={(defaults) => saveDefaults(selectedSnippet.id, defaults)}
          oncopytext={(id, text) => (detailCopy = { id, text })}
          onpin={() => void setPinned(selectedSnippet.id, selectedSnippet.pinned !== true)}
        />
        {/key}
      {:else}
        <div class="empty">
          <p>Select a snippet, or create a new one.</p>
        </div>
      {/if}
    </section>
  </main>

  {#if dialog}
    <!-- Clicking the backdrop does not dismiss: Escape and the Cancel
         button do (keyboard parity, spec §6 keyboard discipline). -->
    <div class="modal-backdrop">
      <div
        class="modal"
        use:modal
        role="dialog"
        aria-modal="true"
        aria-label={dialog.kind === 'folder'
          ? 'New folder'
          : dialog.kind === 'resync'
            ? 'Full resync'
            : dialog.kind === 'discard' ? 'Unsaved changes' : 'Delete snippet'}
        tabindex="-1"
      >
        {#if dialog.kind === 'folder'}
          <h2>New folder</h2>
          <input
            data-modal-initial
            bind:value={folderName}
            placeholder="Folder name"
            aria-label="Folder name"
            onkeydown={(e) => {
              if (e.key === 'Enter') confirmDialog()
              else if (e.key === 'Escape') cancelDialog()
            }}
          />
        {:else if dialog.kind === 'discard'}
          <h2>Unsaved changes</h2>
          <p>Discard your changes and continue?</p>
        {:else if dialog.kind === 'resync'}
          <h2>Full resync</h2>
          <p>Clear the local cache and re-sync everything from the server?</p>
        {:else}
          <h2>Delete snippet?</h2>
          <p>Delete {deleteTargetLabel()}? This can't be undone.</p>
        {/if}
        <div class="modal-actions">
          {#if dialog.kind === 'discard'}
            <button class="danger" onclick={confirmDialog}>Discard changes</button>
          {:else if dialog.kind === 'snippetDelete'}
            <button class="danger" onclick={confirmDialog}>Delete</button>
          {:else}
            <button
              class="primary"
              disabled={dialog.kind === 'folder' && folderName.trim() === ''}
              onclick={confirmDialog}
            >
              {dialog.kind === 'folder' ? 'Create' : 'Clear and resync'}
            </button>
          {/if}
          <button onclick={cancelDialog}>{dialog.kind === 'discard' ? 'Keep editing' : 'Cancel'}</button>
        </div>
      </div>
    </div>
  {/if}

  {#if paletteOpen}
    <CommandPalette {commands} onclose={() => (paletteOpen = false)} />
  {/if}
</div>
