# snp — Work Log

Append-only log of implementation work against
`docs/snp-implementation-plan.md`. Newest entry at the bottom.

**To resume after an interruption:**

1. Read "Current status" below.
2. Read the last log entry.
3. Run `go test ./...` and confirm the tree is green before starting new
   work.

Spec: `docs/snp-design.md` · Plan: `docs/snp-implementation-plan.md`

## Current status

- Duplicate-snippet session (2026-09-21): toolbar/palette duplication opens
  a guarded create draft with copied metadata and sensitivity. Full checks,
  production UI build and browser create/history smoke pass. Changes remain
  local after commit 17f3064; see the latest log entry.

- Password-protection session (2026-09-21): app exports/backups default to
  age encryption; encrypted JSON import and offline `snp decrypt` recovery
  are implemented. Full tests and production builds pass. Preview is
  updated; changes remain local and uncommitted. See newest log entry.

- Data-management session (2026-09-21): snp JSON import/preview, export,
  and full database/key backups are implemented locally after recovery
  commit 42bfd53. Required checks and production builds pass. No release
  or push in this session; see the newest log entry for validation limits.
- Recovery session (2026-09-21): Trash/Restore and 50-version history are
  implemented in the working tree after v0.4.0. Full checks and browser
  smoke pass; see the newest entry. No new release/tag/push in this session.
- Updated: 2026-09-13 (auto-deploy from the checkout on the dev box; command palette)
- Phase: review fixes + desktop app (macOS **and Linux** builds) + appearance + AI generation (command/script/function kinds) + tag filter + .app bundle + **bundled starter pack** merged to main; **Markdown notes**, **read-view syntax highlighting**, **notarization in `make app`**, the **Linux desktop build/launcher** and **`snp seed`** landed; the **GitHub release workflows** and the **header version chip** (`GET /api/version`) landed; **v0.1.0 shipped** (signed + notarized macOS bundle, 7 assets, verified after publish); **draggable pane dividers** landed (spec §6, `web/src/lib/panes.ts`) and **Explain now replaces Notes** with an undo (spec §13); **Phase 10, the UI refinement pass**, is on branch `feat/ui-refinements` (**pushed**, and deployed to the tailnet from a dirty tree — `/api/version` reports `v0.1.0-14-g8e82a64-dirty`): explicit copy actions, distinct create labels, simplified timestamps, the search keyboard workflow, visible saved-default state, two-line titles, and the **Favorites** list on a new `pinned` column; plus the **service-worker update check** and **create/cancel test coverage** added while chasing a stale-shell report; `make test` green (go test + vet + 298 Vitest + svelte-check 0 errors / 0 warnings). The Linux desktop binary **build was verified on an ARM Ubuntu 24 host** (git bundle → `make desktop`), after a first attempt failed because that work was still uncommitted and the bundle therefore carried the old darwin-only tree.
- Next: **Phase 11 on-device checklist** (plan Phase 11 T7; branch `claude/eloquent-maxwell-bcugjn`, T1–T6 built and green): iOS Safari as a tab and as the installed PWA (swipe-back at each depth), Android Chrome hardware back (drawer → detail → leaves the app), the wails app with Settings → Layout = Compact; then merge. After that, as before: **Linux desktop container build + verification** (podman; `libgtk-3-dev` + `libwebkit2gtk-4.1-dev` + Go, `make web` then the desktop build, and exercise `install-desktop.sh` with a scratch `PREFIX=`); then the Windows port, desktop follow-ons (real app icon, startup-error surfacing in the window); then remaining v1 follow-ons (CLI client, SnippetsLab converter, named variable presets per machine — the follow-on named in Phase 10 T5). `feat/ui-refinements` is merged to main and two betas are published (`v0.2.0-beta.1`, and `v0.2.0-beta.2` as the CI validation build); cutting `v0.2.0` is the next release step whenever wanted

## Log

### 2026-09-02

- 09:45 — Started execution. Environment: Go 1.24.4 darwin/arm64, Node
  26.4.0, npm 11.17.0. Repo is a git worktree.
  Created this log.
- 13:00 — Phase 1 (config) and Phase 2 (store) complete; full suite
  green. Fixed the FTS5 "database disk image is malformed" corruption:
  root cause was the old `rewriteFTS` helper DELETE-ing the FTS row on
  every write, including for rowids never indexed — a DELETE for an
  unindexed rowid corrupts the FTS5 external-content index. Split into
  `insertFTSTx`/`deleteFTSTx`: new rows insert only; replace deletes the
  old FTS row before the content update, then inserts; purge deletes FTS
  before the main row. Also: full sync (`since=""`) now returns
  tombstones for soft-deleted snippets and folders;
  `TestFTSUnbalancedQuote` expectation corrected (phrase `abc"` tokenizes
  to `abc` and matches the title → 1 result, not 0);
  `zz_debug_test.go` integrity-check corrected to the FTS5 command form
  `INSERT INTO snippets_fts(snippets_fts) VALUES('integrity-check')`
  (zero rows = consistent). Spec §4 now documents the
  `snippets.body_text` and `snippets.tags` mirror columns and the
  corrected FTS write discipline. Next: Phase 3 (internal/tsauth).
- 14:21 — Phase 3 (internal/tsauth) complete; full suite green. Added
  `tailscale.com v1.102.3` as a direct dependency (go.mod now
  `go 1.26.6`). Package: `Identity{Login,DisplayName}` and the
  `IdentityResolver` interface; `Tailscale` wraps `*tsnet.Server`
  behind a narrow `server`/`whoisClient` interface (adapter
  `realServer`, fakes in tests): `New(stateDir, hostname, authKey)`
  persists node state under `stateDir/tsnet` (0700), authKey only
  needed for the first join; `Listen(ctx)` brings the node up, fails
  with a clear error if another tailnet node already uses this
  hostname (Tailscale allows duplicate names, so peer HostNames are
  checked case-insensitively), then returns a TLS listener on tailnet
  :443 with a Tailscale-issued cert; `WhoIs` maps
  `LocalClient().WhoIs(...).UserProfile` to `Identity` and wraps
  errors so the server can map them to 500. `Dev` returns a fixed
  `dev@local` identity for `--dev-listen`. API note: v1.102.3 has no
  `Listen("https", ...)` — the cert listener is
  `ListenTLS("tcp", ":443")`, which requires MagicDNS + HTTPS enabled
  in the tailnet admin panel (clear startup error otherwise). 12 tests
  pass. Next: Phase 4 (internal/server).
- 16:37 — Phase 4 (internal/server) complete; full suite + `go vet`
  green. The full HTTP surface from spec §5, plus the `serve` subcommand.
  `internal/server/server.go`: `Server` + `New(st, resolver, owner, log)`
  (embeds `web/dist` via `fs.Sub`, panics if missing) and `Handler()`
  returning the root mux. Middleware chain, outermost→innermost:
  `recoverMW` (panic→500) → `loggingMW` (method, path, status,
  duration_ms, caller login; query string deliberately omitted per spec
  §9) → then, on `/api/*` only: `authMW` (`resolver.WhoIs(RemoteAddr)`;
  whois error→500, non-owner→403 "forbidden", else identity into ctx) and
  `guardMW` (POST/PUT/DELETE must be `application/json` else 415;
  `MaxBytesReader` 10 MiB→413). The embedded SPA is served without auth so
  the shell loads for any tailnet peer; every data endpoint still requires
  the configured owner (pass `owner=""` to disable, dev mode).
  `handlers.go`: `apiMux` with all 15 endpoints — `me`; snippet
  list/create/get/replace/delete/raw; folder list/create/update/delete;
  tags; sync; export; import. `snippetReq`/`folderReq`/`folderUpdateReq`
  use pointer fields so an omitted field means "no change". `statusCode()`
  maps store errors per spec §8: `ErrNotFound`→404,
  `ErrFolderNotEmpty`/`ErrNameTaken`→409, `ErrFolderCycle`/
  `ErrInvalidTag`/`ErrInvalid`/`ErrFTS`/`ErrImport`→400, else 500; 5xx
  details are logged, never returned. `handleBodyErr`: `*http.MaxBytesError`
  →413, else 400. `static.go`: `staticHandler` serves the embedded SPA;
  unknown extensionless paths fall back to `index.html` (client-side
  routes), unknown paths with an extension 404.
  `cmd/snp/main.go`: `serve` subcommand wired (was `notImplemented`).
  `runServe` flags: `--config`, `--hostname`, `--owner`, `--state-dir`,
  `--log-level`, `--dev-listen` (plain HTTP, no tsnet, no auth). Flow:
  `config.Load` → `signal.NotifyContext` (SIGINT/SIGTERM) → `MkdirAll`
  state dir 0700 → open+migrate DB → load/create key → tsnet join or dev
  listener → start purge job → serve → graceful drain + shutdown.
  `server_test.go`: httptest-based tests with a fake `IdentityResolver`
  and a temp-file store, covering me, snippet CRUD, folder CRUD (incl. 409
  sibling-name collision, 400 move-cycle), tags, sync tombstones,
  export/import, error mapping (404/409/400/415/413), static SPA fallback,
  and auth (403 non-owner, 500 whois failure). Test gotcha fixed: reusing
  one `syncResp` across two `json.Unmarshal` calls made `encoding/json`
  merge into the existing slice-element map, leaving a stale `title` key on
  the tombstone — unmarshal into a fresh variable instead. Next: Phase 5
  (CLI subcommands).

### 2026-09-03

- 00:40 — Phase 5 (CLI subcommands) complete; full suite + `go vet`
  green. (Session resumed mid-phase at 18:07 on 09-02;
  `cmd/snp/main.go` was found in a partially reconstructed state and
  was rewritten to match the Phase 4 log entry and spec §1.)
  `internal/store/backup.go`: `Store.Backup(dest, fullCheck)` —
  `VACUUM INTO` (dest is a quoted string literal, `'` doubled; VACUUM
  INTO takes no bound parameters); destination must not exist (error
  otherwise); parent dirs created 0700; partial file removed on
  failure; then the copy is opened read-only and verified with
  `PRAGMA quick_check` (or `integrity_check` if fullCheck) — anything
  other than "ok" is an error. The result is a standalone database
  file, no WAL sidecar.
  `cmd/snp/main.go`: all subcommands implemented (were
  `notImplemented`). `backup <dest>` (`-full-check` flag): quiet on
  success, errors to stderr; refuses to run when no database exists
  yet — `store.Open` would create an empty one, and a cron job
  silently "backing up" an empty database after a state-dir typo is a
  footgun. `export [-o file]`: full JSON export via `Store.Export`
  (plaintext bodies, sensitive decrypted), pretty-printed.
  `import <file>` (`-mode merge|replace`): reads the JSON document,
  applies via `Store.Import`, prints created/updated counts.
  `key show-path`: prints the key file path for backup scripts. All
  subcommands load config the same way as `serve` (flags > env > file
  > default) and never touch tsnet. `runServe` rebuilt to match the
  Phase 4 log: `config.Load` → `signal.NotifyContext` (SIGINT/SIGTERM)
  → `MkdirAll` state dir 0700 → open+migrate DB → load/create key →
  tsnet join (`TS_AUTHKEY` env) or `--dev-listen` plain HTTP with
  `tsauth.Dev` and no auth → `StartPurger` → serve → graceful drain
  (`http.Server.Shutdown`, 10s) on signal.
  `cmd/snp/main_test.go`: tests for the testable cores
  (`backupCmd`, `exportCmd`, `importCmd`): backup produces a
  consistent, readable copy (reopened, exported, sensitive body
  decrypted) and refuses when no database exists; export is
  pretty-printed with plaintext bodies including the sensitive one;
  import is idempotent on re-import (created 2/updated 0, then 0/2),
  resolves `folder_path` to the document's folder, re-encrypts
  sensitive bodies, and rejects malformed JSON and bad modes.
  `TestExportImportRoundTrip` satisfies the plan's "export→import
  round trip preserves data" done-when: seed → export → map
  `ExportDoc` to `ImportDoc` → import into a fresh state dir (fresh
  key, so sensitive bodies are re-encrypted under the new key with
  the same ids) → export → field-by-field compare (title, body,
  language, notes, folder, sensitive flag, timestamps).
  Smoke-tested the built binary: `key show-path`, `export` on a fresh
  state dir, `backup`, `import` round trip, and the error paths.
  Note: Go's `flag` stops parsing at the first non-flag argument, so
  flags must precede positional args (`snp backup [flags] <dest>`).
  Next: Phase 6 (frontend, online flows).

### 2026-09-03

- 12:46 — Phase 6 (frontend, online flows) complete; `make test`
  green (go test + 108 Vitest + svelte-check 0 errors/warnings).
  `web/` is now Vite + Svelte 5 (runes) + TypeScript strict, Vitest
  under jsdom, tests colocated with each module. `lib/`:
  `types.ts` (API types), `api.ts` (typed fetch, throws `ApiError`
  with status; /me, snippets CRUD + list with q/lang/folder_id/
  limit/offset, folders CRUD, /sync), `query.ts` (spec §5
  `tag:`/`lang:` filters + FTS remainder, shared by online and
  offline search), `templates.ts` (`{{var}}` /
  `{{var|default}}`), `db.ts` (IndexedDB via `idb`; sensitive
  bodies never persisted), `sync.ts` (pull + idempotent merge —
  upsert, tombstones, `server_time` boundary — and push),
  `search.ts` (MiniSearch, incremental updates on merge),
  `online.ts` (navigator.onLine + fetch-failure tracking).
  Components: `FolderTree` (create/rename/delete, expand/collapse),
  `SnippetList` (debounced search, pagination, sensitive
  masking), `SnippetDetail` (reveal via GET, tags, edit/delete),
  `SnippetForm` (create/edit, template expansion, sensitive
  toggle, inline errors that keep editor state, Cmd/Ctrl+Enter
  save). `App.svelte`: three-pane layout with offline banner
  (offline = read-only) and online/offline state wiring.
  Gotchas: (1) `vite.config.ts` needs
  `resolve.conditions: ['browser']` or `svelte` resolves to its
  server entry under jsdom and component tests lose `mount`.
  (2) Svelte 5: no `key` prop on components — use a `{#key}`
  block to force remount; `$:` legacy reactive statements are out
  (runes); `state_referenced_locally` warnings come from
  referencing rune state in legacy contexts — use derived values.
  Smoke-tested the built binary (fresh mktemp state dir,
  `--dev-listen 127.0.0.1:8091`): SPA shell + hashed JS/CSS +
  favicon served; `/api/me` → `dev@local`; snippet create → list
  round trip; 404 on unknown API path; 415 on non-JSON POST (the
  CSRF invariant). Gotcha: the first smoke attempt on `:8080`
  actually hit a stale dev server from a different checkout
  of this repo (running since 08:11) — our server
  failed to bind and exited with an empty log, and the test rows
  went into that checkout's `.dev-state` DB. Deleted the two test
  rows via its API (204) and verified the DB was back to its
  original state. Side observation: that old binary answers
  `GET /api/snippets` with a `{"snippets":[…]}` envelope; the
  current API returns a bare array, which is what `api.ts`
  expects — the envelope was the stale shape, not a bug.
  Interactive browser smoke (search UX, keyboard shortcuts,
  template copy, sensitive reveal in the UI) is left to manual
  verification; the headless API smoke + component tests cover
  the logic.
  Next: Phase 7 (PWA + offline).

- 15:35 — Phase 7 (PWA + offline) implementation complete; `make test`
  green (go test + 114 Vitest + svelte-check 0 errors).
  `vite.config.ts`: `vite-plugin-pwa` in `generateSW` mode —
  `registerType: autoUpdate`, precache of the built app shell (10
  entries, ~88 KiB), `navigateFallback: /index.html`, runtime cache
  `NetworkOnly` for `/api/*` (API responses are never cached), manifest:
  name/short_name `snp`, `display: standalone`, `start_url: /`,
  theme/background `#0f172a`, 192 + 512 icons (`any` + `maskable`,
  `web/public/pwa-*.png`, plus a 180 for the favicon). `lib/sync.ts`:
  sync triggers — on mount, on `visibilitychange`/`focus`, every 5
  minutes while open (spec §6: the timer alone is not enough on
  mobile), and one sync on reconnect; "Full resync" action (settings
  menu) clears the IndexedDB cache and re-syncs from scratch. Offline
  UX: list/search read from IndexedDB via the MiniSearch index;
  create/edit/delete disabled with the OfflineBanner; sensitive reveal
  blocked with an "online required" hint; nothing queued. Tests: 6 new
  Vitest tests (sync triggers, resync, offline gating) — 114 total.
  Gotchas: (1) the debug-sync test needed a `fake-indexeddb/auto`
  import for an in-memory IndexedDB under jsdom. (2) the resync test
  fixture was missing the `s1` tombstone — a full sync must include
  tombstones for soft-deleted rows (`deleted_at`) or the client cache
  keeps the stale row. (3) tracking the `busy` flag as reactive Svelte
  state created a sync feedback loop — each sync completion updated
  state, which re-triggered the reactive effect: 1615 sync calls in
  ~800 ms. The mutex flag must be non-reactive (a plain module-level
  boolean) so guarding a concurrent sync doesn't itself trigger a
  sync.
  Smoke-tested the built binary (fresh mktemp state dir, `snp serve
  --dev-listen 127.0.0.1:4789`): `/` 200 text/html, `/sw.js` 200
  text/javascript, `/manifest.webmanifest` 200, `/pwa-512x512.png` 200
  image/png, `/api/sync` 200 (no auth in dev mode); index.html links
  the manifest and loads `registerSW.js`. Gotcha: the flags live on the
  `serve` subcommand (`snp serve --dev-listen …`), not the root —
  `snp --dev-listen` fails with "unknown command".
  Remaining per the plan's done-when: the manual PWA checklist —
  install on macOS (Chrome/Safari), Android, and iOS; airplane-mode the
  device → browse + search work, writes blocked, sensitive reveal
  blocked; reconnect → changes from elsewhere appear.
  Next: Phase 8 (Deployment + ops).

- 16:15 — Phase 8 (Deployment + ops) implementation complete.
  `deploy/snp.service`: user `snp`, `HOME=/var/lib/snp`,
  `StateDirectory=snp`, `WorkingDirectory=/var/lib/snp`,
  `ProtectSystem=strict` + `ProtectHome` + `PrivateTmp` +
  `NoNewPrivileges`, `Restart=on-failure`, and
  `EnvironmentFile=-/var/lib/snp/.config/snp/authkey` (the `-` makes it
  optional; the file is kept, not deleted, so a state wipe can re-join
  without re-issuing steps). Two small additions to the plan's unit:
  `Wants=network-online.target` (pair with `After=` so the target is
  actually pulled in) and `RestartSec=5` (avoid hot restart loops when
  the control plane is flaky).
  `deploy/install.sh`: root-only one-command install — creates the `snp`
  user (home /var/lib/snp, `/bin/false` shell), installs the binary to
  /usr/local/bin/snp (runs `snp help` first to catch a cross-build for
  the wrong architecture), writes the starter config to
  /var/lib/snp/.config/snp/config.toml (never overwrites an existing
  one), writes the authkey file when TS_AUTHKEY is set in the
  environment, installs + enables the unit, and installs
  `deploy/backup.sh` as /usr/local/sbin/snp-backup plus a daily cron
  entry (/etc/cron.d/snp-backup, 03:17) into /var/lib/snp/backups.
  `deploy/backup.sh DEST_DIR [retention_days]`: `snp backup` →
  `snp-YYYYmmdd-HHMMSS.db` (VACUUM INTO + integrity check inside the
  binary), key via `snp key show-path` → `snp.key` (0600), prunes
  `snp-*.db` older than the retention days (default 7; arg or
  RETENTION_DAYS override). Quiet on success, errors to stderr with
  exit 1.
  `README.md`: build (incl. cross-compile), one-command install, first
  run, access URL (DNS name, not 100.x), PWA install on macOS / iOS /
  Android, config table, backup/restore procedure, and plain-language
  warnings (key required to read sensitive snippets from a backup;
  export = db+key sensitivity; don't delete state_dir/tsnet;
  --dev-listen is unauthenticated).
  Verified locally: `bash -n` + `shellcheck` clean on both scripts;
  end-to-end backup.sh on macOS — dev server, snippet created, backup
  (db + 0600 key), then restore drill: copy db+key to a fresh state
  dir and `snp export` reads the snippet back in plaintext; retention
  validation (exit 2 on non-integer) and the no-database error path
  (exit 1) both correct.
  Gotcha: `snp backup` and `snp key show-path` load the full config, so
  they require `owner` in non-dev mode — always true under the systemd
  deployment (config.toml is written by install.sh; cron sets HOME to
  the user's home, so the config resolves).
  Also: `.gitignore` now ignores `.dev-state/` (local dev state dir
  from repo-root dev runs).
  Pending (host acceptance): `sudo TS_AUTHKEY=... 
  ./deploy/install.sh -o <login>` on the host (verify the owner login against
  a live `tailscale whois`), PWA install + airplane-mode checklist,
  cron backup + restore on the host.
  Next: host acceptance, then v1 follow-ons (CLI client, SnippetsLab
  converter).

### 2026-09-04

- 14:08 — Post-phase hardening: per-snippet `uses_variables` template
  flag, end to end. `make test` green (go test + 132 Vitest +
  svelte-check 0 errors/warnings).
  Backend: `migrations/0002_uses_variables.sql` adds
  `snippets.uses_variables INTEGER NOT NULL DEFAULT 0` (existing rows
  backfill to 0). `store.SnippetInput`/`SnippetOut` carry the flag;
  create, replace, get, list, sync, and export all read and write it;
  `ImportSnippet` decodes `uses_variables` — older export docs without
  the field decode as false, so imports stay backward compatible. The
  server treats the body as opaque text; the flag only drives frontend
  behavior, per spec §4.
  Frontend: `types.ts` adds `uses_variables` to `Snippet`/`SnippetInput`.
  `templates.ts` gains `previewTemplate` — like `renderTemplate`, but a
  variable with no value and no default stays as its literal `{{name}}`
  placeholder, so unfilled variables remain visible in the live preview
  instead of silently blanking out. `SnippetForm` seeds the flag from the
  initial snippet, shows a "Template" checkbox, and keeps it in sync with
  the body via a `$effect` on `hasTemplateVars(body)` — a manual toggle
  holds until the next body edit, so the flag can't drift from the
  content. `SnippetDetail` shows a variables panel when
  `uses_variables` is set: one input per variable (its default as the
  placeholder), a live preview via `previewTemplate`, and Copy sends the
  rendered text up to `App`, which writes it to the clipboard; the panel
  is hidden otherwise. `App.svelte` wraps `SnippetDetail` in
  `{#key selectedSnippet.id}` so the panel's inputs remount per snippet.
  Tests: `templates.test.ts` — 10 new `previewTemplate` cases (values,
  default fallback on blank/missing, unfilled stays literal, mixed,
  invalid and unclosed placeholders, no-op). `SnippetDetail.test.ts` — 7
  new cases (panel shown/hidden by the flag, default as placeholder,
  live preview updates on input, unfilled variables stay visible, copy
  sends the rendered text with default fallback and entered values,
  blanks for vars without a default). `SnippetForm.test.ts` — 2 new
  cases (the flag auto-checks when the body gains placeholders and
  unchecks when they're removed; the flag is in the submitted payload).
  Fixture updates across `sync.test.ts`, `App.test.ts`, `db.test.ts`,
  `search.test.ts`, `api.test.ts`, `SnippetList.test.ts` to carry the
  new required field. Rebuilt `web/dist` (vite build + PWA). Spec
  §4/§5/§6/§9 updated to document the flag.
  Next: host acceptance, then v1 follow-ons (CLI client, SnippetsLab
  converter).

- 15:52 — Two fixes from the post-deploy review. (1) The variables panel
  in `SnippetDetail` had no CSS — `.vars`, `.var`, and `.preview` were
  unstyled, so the labels fell back to inline display and the whole
  panel rendered on one line. `web/src/app.css` gains a "detail:
  variables panel" section: `.detail .vars` is a stacked column (gap 8,
  top border) with an uppercase heading; `.detail .var` stacks the
  name over the input (matching the form convention) with a bordered
  input; `.detail .preview` is a monospace block on `--code-bg` with
  pre-wrap so long renders stay readable. (2) PWA staleness: the
  static handler set no `Cache-Control` headers, so the browser
  heuristically cached `sw.js`/`index.html` and delayed the
  service-worker update check after a rebuild. `static.go` now sets
  `Cache-Control` per path via `cacheControlFor`: `index.html`,
  `sw.js`, and `*.webmanifest` → `no-cache` (always revalidate, so a
  new build is picked up on the next page load); `assets/*`
  (content-hashed) → `public, max-age=31536000, immutable`; everything
  else → `public, max-age=300`. The handler was restructured to
  resolve the effective path first (the SPA fallback rewrites to `/` →
  `index.html`) and set the header before delegating to
  `http.FileServer`. `TestStaticFallback` extended with `sw.js` in the
  fake FS and Cache-Control assertions on root, the SPA fallback,
  `sw.js`, and assets. Rebuilt `web/dist` + `bin/snp`; `make test`
  green (go test + 132 Vitest + svelte-check 0 errors/warnings).
  Next: host acceptance, then v1 follow-ons (CLI client, SnippetsLab
  converter).

- 16:34 — v1 follow-on (post-plan): persisted per-variable defaults for
  template snippets — backend half of a two-commit feature (the frontend
  half, panel pre-fill + Save defaults button + spec §6, lands in the
  next entry). Agreed design: defaults for sensitive snippets are
  encrypted at rest with the body (no plaintext leak into backups);
  saving is an explicit action, client-side; the client prunes keys to
  the body's current variables on save and the store treats the map as
  opaque.
  `migrations/0003_var_defaults.sql`: `snippets.var_defaults TEXT NOT
  NULL DEFAULT ''` (the JSON `{"name": value}` map for non-sensitive
  rows, `''` for sensitive rows) + `snippets.var_defaults_enc BLOB`
  (the same map sealed under the snippet id; NULL when empty). Neither
  column is FTS-indexed, so the FTS write-ordering discipline is
  untouched.
  `store`: `SnippetInput`/`SnippetOut`/`ImportSnippet` carry
  `VarDefaults map[string]string` (JSON `var_defaults`). New helpers
  `storeVarDefaults` (JSON text, or sealed map + plain `''` for
  sensitive rows; `ErrNoKey` when a key is required) and
  `loadVarDefaults` (decrypts under the id; `''`/NULL decode to an
  empty map). Create/replace/get/list/sync/export/import all carry the
  new columns; replace re-serializes both columns from the input map,
  so toggling `is_sensitive` moves the map between the plain and sealed
  forms in either direction. Output rule: `var_defaults` is a non-nil
  `{}` when known and `null` for sensitive snippets in create/list/sync
  responses (mirroring `body`); `GetSnippet` returns the decrypted map.
  `handlers.go`: `snippetReq.VarDefaults` (omitted decodes to an empty
  map; full-replace semantics) — no new endpoints.
  Spec §4 (schema, mirror-column rule, Encryption, Templates:
  precedence entered > saved > inline `{{name|default}}` > empty,
  client prunes on save) and §5 (snippet JSON + the null rule, PUT
  note, export/import back-compat: docs without the field import as an
  empty map).
  Tests: 7 new store tests in `vardefaults_test.go` (CRUD round trip +
  `{}`-not-null stability; sensitive sealed at rest with the plain
  column `''` and outputs hidden; the is_sensitive toggle both ways
  with the ciphertext column managed; `ErrNoKey`; sync carry + JSON
  null for sensitive; export/import round trip with re-encryption under
  a fresh key; a legacy doc without the field importing as empty) and
  1 handler test (`TestSnippetVarDefaults`). Gotchas: `sql.Null[[]byte]`
  exposes its value as `.V` (not `.Bytes`/`.Val`); in a restricted build
  environment, `go test` needs `GOCACHE=/tmp/...` because the user-level build cache
  sits outside the writable tree. `make test` green (go test + 132
  Vitest + svelte-check 0 errors; web side untouched).
  Next: frontend half of the var_defaults feature (commit 2), then
  host acceptance.

- 16:53 — var_defaults feature, frontend half (commit 2 of 2). The
  variables panel now pre-fills its inputs from the snippet's saved
  defaults (`snippet.var_defaults`; a saved value beats the inline
  `{{name|default}}` placeholder; keys for variables no longer in the
  body and blank values are dropped), and an explicit "Save defaults"
  button persists the current inputs: a full-replace of the snippet
  carrying its current fields plus the new `var_defaults` map — a blank
  input clears that default, keys absent from the body are pruned
  client-side (the server treats the map opaquely, spec §4/§5). The
  button is disabled offline like every other write; pre-fill still
  works because the map rides in the local cache (non-sensitive
  snippets; sensitive ones have `var_defaults: null` locally, like the
  body). `SnippetForm` carries `var_defaults` forward on a normal save,
  pruned to the body's current variables, so an edit never wipes the
  defaults; a body with no variables saves an empty map.
  Types: `Snippet.var_defaults` (null for sensitive in list/sync) and
  `SnippetInput.var_defaults` (optional; omitted = empty map). The
  IndexedDB cache, MiniSearch index, and sync merge need no changes —
  the field rides the snippet document, and defaults are never
  searched, matching the server (spec §4: neither column FTS-indexed).
  Spec §6: the Copy bullet (pre-fill + precedence + Save defaults
  semantics), the Edit bullet (carry-forward), and the offline bullet
  (saving is a server write; pre-fill still available).
  Smoke test against a fresh dev server (fresh state dir, `--dev-listen
  :8080`) surfaced a pre-existing gap: `CreateSnippet`/`ReplaceSnippet`
  built their `SnippetOut` from the input fields without
  `UsesVariables`, so the POST/PUT responses always reported
  `uses_variables: false` even though the row stored the flag — the
  client cache (seeded from the response) lost the flag until the next
  sync, so a freshly created template showed no variables panel. Fixed
  both out literals (regression test `TestUsesVariablesInOut`); the
  create/replace/smoke flows now agree with the row.
  Tests: +10 Vitest — panel (pre-fill; copy precedence saved > inline;
  stale keys not pre-filled and pruned on save; a cleared input clears
  that default; typed values persist; offline disables the button),
  form (carry-forward pruned to the body; all pruned when the body
  stops using variables), App (select + type + Save defaults issues
  the correct PUT payload and the inputs pre-fill from the synced map)
  → 142 green. Rebuilt `web/dist` (new service-worker precache list in
  the tracked index.html). `make test` green (go test + 142 Vitest +
  svelte-check 0 errors/warnings).
  Feature complete across d66ac37 (backend) and this commit.
  Next: host acceptance, then the remaining v1 follow-ons (CLI client,
  SnippetsLab converter).
### 2026-09-06

- 12:45 — Independent code review of the whole repo (docs + Go + web +
  deploy) found and fixed the top five issues, one commit each on branch
  `fix/review-1-5`:
  1. `4a4815f` — web: UI delete 415 + sensitive-edit data loss. `api.ts`
     only set `Content-Type: application/json` when a body was present, so
     every body-less `DELETE` from the SPA hit the server's CSRF guard
     (415). And editing a sensitive snippet seeded the form from the
     cached row's `null` body, so saving full-replaced the row with `''`
     (irrecoverable). The header is now sent for all POST/PUT/DELETE;
     `startEdit` fetches the decrypted body first and refuses to open the
     editor when the fetch fails. Tests: DELETE header assertions +
     App-level edit-a-sensitive-snippet test.
  2. `c9fe04e` — store: sync same-second hole. `server_time` and stored
     timestamps are whole seconds, so a row written in the same wall-clock
     second as a sync snapshot had `updated_at == server_time` and the
     strict `>` filter skipped it on every later sync. Live rows,
     tombstones, and folder deltas now filter `>=` (idempotent merge makes
     the bounded duplicate harmless). Regression test pins the frozen-clock
     boundary.
  3. `b05cedd` — store/server: folder moves required a live destination and
     a unique sibling name. `UpdateFolder` silently accepted a missing or
     soft-deleted parent (500 via unmapped FK, or a live folder orphaned
     under a tombstone that then failed the daily purge with an FK error
     forever — reproduced in a scratch test); destination is now validated
     live up front and a broken ancestor chain refuses the move. Moves that
     change the parent now enforce sibling-name uniqueness (was rename-only).
  4. `1fc2ee7` — docs/web: spec §6 reconciled with the implemented v1
     frontend (CodeMirror/Markdown/API-search/keyboard/tags-pane/responsive
     never built — amended the spec with a revision note instead of
     implementing); removed the dead `@codemirror/*`, `codemirror`,
     `marked`, `dompurify` dependencies.
  5. `73dab73` — cmd/deploy/store/make: `--dev-listen` loopback-only
     (`:8080` used to bind every interface, unauthenticated); http.Server
     ReadHeader/Idle timeouts; `StartPurger` takes the configured logger
     and returns a done channel joined before store close; systemd
     `TimeoutStartSec=600`; `make test` now runs `go vet` and installs web
     deps when `node_modules` is missing.
  Gotchas: `go vet`/`go test` in a restricted build environment need `GOCACHE=/tmp/...`
  (the user-level cache sits outside the writable tree); a frozen-clock
  sync test must advance the clock *and* use the new sync's `server_time`
  to prove duplicates stop. `make test` green at the end (go test + vet +
  143 Vitest + svelte-check 0). Not in scope here (left for later passes):
  offline search OR-vs-FTS5-AND parity, API-backed online search, folder
  move-to-root expressibility, delete confirmation UX.
  Next: host acceptance, then the remaining v1 follow-ons.

### 2026-09-08

- 08:30 — Wails desktop app landed on branch `feature/wails-desktop`
  (spec §12). User decision: a **local desktop instance** — same store
  and same SPA in a native window, no tailnet, no HTTP port — reusing
  the CLI state dir, macOS first. `snp serve`/PWA/tailnet deployment
  untouched. Three commits + work log:
  1. Go core: `cmd/snp-desktop` (darwin-tagged Wails binary; production
     build tag) + `internal/desktop` (in-process API bridge:
     `App.CallAPI(method, path, body)` runs each request against the
     same handler the serve subcommand builds, via httptest; applies
     the spec §3 JSON content-type rule itself; no wails import, so it
     is platform-neutral and unit-tested with a real temp-file store) +
     `config.LoadDesktop` (Load minus the owner-required rule) + tests
     (bridge CRUD/guards/sensitive round trip; LoadDesktop).
  2. Web: `lib/desktop.ts` detects the shell (`window.__SNP_DESKTOP__`
     stamped into index.html by the wails asset-server middleware) and
     `api.ts` routes every call through `window.go.desktop.App.CallAPI`
     when present, sharing one parse/error path with fetch; 12 new
     Vitest cases (155 total).
  3. Docs/make: spec §12, README "Desktop app (macOS, Wails)", CLAUDE
     (layout + "desktop rule": cmd/snp must never import wails, or the
     server's Linux cross-builds break), Makefile `desktop`/`run-desktop`.
  Verified here: `make test` green; desktop binary built with
  `-tags production` and **launched**: window up, store created
  (`snp.db` + `key`) in the state dir; `cmd/snp` still cross-compiles
  statically for Linux (no wails in the server path).
  Gotchas: wails v2 needs the `production` build tag or its stub
  refuses to start; its darwin code references `UTType` without linking
  UniformTypeIdentifiers, so the build needs
  `CGO_LDFLAGS="-framework UniformTypeIdentifiers"` against the
  macOS 26 SDK (both pinned in the Makefile). Sandbox noise: WebKit
  per-app dirs under ~/Library/WebKit are denied here — cosmetic on a
  real machine. Follow-ons: signed .app bundle (icon, codesign,
  notarization), Windows/Linux, option to point the window at a remote
  snp server.

### 2026-09-08

- 08:45 — Desktop hotfix on main (`c2e827f`, branch `fix/folder-dialog`):
  "New Folder does not work" in the window. Root cause: folder creation
  and full-resync confirmation used `window.prompt`/`window.confirm`,
  which WKWebView (the wails webview) does not implement — the prompt
  returned null and nothing happened, silently. Replaced both with an
  in-app dialog in `App.svelte` (backdrop + `role="dialog"`, Enter
  confirms, Esc cancels, auto-focused name input, Create disabled while
  empty) so browsers and the desktop window behave identically.
  Gotcha: `<svelte:window>` must sit at component top level, not inside
  `{#if}`; a11y warnings drove `tabindex="-1"` on the dialog and no
  click-to-dismiss on the backdrop (Escape/Cancel only). Tests: new
  App-level folder-create test (root + subfolder via the dialog, POST
  payloads asserted); resync tests click the dialog buttons instead of
  stubbing `confirm`. 156 Vitest, svelte-check 0. Rebuilt web/dist +
  `bin/snp-desktop`; window smoke-launched OK. The work-log reminder to
  test the desktop UI flows (dialogs, delete, rename) stands — prompt/
  confirm were the only JS dialogs left.

### 2026-09-08

- 09:05 — Appearance settings on branch `feature/themes-settings`
  (requested: "themes with a selection include solarized light/dark,
  kimbie dark, tokyo night, etc. And a slider for interface text size,
  under the same settings button"). Two commits:
  1. `32fbf5d` — `app.css` gains one `:root[data-theme=…]` palette block
     per theme (auto/light/dark, Solarized Light/Dark, Kimbie Dark,
     Tokyo Night) remapping the existing palette variables — the blocks
     sit after the prefers-color-scheme media query with higher
     specificity, so explicit themes win; "auto" is no attribute.
     Every font-size declaration now multiplies by a `--text-scale`
     custom property (default 1), so the slider scales text without
     touching layout. New `lib/settings.ts` (theme list, persistence in
     localStorage `snp.theme`/`snp.textScale`, clamp + apply helpers);
     `main.ts` applies saved settings before first paint.
  2. `1ec947a` — the ⚙ dropdown is now a settings panel: Theme select +
     Interface text size slider (75–150%, live % readout) + full resync
     below a divider. Changes apply immediately and persist; no native
     dialogs, so the wails window behaves identically.
  Gotcha: `Number(localStorage.getItem(...))` is 0 for a missing key,
  and `clampTextScale(0)` → 75, so loadSettings must null-check before
  parsing. Tests: 9 new (settings unit suite + 2 App-level:
  data-theme persistence incl. auto-clears-attribute; --text-scale
  persistence). 165 Vitest, svelte-check 0. Rebuilt web/dist.
  Theme palette values are hand-picked approximations of the named
  schemes; fine-tuning the hex values is a one-line-per-var edit in
  app.css.


### 2026-09-08

- 10:10 — AI snippet generation on branch `feature/ai-generate`
  (spec §13). Design agreed with the user: OpenAI-compatible endpoint,
  one shot with no chat history, output is a templatized snippet ready
  to save; **generate into the snippet form for review** (never saved
  un-reviewed); normal config keys; full vertical slice.
  Three commits:
  1. Backend — config gains `ai_endpoint` (default
     https://api.openai.com/v1), `ai_model` (default gpt-4o-mini),
     `ai_key` (feature enabled when set) via flag/env/file;
     `internal/ai` client: one system+user message pair, temperature 0,
     JSON-envelope system prompt teaching `{{name}}` /
     `{{name|default}}`, tolerant parsing (fences/prose), typed
     UpstreamError/OutputError, fields exported for httptest-based
     tests; server `NewWithAI` + `GET /api/ai/status` (enabled+model,
     never the key) + `POST /api/ai/generate` (400 empty prompt, 502
     provider/parse failures — detail logged only, 503 unconfigured);
     wired into `snp serve` and the desktop `NewHandler` (the desktop
     window gets AI through the in-process bridge like every other
     route).
  2. Web — `api.ts` aiStatus/generateSnippet (fetch + desktop bridge);
     SnippetForm "Ask AI…" details control: queries status on mount
     (hidden when unconfigured), fills title/language/body for review,
     inline errors; uses_variables syncs from the body as usual.
  3. Docs — design §13, README (config table rows, AI section incl.
     self-hosted endpoint example, privacy warning), CLAUDE (layout +
     "AI rule": one-shot, no history, never log prompts/responses/key).
  Gotchas: the first ai tests parsed the whole chat `choices` envelope
  as the snippet JSON (green locally, red on the envelope) — decode the
  chat response first, then parse `choices[0].message.content`;
  `Number(null)` → 0 would have made the unset text-scale default 75
  (fixed in settings.ts earlier). Tests: ai client suite (request
  shape incl. exactly 2 messages, tolerant parse, upstream/output
  errors, endpoint joining), config AI precedence/defaults, server AI
  handlers (status enabled/disabled, generate happy + 400 + 503 +
  502), SnippetForm Ask-AI (fills form, error path), api client calls.
  169 Vitest, svelte-check 0. Verify-by-hand step (not run here — no
  live provider available here): point ai_endpoint/ai_key at a real
  endpoint and run one Ask-AI round trip.

### 2026-09-08

- 12:15 — AI refinement on `feature/ai-generate` (`6e341e6`): Ask-AI now
  also returns a `notes` field — bare command in Body, plain-text
  explanation in Notes, so a generated snippet documents itself. System
  prompt extended (title/language/body/notes; notes short, plain, no
  fences); a missing notes key parses as empty for tolerance. Tests
  updated end to end. 169 Vitest; Go suites + svelte-check green.


- 12:55 — Left-pane tag filter on branch `feature/tags-pane` (spec §6;
  the tags pane was designed at the outset and documented as "not
  built" — now implemented). User-confirmed semantics: clicking a tag
  FILTERS the list, several selected tags combine with AND (all must be
  present, like `tag:a tag:b`), and active tags AND with the folder
  selection and the search box; clicking an active tag clears it.
  `TagList.svelte` renders tags with live counts (most-used first),
  derived from the local cache so filtering works offline; no server
  change needed. `visibleSnippets` applies folder → tags → (no-query)
  time sort / (query) relevance order preserved. aria-pressed chips
  with a one-line "Showing snippets with …" hint.
  Gotcha: `getByText` THROWS when the element is absent (only
  `queryByText` is null-safe) — the first version of the App test used
  getByText for "Alpha is hidden" and failed on the positive path.
  Tests: TagList unit (render order/counts, pressed state, toggle
  calls, empty hint) + an App-level test covering multi-tag AND,
  clearing, and folder+tag combination with a bespoke sync payload.
  173 Vitest; Go suites + svelte-check green. Rebuilt web/dist.
- 14:05 — macOS .app bundle + codesigning on branch
  `feature/mac-app-bundle` (user: "We don't build a Mac snp.app bundle,
  do we?" → add it, signed with Developer ID
  a Developer ID). `deploy/make-app.sh`
  packages `bin/snp-desktop` into `build/snp.app`: Info.plist
  (com.jstevewhite.snp), AppIcon.icns derived from the PWA icon,
  hardened-runtime codesign with the network-client entitlement (Ask-AI
  makes outbound calls in-process), then `codesign --verify --deep
  --strict`. Makefile: `app` (default SIGN_IDENTITY = the Developer
  ID; `-` = ad-hoc) and `run-app` (`open`). Gitignored build/.
  Verified: identity present in keychain; bundle signed (runtime flag,
  TeamIdentifier <your-team-id>, entitlement embedded), Info.plist lints,
  icon + binary in place.
  Gotchas: iconutil refuses to write INTO the repo path in a restricted
  environment (cp works) — render the icns to a temp dir, then copy it in;
  `for spec in …` with numeric strings is fine (xtrace display is
  misleading). The AppIcon is an upscaled PWA icon — a real 1024px
  marketing icon and notarization for out-of-account distribution are
  follow-ons.

### 2026-09-08

- 16:15 — Desktop startup diagnosis (feature/desktop-debug `889b8e2`,
  `a508911`). User's `--debug` run showed `store ready elapsed_ms=40964`
  on a 77KB DB (a copy opens in 0ms) — lock contention with a STALE
  snp-desktop instance (pid 83763, a blank window left running) that
  still held the state dir. Fixes: busy_timeout is now set before
  journal_mode(WAL) in the DSN pragma order (the WAL switch needs an
  exclusive lock; without a busy handler first, a contended open could
  stall ~40s instead of erroring after 5s); page errors/unhandled
  rejections now forward to the wails runtime log so `--debug` shows
  frontend failures instead of a silent blank window. Killed the stale
  instance; two-instance contention now opens in ~1ms. Advice:
  don't leave blank instances running; kill by exact
  process name (`pkill -x snp-desktop` — NOT `-f`, which matches any
  command line containing the substring) before relaunching.


### 2026-09-09

- 09:35 — Read-view syntax highlighting (spec §6). highlight.js core plus
  a curated, lazily code-split grammar set (`web/src/lib/highlight.ts`:
  static loader map so Vite splits each language; alias map for the
  free-text `language` field; DOMPurify second boundary over the escaped
  hljs output). `SnippetDetail` renders `<pre class="body hljs">` when a
  grammar resolves, falling back to the plain `<pre>` for empty/unknown
  languages. Token colors are theme variables (`--syn-*`) defined per
  palette, so all seven themes highlight consistently. Editing stays a
  plain textarea. Tests: highlight unit suite (known language, aliases,
  fallback, HTML escaping) + detail view (highlights `go`, plain for an
  unknown language). 185 Vitest, svelte-check 0.


### 2026-09-10

- 10:10 — `make app` now notarizes and staples, using the `snp-notary`
  keychain profile (`NOTARY_PROFILE`, Makefile default). make-app.sh
  signs with a secure timestamp (`--timestamp`) — notarization requires
  one — then zips the bundle with `ditto`, submits via
  `notarytool submit --wait`, verifies `status: Accepted`, staples and
  validates the ticket, re-zips the stapled bundle to `build/snp.zip`,
  and best-effort runs `spctl --assess` (warning only). Skipped for an
  ad-hoc signature or an empty profile (`make app NOTARY_PROFILE=`).
  Verified end to end on this machine: signature carries the timestamp,
  notarization Accepted, staple validated, `spctl` reports
  `accepted source=Notarized Developer ID`. Note: notarytool's cache
  writes under ~/Library/Caches fail in a restricted environment (noise);
  harmless on a normal terminal.

- 10:40 — Ask-AI body must be one command. The generate system prompt now
  requires `body` to be exactly one executable command on a single line
  (pipes/redirections/&& are fine; no `#` comments, no blank lines, no
  second or alternative command, no prose) and directs alternatives,
  flags, gotchas, and caveats to `notes`. Prompted by a real reply that
  returned a commented multi-command block (ditto/codesign/spctl) for a
  one-command request. Test pins the instruction; design §13 updated.

- 11:28 — Design-doc accuracy audit (doc vs. code), then reconciled
  `docs/snp-design.md` to the implementation. Fixes: §5 sync filters are
  `>=` on `updated_at`/`deleted_at`, not `>` (whole-second timestamps;
  strict `>` skips a same-second row forever —
  `TestSyncSameSecondBoundary` pins it), and the boundary paragraph now
  says so; §3 states that only `/api/*` is authenticated while the SPA
  shell is served unauthenticated (matches `server.go` and AGENTS.md,
  previously "every request"); the stale §6 revision note no longer
  claims notes are escaped-plain / the left pane is folder-only /
  `marked`+`dompurify` were removed (they are dependencies again, and
  highlight.js is implemented); §9 store tests are temp-file
  (`t.TempDir` via `newTestStore`), not in-memory; §2 adds `$SNP_CONFIG`
  to the config search order; §4 tag grammar carries the `{0,63}` length
  cap; §10 adds `internal/ai/` and the `deploy/` extras. Gotcha left in
  code: the `handleAIExplain` doc comment (`internal/server/handlers.go`)
  still says "capped at 500 tokens server-side" though there is no
  `max_tokens` — the design §13 text (prompt asks for <500, no hard cap)
  is correct; fix the comment next time that file is touched.

### 2026-09-11

- 00:30 — Ask-AI output **kind**: command / script / function (spec §13
  revised). The generate control gained an "Output" selector — Command
  (default), Script, Function — and `POST /api/ai/generate` accepts
  `"kind"` (case-insensitive, whitespace-trimmed; unknown → 400, never a
  silent command). The single-line rule was only ever a *prompt* rule, so
  nothing downstream needed changing: `parseSnippet` already keeps
  newlines and the form's Body is a textarea, so this was prompt + request
  plumbing. `internal/ai` now composes the system prompt from a per-kind
  body rule (`commandRule`/`scriptRule`/`functionRule`) plus a shared half
  holding the template-placeholder grammar and JSON envelope
  (`bodyRule`/`systemPromptFor`); `Generate` takes a `GenerateParams`
  struct instead of positional args. Frontend: `AIKind` type + optional
  `kind` on `AIGenerateInput`, `aiKind` state in `SnippetForm`. Tests:
  `TestParseKind`, `TestGenerateKindPrompts`,
  `TestGenerateScriptMultiLineBody`, `TestAIGenerateKind` (a new
  `newTestServerWithAIUpstream` helper wires a caller-owned httptest
  upstream into a Server so the handler test can assert which system
  prompt the provider received), plus Vitest cases in `api.test.ts` and
  `SnippetForm.test.ts`. `make test` green (203 Vitest, svelte-check 0).
- Also verified while touching `internal/server/handlers.go`: the stale
  `handleAIExplain` "capped at 500 tokens server-side" comment the last
  entry flagged is already gone (the comment now states there is no
  `max_tokens` cap), so that gotcha is closed.
- Gotcha for next time: the `internal/ai` prompts are composed at call
  time now, so a test pinning prompt wording must assert on the composed
  system message (as `TestGenerateKindPrompts` does), not on a constant —
  and adding a kind means adding its marker to that table.
- 00:40 — Read-view long-body collapse. A body of more than ten lines now
  renders in a ~10-line box with an inner scroll plus a Show all / Show
  less toggle (`SnippetDetail.svelte` `bodyLines`/`bodyLong`/`bodyCapped`
  + `expanded` state, `.detail .body.clamped` and `.detail .show-all` in
  `app.css`). Read-view only, per the ask: the form's Body textarea (rows
  14, already scrolling) and the template Rendered preview are untouched.
  The cap is CSS (`max-height: calc(10 * 1.45em + 26px)` — 1.45 is the
  inherited line-height, 26px the box padding+border), so wrapped long
  lines are capped by height rather than by logical line. Line counting
  drops one trailing newline so a 10-line body ending in `\n` does not
  trip the cap. Three Vitest cases (cap+toggle, ten lines/trailing
  newline, sensitive-unrevealed offers none). Gotcha: the `.body` class
  is shared by the highlighted and plain `<pre>` branches, so
  `class:clamped` had to be added to both.
- 00:45 — Same collapse for the template **Rendered** preview, at the
  user's request. State is independent of the body box (`renderedExpanded`
  / `renderedLong` / `renderedCapped` off `preview`), because a short
  template can render long when a variable holds many lines, and expanding
  one box must not force the other open. The line-count regex moved into a
  shared `lineCount()` helper; the CSS cap is now
  `.detail .body.clamped, .detail .preview.clamped` (both boxes share
  font-size/padding, so one calc covers them). Test note: with both boxes
  long there are two "Show all" buttons, so the new Vitest case scopes with
  `within(...)` rather than `screen.getByText` — a future test that adds a
  long body *and* `uses_variables` must do the same.
- 01:15 — **Linux desktop build** (spec §12). `cmd/snp-desktop/main.go` is
  now tagged `darwin || linux` and stays platform-neutral; the per-OS
  wails options moved into `platform_darwin.go` (a deliberate no-op — the
  macOS defaults are right) and `platform_linux.go`. The Linux file sets
  three options whose defaults are wrong there, all verified against the
  vendored wails source rather than guessed:
  `WebviewGpuPolicy: OnDemand` (wails forces `Never` — software rendering
  — when `options.Linux` is nil), `ProgramName: "snp"` (GTK
  `g_set_prgname`; the `.desktop` entry's `StartupWMClass` must match or
  the running window is a second unnamed taskbar entry) and `Icon` (GTK
  window icon, embedded from `web/public/pwa-512x512.png` as
  `webembed.IconPNG` — `go:embed` cannot reach a parent directory from
  `cmd/snp-desktop`, and `web/embed.go` already owns the embed).
  The Makefile `desktop` target is OS-aware and still emits the *identical*
  darwin command (`CGO_LDFLAGS="-framework UniformTypeIdentifiers"`,
  `-tags "production"`); on Linux it drops that flag (invalid there) and
  adds wails' `webkit2_41` tag when pkg-config finds webkit2gtk-4.1
  (`WEBKIT2=` forces 4.0). New: `deploy/snp.desktop`,
  `deploy/install-desktop.sh` (per-user: binary to `<prefix>/bin`, entry
  to `<prefix>/share/applications`, 192/512 icons into the hicolor theme;
  `$HOME/.local` default, no root) and `make desktop-install`.
  Docs updated: spec §12 (platform split, Linux packaging, the
  no-cross-compile rule), §10 layout, AGENTS.md + CLAUDE.md desktop rule,
  README.
  **Verified today:** gofmt clean; `make -n desktop` prints the unchanged
  darwin line; the darwin desktop build succeeds (45 MB
  `bin/snp-desktop`); `make test` green (207 Vitest, svelte-check 0).
  **Not verified:** the Linux compile itself — wails links against
  GTK3/WebKit headers, so it cannot be built from macOS. That is the next
  session's container build: `libgtk-3-dev` + `libwebkit2gtk-4.1-dev` +
  Go, `make web` then the Linux desktop build, and a dry-run of
  `install-desktop.sh` against a scratch `PREFIX=`. The script is
  `bash -n` clean but has never run on Linux.
  Gotcha for whoever touches this next: keep OS conditionals out of
  `main.go`. The platform files exist precisely so the darwin build stays
  byte-identical and Linux has somewhere to add options without a `uname`
  branch in shared code.
- 09:00 — Bundled **starter pack** (spec §5), asked for as "some
  pre-packaged snippets (specifically a config file)". `internal/starter`
  embeds `pack.json` — an ordinary import document (version 1) — so
  seeding is the existing, tested import path: no second format and no new
  store code. Four snippets land in a `Starter` folder via `folder_path`
  (which import creates): snp's own annotated `config.toml` (the requested
  config file), a `{{var}}`/`{{var|default}}` template example, a
  loopback static file server, and `lsof` for a listening port. The ids
  are pinned ULIDs, so applying the pack twice updates those rows rather
  than duplicating them.
  **Explicit only** — `snp seed`, `POST /api/seed`, and a settings-panel
  button ("Add starter snippets") that seeds and then re-syncs so the rows
  arrive in the local cache. That is a deliberate consequence of reading
  `internal/store/import.go` before writing anything: merge mode sets
  `deleted_at = NULL`, so *any* automatic apply would resurrect snippets
  the user had deleted — hence no first-run hook, no `meta` table
  migration, and no `.seeded` sentinel. The answer to both
  design questions (content vs. mechanism; explicit vs. auto) picked this.
  Docs: spec §5 (feature table, CLI list, API table row, and a new
  "Starter pack" subsection), §10 layout, README, AGENTS.md + CLAUDE.md
  layout lines.
  Tests: `internal/starter` (well-formed pack, config snippet pinned,
  applies idempotently against a real temp store), `cmd/snp` (`seedCmd`
  twice), server (`POST /api/seed` creates the pack, folder present,
  second call updates), Vitest (`api.seedStarter`, and the settings button
  seeding + re-syncing). `make test` green (209 Vitest, svelte-check 0).
- Gotcha for whoever edits `pack.json` next: keeping a pinned id means a
  re-seed **overwrites** any user edits to that snippet, because merge
  upserts by id. Adding snippets is always safe; changing one that already
  shipped is a deliberate overwrite — use a new id if that is not what you
  want.
- 2026-09-11 — README review against the current Makefile, dependency
  manifests, configuration, installer, AI handlers/frontend, and design.
  Feedback only; existing README edits preserved. Findings: cross-build
  example leaves the shell in web/; documented Go/Node minimums lag the
  manifests; AI privacy wording omits Explain/Suggest tags sending the
  current body (including sensitive bodies) and tag vocabulary; desktop
  network and shared-state wording needs qualification; macOS Safari uses
  Add to Dock; make app does not notarize by default. Spec §13 repeats the
  unsupported sensitive-content guarantee and needs reconciliation too.
  Suggested leading with the product overview and moving model provenance
  later. Validation was source inspection plus Apple documentation for the
  Safari menu; no builds/tests run for this feedback-only review.
- 2026-09-11 — Applied the requested README review fixes. Led with the
  product overview and moved the existing model-provenance story to the
  end; split server/desktop requirements and quick starts; corrected Go
  and Node requirements, cross-build working directory, Safari install,
  desktop networking/state sharing, and default macOS notarization claims.
  Added search/template examples and explicit AI payload/sensitive-body
  behavior. Moved platform build and signing detail to docs/desktop.md.
  Validation: markdownlint-cli2 clean for both documents; local links and
  anchors resolve; all shell examples pass bash -n; git diff --check clean.
  No application builds/tests needed for these documentation changes.
  Gotcha remains: spec §13's sensitive-content guarantee disagrees with
  the implementation; this session updates the requested README and guide.
- 2026-09-11 — Header refactor (user request): the top bar is now
  `snp <version> [online] …… [error] synced <ts> [Resync] [⚙]` — the
  release version sits immediately after the wordmark and the synced
  timestamp moved to the far right, immediately left of Resync. The
  version had no source at all, so this needed plumbing: new
  `GET /api/version` → `{"version": ...}` (`internal/server/handlers.go`
  `handleVersion`, `internal/buildinfo.String()`, so an unstamped build
  reports `dev`), spec §3 prose + §5 endpoint table updated.
  Chose an additive endpoint over stamping the SPA shell
  (`window.__SNP_VERSION__`): the endpoint rides the existing desktop
  bridge unchanged (`internal/desktop.CallAPI`), needs no HTML rewrite in
  `static.go` or in the wails asset middleware, and the PWA's `/api/*`
  `NetworkOnly` workbox rule (vite.config.ts) means the service worker can
  never serve a stale version. Offline cost is covered by caching the
  value in localStorage (`web/src/lib/version.ts`, key `snp.version`), so
  the chip renders from cache when the request fails; a failed lookup
  never surfaces as an app error.
  Tests: Go `TestVersion` (unstamped → `dev`, then a stamped global →
  the tag), Vitest `version.test.ts` (cache round-trip, offline fallback,
  no-cache-and-offline → null, blank value ignored) and an App test that
  asserts the DOM order explicitly (`brand.nextElementSibling` is
  `.version`; `.synced.nextElementSibling` is the Resync button).
  `make test` green: vet + `go test ./...` + 214 Vitest + svelte-check 0.
- Gotcha for local verification: `bin/snp` and `bin/snp-desktop` in a
  working tree from before this change are stale — `./bin/snp version`
  and `./bin/snp-desktop -version` fail with "unknown command" / "flag
  provided but not defined" until `make build` / `make desktop` reruns.
  Also: release tarball names drop the leading `v` (`snp_0.2.0_…`) while
  the stamped binary prints `v0.2.0`; and the sandboxed `go build` needs
  `GOCACHE` inside the workspace if `~/Library/Caches/go-build` is not
  writable.
- 2026-09-11 — Shipped **v0.1.0**. Commit `e9aeec7` (the header/version
  change) went to `main`, then a lightweight `v0.1.0` tag (matching how
  `v0.1.0-beta.1` was cut) triggered `release.yml` → run 34626078787,
  green in ~9 min. Prerequisites were checked *before* pushing, not after:
  HEAD was exactly on `v0.1.0-beta.1`, no `v0.1.0` release existed, and
  all six Apple secrets were present, so the macOS path really signed and
  notarized instead of quietly falling back to ad-hoc.
  Verified the artifacts, not just the green check: 7 assets, neither
  draft nor prerelease; `SHA256SUMS` matches for the files downloaded;
  the shipped `snp_0.1.0_darwin_universal.tar.gz` runs and prints
  `snp v0.1.0` (so the new header chip reads a real release, not `dev`);
  and the desktop zip passes `spctl` as "accepted, source=Notarized
  Developer ID" (TSS Studios, LLC / L8D425K33D) with a valid stapled
  ticket. That last one was the real unknown on a first notarized
  release — it also retires the "do the Apple secret names actually
  resolve" question left open by 135f521.
- Gotcha: `git describe --dirty` reported `v0.1.0-beta.1-dirty` before
  the commit. `--dirty` is the Makefile's marker for *modified tracked
  files* at build time (untracked files do not trip it), and the value is
  linked into the binary, so a binary built from a dirty tree keeps
  saying `-dirty` after you commit — rebuild, or `make build
  VERSION=…`. Separately, `web/dist/index.html` is tracked while its
  hashed siblings are gitignored, and every `npm run build` rewrites the
  two hashes in it: stage source files explicitly (`git add <paths>`), or
  `git checkout -- web/dist/index.html`, rather than reaching for
  `git add -A`.
- Follow-up, not done: the release run annotates that
  `actions/checkout@v4`, `actions/setup-node@v4`,
  `actions/upload-artifact@v4`, `actions/download-artifact@v4` and
  `actions/setup-go@v5` target Node 20 and are being force-run on Node
  24. Bump them to their Node-24 majors before the next release;
  harmless today.
- Session aside (harness, not snp): the `botmem` MCP server is up again —
  loopback `127.0.0.1:8586`, `serverInfo` botmem 0.1.0, 11/11 tools, and
  a live `get_active_project`/`list` round trip through the harness.
  Correcting an earlier claim of mine in the same session: its tools were
  registered in the GUI all along, and only the server was down, so no
  profile edit or restart was needed. botmem's active project is the
  shared `dsh` base; snp-specific knowledge stays in this `docs/` tree
  (per AGENTS.md, this log is the resume point).
- 13:25 — Draggable **pane dividers** (user request; spec §6 revised). The
  three panes were a fixed
  `grid-template-columns: 230px minmax(280px, 1fr) minmax(0, 2fr)`; they are
  now `var(--folders-w) | 6px | var(--list-w) | 6px | minmax(0, 2fr)` with
  two focusable `role="separator"` drag handles. `App.svelte` renders both
  from one `{#snippet}` so they share a single ARIA/keyboard wiring; the
  width arithmetic lives in the new `web/src/lib/panes.ts` (+
  `panes.test.ts`, 14 cases). Three choices worth keeping:
  - **The detail pane is the flexible one**, so only two widths are tracked.
    Dragging the folders divider therefore trades width with the list pane
    (their sum is held constant, which is what keeps the detail pane still);
    dragging the list divider is absorbed by the detail pane. Each divider
    clamps against its own min/max *and* against the room the container
    leaves once `DETAIL_MIN` (280px) is reserved, so no drag can collapse
    the detail pane to nothing.
  - **First paint is unchanged.** Widths start unset, so the stylesheet's
    own `1fr : 2fr` proportions still drive the layout; a `$effect`
    measures the rendered panes once they are in the DOM and pins *those
    pixels*. Hardcoding a default list width instead would have frozen it
    (≈776px → 360px on a 2560px window) and let the detail pane balloon on
    wide screens.
  - **Only a real drag persists** (`snp.paneWidths`, localStorage, same
    best-effort pattern as `lib/settings.ts`), so a window whose dividers
    were never touched keeps adapting to its size. Stored widths are
    clamped on load.
  Pointer/keyboard parity: arrows resize by 16px (1px with Shift); Home or
  double-click clears the stored pair and re-measures the default layout
  (clearing the vars forces the CSS default back, and reading a rect then
  forces the synchronous layout the measurement needs). `touch-action:
  none` keeps a touch drag from scrolling the pane instead of resizing.
  Tests: `panes.test.ts` (clamping, the container-aware room limit,
  persistence plus malformed/clamped stored values, style vars, measurement
  including the jsdom zero-rect case) and two `App.test.ts` cases that drive
  real pointer and keyboard events through the rendered dividers and assert
  the inline custom properties and the persisted JSON. `make test` green
  (230 Vitest, svelte-check 0 errors / 0 warnings).
- Gotchas for the next person in this area:
  - The splitter's 6px CSS width and `SPLITTER_WIDTH` in `lib/panes.ts` must
    agree — the clamp math subtracts two of them from the container.
  - Svelte's a11y linter fires `a11y_no_noninteractive_element_interactions`
    *and* `a11y_no_noninteractive_tabindex` at a focusable
    `role="separator"`, which is the documented ARIA window-splitter pattern
    rather than a mistake; both are suppressed by one `svelte-ignore` above
    the element. Multiple codes must be **comma-separated** —
    space-separated codes silently suppress only the first, which cost a
    check cycle here.
  - `measurePaneWidths` deliberately returns `null` on zero rects (jsdom has
    no layout, and so would a hidden window) so callers keep the stylesheet
    default instead of pinning 0px. That is why the App test drags from the
    documented 230/360 fallback rather than from measured widths.
  - In `clampPaneWidths` the folders-shrink branch only triggers when the
    available room is below `FOLDERS_MAX + LIST_MIN` (700px here); at or
    above it the list alone absorbs the overflow. A first draft of that test
    asserted the wrong scenario and failed against correct code.
  - Still unbuilt and unrelated: responsive pane **stacking** at narrow
    widths (spec §6 revision note). Panes stay side by side at every width.
- 13:35 — Promoted the `web/dist/index.html` gotcha from this log to a
  standing rule, at the user's suggestion: "Build coupling: web/dist must
  exist" in **both** AGENTS.md and CLAUDE.md (they move together) now says
  that every web build rewrites the tracked stub's two hashed asset
  references, why committing that breaks a bare `go build` for a fresh
  clone, and the two remedies (stage source paths explicitly, or
  `git checkout -- web/dist/index.html` afterwards). Recorded the precision
  that `make test` does *not* build and so leaves the file alone —
  verified by running `npm test`, `npm run check`, `go test ./...` and the
  full `make test` while watching `git status dist`, none of which touch
  it. Only `npm run build` / `make web` / `make build` do. Since this
  entry was the first thing it would have caught: `git ls-files web/dist`
  confirms `index.html` is the only tracked path under `dist`, which is
  what makes the `.gitignore` negation load-bearing. Docs-only change; no
  build or test rerun needed beyond the `make test` already green above.
- 13:45 — **Explain replaces Notes instead of appending** (user request;
  spec §13 revised). With text already in Notes, each Explain run glued the
  new explanation onto the old one, so re-running meant hand-deleting the
  previous text first. `explain()` now assigns `notes = res.notes` and keeps
  the replaced text in `explainUndo`, which surfaces an **Undo** button
  beside Explain (only while a snapshot exists) that puts the old text back.
  Two deliberate limits on that undo point: it is snapshotted only after a
  successful response, so a 502 or an empty explanation leaves both the
  notes and any earlier snapshot intact (the existing error test now also
  asserts no Undo appears); and typing in Notes by hand clears it, so Undo
  can only revert the Explain overwrite and can never silently discard
  typing that came after it. It is one level, not a stack — Undo consumes
  the snapshot. The Ask-AI Generate path already *replaced* notes
  (`notes = res.notes`), so Explain was the odd one out. Generate still
  clobbers notes with no undo; left alone deliberately, because its whole
  reply (title, language, body, notes) lands as one reviewed package and
  undoing only the notes would be half a revert — a form-wide undo would be
  the real fix if that ever bites.
  Tests: the append case became an overwrite assertion, plus two new cases
  (Undo restores the replaced text; hand-editing drops the affordance), 19
  in `SnippetForm.test.ts`. `make test` green (232 Vitest, svelte-check 0
  errors / 0 warnings).
- Gotcha for anyone extending this: the Notes textarea now carries both
  `bind:value` and an `oninput` that clears `explainUndo`. Programmatic
  writes (`notes = …` from Explain/Undo) do not fire `input`, which is what
  makes the snapshot survive the very write it is meant to undo — so a
  future change that sets notes by dispatching an input event, or that
  moves the clearing into a `$effect` on `notes`, would silently break Undo
  by clearing the snapshot the instant Explain set it.
- 2026-09-11 — Feature-set review (suggestions only). Read the design,
  implementation plan, README, work log, and current UI/store/API paths.
  Recommended prioritizing keyboard search/copy, recoverable edits and
  Trash, clearer template copying, responsive mobile navigation, quick
  capture/CLI, search refinements, and lightweight organization. Desktop
  remote-server mode is a conditional follow-on for using one library
  across devices; the current desktop app is an independent local store.
  Concrete gaps found by source inspection: selecting another snippet
  abandons editor state; Copy has no success/error UI; unfilled template
  variables disappear on copy despite remaining visible in preview;
  Save defaults replaces the whole cached snippet without a revision
  precondition; sensitive Explain/Suggest Tags send the current body;
  revealed bodies remain in App-level memory across selections; and an
  incremental sync after tombstones have been purged cannot remove those
  stale cached rows. Recommend stale-cursor full refresh and restore-aware
  cache invalidation. Spec §13's sensitive-content exclusion is a
  documentation bug relative to the shipped, README-documented AI behavior;
  its policy should be made explicit before expanding AI features.
  Checked RFC 9110 If-Match and W3C reflow guidance for the concurrency and
  narrow-layout recommendations. No application changes or tests/builds;
  review conclusions are source-based, not reproduced runtime defects.
  Existing modification to web/dist/index.html preserved.
- 2026-09-11 — **Phase 10, the UI refinement pass** (branch
  `feat/ui-refinements`, one commit per task, `make test` green at each).
  Driven by a seven-item review of the built UI. Executed highest-value
  first, so the cheap visible wins landed before the only task that needed
  a schema change. Nine commits plus the spec, plan, and log:

  1. **Explicit copy actions** (T3). `CopyButton.svelte` +
     `clipboard.ts`. *Copy template* beside the Template box writes the raw
     `{{var}}` body, *Copy rendered* beside the Rendered box writes the
     filled-in text, and the footer Copy is unchanged as the primary
     action. Every control flashes "Copied." — or "Copy failed", which
     finally gives the copy path a failure state.
  2. **Distinct create labels** (T4): "New folder" / "New snippet".
  3. **Simplified timestamps** (T6). `time.ts`: the header reads "Synced
     2 minutes ago" and the detail footer "Updated Sep 11", each carrying
     the precise local timestamp in a tooltip, with a 30s tick so the age
     does not go stale.
  4. **Search keyboard workflow** (T7). `keys.ts`: `Cmd/Ctrl+K` focuses the
     field (hinted inside it), `Up`/`Down` walk the results, `Enter` copies,
     `Escape` clears then leaves. Arrows and Enter are scoped to the search
     field so the editor keeps its keys — there is a regression test for
     exactly that.
  5. **Visible saved-default state** (T5): Save defaults is disabled until
     the inputs differ from what is stored, and a successful write confirms
     with "Defaults saved".
  6. **Long-title discovery** (T8): the full title in a tooltip on every
     row, plus a persisted "Two-line titles in the list" setting.
  7. **Favorites** (T1 + T2): a `pinned` column (migration 0004) carried
     through store, API, sync, export and import, with a pin control in the
     detail header and a Favorites list above the folder tree.

  - Gotcha — **the Go build cache lives outside the workspace.** Under the
    sandbox, `go build`/`go test` fail with "operation not permitted" on
    `~/Library/Caches/go-build` as soon as a source change forces a
    recompile (a warm cache hides it, which is why `make test` passed for
    the frontend-only commits). `GOCACHE=/tmp/snp-gocache make test` works
    and keeps the canonical command; nothing in the repo needed changing.
  - Gotcha — **PUT is a full replace, so every new snippet field is a
    trap for every partial update.** `pinned` had to be carried by three
    separate paths that rebuild a full `SnippetInput`: `App.saveDefaults`,
    the new pin toggle, and `SnippetForm.submit`. Miss one and the field
    silently resets — saving defaults would have unpinned, and toggling a
    pin would have wiped `var_defaults`. `replaceSnippet(id, patch)` in
    `App.svelte` now rebuilds the payload once and merges the patch, and
    `SnippetForm` seeds `pinned` and sends it back. Both have regression
    tests. The next field added to a snippet must do the same.
  - Gotcha — **sensitive rows hide `body` *and* `var_defaults` in list and
    sync responses** (spec §5). A full-replace payload built from a cached
    sensitive row would blank both, so `replaceSnippet` fetches the row
    first via `GET /api/snippets/{id}`.
  - Deliberate omission — **no pin toggle on the list rows.** Each row is a
    `<button>`; nesting a pin button inside it is invalid HTML, and
    restructuring the row (plus its tests and the keyboard selection path)
    was more churn than the feature needs. Pin lives in the detail header
    and unpin also on each Favorites row. A row-level toggle is a
    follow-on.
  - Gotcha — **the "Synced …" age must come from the client's observation
    of the sync, not from `server_time`.** `server_time` is the sync cursor
    (`lib/sync.ts`); comparing it to the local clock renders a negative age
    on a client whose clock trails the server's, so `App` stores
    `Date.now()` at sync completion and a future timestamp reads "just
    now". The 30s tick is a separate effect from the sync effect on
    purpose: a reactive read of the clock there would re-trigger a sync.
  - Gotcha — **`getByText` goes ambiguous once a snippet is pinned**: the
    title appears in both Favorites and the list. The App tests select via
    `.snippet-list .title` instead.
  - `$bindable` was needed to hand the search `<input>` from `SnippetList`
    up to `App` for the global focus shortcut.
  - Docs: spec §4 gains the column and a "Favorites" subsection, §5 the
    JSON field and its full-replace note, §6 the Favorites list, the copy
    actions, the real keyboard map, the timestamp rules, and the two-line
    title setting; the implementation plan gains Phase 10; `AGENTS.md` and
    `CLAUDE.md` now say phases 0–10 and list the three new lib modules.
  - `make test` green: go vet, `go test ./...` (all packages), 290 Vitest
    across 25 files, svelte-check 0 errors / 0 warnings.
- 2026-09-12 — Follow-up to Phase 10: **stale app shells**, and the create
  flow's missing tests. A report that Create and Cancel "did nothing" on one
  machine — while the same build worked on another — turned out to be
  environmental, but only after ruling out the code:

  - The served bundle was current: every Phase 10 change is present in the
    deployed `index-hqkw1XYL.js`, and `/api/version` reports
    `v0.1.0-14-g8e82a64-dirty` (branch tip, built from a dirty tree).
  - Driving that live instance in real headless Chrome — real bundle, real
    data (50 snippets) — reproduced **neither** symptom: Cancel closed the
    editor, Create created and closed it, and the page logged no exception.
    The harness was a local proxy of the live site with a driver script
    injected into the served HTML, reporting back over a `POST /__diag`; it
    created nothing (Create is exercised with cleanup, and the last full run
    made no POST at all).
  - **Gotcha — a stale service worker is invisible from the server.** The
    logs show ordinary 2xx responses and `/api/version` reports the
    *server's* version, so a device can execute an old bundle while looking
    completely current. The only reliable check is the script URL the client
    is actually running (`document.querySelectorAll('script[src]')`) against
    what the server serves. Deploying repeatedly from a mid-work tree is
    exactly how a device ends up caching an intermediate shell.
  - **Fix shipped**: `web/src/lib/sw.ts` — `registration.update()` on window
    focus and on becoming visible again, plus a single reload on
    `controllerchange` when a worker was already in control. With
    `registerType: 'autoUpdate'` a new worker claims clients, but the
    already-loaded page keeps executing its old script until it reloads, so
    without this a PWA that never navigates can serve an old bundle
    indefinitely. The reload is deliberately skipped on a first install,
    where claiming clients fires the same event.
  - **Test gap closed**: the create-from-form and cancel flows had no tests
    at all, which is why the report could not be settled from the suite.
    Both are covered now.
  - Diagnosis tooling (throwaway, not committed): under the sandbox Chrome
    will not start at all without full filesystem access — its profile and
    crashpad writes are denied under `workspace-write` — and `GOCACHE` must
    point inside `/tmp` once a Go source change forces a recompile.
  - `make test` green: go vet, `go test ./...`, 298 Vitest, svelte-check 0
    errors / 0 warnings.
- 2026-09-12 — Search: **prefix matching, and the same behaviour online and
  offline**. "Does search cover the title?" — yes, alongside notes, body and
  tags, with the title weighted highest (bm25 10.0 on the server, `boost:
  {title: 10}` offline). Checking that properly turned up a real divergence.

  - **The bug**: `web/src/lib/search.ts` asserted the engines agreed
    ("MiniSearch's default combineWith is OR, like FTS5"), and a test name
    pinned it: `'ORs multiple FTS terms, like FTS5'`. FTS5 does not OR, it
    **ANDs** — so `zebra nonexistentterm` returned nothing online and one
    snippet offline. The same query gave different results depending on
    connectivity, which spec §6 promised would not happen.
  - **Server** (`internal/store/search.go`): `ParseQuery` now returns terms
    individually and `prefixExpr` renders each as a quoted prefix query
    (`"term"*`), which FTS5 ANDs. Quoting makes FTS5 syntax literal —
    `AND`/`OR`/`NOT`/`NEAR` and `title:x` are ordinary words now — which is
    deliberate: the offline engine has no operators, so treating them
    literally is what buys parity. Terms with no letters or digits are
    dropped, since an empty quoted phrase is a syntax error. The
    quoted-phrase retry stays as a safety net, and the 400 path with it.
  - **Client**: MiniSearch gets `prefix: true, combineWith: 'AND'`;
    `parseQuery` returns a term array instead of a joined string.
    `SnippetIndex.search` re-joins it, because MiniSearch's `search()` takes
    a string (or `Wildcard`/`QueryCombination`), **not** an array — passing
    one type-checked as `SearchOptions` and threw at runtime.
  - **Verified rather than assumed**: seeded the same fixture into a real
    server and ran 17 queries through both the real FTS5 path and the real
    `SnippetIndex`, comparing matched sets. All 17 identical, including the
    bug case (now empty on both), prefix hits (`rest`, `depl`, `back`),
    infix non-matches (`estart`, `ing`) and a sensitive snippet whose body is
    unsearchable in both. Throwaway test, since it needs a live server.
  - Left alone deliberately: **ranking**. bm25 and MiniSearch cannot be made
    to score identically, so the matched set is shared but the order of
    equally-matching results can still differ. Making the order identical too
    would mean replacing both rankers with one shared rule (title match
    first, then `updated_at DESC`) — available on request.
  - Docs: spec §5 "Query syntax" and the endpoints table, and §6's search
    bullet, now state prefix + AND + literal operators, and that only the
    set — not the ranking — is shared.
  - `make test` green: go vet, `go test ./...`, 300 Vitest, svelte-check 0
    errors / 0 warnings.
- 2026-09-12 — **CI: the Node-20 GitHub Actions bump**, plus the first beta
  since the Phase 10 pass.

  - Beta `v0.2.0-beta.1` dispatched from `beta.yml` (run 34707463570) against
    `main` at `340f0e0`; it publishes as a prerelease and the tag is created
    by the release, pointing at the commit the run started from. Same 7-asset
    set as a real release.
  - **Why not just take the latest majors**: the newest tags are `v7` (and
    `v8` for download-artifact), but the goal is the smallest bump that
    leaves Node 20. The `runs.using` field at each major says which ones
    actually moved to node24, and two traps are invisible from the tag
    numbers — `upload-artifact@v5` and `download-artifact@v5`/`v6` are
    *still* node20. The minimal Node-24 majors: checkout `v4→v5`,
    setup-node `v4→v5`, setup-go `v5→v6`, upload-artifact `v4→v6`,
    download-artifact `v4→v7`, and `apple-actions/import-codesign-certs`
    `v3→v6`.
  - Each jump was checked rather than assumed: the `inputs:` blocks were
    diffed across the bump. checkout/upload-artifact/download-artifact are
    unchanged; setup-node adds `package-manager-cache` (the workflow sets
    `cache: npm` explicitly) and setup-go adds `go-download-base-url` plus
    description edits; the codesign action's inputs are byte-identical, and
    it gains a `post` step (temp-keychain cleanup). The workflows were then
    re-parsed to confirm all six jobs still resolve.
  - **Verified** by re-running the beta on the bumped commit (`v0.2.0-beta.2`,
    run 34707798309): all eight jobs passed, the macOS bundle still signed and
    notarized, `v0.2.0-beta.2` published as a prerelease with all 7 assets —
    and the run carries **no annotations at all**, where both earlier runs were
    annotated in every job. The bumped refs are visible in the step list
    (checkout@v5, setup-node@v5, setup-go@v6, upload-artifact@v6,
    download-artifact@v7, import-codesign-certs@v6 — including its new post
    step, which ran).
  - Gotcha — **GitHub's Node-20 annotation covers first-party actions only**.
    The macOS job ran `apple-actions/import-codesign-certs@v3` (also node20)
    without it being listed anywhere in the annotations, so treating that
    warning as an inventory of what needs bumping misses third-party actions.
    Grepping `uses:` and reading each action's `runs.using` is the reliable
    way. (Relatedly: the earlier guess that the signing step must be skipped
    because it was unannotated was wrong — the beta's macOS job imported the
    certificate and notarized successfully, so the Apple secrets are live.)
  - `make test` unaffected (the change is `.github/` only).

### 2026-09-12

- 18:51 — Read-view copy actions consolidated. The detail pane had three
  copy buttons, and the footer **Copy** was a duplicate of **Copy
  rendered** for templates: both passed `copyText`, and because `showVars`
  requires `!hidden`, the footer's `disabled={hidden}` was always false
  whenever the Rendered box was visible. The footer button is gone; every
  code box now owns its copy action in its `.box-head`, and those buttons
  carry the accent fill/border the footer button used to have
  (`.detail .box-head .copy-button`, with a higher-specificity `.failed`
  override so a rejected write still reads danger). A non-template body
  box gets **Copy snippet** (disabled while a sensitive body is hidden);
  templates keep **Copy template** (raw placeholders) and **Copy rendered**
  (filled). The footer holds only Edit. The keyboard copy is unaffected —
  it uses the published `detailCopy`, not the button. Tests retargeted
  (footer clicks → "Copy snippet"/"Copy rendered", the unrevealed-sensitive
  assertion now checks the disabled snippet button) and spec §6 updated.
  `make test` green (300 Vitest, svelte-check 0).

- (later) — **Compact layout designed and planned; no code.** Discussed
  the phone / narrow-window layout and settled on a stacked navigation
  model rather than the "responsive pane stacking" placeholder the
  2026-09-06 revision note had carried: the list (search on top) is the
  root screen, tapping a snippet pushes a detail screen with Back, and the
  left pane (Favorites, folders, tags, New folder) becomes a left drawer.
  Plus a three-way **Layout** setting — Auto / Wide / Compact — modelled
  on the theme select: Auto follows a 720px `matchMedia` breakpoint live,
  the two manual values ignore the viewport entirely.
  - Spec §6: new "Compact layout" subsection (setting, screens, drawer,
    top bar, history, keyboard, what does not change, Back-in-editor is
    Cancel); the "Panes stay side by side at every width" sentence and the
    revision note now point at it and say it is designed, not built.
  - Plan: **Phase 11** (T1 setting, T2 `lib/layout.ts` resolution + screen
    stack with an injected history, T3 compact grid/drawer CSS scoped
    under `.app.compact` — a class, not a media query, so the override and
    Auto share one path and tests can force it — T4 compact top bar with
    the chips relocated into the settings panel, T5 navigation wiring in
    App, T6 App tests with a fake `history`, T7 phone checklist + docs),
    milestone M9, a test-plan row, and a history/popstate risk entry.
  - Decisions worth knowing before starting: every push tags its history
    entry with its depth and popstate ignores anything else, so a stale
    entry from a previous load or a foreign entry cannot desync the
    stack; the in-app Back goes through `history.back()` so the OS gesture
    and the button share one path; `toRoot()` uses one `history.go(-depth)`
    for the ⌘/Ctrl+K shortcut instead of chained backs; the list stays
    mounted (hidden) under detail so its state survives; the wails
    window's 900px `MinWidth` means Auto never trips there but manual
    Compact does. The dirty-editor guard is explicitly a follow-on for
    both layouts.
  - Gotcha for T6: jsdom has no `matchMedia`, so `watchNarrow` must treat
    its absence as "not narrow" and the compact tests force the setting via
    `localStorage['snp.layout'] = 'compact'` rather than a viewport.
  - `make test` untouched (docs only); `web/dist/index.html` untouched.

- (later) — **Phase 11, the compact layout, built** (T1–T6, one commit
  each, `make test` green at each: go test + vet, 324 Vitest, svelte-check
  0/0). One unplanned commit between T5's two halves fixed a stack bug.
  - **T1** `settings.ts`: `Layout` type, `LAYOUTS`, `snp.layout` (Auto is
    the absence of the key), *Layout* select under *Theme*.
  - **T2** `lib/layout.ts`: `resolveLayout`, `watchNarrow` (matchMedia
    `(max-width: 719px)`; "not narrow" once when matchMedia is missing),
    `createCompactNav` with an injected `NavHistory`, `browserHistory()`,
    `isTextInput()`. 15 unit tests with a fake history that answers
    `back()`/`go()` asynchronously via a `flush()`.
  - **T3** `.app.compact` rules: one-column grid, list/detail share the
    cell with the inactive one `hidden` (list stays mounted), the folders
    pane is an `absolute` drawer *inside* `.panes` with a scrim and its
    own × — positioned in the panes row rather than `fixed` from a
    measured chrome height, because `bind:clientHeight` needs
    ResizeObserver, which jsdom lacks (first attempt failed all 28 App
    tests). Settings popover becomes a sheet under a `position: relative`
    `.chrome`. Splitters not rendered, no inline pane widths.
  - **T4** compact bar: ☰ / ← at the left (Back whenever `depth > 0`),
    wordmark, error/notice, gear; version/conn/synced/Resync in a status
    row at the top of the settings sheet via one shared `{#snippet
    syncStatus()}`; *Folders* button in the list toolbar (`onfolders`
    prop, button only when passed).
  - **T5** wiring: `selectSnippet` (tap) pushes, `setSelection` (arrows)
    does not; create/edit push; `cancelEdit` of a *create* goes back;
    an effect turns "landed on list while editing" into `cancelEdit()`
    (Back-is-Cancel) and "detail with nothing to show" into `back()`;
    folder pick closes the drawer; Escape pops drawer then detail unless
    editing or in a text field; ⌘/Ctrl+K → `toRoot()` then focus after
    svelte `tick()` (a `hidden` field cannot take focus — and App already
    has its own `tick()` for sync, so svelte's is imported as `settle`);
    mode-switch effect with `untrack` around the selection reads;
    `SnippetList` `visible` prop re-runs the scroll-into-view.
  - **Stack bug found in the browser, not in unit tests:** wide → compact
    (detail) → wide → compact pushed a second `{snp:1}` on top of the
    stale one, so Back landed on the stale entry, which *looked like*
    depth 1 and was ignored — the "leftover entries are ignored" rule
    only held while nothing was pushed on top of them. Fix: entries carry
    a `ses` id renewed on every `enter()`, popstate trusts only this
    session's entries (anything else at depth 1 is the root), and
    `enter(true)` onto a stale depth-1 entry reuses it via
    `replaceState`. Two regression tests.
  - **T6** App tests: a `stubHistory()` that spies `window.history` and
    dispatches `popstate` synchronously from `back()`/`go()`; compact is
    forced via `localStorage['snp.layout']`. Gotcha: a new wide-mode test
    that clicked *Caddyfile* before the old "selecting a snippet" test
    primed the lazy highlight module, after which the body renders as
    spans and `getByText('http://localhost:8080')` fails — the new tests
    match on `.detail .body` `textContent` and the wide test runs last.
  - **Browser verification** (headless Chromium via playwright-core in the
    scratchpad, against `./bin/snp serve --dev-listen` seeded with the
    starter pack): at 400×800 — list, ☰ drawer, folder pick closes it,
    tag toggle keeps it, tap → detail (h1 matches), ← and `page.goBack()`
    and Escape each pop one level with the row still selected, ⌘K from
    detail/drawer → list with search focused, New → Cancel → list, Edit →
    Cancel → detail, `goBack()` out of the editor cancels it, delete →
    list, settings sheet holds Resync + Layout, forced Wide at 400px is
    cramped but usable, forced Compact at 1200px works and survives a
    reload at the root; 400 → 1200 → 400 with a snippet open shows three
    panes with two splitters and then lands back on detail; no horizontal
    overflow at any step. `pkill -f "bin/snp serve"` from the tool shell
    matches the shell itself — start the server as a background task.
  - **Not done:** the on-device checklist (iOS tab + installed PWA,
    Android hardware back, wails forced Compact). Recorded as the next
    step above.

### 2026-09-13

- 01:05 — **Auto-deploy on the dev box** (spec §7, "Auto-deploy from a
  checkout"), on `main`. The tailnet server on this host now runs as a
  systemd *user* unit from the checkout's `bin/snp` instead of the
  nohup + `.pid` script, and `snp-update.timer` (5 min) redeploys when
  `origin/main` moves: `deploy/update.sh` (keep old binary → ff → build →
  backup with the **old** binary → restart → poll `/api/me` ≤60s →
  rollback + `deploy/.last-failed`), `deploy/autoupdate.sh` (the timer
  target), `deploy/user/` templates and `deploy/install-autoupdate.sh`.
  README and CLAUDE.md updated.
  - Verified on this host: install retired the nohup instance and the
    unit came up healthy in ~4s; the first timer tick deployed end to end
    in 16s CPU; a forced deploy against a 404 health URL rolled back
    (binary swapped, schema version 4 unchanged so the live database was
    kept, marker written, server active) in 25s wall including two 10s
    waits.
  - **Gotchas:** (1) a local `main` merely *ahead* of origin counted as
    "moved" and triggered a deploy that reported origin's commit —
    both scripts now require HEAD to be an ancestor of `origin/main`.
    (2) `snp backup` opens the store and therefore runs migrations, so
    the pre-deploy backup must be taken with the previous binary
    (`bin/snp.prev`), after the build. (3) Every build rewrites the
    tracked `web/dist/index.html`; `update.sh` resets it before the
    clean-tree check and after the build, or `--ff-only` refuses.
    (4) `git fetch` over SSH works inside a user service here (key
    without passphrase), so no remote change was needed. (5) User
    services start with a bare PATH; the oneshot sets one that includes
    `~/go/bin`.
  - **Caveat:** this checkout is also the working tree, so a feature
    branch or uncommitted change pauses deploys until it is back on a
    clean `main`; a dedicated clone would decouple that.
- 01:10 — **Request log fix**: every line printed `login=""` because
  `loggingMW` wraps the handler outside `authMW`, whose `r.WithContext`
  copy never reaches the outer request. The logger now plants a
  `*requestInfo` in the context and `authMW` fills the login before the
  owner check, so 403s are logged by name too. Test captures the slog
  line for owner / rejected / static.
- 01:45 — **Command palette** (spec §6 "Keyboard", brainstormed: palette
  over slash-in-search, commands only, `Cmd/Ctrl+Shift+P` as the single
  trigger), built test-first in a worktree so the auto-deploy kept
  flowing from `main`. `lib/commands.ts` (`Command` + `filterCommands`,
  5 tests), `lib/keys.ts` (`isPaletteShortcut` on `e.code`,
  `paletteShortcutLabel`, 3 tests), `lib/CommandPalette.svelte` (8
  tests: focus, filter, wrap, Enter, disabled-with-reason, click, Escape
  / scrim, empty), `App.svelte` (a `$derived.by` command list from the
  existing actions, one keydown branch, a shortcut hint line in the
  settings sheet; 4 App tests), palette CSS in `app.css`. `make test`
  green: 344 Vitest, svelte-check 0/0.
  - **Browser verification** (headless Chromium, `--dev-listen :8090`,
    seeded): 1200 light + dark and 400: shortcut opens with focus in the
    filter, greyed rows carry the reason, `foc` + Enter lands focus in
    the search field, Copy from the palette flashes "Copied.", New
    opens the form (compact too); pixel check confirmed the 45% scrim.
  - **Gotchas:** (1) `button:hover` (0,1,1) in `app.css` beat the
    scrim's `.palette-scrim` (0,1,0) `background: transparent`, so the
    page behind the palette went opaque `--bg-alt` whenever the mouse
    rested on the scrim — the rule is now `.modal-backdrop >
    button.palette-scrim`. (2) A clickable backdrop `<div>` trips two
    svelte-check a11y warnings; the compact drawer's scrim-button pattern
    keeps the check at zero. (3) The worktree-isolation guard refuses
    compound shell lines that mention git; keep git invocations plain.


### 2026-09-14

- **Desktop minimum width below the compact breakpoint.** v0.2.0's wails
  window set `MinWidth: 900`, above the SPA's `(max-width: 719px)`
  compact breakpoint, so Auto could never reach compact on the desktop
  (spec §6 recorded this as intended; it is not what we want). Now
  `MinWidth: 400` — the narrowest width the Phase 11 browser check
  confirmed has no horizontal overflow. Spec §6 and README updated.
  `make desktop` + `go vet` clean; resizing the window across 720px
  still needs a by-hand check in the running app.
- **Desktop resize did not switch to compact (v0.2.1).** With the
  400px minimum in place, dragging the wails window under 720px still
  kept the three panes: `watchNarrow` relied solely on the
  MediaQueryList `change` event, which the embedded webview did not
  deliver on a native window resize. The same bundle in headless
  Chromium (dev server, viewport 1200 → 600 → 1000) switched both ways,
  so the SPA logic was sound and the signal was the gap. `watchNarrow`
  now re-reads `mql.matches` on the `change` event, on every window
  `resize` and on a `ResizeObserver` of `document.documentElement`, and
  reports only when the answer flips (3 tests). Spec §6 and plan Phase
  11 T1 updated. `make test` green. **Needs the by-hand check in the
  wails app** (Settings → Layout = Auto, drag across 720px both ways);
  if it still does not switch, the next thing to verify is that the
  Layout setting is Auto rather than Wide, then whether `resize` fires
  at all in that webview (`--debug` + a `pageLog` on resize).
- **Auto-deploy: hand runs rolled back healthy servers.** Two of today's
  four deploys (13:57, 14:24) rolled back although the new server was up
  in ~2s and serving the browser: the journal shows zero `/api/me`
  probes reaching either instance, while the two successful deploys
  each show one right after startup. Cause: `deploy/update.sh` defaulted
  the node name to `snp` when run without `-n`, so the check polled
  `https://snp.<tailnet>/api/me` (no such node → curl 000) for 60s and
  "failed". The timer was unaffected because the installer bakes `-n
  snip` into its unit. Fix: `update.sh` now reads the node name from the
  installed unit's `ExecStart --hostname` (flag still overrides, `snp`
  only with no unit), and preflights the health URL against the server
  already running — a URL that does not answer 200 before the deploy is
  a wrong URL, so it refuses up front instead of rolling back later
  (skipped, with a warning, when the unit is not running). README, spec
  §7, the installer's hint updated; the stale `deploy/.last-failed` and
  `bin/snp.failed` removed. Verified: `update.sh -f` with no `-n`
  derives `snip` and deploys healthy; `-U` with the wrong host is
  refused before any restart.

### 2026-09-17 — Frontend remediation transferred to the current repository

- Ported the applicable fixes from the older `~/CODE/qwen/snp` checkout.
  Kept this repository's Favorites/pinned data, prefix-AND search, compact
  history navigation, draggable panes, AI output kind and Explain Undo,
  clipboard fallback/feedback, and darwin/Linux desktop wiring.
- Sensitive reveals now retain body and defaults together, refresh after
  saves, and expire on selection/sync; stale async responses are ignored.
  Cache writes and sync defensively redact sensitive content. Search derives
  from current rows so unchanged queries refresh after sync and mutations.
- Dirty drafts are protected across selection, Favorites, New, Cancel,
  compact Back/search, full resync, browser unload and native close. PWA
  updates defer reload while a draft is dirty or a write is pending. Dialogs,
  palette and drawer contain focus; destructive dialogs start on Cancel.
- Saves are serialized with sync and other snippet writes, disable duplicate
  submissions and offline writes, and keep drafts with inline errors on
  failure. Existing null folders remain unfiled; new snippets inherit the
  active filter; folder labels show ancestor paths. Desktop shell injection
  now reads the actual embedded `dist/index.html`.
- Validation: `make test` green (Go vet/tests; 368 Vitest tests, up from 346;
  svelte-check/TypeScript clean). Production web, server and macOS desktop
  builds passed. Browser smoke against a disposable database confirmed
  sensitive create/defaults/edit-save, invalidation on sync, compact Back
  Keep editing/discard, and no console warnings/errors.
- Gotchas: broad cherry-picks would regress the newer layout and pin/default
  replacement path; changes were integrated selectively. Compact Back must
  restore its history entry before showing the discard dialog. Sync checks
  must read the save flag untracked to avoid making the sync effect subscribe
  to writes. Linux build tags are preserved; Linux-native execution and
  physical mobile/PWA hardware checks remain unverified on this macOS host.
  Generated web/dist/index.html is restored to its tracked stub after builds.

### 2026-09-20 — UI cards and tag cloud transfer

- Ported the UI adjustments from ~/CODE/qwen/snp onto
  codex/ui-cards-tag-cloud: wrapping tag pills with counts, snippet result
  cards, a detail card, and persisted Compact / Regular / Large list sizes.
  Large cards preview only non-sensitive bodies, limited to three lines.
- Preserved this repository's Favorites, resizable panes, compact navigation,
  search shortcuts/selection scrolling, date formatting, and independent
  two-line title setting. Search controls wrap within narrow panes. Renamed
  the detail pane class so the card styling applies only to its contents;
  updated compact CSS and existing navigation test selectors accordingly.
- Validation: make test passed (Go vet/tests, 369 Vitest tests, zero
  Svelte/TypeScript errors/warnings); production Vite/PWA build passed.
  Regression coverage verifies saved size selection, invalid-value fallback,
  and sensitive-body exclusion. No browser/native visual smoke this session.
  Restored the tracked dist stub after building; generated assets excluded.

### 2026-09-20 — SVG toolbar icons

- Replaced the compact Folders menu glyph with an outlined folder SVG and
  the Settings gear glyph with an outlined gear SVG. Both match the detail
  pin/trash stroke weight, inherit theme color, and scale with interface text.
  Existing accessible button labels remain; decorative SVGs are aria-hidden.
- Validation: make test passed (Go vet/tests, 369 Vitest tests, zero
  Svelte/TypeScript errors/warnings). Left the pre-existing generated
  web/dist/index.html change untouched and excluded it from the commit.

### 2026-09-20 — Recheck nested-folder export/import after UI update

- Checked main at 19e66b9 against the nested-folder export/import finding
  in docs/grok-review.md. Export/import implementation is unchanged from
  the reviewed revision; the issue remains and contradicts the intended
  export/import round trip (spec §5, plan Phase 5).
- Reproduced with disposable stores: create Work/bash and a snippet in bash,
  export, JSON-round-trip into ImportDoc, then import into an empty store.
  Both merge and replace fail with FOREIGN KEY constraint failed (787),
  twice each. Export orders bash before Work; import inserts in that order.
  Reversing only the two folder entries makes both modes pass, twice each,
  with both folders and the snippet's folder reference restored.
- Existing tests miss this nested-folder round trip. Recommended fix is
  dependency-aware folder import, so previously generated exports work too,
  with regression coverage for child-before-parent documents in both modes.
  This session was verification only; temporary repro tests were removed
  and application code was not changed.
- Validation: make test passed (Go vet/tests, 369 Vitest tests, zero
  Svelte/TypeScript errors/warnings). Initial sandboxed Go suite could not
  bind local HTTP test listeners; rerun with listener access passed.

### 2026-09-20 — Fix nested-folder export/import ordering

- Added a regression test before the fix: a three-level Work/bash/awk tree
  exports children first and failed to import into an empty store in both
  merge and replace modes with FOREIGN KEY constraint failed (787).
- Import now orders document folders by parent dependency before upserting,
  without changing the document or export format. Existing exports therefore
  work without manual reordering. Parents outside the document are resolved
  against live stored folders. Duplicate folder IDs, document cycles, and
  missing parents return ErrImport; failures leave stored data unchanged.
- Regression coverage checks both modes, repeat imports (updates), the full
  restored tree and snippet fields, existing parents outside the document,
  invalid dependencies, and rollback. The original failure now passes.
- Validation: make test passed (Go vet/tests, 369 Vitest tests, zero
  Svelte/TypeScript errors/warnings); git diff --check clean. No web build or
  generated assets. Unrelated untracked review notes and screenshot excluded.

### 2026-09-20 — Dismiss settings on outside clicks

- Reproduced the settings flyout remaining open after clicking a snippet in
  both wide and compact layouts; there was no outside-click listener.
- Added a document capture listener while settings is open, cleaned up on
  close/unmount. The boundary includes the panel and its toggle, so controls
  inside remain usable and the toggle still closes normally. Outside clicks
  dismiss settings without consuming the clicked control's action. Settings
  now exposes its expanded state to assistive technology.
- Added two failing-then-passing component regression cases covering wide
  and compact layouts, inside controls, outside selection, toggle dismissal,
  and blank-area clicks.
- Validation: make test passed (Go vet/tests, 371 Vitest tests, zero
  Svelte/TypeScript errors/warnings); git diff --check clean. No browser or
  native visual smoke performed; no generated assets changed.

### 2026-09-20 — Explore visual identity without workflow changes

- Reviewed the current appearance CSS, list/detail components, spec §6,
  and repository screenshots for a design discussion. Screenshots predate
  some current card and toolbar treatments; source informed the concepts.
- Prepared a separate conversation mockup with warm moss, quiet violet,
  and slate-blue treatments. Suggested consistent typography, quieter
  borders, a distinctive selected-row marker, and a small brand mark.
- Application code and generated assets unchanged. This was a visual
  exploration, not an implemented or browser-verified redesign; no tests
  run. Existing themes, text scaling, list density, and compact navigation
  should be preserved in any implementation.

### 2026-09-20 — Implement the theme-aware Slate Blue visual treatment

- Added opt-in Slate Blue (system), with light/dark UI and syntax palettes.
  Existing theme IDs, colors, defaults, and stored preferences are preserved.
- Shared CSS now supplies quieter borders, flat cards, neutral metadata
  chips, a clipped-bracket wordmark, accent selection markers, technical
  section labels, and copy-action accents. Surface colors derive from the
  active palette. Button color transitions honor reduced motion. Pane
  sizing, card padding, density options, text-size controls, and navigation
  logic remain unchanged; no application event handlers changed.
- Extended the existing settings component test to cover selecting and
  persisting Slate Blue, then returning to Auto. Updated spec §6.
- Validation: make test passed (Go vet/tests, 372 Vitest tests, zero
  Svelte/TypeScript errors/warnings). Production Vite/PWA build passed with
  output in a temporary directory, leaving the tracked web/dist stub alone.
  Headless Chrome against an isolated local server verified identical
  measured geometry across all eight themes, live Slate Blue light/dark
  switching, persistence on reload, template preview/copy, all densities,
  150% text scaling, and compact detail/back/drawer navigation across named
  themes without page errors. Inspected light/dark/390px screenshots.
- Gotchas: local preview listeners and browser networking require sandbox
  escalation. Bundled Playwright had no downloaded browser; used installed
  Chrome with a fresh temporary profile. No native Wails smoke or deployment.

### 2026-09-20 — Match sensitive locks to the SVG glyphs

- Replaced the lock emoji in snippet cards and the detail sensitivity notice
  with a monochrome outlined SVG using the existing glyph stroke style.
  The icon inherits the active theme color and interface text scaling.
  Cards expose an accessible Sensitive label; detail icons are decorative
  alongside the existing visible notice. Sensitive-body behavior unchanged.
- Validation: make test passed (Go vet/tests, 372 Vitest tests, zero
  Svelte/TypeScript errors/warnings); git diff --check clean. Initial Go
  HTTP tests hit sandbox listener restrictions; rerun with access passed.
  No new tests for this visual-only change; no generated assets changed.

### 2026-09-21 — Block body AI actions for sensitive snippets

- Reproduced the spec §13 violation with failing component and HTTP tests:
  Suggest tags/Explain accepted sensitive bodies, and pending responses
  still changed Tags/Notes after the user checked Sensitive.
- Both buttons now disable immediately for sensitive drafts (new or saved),
  with an explanatory hint; action handlers independently refuse calls.
  Sensitivity changes invalidate pending results and errors, including an
  on/off toggle before completion. Already-sent content cannot be recalled.
- Both API requests now require an explicit is_sensitive boolean. True
  returns 403; omitted, null, or invalid returns 400 without contacting the
  provider. Browser and desktop use the same handlers. This trusts the
  draft's declared classification; it does not detect arbitrary secrets.
  Older clients must refresh before these actions work. Updated spec §13.
- Validation: failing regressions passed after the fix; make test passed
  (Go vet/tests, 380 Vitest tests, zero Svelte/TypeScript errors/warnings).
  Coverage includes handler guards even when DOM disabling is bypassed,
  re-enabling after unchecking, late success/error responses, zero provider
  calls for rejected requests, and ordinary non-sensitive AI behavior.
  Go checks needed sandbox access to the build cache and test listeners.
  No browser/native smoke or web build; generated assets unchanged.

### 2026-09-21 — Nixie / CRT theme mockup

- Added a throwaway, fixture-only visual study at web/theme-prototype.html,
  launched with `cd web && npm run prototype:theme`. Three URL variants:
  A (amber three-pane workspace), B (green terminal with horizontal folders),
  C (hybrid instrument header and green code display). Includes search,
  selection, clipboard copy, and independent static glow/scanline controls.
- Uses a separate Vite development HTML entry to avoid accessing live snippet
  data or changing the application theme. No production entry or generated
  assets changed. Layout experiments are proposals, not changes to spec §6;
  a production theme should preserve existing geometry unless separately
  approved. No winner selected; prototype remains uncommitted for review.
- Validation: inspected all variants at desktop width and hybrid at 390px;
  exercised search, selection, and effect toggles; browser reported no
  warnings/errors. git diff --check clean. No automated tests added or run
  for the isolated visual prototype. Clipboard copy was not exercised.
- Gotcha: local Vite listener required sandbox escalation. Preview currently
  runs on 127.0.0.1:5178, with variant C open in the in-app browser.

### 2026-09-21 — Add Nixie and CRT appearance themes

- Promoted the selected amber and green directions into opt-in Nixie and
  CRT entries in Settings → Theme. Preserved layout, fonts, scaling, pane
  geometry, and existing theme preferences. Added syntax palettes, soft
  branding/code/selected-title glow, and a glass version badge for Nixie.
- Strengthened scanlines and placed them behind code text, including both
  raw templates and rendered previews. Effects are static and do not
  intercept text selection or pointer events. Updated spec §6.
- Preserved the original study and decision on local branch
  `codex/prototype-nixie-crt` (d4d3bf4); removed its standalone HTML and npm
  launcher from the production checkout. Both themes were selected; hybrid
  layout and decorative counters were not promoted.
- Validation: make test passed (382 Vitest tests, Go vet/tests, zero
  Svelte/TypeScript errors/warnings); extended the existing settings test
  for selection/persistence of both themes. Production build passed with
  output under /tmp. Browser verified both palettes, visible scanlines,
  identical pane/detail/code geometry against Auto, CRT persistence after
  reload, and both themes on a 390px template detail view. No browser
  warnings/errors. No native Wails smoke or deployment performed.
- Preview uses a temporary starter-pack database, a loopback API on :8080,
  and the production build on :5179. The existing Vite development entry
  was blank, so visual verification used the production build. A separate
  build changed the tracked web/dist stub during this session; it was not
  modified or staged by this task.

### 2026-09-21 — Pinnable folders flyout and two-pane wide layout

- Added a folders-header pin control, defaulting to pinned. Unpinning
  removes the folders column/divider and exposes a topbar Folders button
  for the favorites/folders/tags flyout. Pinning restores the three panes.
  Persisted the preference and kept independent widths/reset behavior for
  two- and three-pane layouts; unpinned resizing reserves no folder space.
- Reused the compact drawer surface and modal focus handling. Added inert
  closed/background surfaces, initial close-button focus, opener focus
  restoration, Escape/outside dismissal, folder/favorite dismissal, and
  multi-tag selection without dismissal. Pinning preserves editor drafts.
  Compact mode retains its history navigation and hides the pin; mode
  transitions dismiss the desktop flyout without changing the preference.
- Updated spec §6. Added component regressions for persistence, filtering,
  focus, dismissal, draft retention, search shortcut, mode changes, and
  independent sizing/reset; added a two-pane width-limit regression.
- Validation: make test passed (Go vet/tests, 387 Vitest tests, zero
  Svelte/TypeScript errors/warnings). Temporary production build passed.
  Browser verified docked/two-pane/flyout views, reload persistence, focus,
  a live 390px transition, compact folder dialog/cancel and return to wide;
  no browser warnings/errors. git diff --check clean. No native Wails smoke
  or deployment. Preview remains at 127.0.0.1:5179 with temporary data.
- Gotchas: wait until the layout update settles before measuring a newly
  selected pane arrangement; hidden drawer width must not constrain a
  two-pane resize. jsdom inert is a property without attribute reflection.
  An independently rebuilt web/dist/index.html remains excluded from this
  task's commit.

### 2026-09-21 — Prepare v0.4.0 documentation and release tag

- Updated README usage for pinnable folders, the two-pane flyout, separate
  remembered divider widths, theme selection (including Nixie and CRT),
  text scaling, and list density. Updated compact navigation wording and
  the release-tag example to v0.4.0.
- Corrected stale AI documentation: Explain replaces Notes with Undo, and
  sensitive drafts block Explain/Suggest tags and discard pending results.
  Kept the distinction that an explicit Ask AI prompt still goes to the
  configured provider. Reflowed release asset details to pass Markdown lint.
- Validation: markdownlint-cli2 README.md passed; make test passed (Go
  vet/tests, 387 Vitest tests, zero Svelte/TypeScript errors/warnings);
  git diff --check clean. Remote main matched the checkout and v0.4.0
  was absent when checked. User requested this docs commit, an annotated
  v0.4.0 tag, and pushing main plus that tag to origin; the tag triggers
  the existing cross-platform release workflow.

### 2026-09-21 — Trash, restore and revision history

- Added paginated online Trash with restore, a 10-second deletion Undo,
  and command-palette entries. Restore preserves content/favorites, clears
  the tombstone, advances updated_at for sync, and falls back to Unfiled
  when the original folder is deleted. Retained FTS rows are reused.
- Added migration 0005 and transactional previous-version capture on
  meaningful replacements and import overwrites. Retain 50 revisions per
  snippet, using monotonic IDs for same-second ordering. Favorite-only and
  no-op changes are excluded. Version restore captures current content
  before replacing it, preserves creation time/current favorite state, and
  resolves missing folders to Unfiled. Hard purge cascades to revisions.
- Sensitive snapshots encrypt the entire payload with AES-GCM and AAD
  bound to both snippet and revision ID. Enabling sensitivity encrypts
  earlier plaintext versions in the same transaction. Protected restores
  stay sensitive. Summary responses contain no historical content; the
  preview endpoint requires explicit reveal=1 for protected versions.
  Recovery responses and single-snippet reads use Cache-Control: no-store.
- Added a responsive History dialog beside Edit, with side-by-side body,
  notes, metadata and variable-default comparison. Recovery data stays in
  dialog memory, sensitive previews require reveal, and stale requests are
  discarded after selection changes/disconnect/close. Existing modal,
  dirty-editor and serialized-write protections cover recovery actions.
- Fixed folder purge to defer folders still referenced by newer snippet
  or folder tombstones, avoiding foreign-key failures during cleanup.
- Updated README, spec and plan. History starts with future edits;
  database backups retain history/trash, JSON exports remain live current
  versions only. No history is synced into the offline cache.
- Validation: make test passed (Go vet/tests, 397 Vitest tests in 32 files,
  zero Svelte/TypeScript errors or warnings), production web build and Go
  API build passed, README Markdown lint and git diff --check passed.
  Regressions cover FTS/sync restore, missing folders, retention/no-op,
  encryption/promotion/AAD binding, import rollback, purge, API auth/CSRF,
  explicit reveal, UI failures/stale responses, Undo and cache redaction.
- Browser smoke against temporary data: edit → compare → restore, delete →
  Trash → restore with history intact, 390px responsive comparison, and
  sensitivity promotion → reveal → restore with body hidden afterward.
  No console errors/warnings. No native Wails smoke or deployment.
  Preview remains on 127.0.0.1:5179, with the updated test API on :8080.
- Gotchas: UI test fireEvent.click does not focus the opener; set focus
  before asserting modal restoration. Testing Library role queries use
  exact string names without Playwright's exact option. After a preview
  rebuild the service worker can refresh once more during initial actions.
  web/dist/index.html was left untouched; all builds used temporary output.

### 2026-09-21 — In-app import, export and full backup

- User chose snp JSON for the first import format. Added Settings and
  command-palette access to a responsive Import, export & backup dialog.
  Browser uses file input/downloads; desktop has native Open/Save dialogs,
  injected into the testable desktop bridge without Wails dependencies.
- JSON import validates the file shape/10 MiB app limit, defaults to merge,
  and previews through POST /api/import?preview=1. The complete import
  transaction rolls back for preview, including FTS, tags and revisions.
  Replace previews the count moving to Trash and requires acknowledgement.
  Apply refreshes the local cache under the existing write/sync exclusion.
  If commit succeeds but refresh fails, the UI reports the import as saved
  and clears the file instead of encouraging a duplicate retry.
- Imported created_at is normalized to UTC seconds; updated_at now uses
  import time. Preserving old updated_at was a sync bug: other clients
  with newer cursors missed imported rows. Updated the timestamp regression
  and documented the intentional behavior change. Combined folder trees
  reject cycles/duplicate sibling names; replace keeps ancestors of live
  folders, including folder_path-created trees.
- Export now reads folders/snippets/tags in one transaction and uses
  no-store responses. JSON remains live current content, including plaintext
  sensitive bodies/defaults; it excludes Trash and history.
- Added POST /api/backup: verified VACUUM INTO snapshot, ZIP with snp.db,
  matching key, and RESTORE.txt. Private temporary paths are cleaned up;
  concurrent backups on the same server are rejected. Archive includes
  Trash/revisions, not preferences/config/tsnet state. ZIP is not password
  encrypted; the UI explains that its holder can read sensitive content.
  Restore instructions require stopping snp and using an empty state dir.
- Native file saves generate only known export/backup content after the
  user chooses a destination. Writes use a private temp file and atomic
  rename, preserving existing files on failure; cancellation is a no-op.
  Binary backup bytes never pass through the text-only CallAPI result.
- Validation: make test passed (Go vet/tests, 409 Vitest tests in 34 files,
  zero Svelte/TypeScript diagnostics). Production web, headless Go, and
  macOS desktop production builds passed. README Markdown lint and
  git diff --check passed. Backup tests reopened the archive as a fresh
  store and verified sensitive content/history, Trash and the matching key;
  native tests verified JSON/ZIP writes, 0600 mode, cancellation and failure.
- Browser smoke: chose a temporary JSON file, previewed 1 new snippet,
  applied it successfully, and checked the panel at 390px. Both download
  controls reached Download started without console errors; the in-app
  browser did not emit the awaited download event, so browser file delivery
  was not independently confirmed. Direct live endpoint downloads verified
  valid JSON (6 snippets/1 folder) and ZIP integrity/database/key/instructions.
  Native OS dialog interaction was not smoke-tested; its wiring compiled
  and file-operation regressions passed. Preview remains at :5179 with
  the updated loopback API on :8080, using only temporary data.
- Gotchas: the automation file-picker call took unusually long to return.
  Viewport changes need a settled observation before judging screenshots.
  Build outputs stayed in /tmp; web/dist/index.html was not changed. README,
  spec and plan were updated. Changes remain in the working tree.


### 2026-09-21 — Password-protected export and backup

- Added the standard age Go library (v1.3.2), using its default scrypt
  passphrase recipient and ASCII armor. Complete JSON/ZIP files are
  encrypted, including the backup key, Trash and revision history. Fresh
  salt and file key for every download; no custom encryption format.
- UI protection defaults on for all downloads. Password/confirmation
  require at least 12 characters, with the requested warning: "If you
  forget this password, you're toast." Turning protection off shows a
  plaintext warning. Passwords are only in memory/request bodies, cleared
  after attempts and on close. Native saves preserve raw encrypted bytes.
- Encrypted JSON files unlock before the normal preview/apply sequence.
  Decryption performs no store writes and never returns partial plaintext.
  Armored input is capped at 16 MiB and decrypted JSON at 10 MiB; excessive
  scrypt work is rejected and one password operation per server bounds RAM.
  Owner/JSON CSRF checks and no-store headers apply to new endpoints.
- Added `snp decrypt input.zip.age output.zip` (also works for JSON), with
  a non-echoing terminal password prompt independent of the original store
  and key. Stages private output, verifies through EOF, then publishes with
  a non-overwriting link; errors remove partial files. Restore remains an
  explicit stopped-app procedure. Standard age APIs also recover output.
- Validation: make test passed (Go vet/all Go tests, 414 frontend tests,
  zero Svelte/TypeScript diagnostics). Tests cover randomization, standard
  age interoperability, wrong passwords, truncated/tampered payloads,
  excessive KDF work, native encrypted output, import unlock, confirmation,
  plaintext opt-out and password clearing. Production frontend, headless Go
  and macOS desktop builds passed; desktop emits an existing Wails/AppKit
  deprecation warning. README lint and git diff --check passed.
- Live smoke: disposable preview generated an encrypted backup; downloaded
  it directly from the API and recovered it through the CLI's private
  terminal prompt. Verified ZIP CRC, database header, matching key entry
  and 0600 output permissions. Browser download control reached success,
  and visible password fields were cleared. Native dialog interaction and
  browser file delivery were not independently verified (same limits as
  previous session); native-save and API files have automated coverage.
- Updated README, spec and plan. Existing plaintext CLI/API formats remain
  compatible. UI built only into /tmp, preserving web/dist/index.html.
  Preview stays at :5179 with the updated loopback API on :8080. No commit,
  tag, push or release was performed in this session.


### 2026-09-21 — Duplicate snippet

- Added Duplicate in the detail toolbar and Duplicate snippet in the command
  palette. Opens a new `Title (copy)` draft, retaining body, notes, language,
  folder, tags, saved template defaults, sensitivity and favorite status.
  Uses the ordinary Create endpoint; no source ID, timestamps or history
  are copied. The original remains untouched.
- Kept duplicate seeds separate from edit targets. Prefilled copies use the
  existing unsaved-changes guard; all editor exit/reset paths clear seeds.
  Sensitive copies fetch original bodies/defaults on demand, ignore late
  results after navigation, and remain redacted in IndexedDB. Offline
  duplication is disabled. Rendered template values are not copied.
- Validation: make test passed (Go vet/tests, 419 frontend tests, zero
  Svelte/TypeScript diagnostics), production UI build passed, README lint
  and git diff --check passed. New integration regressions cover ordinary
  and sensitive creation/metadata/source preservation, redacted caching,
  cancel/seed clearing, stale fetches and offline controls. Test fixtures
  need their own cleanup/cache reset when outside the existing App suite.
- Browser smoke on disposable :5179 preview: duplicated JSON import demo,
  confirmed prefilled draft/Create, saved a separate copy alongside the
  original, and verified its empty initial revision history. README/spec/
  plan updated; build output stayed in /tmp, leaving the embed stub intact.
  No commit, tag, push or release performed in this session.
