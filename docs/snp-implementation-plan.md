# snp — Implementation Plan

Date: 2026-09-02
Status: ready to execute
Spec: `docs/snp-design.md` (this plan implements that document; section refs
like "spec §4" point there)

## 0. Conventions

- **Module**: `github.com/jstevewhite/snp` (adjust if the repo lands elsewhere).
- **Go**: current stable (≥ 1.24). Routing uses the stdlib `http.ServeMux`
  method+pattern support (Go 1.22+); no router dependency.
- **Third-party Go deps** (all of them):
  - `github.com/pelletier/go-toml/v2` — config
  - `modernc.org/sqlite` — SQLite driver (bundles FTS5; no cgo)
  - `tailscale.com` — `tsnet`, `WhoIs`
  - `github.com/oklog/ulid` — ids
- **Frontend deps**: Svelte 5, Vite, TypeScript, `marked`, `dompurify`,
  `minisearch`, `idb`, `@codemirror/*` (state, view, commands, language,
  lang-*), `vite-plugin-pwa`, Vitest.
- **Time**: every stored and serialized timestamp is RFC3339 UTC, second
  precision, no fractions, always `Z` (spec §4). One helper,
  `store.Now() time.Time` → `.Truncate(time.Second)`, formatted
  `"2006-01-02T15:04:05Z"`. The store takes a `Clock` interface so tests can
  control time (needed for the sync-boundary and purge tests).
- **Errors**: the store returns typed errors — `ErrNotFound`,
  `ErrFolderNotEmpty`, `ErrFolderCycle`, `ErrNameTaken`, `ErrImport` — and
  the server maps them to status codes (spec §8). No error text leaks
  storage details.
- **Logging**: `log/slog` to stdout; request-logging middleware includes
  method, path, status, duration, tailnet login.
- **Commits**: one commit per task below; `make test` green on every commit.

## Phase 0 — Scaffold

Goal: the repo builds and tests from a clean checkout.

- `go mod init github.com/jstevewhite/snp`
- Create the layout from spec §10: `cmd/snp/`, `internal/config/`,
  `internal/store/` (+ `migrations/`), `internal/server/`, `internal/tsauth/`,
  `web/`, `deploy/`, `docs/`.
- `web/embed.go` (package `webembed`): `//go:embed all:dist` exporting
  `FS embed.FS`. A stub `web/dist/index.html` is checked in so a bare
  `go build` works before the web build has ever run (spec §1 build note).
- `Makefile`:

  ```make
  web:
  	cd web && npm ci && npm run build
  build: web
  	go build -o bin/snp ./cmd/snp
  test:
  	go test ./...
  	cd web && npm test && npm run check
  dev: build
  	./bin/snp serve --dev-listen :8080
  ```

- `cmd/snp/main.go`: subcommand dispatch skeleton (`serve`, `backup`,
  `export`, `import`, `key`) printing "not implemented".

**Done when**: `make build` and `make test` succeed on a fresh clone.

## Phase 1 — Config (`internal/config`)

Goal: spec §2 resolution, fully tested.

- `Config{ Hostname, Owner, StateDir, LogLevel string }` plus
  `DevListen string` (empty = production mode).
- Resolution order, per key: flag > `SNP_*` env > TOML file > default
  (spec §2). Env names: `SNP_HOSTNAME`, `SNP_OWNER`, `SNP_STATE_DIR`,
  `SNP_LOG_LEVEL`, `SNP_CONFIG` (file path only).
- File discovery: `--config` → `$XDG_CONFIG_HOME/snp/config.toml` →
  `~/.config/snp/config.toml`. A missing file is not an error; malformed
  TOML is.
- Expand `~` in `state_dir`; after expansion it must be absolute.
- Defaults: `hostname=snp`, `log_level=info`,
  `state_dir=~/.local/share/snp`. `owner` is required unless dev mode.
- `Load(flags)` returns the resolved config plus which source each value
  came from (useful for a future `snp config show`).

**Tests** (`config_test.go`): precedence matrix for every key (flag beats
env, env beats file, file beats default); `~` expansion; missing vs
malformed TOML; owner required in prod; dev mode without owner.

**Done when**: table-driven tests green.

## Phase 2 — Store (`internal/store`)

The largest phase. Files and responsibilities:

| File | Responsibility |
|---|---|
| `store.go` | `Store` type, `Open(path)` (DSN pragmas), `Close`, `Clock` injection |
| `migrate.go` | embedded `migrations/*.sql`, `schema_version` table, apply in a tx |
| `snippets.go` | CRUD, soft delete, raw body, FTS rewrite on every write |
| `folders.go` | CRUD, move, cycle check, sibling-uniqueness check |
| `tags.go` | normalize + upsert, snippet↔tag joins, tag listing with counts |
| `search.go` | query parsing, SQL building, FTS fallback, pagination |
| `sync.go` | `SyncSince(since string)` (empty = full), `server_time` capture |
| `purge.go` | 30-day hard purge, tag pruning, `StartPurger(ctx)` (startup + 24h ticker) |
| `crypto.go` | key load/create (0600), AES-256-GCM seal/open, AAD = snippet id |
| `export.go` | export document (live rows, bodies decrypted) |
| `import.go` | merge/replace import, `folder_path` creation |
| `migrations/0001_init.sql` | full schema from spec §4 |

Key implementation points:

- **Open pragmas**: `journal_mode=WAL`, `busy_timeout=5000`,
  `foreign_keys=ON`, `synchronous=NORMAL` (spec §4).
- **Schema**: exactly spec §4 — `snippets` carries the `rowid INTEGER
  PRIMARY KEY` surrogate and `id TEXT NOT NULL UNIQUE`; `snippets_fts` is
  external content: `content='snippets', content_rowid='rowid'`.
- **FTS write helpers** (`snippets.go`): `insertFTSTx(tx, rowid, title,
  notes, bodyForIndex, tagsJoined)` and `deleteFTSTx(tx, rowid)`.
  `bodyForIndex` is `""` when `is_sensitive`; `tagsJoined` is space-joined
  (safe: tag names match `[a-z0-9][a-z0-9-]*`, rejected otherwise with
  400 at the API layer). New rows get insert only — a DELETE for a rowid
  never indexed corrupts the FTS5 index. Replace deletes the old FTS row
  **before** the content update, then inserts. On snippet removal the FTS
  row is deleted **before** the main row, same transaction (spec §4).
- **Search** (`search.go`):
  - Parse `q`: split on whitespace; `tag:x` / `lang:x` tokens become
    filters (repeatable, ANDed); the remainder is rejoined as the FTS
    expression. `tag:`/`lang:` tokens AND with the separate
    `tag=`/`lang=` params.
  - SQL: base `snippets` with `deleted_at IS NULL`; folder filter on
    `folder_id`; lang filter on `language`; tag filter via
    `EXISTS (… snippet_tags JOIN tags …)`; FTS via
    `JOIN snippets_fts ON snippets_fts.rowid = snippets.rowid AND
    snippets_fts MATCH ?`.
  - Ordering: with FTS → `bm25(snippets_fts, 10.0, 1.0, 1.0, 1.0)`;
    without → `updated_at DESC, id DESC`.
  - Pagination: default `limit` 50, max 200, `offset` ≥ 0.
  - Fallback: if the first `MATCH` errors, retry the whole expression as a
    quoted phrase (double any embedded `"`); second failure →
    `ErrFTS` → 400.
  - Empty remainder after filter extraction → no `MATCH` clause at all.
- **Sync** (`sync.go`): capture `serverTime := Now()` **before** the read;
  return live rows with `updated_at > since` plus tombstones with
  `deleted_at > since` (folders and snippets), sensitive bodies as `null`,
  and `server_time` = the captured value. Empty `since` → full sync
  (spec §5).
- **Purge** (`purge.go`): hard-delete snippets with
  `deleted_at < now - 30d` (snippet_tags, then FTS row, then main row, one
  tx per row-batch); same for folders; then delete tags with no live
  `snippet_tags` rows. Runs at startup and on a 24h ticker.
- **Crypto** (`crypto.go`): key = 32 bytes `crypto/rand` at
  `state_dir/key` (create 0600 if absent, refuse to run if the file has
  the wrong size). `Seal(id, plaintext) → nonce(12) || GCM(id, nonce, pt)`
  with AAD = `id`; `Open` is the inverse; AAD mismatch surfaces as a
  distinct error. Toggling `is_sensitive` re-encrypts/decrypts and rewrites
  the FTS row.
- **Folders** (`folders.go`): create/rename enforce unique name per parent
  (`ErrNameTaken`); move walks the `parent_id` chain from the destination
  up — if it reaches the folder being moved, `ErrFolderCycle`; delete
  refuses live children or live snippets (`ErrFolderNotEmpty`).
- **Import** (`import.go`): document type mirrors the export JSON
  (spec §5). `merge`: upsert by id — existing ids keep `created_at`,
  missing ids get a ULID + `created_at = now`. `replace`: upsert everything
  in the document, then soft-delete live snippets/folders not in it, one
  transaction, snippets before folders. Duplicate ids inside one document
  → `ErrImport`. `folder_path` ("shell/deploy"): walk segments from root,
  reuse an existing live child with that name, create otherwise. Imported
  bodies are plaintext; if `is_sensitive` is set, re-encrypt under the
  final (possibly new) id so the AAD matches.
- **Export** (`export.go`): `{version: 1, exported_at, folders, snippets}`
  with all bodies decrypted (requires the key).

**Tests** (in-memory DB, `Clock` faked where time matters) — the full list
from spec §9:

- CRUD round trip; tag normalization (case/trim, charset rejection);
- FTS rowid mapping across create/update/delete, including external-content
  delete ordering;
- FTS matching, the quoted-phrase fallback, and the 400 path;
- `tag:`/`lang:`/`folder:` filters, mixed with FTS terms, AND semantics;
- sensitive bodies excluded from FTS (search cannot match them;
  title/notes/tags still can);
- encrypt/decrypt round trip + AAD-mismatch error;
- sync: since boundary with the fake clock (a row written just after the
  snapshot is not lost; duplicates harmless), full sync with empty since,
  tombstone shape;
- purge: 30-day hard delete, tag pruning, folder purge;
- folder delete refusal, move cycle refusal, sibling name collision;
- import merge and replace, `folder_path` creation, duplicate-id rejection,
  timestamp preservation, sensitive re-encryption on import.

**Done when**: all store tests green; `go vet` clean.

## Phase 3 — tsauth (`internal/tsauth`)

Goal: one interface the server can use in prod, dev, and tests.

- `Identity{ Login, DisplayName string }`
- `IdentityResolver` interface: `WhoIs(ctx, remoteAddr string) (Identity,
  error)`
- `tsauth.Tailscale`: wraps `tsnet.Node`.
  - `New(stateDir, hostname, authKey)`: `tsnet.NewNode` with state persisted
    under `stateDir/tsnet`; `authKey` from `TS_AUTHKEY` (first join only —
    afterwards the state file suffices).
  - `Listen(ctx)` → `node.Listen("https://:443")` (Tailscale cert, spec §1).
  - `WhoIs` delegates to `node.WhoIs`.
  - Node name collision (another `snp` in the tailnet) → clear startup
    error.
- `tsauth.Dev`: fixed `Identity{Login: "dev@local"}`; no tsnet.
- WhoIs failures (transient control-plane issues) return an error the
  server maps to 500, logged with detail.

**Tests**: `Dev` behavior; `Tailscale.WhoIs` error mapping (fake node).
Real tailnet behavior is covered by the Phase 4 smoke.

**Done when**: `snp serve` can start a tsnet node and serve `/` on the
tailnet (verified by hand — see Phase 4 smoke).

## Phase 4 — Server + API (`internal/server`)

Goal: the full HTTP surface from spec §5, tested with `httptest`.

- **Router**: stdlib `ServeMux` patterns, one handler per endpoint:
  `GET /api/me`, `GET/POST /api/snippets`, `GET/PUT/DELETE
  /api/snippets/{id}`, `GET /api/snippets/{id}/raw`, `GET/POST
  /api/folders`, `PUT/DELETE /api/folders/{id}`, `GET /api/tags`,
  `GET /api/sync`, `GET /api/export`, `POST /api/import`.
- **Middleware chain** (in order): recover → request logging → auth
  (`WhoIs` on `r.RemoteAddr`, compare to `owner`, 403 JSON on mismatch,
  identity into context) → content-type check (state-changing methods
  require `application/json`, else 415 — spec §3 CSRF rule) →
  `http.MaxBytesReader` 10 MiB (413).
- **Handlers** map 1:1 to store methods; error mapping:
  `ErrNotFound`→404, `ErrFolderNotEmpty`→409, `ErrNameTaken`→409,
  `ErrFolderCycle`→400, tag-charset/FTS/other validation→400,
  store/crypto failures→500 (detail logged only).
- `/raw`: `Content-Type: text/plain; charset=utf-8`, decrypted body
  verbatim (no added newline), 404 when deleted.
- `/api/import`: `?mode=merge|replace` (default merge).
- **Static**: `webembed.FS` served for everything non-`/api`; SPA fallback
  to `index.html` for unknown paths without a file extension.
- **`serve` wiring** (`cmd/snp`): load config → open store (migrate) →
  load/create key → tsnet listen (or dev listener on
  `127.0.0.1:8080` default) → start purger → `http.Serve` → graceful
  shutdown on SIGINT/SIGTERM (close listener, stop purger, close node and
  DB).

**Tests** (`httptest`, fake `IdentityResolver`, real in-memory store):

- Auth: accept owner, reject other login (403 JSON), whois-error → 500.
- 415 on `POST/PUT/DELETE` without JSON content type; 413 over 10 MiB.
- Every endpoint's happy path and validation errors (spec §9 list),
  including: snippet list filters + pagination defaults, FTS fallback 400,
  folder create/rename collision 409, folder move cycle 400, folder delete
  non-empty 409, sync shape + `server_time`, import merge/replace,
  export shape, `/raw` plaintext.

**Smoke (manual, `make dev`)**: `curl` the full endpoint set against
`--dev-listen`; then a real `snp serve` on the tailnet: `curl
https://snp.<tailnet>.ts.net/api/me` from a second tailnet machine.

**Done when**: handler tests green + both smoke passes.

## Phase 5 — CLI subcommands

Goal: spec §1 subcommands complete and usable from cron.

- `snp backup <dest>`: `VACUUM INTO '<dest>'`, then open the destination
  and run `PRAGMA quick_check`; non-zero exit on failure. (Full
  `integrity_check` available via a `--full-check` flag.)
- `snp export [-o file]`: default stdout; pretty-printed JSON.
- `snp import <file>`: reads the JSON document, applies with
  `--mode=merge|replace` (default merge).
- `snp key show-path`: prints the key file path.
- All subcommands load config the same way as `serve`; none of them touch
  tsnet.

**Done when**: backup→restore drill passes (restore the backup into a dev
DB, diff against the original); export→import round trip preserves data.

## Phase 6 — Frontend, online flows (`web/`)

Goal: the three-pane app, fully functional while online (spec §6).

- **Scaffold**: `npm create vite web -- --template svelte-ts`; Svelte 5
  (runes), TypeScript strict; Vitest + `jsdom`; `svelte-check` in the
  `check` script.
- **Structure**:

  ```
  web/src/
    lib/
      types.ts       API types (snippet, folder, tag, sync, export)
      api.ts         fetch wrapper; typed; throws on !ok
      query.ts       parse/serialize the spec §5 query syntax
      templates.ts   {{var}} / {{var|default}} parse + render
      db.ts          IndexedDB (idb): folders, snippets, meta{server_time}
      sync.ts        sync + idempotent merge (upsert, tombstones, server_time)
      search.ts      MiniSearch index, incremental updates on merge
      online.ts      navigator.onLine + fetch-failure tracking
    components/
      FolderTree.svelte  TagList.svelte  SearchBox.svelte
      ResultList.svelte  SnippetView.svelte  SnippetEditor.svelte
      TemplateDialog.svelte  OfflineBanner.svelte
    App.svelte       three-pane layout; breakpoint → stacked panes
  ```

- **Behaviors** (spec §6):
  - Search: 250 ms debounce; online → `/api/snippets?q=…`; offline →
    local index; same query syntax both ways.
  - View: title, language badge, tags, folder path, notes rendered with
    `marked` + `DOMPurify`, body in read-only CodeMirror with highlighting;
    language packs lazy-loaded via a `Record<string, () =>
    Promise<LanguageSupport>>` map.
  - Edit: CodeMirror body, textarea notes, language select, folder picker,
    tag input with suggestions from `/api/tags`; save = `PUT` (create =
    `POST`), `Cmd/Ctrl+Enter`; inline error keeps editor state on failure.
  - Copy: `c` or button; template variables → dialog listing each with its
    default; blank no-default variable → empty string; rendered text to
    clipboard.
  - Sensitive: masked body; reveal fetches `GET /api/snippets/{id}` and
    holds the body only in that view's state.
  - Keyboard: `/` focus search, `n` new, `Cmd/Ctrl+Enter` save, `Esc`
    cancel, `c` copy — all suppressed while typing in an input, textarea,
    or CodeMirror (`closest('.cm-editor')` / `isContentEditable` check).

- **Unit tests** (Vitest): `query.ts` parsing (filters, FTS remainder,
  empty-after-filters), `templates.ts` parse/render (defaults, blank,
  invalid names ignored), `sync.ts` merge (idempotent upsert, tombstones,
  `server_time` storage, sensitive bodies stay null), `search.ts` offline
  search against a fixture cache.

**Done when**: `make dev` smoke — create, search (FTS + filters), edit,
copy with templates, view sensitive reveal, keyboard shortcuts — all work;
unit tests green.

## Phase 7 — PWA + offline

Goal: installable, offline read, honest write-disabled state (spec §6).

- `vite-plugin-pwa`: manifest (name/short_name `snp`, 192 + 512 icons,
  maskable, `display: standalone`, `start_url: /`), `registerType:
  autoUpdate`, precache the built app shell, `navigateFallback:
  /index.html`, runtime caching: `/api/*` → `NetworkOnly` (never cached).
- Sync triggers: on mount, on `visibilitychange`/`focus`, and every 5
  minutes while open (spec §6 — the timer alone is not enough on mobile).
- Offline: list/search read from IndexedDB via the MiniSearch index;
  create/edit/delete disabled with `OfflineBanner`; nothing queued;
  sensitive reveal disabled with an "online required" hint.
- "Full resync" action (settings menu): clear IndexedDB cache and re-sync
  from scratch (spec §6 recovery path).
- After reconnect: one sync, then the UI refreshes from the local store.

**Manual verification**: install on macOS (Chrome/Safari), Android, and
iOS; airplane-mode the device → browse + search work, writes blocked,
sensitive reveal blocked; reconnect → changes from elsewhere appear.

**Done when**: all three platforms pass the manual checklist.

## Phase 8 — Deployment + ops

Goal: one-command install on a host, cron backup, honest README (spec §7).

- `deploy/snp.service`:

  ```ini
  [Unit]
  Description=snp snippet manager
  After=network-online.target

  [Service]
  User=snp
  Group=snp
  Environment=HOME=/var/lib/snp
  StateDirectory=snp
  WorkingDirectory=/var/lib/snp
  ExecStart=/usr/local/bin/snp serve
  Restart=on-failure
  ProtectSystem=strict
  ProtectHome=true
  PrivateTmp=true
  NoNewPrivileges=true

  [Install]
  WantedBy=multi-user.target
  ```

  `TS_AUTHKEY` via `EnvironmentFile=-/var/lib/snp/.config/snp/authkey`
  (the `-` makes it optional after first join; the file is kept, not
  deleted, so a state wipe can re-join without re-issuing steps).
- `deploy/install.sh`: create the `snp` user (home `/var/lib/snp`), copy
  the binary to `/usr/local/bin/snp`, write the starter config (hostname,
  owner, log level) to `/var/lib/snp/.config/snp/config.toml`, install and
  enable the unit.
- `deploy/backup.sh DEST_DIR` (cron-able, runs as `snp`):

  ```bash
  ts=$(date +%Y%m%d-%H%M%S)
  snp backup "$DEST_DIR/snp-$ts.db"     # VACUUM INTO + quick_check inside
  cp /var/lib/snp/.local/share/snp/key "$DEST_DIR/snp.key"
  find "$DEST_DIR" -name 'snp-*.db' -mtime +"$RETENTION_DAYS" -delete
  ```

  `RETENTION_DAYS` defaults to 7 (spec leaves it configurable).
- `README.md`: install, first run (authkey), the access URL
  (`https://snp.<tailnet>.ts.net` — DNS name, not 100.x IP), PWA install,
  backup/restore, and the plain-language warnings: the key file is
  required to read sensitive snippets from a backup; export files are as
  sensitive as db+key; deleting `state_dir/tsnet` requires the authkey
  again; `--dev-listen` is unauthenticated and local-only.

**Acceptance**: fresh install on the host via `install.sh`; reachable from a
second tailnet machine; cron backup runs and a backup restores cleanly;
README claims match observed behavior.

## Phase 9 — Hardening + release

- Walk the spec §8 error table endpoint by endpoint; confirm each status
  code and that 500s never leak detail.
- Log audit: no secrets, no bodies, no full query strings containing
  sensitive content.
- Full smoke pass of the Phase 4/6/7 checklists on the deployed instance.
- Tag `v0.1.0`.

## Phase 10 — UI refinement pass

A second review pass over the built UI (2026-09-11). Executed
highest-value-first: the cheap visible wins landed before the one task that
needed a schema change. Each task is one commit with `make test` green.

- **T3 — explicit copy actions** (`web/src/lib/CopyButton.svelte`,
  `web/src/lib/clipboard.ts`, `SnippetDetail`): a small *Copy template*
  beside the Template box (raw `{{var}}` text, for reusing the shape) and
  *Copy rendered* beside the Rendered box, with the footer Copy unchanged as
  the primary action. Every control briefly reports its own outcome —
  "Copied.", or "Copy failed" when the write is rejected — and the write
  prefers `navigator.clipboard` with a hidden-textarea fallback for
  webviews that lack it.
  *Done when*: each of the three controls writes the right text and the one
  clicked briefly reads "Copied."
- **T4 — distinct create labels** (`App`, `SnippetList`): "New folder" and
  "New snippet" replace the two buttons that were both labelled "New".
  *Done when*: no two controls share the label `New`.
- **T6 — simplified timestamps** (`web/src/lib/time.ts`, `App`,
  `SnippetList`, `SnippetDetail`): "Synced 2 minutes ago" in the header and
  "Updated Sep 11" in the detail footer, the compact list date unchanged,
  each carrying the precise local timestamp in a tooltip. The sync age is
  measured from when this client observed the sync, not from `server_time`.
  *Done when*: no raw RFC3339 string is user-visible and every simplified
  label has the exact value on hover.
- **T7 — search as the keyboard entry point** (`web/src/lib/keys.ts`,
  `SnippetList`, `SnippetDetail`, `App`): `Cmd/Ctrl+K` focuses the search box
  (with a platform-aware hint inside the field), `Up`/`Down` walk the visible
  results with wrapping, `Enter` copies the selection, `Escape` clears the
  query and then leaves the field. Arrows and Enter are scoped to the search
  box, so the editor keeps its own keys.
  *Done when*: `Cmd/Ctrl+K` → type → Down → Enter copies without the mouse,
  and typing in the editor is unaffected.
- **T5 — saved-default state** (`SnippetDetail`, `App`): Save defaults is
  disabled until the inputs differ from what is stored, and a write the
  server accepted confirms with a brief "Defaults saved" (a failure reads
  "Save failed" and keeps the same write on offer).
  *Done when*: the button is inert until there is something to save, and the
  confirmation only follows a successful write.
- **T8 — long-title discovery** (`SnippetList`, `settings.ts`, `App`):
  every list title carries its full text as a tooltip, plus a persisted
  "Two-line titles in the list" setting that wraps a long title instead of
  truncating it at one line.
  *Done when*: a truncated title is readable without opening the snippet,
  and the toggle survives a reload.
- **T1 — pinned flag, store and API** (`internal/store` migration 0004,
  `internal/server`): `pinned` follows the `uses_variables` pattern through
  create, replace, get, list/search, sync, export and import; plain,
  unencrypted, and not FTS-indexed.
  *Done when*: a pinned snippet reads back pinned from every read path, and
  an export/import round trip preserves it.
- **T2 — Favorites UI** (`web/src/lib/Favorites.svelte`, `SnippetDetail`,
  `SnippetForm`, `App`): a pin control in the detail header, a Favorites list
  above the folder tree, and one `replaceSnippet()` helper behind both the
  pin toggle and the defaults save so a full-replace PUT cannot drop the
  other field.
  *Done when*: pinning shows the snippet under Favorites and that survives a
  sync and a defaults save, and editing a snippet does not unpin it.
- **T9 — docs**: this phase, spec §4/§5/§6, and the work log.
  *Done when*: spec and code agree on `pinned` and on the keyboard map.

## Phase 11 — Compact layout (phone / narrow window)

Status (2026-09-12): T1–T6 landed on `claude/eloquent-maxwell-bcugjn`,
`make test` green (324 Vitest); T7's docs are done and the flow was
walked in headless Chromium at 400px and across the breakpoint (see the
work log), but the on-device part of the checklist — iOS Safari tab and
installed PWA, Android hardware back at each depth, the wails app with
Layout = Compact — has not been run and stays open.

Spec §6 "Compact layout" (2026-09-12). One screen at a time below a
width breakpoint or on request: the list is the root, a snippet pushes a
detail screen with Back, and the left pane becomes a drawer. Replaces the
"responsive pane stacking" placeholder from the 2026-09-06 revision note.
All frontend; no store, API, or Go changes. Each task is one commit with
`make test` green; the order below is dependency order, and T1–T2 are
pure TypeScript modules with tests before any markup moves.

Decisions fixed up front (spec §6), so they are not re-litigated per task:

- Breakpoint **720px**: `(max-width: 719px)`. Chosen because
  `FOLDERS_MAX + LIST_MIN` in `panes.ts` is 700px, below which the wide
  grid is already clamping folders first; a phone in landscape (≈ 670–850
  CSS px) mostly lands in compact, a tablet mostly in wide.
- Layout setting **Auto / Wide / Compact**, default Auto, manual values
  ignore the viewport. Persisted like the theme.
- Compact is a **screen stack with history entries**, not CSS stacking of
  the three panes. Wide mode pushes no history.
- Back in the editor **is Cancel**; the dirty-editor guard is a separate
  follow-on for both layouts.
- Folder choice closes the drawer; tag toggles do not.

- **T1 — layout setting** (`web/src/lib/settings.ts`, `settings.test.ts`,
  `web/src/main.ts`, `App` settings panel):
  - `LAYOUTS: LayoutOption[]` = `auto` "Auto (by window width)", `wide`
    "Wide (three panes)", `compact` "Compact (one screen)";
    `LAYOUT_STORAGE_KEY = 'snp.layout'`; `AppearanceSettings.layout`;
    `loadSettings()` reads it (unknown value → `auto`); `saveLayout()`
    removes the key for `auto`, mirroring `saveTheme`.
  - A *Layout* `<select aria-label="Layout">` directly under *Theme* in the
    settings panel, bound like `theme` (`$effect` → `saveLayout`).
  - `main.ts`: nothing to apply before mount — the resolved mode is App
    state (T2), not a document attribute — but `loadSettings()` already
    runs there and now carries `layout` through.
  *Done when*: the select round-trips through localStorage, an unknown
  stored value falls back to Auto, and Auto stores no key.
- **T2 — layout resolution and the screen stack** (`web/src/lib/layout.ts`,
  `layout.test.ts`; new module, no Svelte import so it tests without the
  DOM):
  - `COMPACT_MAX_WIDTH = 719`, `COMPACT_MEDIA = '(max-width: 719px)'`.
  - `resolveLayout(setting: Layout, narrow: boolean): 'wide' | 'compact'`
    — `wide`/`compact` return themselves; `auto` returns `compact` iff
    `narrow`.
  - `watchNarrow(cb: (narrow: boolean) => void): () => void` — wraps
    `window.matchMedia(COMPACT_MEDIA)`, calls `cb` once with the current
    value and again on every `change`, returns an unsubscribe. When
    `matchMedia` is missing (jsdom, the App tests) it calls `cb(false)` and
    returns a no-op, so the wide layout is the test default and compact is
    opted into by forcing the setting.
  - `createCompactNav(history: NavHistory)` — the stack. `NavHistory` is
    the two-method slice of `window.history` the module needs
    (`pushState(state, '', url?)`, `back()`) plus a `popstate` subscription,
    so tests inject a fake and the wails webview gets the real one.
    Reactive state (`$state` is a Svelte rune, so the module exposes plain
    getters and the App mirrors them; or the module is `.svelte.ts` — pick
    `.svelte.ts` if it keeps App simpler, the tests still run under
    Vitest): `screen: 'list' | 'detail'`, `drawerOpen: boolean`,
    `depth: number`.
    Operations: `openDetail()` (no-op if already on detail), `openDrawer()`,
    `closeDrawer()`, `back()`, `enter(hasDetail: boolean)` (called on the
    switch to compact: resets, and pushes detail when `hasDetail`),
    `toRoot()` (synchronously zeroes the state and calls
    `history.go(-depth)` once, so the later popstate finds `depth` already
    0 and is ignored — the keyboard search shortcut uses this),
    `leave()` (switch to wide: resets state; does *not* call
    `history.back()` `depth` times — the leftover entries are tagged and the
    popstate handler ignores them once `depth` is 0).
    Every push does `history.pushState({ snp: depth+1 }, '')`; `back()` and
    the close operations call `history.back()` and let the `popstate`
    handler do the state change, so the in-app control and the OS gesture
    are the same code path. The handler ignores a popstate whose
    `event.state?.snp` is not `depth - 1` (a foreign entry, or a stale one
    from a previous page load) and otherwise pops one level: drawer open →
    closed, else detail → list. A push while the drawer is open closes
    the drawer first (so depth is never > 2).
  *Done when*: unit tests cover `resolveLayout` (six cases), `watchNarrow`
  with and without `matchMedia`, and the stack: detail push/back, drawer
  open/close, gesture pop via a fake popstate, a foreign popstate ignored,
  `enter(true)` landing on detail with depth 1, `leave()` zeroing without
  calling `back()`, `toRoot()` from depth 2 calling `go(-2)` once, and no double-push on a repeated `openDetail()`.
- **T3 — compact grid and drawer CSS** (`web/src/app.css`, `App.svelte`
  root class only):
  - App resolves `mode = resolveLayout(layout, narrow)` and sets
    `class:compact={mode === 'compact'}` on `.app`. Every compact rule is
    scoped under `.app.compact` — **not** a media query — so the manual
    override and Auto share one stylesheet path and the tests can force
    compact by class.
  - `.app.compact .panes`: `grid-template-columns: minmax(0, 1fr)`; the
    inline `--folders-w` / `--list-w` are not emitted in compact (App passes
    `null` to `paneStyleVars`) and the two splitters are not rendered
    (`{#if mode === 'wide'}` around each `{@render paneSplitter(...)}`).
  - `.app.compact .pane.folders` becomes the drawer: `position: fixed`
    (top: the top bar's bottom, so the bar stays tappable), left 0, height
    to the viewport bottom, `width: min(85vw, 320px)`, `translateX(-100%)`
    unless `.open`, `transition: transform 160ms` honoring
    `prefers-reduced-motion`, `z-index` above the panes; a `.scrim`
    `<button aria-label="Close folders">` behind it covering the rest of the
    viewport.
  - `.app.compact .pane.list` and `.pane.detail` each fill the single grid
    cell; the inactive one carries the `hidden` attribute (app.css has no
    `[hidden]` rule today, and `.pane` sets `overflow: auto` with no
    `display`, so the UA default applies; add an explicit
    `.app.compact .pane[hidden] { display: none }` anyway so a future
    `display: flex` on `.pane` cannot un-hide it) so the
    list stays mounted behind detail.
  - Touch sizing under `.app.compact`: list rows `min-height: 44px`; the
    top-bar buttons and `.box-head` copy buttons `min-height: 40px`;
    `.settings .panel` becomes `position: fixed; left: 0; right: 0;
    width: auto; top: <bar height>; border-radius: 0 0 8px 8px`.
  *Done when*: with the class forced in the browser (devtools) at 400px,
  the list fills the width, the folders pane is off-screen, and no
  horizontal scrollbar appears; at 1200px without the class nothing has
  changed (pixel-compare the three-pane screenshot before/after).
- **T4 — compact top bar** (`App.svelte`, `app.css`):
  - Left of the wordmark, only in compact: a Back button
    (`aria-label="Back"`, `←`) when `screen === 'detail'` or the drawer is
    open, else the drawer toggle (`aria-label="Folders"`, `☰`,
    `aria-expanded`).
  - In compact the version chip, connection pill, `Synced …` age and
    *Resync* button are rendered inside the settings panel instead of the
    bar (one `{#if mode === 'compact'}` block in the panel, the bar's
    copies wrapped in `{#if mode === 'wide'}`); `.error` and `.notice`
    stay in the bar since they are transient and the bar has the room once
    the chips are gone. The gear is rendered in both modes.
  - The list toolbar gets a *Folders* button beside *New snippet* in
    compact only, opening the drawer — the ☰ alone is easy to miss on a
    first visit.
  *Done when*: at 400px the bar holds exactly ☰/Back, `snp`, gear (plus a
  transient notice), and every relocated control still works from the
  panel.
- **T5 — navigation wiring** (`App.svelte`):
  - `nav = createCompactNav(window.history-backed adapter)`, created once;
    the `popstate` listener is attached in an `$effect` with cleanup.
  - Mode switch `$effect`: on `wide → compact` call
    `nav.enter(editing || selectedSnippetId !== null)`; on `compact → wide`
    call `nav.leave()`.
  - `selectSnippet()` → after setting the selection, `if compact
    nav.openDetail()`. `startCreate()` and `startEdit()` likewise.
    `cancelEdit()` from a create (no `editingSnippet`) → `nav.back()` in
    compact; from an edit it stays on detail. A successful create selects
    the new snippet, which already lands on detail.
  - The `back()` pop, when it lands on the list while `editing`, calls
    `cancelEdit()` (Back is Cancel, spec §6).
  - Folder select (`FolderTree onselect`, and the *All* affordance) →
    `nav.closeDrawer()` in compact; `toggleTag` unchanged.
  - Keyboard (`onGlobalKeydown`): before the search-field branch, in
    compact: `Escape` with the drawer open → `nav.back()`; `Escape` on
    detail when `!editing` and the active element is not an
    input/textarea/contenteditable → `nav.back()`. `isSearchShortcut` →
    `nav.toRoot()` (one synchronous state change plus one `history.go`,
    rather than chained `back()` calls that would each wait on popstate),
    then `focusSearch()` after a `tick()` so the list is visible when it
    focuses.
  - Arrow keys in the search box move the selection without
    `openDetail()`: route them through a `setSelection(id)` that
    `selectSnippet` and `moveSelection` share, and only `selectSnippet`
    (click/tap) pushes.
  - `SnippetList`: the scroll-into-view `$effect` also re-runs when the
    list becomes visible again (add `hidden` to its dependencies via a
    prop, or key the effect on a `visible` prop), so returning from detail
    restores the place.
  *Done when*: tap → detail → Back (button, Escape, and a synthetic
  `popstate`) each return to the list with the row still selected and in
  view; New → Cancel returns to the list; Edit → Cancel returns to
  detail; `Cmd/Ctrl+K` from detail shows the list with the search box
  focused; the arrow keys never leave the list.
- **T6 — App tests** (`web/src/App.test.ts`): the existing harness mounts
  App against a fake `api` and IndexedDB; add a compact describe block that
  seeds `localStorage['snp.layout'] = 'compact'` (jsdom has no
  `matchMedia`, so Auto would resolve wide) and a fake `history` on
  `window` with a recorded stack and a `dispatchEvent(new
  PopStateEvent('popstate', { state }))` helper. Scenarios: the T5 done-when
  list, the drawer opening from ☰ and from *Folders*, folder click closing
  it, tag click not closing it, the settings panel holding *Resync* in
  compact, and — the regression guard — that in wide mode `history.pushState`
  is never called and both splitters render.
  *Done when*: the scenarios pass and the wide-mode assertions run in the
  existing describe blocks unchanged (no existing test edited except to
  share the `history` fake).
- **T7 — manual verification and docs** (`README.md`, spec §6 status line,
  this phase, work log): phone checklist run on iOS Safari (as a tab and
  as the installed PWA — standalone mode has no browser back button, so
  the swipe gesture and the in-app Back are the only ways out), Android
  Chrome (hardware back at each depth: drawer, detail, then leaves the
  app), a desktop browser window dragged across 720px both ways with a
  snippet open, and the wails app with Layout = Compact. Confirm the
  offline banner, reveal, copy and the variables panel behave identically
  in compact. README gets a short "Phone layout" paragraph under the PWA
  section naming the setting.
  *Done when*: the checklist is recorded in the work log with the device
  list, spec §6 "Compact layout" no longer says "not yet built", and the
  revision note is updated.

Follow-ons named here, deliberately out of scope: the dirty-editor
"discard changes?" guard (both layouts; sits in front of Cancel);
swipe-to-go-back on the detail screen (the OS gesture covers it on both
phone platforms); remembering the last screen across a reload.

## Test plan (summary)

| Layer | Where | Coverage |
|---|---|---|
| Store (in-memory SQLite, fake clock) | `internal/store/*_test.go` | spec §9 store list, incl. FTS rowid mapping, sync boundary, purge, folder invariants, import modes |
| Handlers (`httptest`, fake resolver) | `internal/server/*_test.go` | auth accept/reject, 415/413, every endpoint happy + validation paths |
| Frontend (Vitest) | `web/src/lib/*` | query parsing, template parse/render, sync merge, offline search, timestamp formatting, clipboard writes, keyboard shortcuts, favorites, layout resolution + compact screen stack (fake history) |
| Manual | `make dev` + tailnet + 3 platforms | Phase 4 smoke, Phase 7 PWA checklist, Phase 8 acceptance |

No browser end-to-end tests in v1 (spec §9).

## Milestones

| M | Phases | Acceptance |
|---|---|---|
| M0 | 0 | `make build` + `make test` green on fresh clone |
| M1 | 1 | config precedence tests green |
| M2 | 2 | full store test list green |
| M3 | 3–5 | tailnet smoke + dev smoke pass; CLI backup/export/import round trip verified |
| M4 | 6 | online flows smoke pass in dev |
| M5 | 7 | PWA installed on macOS/Android/iOS; offline checklist passes |
| M6 | 8 | deployed on a host; second-machine access; cron backup + restore drill |
| M7 | 9 | error table verified; `v0.1.0` tagged |
| M8 | 10 | refinement pass smoke: copy actions, favorites, keyboard search workflow |
| M9 | 11 | compact layout: phone checklist (iOS PWA, Android back at each depth), narrow desktop window across the breakpoint, desktop app forced Compact |

## Risks and mitigations

- **tsnet cert/DNS surprises** (cert only valid for tailnet DNS names,
  node-name collisions, authkey semantics) — spike this *first* in Phase 3
  with a hello handler before building on top; README documents the DNS
  name requirement.
- **modernc.org/sqlite FTS5 quirks** (external-content delete ordering,
  rebuild) — covered by dedicated store tests; `rebuild` statement
  documented as the recovery path.
- **Sync boundary regressions** — the fake-clock store test pins the
  `server_time`-before-read rule; any refactor that moves the capture
  fails the test.
- **Bundle size from CodeMirror language packs** — lazy per-language
  dynamic imports; only the active language is loaded.
- **iOS PWA limitations** (no background sync, app suspension) —
  visibility/focus-triggered sync; documented in the README.
- **History/popstate divergence in compact mode** (a foreign entry, a
  stale entry from a previous load, the iOS standalone PWA's swipe-back)
  — every entry we push is tagged with its depth and the popstate handler
  ignores anything else; the stack is never persisted, so a reload always
  starts at the root; covered by the fake-history unit tests in Phase 11.
- **Single-machine SPOF** (host down = no snippets) — out of scope by
  design; mitigated by cron backups + offsite copy (operator's job,
  README).

## Open items (resolve during execution)

- Confirm the exact module path / repo location for
  `github.com/jstevewhite/snp`.
- Verify the `owner` login string against a live `tailscale whois` on
  first run (spec example: `jstevewhite@github`).
- PWA icon set: ship a generated placeholder at M5, real icons as a
  follow-up.
- Default backup retention: 7 days assumed; confirm with the operator.
