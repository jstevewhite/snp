# snp — Implementation Plan

Date: 2026-09-02 · Last updated: 2026-09-28
Status: Phases 0–11 complete and merged; follow-on session work through
`snp pick` (2026-09-28) complete on `main`. `main` is 8 commits past the
`v0.5.0` tag — the doctor and picker work is unreleased. Open work is listed
under "Status and roadmap" below.
Spec: `docs/snp-design.md` (this plan implements that document; section refs
like "spec §4" point there)

## Status and roadmap (2026-09-28)

Every numbered phase (0–11) is complete and merged to `main`, together with the
follow-on session work. `main` is at `v0.5.0-8-gd14e82f` — 8 commits past the
`v0.5.0` tag, and the last commits (the doctor work and the shell picker) are
**unreleased**. History since `v0.5.0`: icon updates, the doctor plan note,
`snp doctor` (store, endpoints, dialog), a work-log refresh, `snp pick`, and the
`web/dist` stub fix.

### Shipped

| Area | Landed | Notes |
|---|---|---|
| Phases 0–11 | through 2026-09-12 | core app, PWA/offline, deploy + ops, UI refinement pass, compact layout (T1–T6; T7 verification open) |
| Recovery — trash + revision history | 2026-09-21 | 30-day trash, 50 revisions per snippet, online-only dialogs |
| Data management — JSON + full backup | 2026-09-21 | import / export / backup from Settings and the palette |
| Password-protected data files | 2026-09-21 | age passphrase encryption; offline `snp decrypt` |
| Duplicate snippet | 2026-09-21 | guarded create draft; sensitivity preserved |
| `snp doctor` | 2026-09-25 | merged `cfc2d87`; store, `/api/doctor[/repair]`, health dialog |
| `snp pick` shell client | 2026-09-28 | merged `4bb6f33`; picker + `snp widget` zsh binding |
| `web/dist` stub build fix | 2026-09-28 | merged `d14e82f`; `restore-dist-stub` runs after the compile |

### Next (priority order)

1. **Phase 11 T7 — on-device compact-layout checklist** (verification only; the
   code is merged). iOS Safari as a tab and as the installed PWA (swipe-back at
   each depth); Android Chrome hardware back (drawer → detail → leaves the app);
   the wails app with Settings → Layout = Compact.
2. **Cut a release** covering `snp doctor` and `snp pick` — `main` is 8 commits
   past `v0.5.0`. Use the existing tag-triggered multi-platform workflow.
3. **CLI snippet editor — `snp add` / `snp edit`** (in progress — E1/E2 done
   2026-09-29; see below): a Bubble Tea
   panel that mirrors the GUI editor — the same fields, the same validation, the
   same template / `var_defaults` rules. The API (`POST` and `PUT
   /api/snippets`) and the picker's library plumbing already exist; the new work
   is a write path and the form.
4. **Linux desktop container build + verification** — podman with
   `libgtk-3-dev` + `libwebkit2gtk-4.1-dev` + Go; `make web`, then the desktop
   build; exercise `install-desktop.sh` with a scratch `PREFIX=`.
5. **Windows desktop port.**
6. **Desktop follow-ons** — real app icon; surface startup errors in the window.
7. **`snp doctor` deferred work** (its "Deferred" bullet below): `--deep`, the
   key-requiring `revisions` check that turns a wrong or replaced key file into a
   specific diagnosis instead of a generic 500; point a search that fails with
   index corruption at the health check; offer orphan repair in the UI, today
   CLI-only behind `--fix-orphans`.

### Backlog / later (v1 follow-ons)

- **SnippetsLab converter** — the last named follow-on from spec §11; the CLI
  client is done, as `snp pick`.
- **Named variable presets per machine** — the follow-on named in Phase 10 T5.
- **Real PWA icon set** — a generated placeholder has shipped since M5.
- **bash / fish picker integration** — `snp pick` works in any terminal, but
  `snp widget` and the inline binding are zsh-only; the other shells are a
  later follow-on.

### Release history

- Latest tag: **`v0.5.0`**. Signing/notarization and the per-platform release
  workflows are in place; releases are tag-triggered.
- `v0.2.0-beta.4` tagged the Phase 11 branch, now merged to `main`.
- No tag yet covers the doctor or picker work.

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
- Back in the editor **is Cancel**; the dirty-editor guard now protects
  both layouts (frontend remediation transfer, 2026-09-17).
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
    value and again whenever it flips — re-read on the `change` event, on
    every window `resize` and on every root-element `ResizeObserver`
    notification, because the wails webview did not deliver `change` on
    a native window resize — and returns an unsubscribe. When
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

## Frontend remediation transfer — 2026-09-17

One integration task: port the reviewed fixes from the older `qwen/snp`
checkout while retaining Favorites, compact history, pane resizing, AI output
kind/Explain Undo, clipboard fallback, prefix-AND search and Linux desktop.

- Sensitive body/default state, async reveal invalidation and cache redaction.
- Dirty-editor, modal focus, native-close and deferred PWA-update guards.
- Serialized/offline saves with inline errors and sync ordering.
- Reactive search results, null-folder preservation and full folder labels.
- Correct the desktop embedded shell path and cover it on darwin/linux.

Done when: transferred regressions and the existing suite pass `make test`,
web/desktop builds succeed, and compact navigation still preserves drafts.

## Recovery — Trash and revision history (2026-09-21)

- Expose the existing 30-day snippet trash and restore into a live folder
  or Unfiled; preserve FTS and emit restored snippets through sync.
- Save the previous content on meaningful edits and import overwrites,
  retaining 50 revisions per snippet. Encrypt sensitive history, including
  earlier versions when sensitivity is enabled. Restore atomically while
  preserving the current version and favorite flag.
- Add online-only Trash and History dialogs with preview, comparison,
  explicit sensitive reveal, restore, and a delete Undo action.
- Done when store/API and UI regressions pass make test, production build
  succeeds, and recovery is smoke-tested without caching historical bodies.

## Data management — snp JSON and full backup (2026-09-21)

- Add Settings/command-palette access to JSON import, JSON export and a
  complete database/key backup. Native desktop uses file dialogs.
- Preview imports through the same validation/transaction path with rollback;
  merge is default, replace explicitly previews snippets moving to Trash.
- Import writes use current updated_at for sync; retain valid created_at.
  Export reads a consistent snapshot. Backup includes history, trash, key,
  and restore instructions; restoration is performed with the app stopped.
- Done when API/store, desktop file operations and UI regressions pass
  make test; production builds and local browser smoke pass.

## Password-protected data files (2026-09-21)

- Default app exports/backups to standard age passphrase encryption over the
  whole JSON/ZIP. Confirm passwords and show explicit forgotten-password and
  plaintext warnings; never persist passwords or unlocked import content.
- Unlock encrypted JSON before the existing preview/apply flow. Provide
  offline backup/JSON recovery through a private `snp decrypt` prompt with
  authenticated, private, non-overwriting output.
- Bound password KDF work/concurrency and decrypted import size; preserve
  owner/CSRF guards and plaintext API/CLI compatibility.
- Done when full checks/builds pass, wrong-password/damaged-file recovery
  leaves no published output, native protected saves work, and UI password
  confirmation, clearing and encrypted import have regression coverage.


## Duplicate snippet (2026-09-21)

- Add toolbar/palette duplication into a guarded create draft with copied
  content and metadata, preserving sensitivity and original template text.
- Keep creation independent of the source ID/history and invalidate stale
  sensitive fetches; clear discarded seeds and preserve cache redaction.
- Done when source-preservation, create/cancel, offline and sensitive-copy
  regressions pass make test and production UI build/smoke pass.

## Health check and index repair — `snp doctor` (2026-09-25)

**Status: complete** — merged to `main` (`cfc2d87`, 2026-09-25). D1–D3
shipped; the deferred checks below are tracked in "Status and roadmap".

One command, flags for the sub-features: `doctor` is the only interface,
`--repair` fixes, `--only=<check>` narrows the run, `--reindex` is shorthand
for `--repair --only=fts`, `--deep` adds the key-requiring checks, and
`--json` / `--strict` serve scripting and cron. Build it as three slices: D1 is
useful on its own over SSH, and D2/D3 are what make it usable from a phone,
which is the moment index corruption actually bites — search is broken and
shell access may not be at hand.

- D1 — `internal/store/doctor.go`: a `DoctorReport` carrying `ok`/`warn`/`error`
  per check, one function per check, and the repair primitives
  (`clearSensitiveMirrorsTx`, `resyncTagMirrorsTx`, `rebuildFTSTx`). Checks:
  `sqlite` (`quick_check`, or `integrity_check` under `--full`), `fts` (FTS5's
  `integrity-check`, which covers structure only, plus a bounded
  column-restricted content probe — see the note below), `fts_count` (the
  FTS5 `docsize` shadow table against `snippets`), `tags`, `sensitive`,
  `orphans`, `timestamps`, `schema`, `key`. Repair order is
  clear-sensitive-mirrors → resync-tag-mirror → rebuild FTS, because `rebuild`
  reads the content table and would otherwise re-index a leak; a rebuild
  normalizes the mirrors first, and any mirror change forces a rebuild.
  `--fix-orphans` nulls a live row's dead folder/parent reference and is never
  part of `--repair`. The CLI registers config flags, takes no positionals,
  opens without the key, and exits 0 (healthy), 1 (an `error`, or any `warn`
  under `--strict`), or 2 (usage, IO or open failure).
- Note from building D1, verified against the real database and now recorded
  in `AGENTS.md`/`CLAUDE.md`. Three assumptions did not survive contact:
  `COUNT(*)` against `snippets_fts` reads the *content* table because it is
  external-content, so it can never disagree with `snippets` — the index's own
  row count has to come from the `docsize` shadow table; FTS5's
  `integrity-check` passes both a deleted index row and content rewritten
  underneath the index, so it covers structure only and the content probe is
  what detects drift; and clearing a leaked sensitive mirror without rebuilding
  leaves the leaked term searchable, so a mirror change must force a rebuild.
  The probe takes one token per column and restricts the MATCH to that column,
  because an unrestricted match is satisfied by an occurrence in any column and
  masks the stale one.
- D2 — `internal/server/doctor.go`: `GET /api/doctor` (read-only report) and
  `POST /api/doctor/repair`, registered in `internal/server/handlers.go`, with
  a new `doctorMu` on `Server` alongside `backupMu`/`cryptoMu` (`TryLock` →
  409). Owner-guarded, no-store, JSON content type via the existing guard — no
  change to the CSRF rule.
- D3 — `web/src/lib/DoctorDialog.svelte` plus `api.ts` types and tests. Opened
  from Settings ("Check library health") and the command palette ("Run health
  check"), greyed with the palette's existing offline reason. Runs the check on
  open, conveys status as text as well as colour, names the changes before
  applying them, re-checks afterwards to show before/after, and offers the
  derived repairs only. Reuse `modal.ts` focus behaviour, `data-modal-initial`
  on Close, `role=status`/`role=alert`, and the inert-background pattern.
- Deferred — `--deep`, the `revisions` check (protected payloads decrypt, at
  most 50 versions per snippet). It is the only check that needs the
  encryption key and the slowest, and it is additive: D1–D3 are unaffected by
  leaving it out. Its value is diagnosing a wrong or replaced key file, which
  currently surfaces as a generic 500; add it as its own slice once the rest
  has seen use.
- Docs — `AGENTS.md` and `CLAUDE.md` both present
  `INSERT INTO snippets_fts(snippets_fts) VALUES('rebuild')` as the recovery
  path with no command behind it (`AGENTS.md:107`, `CLAUDE.md:144`); both must
  name `snp doctor --repair` instead, in the same change, per the rule that a
  change to one is mirrored in the other. The README gains a troubleshooting
  entry; the spec gains "Health check and index repair (`snp doctor`)".

- Done when: a store with a deleted FTS row and a store with stale indexed
  content (row counts equal, title changed underneath) are each detected — the
  second proving `fts_count` alone cannot certify the index; a leaked sensitive
  `body_text` is detected and its term is not searchable after repair (the
  ordering test); a non-conforming timestamp and a newer `schema_version` are
  reported and never rewritten; doctor completes with no key set; `snp doctor`
  returns 0/1/2 as specified; the health dialog repairs from the UI and shows
  the before/after; `make test` is green and `web/dist/index.html` is unchanged.

## Shell picker — `snp pick` (2026-09-28)

**Status: complete** — merged to `main` (`4bb6f33`, 2026-09-28). This is the v1
"CLI client" follow-on (spec §11); the SnippetsLab converter is the only named
follow-on still open. Spec: `docs/snp-design.md`, "Shell picker (`snp pick`)".

A terminal snippet picker plus the zsh binding that leaves the chosen command on
the prompt. The binary writes only the accepted command to stdout and draws the
screen on `/dev/tty`, so `out=$(snp pick)` still shows the UI and the widget can
capture the command.

- P1 — `internal/pick`: Bubble Tea v2 + Lip Gloss UI — a list with filter, the
  variable form, `library.go` source resolution, `run.go`, `style.go`,
  `view.go`. Pure Go, so `cmd/snp` still cross-compiles without cgo.
- P2 — `internal/template`: the `{{var}}` / `{{var|default}}` grammar, shared
  with `web/src/lib/templates.ts`. Each box starts from the saved default, then
  the inline default; clearing a box leaves that spot empty.
- P3 — CLI + shell: `snp pick` / `snp widget`, `deploy/snp.zsh`, Ctrl-G
  inserting into `LBUFFER`, and a `print -z` wrapper so a typed `snp pick` lands
  on the next prompt. Sensitive commands are emitted with a leading space for
  `HIST_IGNORE_SPACE`.
- Library resolution: `--url` / `SNP_URL` / config `url`, else `state_dir/snp.db`;
  `--local` forces the file. `snp serve` ignores `url`, and a picker-only
  machine needs no `owner`. Local mode never creates a database or a key —
  `store.LoadKey` is the read-only loader split out of `LoadOrCreateKey`. Config
  gains the `url` key (flag / env / file), same precedence as the rest.
- Done when: `go test` green for template, pick, config, store, and `cmd/snp`;
  `go vet` clean on the picker; `GOOS=linux GOARCH=amd64 go build` of `cmd/snp`;
  a pty run of `snp pick --local` prints exactly the rendered command with empty
  stderr. `make test` was green on `main` at merge.

## CLI snippet editor — `snp add` / `snp edit` (2026-09-29)

**Status: in progress — E1 and E2 done (2026-09-29); E3–E6 remain.**
Decisions (2026-09-29): a separate
`internal/edit` package over a shared `internal/tui`; `snp add` takes prefill
flags (including comma-separated `--tags`); `snp edit` with no argument opens the
picker list to choose; local mode creates the key **lazily**; folder creation is
out of scope.

Goal: create and edit snippets from the terminal in a Bubble Tea panel that
mirrors the GUI editor. The GUI editor is already plain inputs (spec §6: "a
plain textarea for the body, a plain textarea for notes, a language text input,
a folder picker, a comma-separated tag input with a sensitive checkbox, and a
template checkbox that stays in sync with the body's `{{var}}` placeholders"),
so the form ports near 1:1. The only divergences are the read/render layer
(syntax highlighting, Markdown notes) and the mouse-driven chrome — neither of
which is the editor.

### Parity target (GUI → TUI)

| GUI (spec §6) | TUI panel | Fidelity |
|---|---|---|
| Title (create draft) | single-line input | full |
| Body | multi-line editor | full (the GUI editor is a plain textarea too; no highlighting either side) |
| Notes | multi-line editor | full (Markdown rendering lives in the GUI read view, not the editor) |
| Language | text input with the known-language list | full |
| Folder | keyboard picker over the live tree, nested `parent/child` labels | full |
| Tags | comma-separated input with suggestions from the tag list | full |
| Sensitive | toggle; masked until revealed; reveal fetches the decrypted body first | full |
| Template | derived from `template.HasVars(body)`, shown with the variable list (not hand-set) | full |
| Favorite / pin | toggle | full |
| Save (button, `Cmd/Ctrl+Enter`) | `Ctrl+S` / `Ctrl+Enter` | full |
| Cancel (`Esc`; Back = Cancel) | `Esc`, with the dirty guard | full |
| Failed write keeps the editor state | status line; the panel stays open | full |
| Read view: syntax highlighting, Markdown notes, 10-line cap + Show all | none | **diverges** — plain text |
| Three-pane mouse UI, palette, dialogs | keyboard only | **diverges** |

### Library surface (the delta)

`internal/pick`'s `Library` is read-only (`Search`, `Reveal`). The editor needs
reads for its pickers and a write path, and `Snippet` needs two fields it lacks.

```go
// Snippet gains FolderID and Pinned. Without them a full-replace PUT would
// silently un-file or un-favorite the row (the hazard the GUI's replaceSnippet
// helper guards against).
type Snippet struct {
	ID, Title, Body, Language, Notes string
	Tags          []string
	FolderID      *string
	Sensitive     bool
	UsesVariables bool
	Pinned        bool
	VarDefaults   map[string]string
}

// Input mirrors store.SnippetInput and the API's snippetReq.
type Input struct {
	Title, Body, Language, Notes string
	FolderID                     *string
	Tags                         []string
	IsSensitive, UsesVariables, Pinned bool
	VarDefaults                  map[string]string
}

type Folder struct {
	ID       string
	ParentID *string
	Name     string
}
type TagCount struct {
	Name  string
	Count int
}

// Editor is the read + write surface; Library stays the read-only one the
// picker uses.
type Editor interface {
	Library
	Folders(ctx context.Context) ([]Folder, error)
	Tags(ctx context.Context) ([]TagCount, error)
	Get(ctx context.Context, id string) (Snippet, error)
	Create(ctx context.Context, in Input) (Snippet, error)
	Update(ctx context.Context, id string, in Input) (Snippet, error)
}
```

- `Local` methods delegate to the held `*store.Store`: `ListFolders`,
  `ListTags`, `GetSnippet`, `CreateSnippet`, `ReplaceSnippet`.
- `HTTP` methods: `GET /api/folders`, `GET /api/tags`, `GET
  /api/snippets/{id}`, `POST /api/snippets`, `PUT /api/snippets/{id}`. `PUT` is
  a full replace, so the editor loads the row first and sends every field back.
- `fromOut` maps the two new fields; `Reveal` generalizes to `Get`, with the
  picker's `Reveal` kept as a thin alias.

### Slices

- **E1 — `internal/tui` (done 2026-09-29).** Extract from `internal/pick`: the palette
  (`theme` / `newTheme` / `styleInput` → `tui.Theme`, `tui.NewTheme(dark)`,
  `tui.StyleInput`) and the tty/program setup in `run.go` → `tui.Run(ctx,
  tea.Model) error` (`tea.OpenTTY`, `WithColorProfile(TrueColor)`,
  `WithContext`). `internal/pick` keeps its behavior by calling into `tui`; the
  existing picker tests must stay green. No user-visible change.
- **E2 — the write path** (`internal/pick`, done 2026-09-29): the `Editor` interface and the
  `Input` / `Folder` / `TagCount` types above, the `Local` and `HTTP`
  implementations, and the `Snippet` / `fromOut` additions. Tests: `Local`
  against a temp-file store; `HTTP` against `httptest` — create returns a row,
  update round-trips `folder_id` and `pinned`, and a validation error surfaces
  the same message the API gives.
- **E2a — review remediation** (2026-09-29, folded into E2 before commit): a
  `Local` validation-error test — `ErrInvalidTag` on create, `ErrNotFound` on
  update-by-unknown-id — pinning the store-error path the HTTP twin already
  covers and the panel will show inline; cross-reference comments on the two
  `snippetReq` mirrors (`internal/server/handlers.go` ↔
  `internal/pick/editor.go`) naming the full-replace drop hazard.
- **E3 — the editor panel** (`internal/edit`, new): one Bubble Tea model with
  the field set above. Tab / Shift-Tab between fields; the body is a multi-line
  editor; `internal/template` drives the template flag and the variable list
  (never hand-set); `var_defaults` is carried forward pruned to the variables
  the body still uses (the GUI save rule). A sensitive body is masked until
  revealed; a write that fails shows the mapped error inline and keeps the
  draft; `Esc` honors the dirty guard; `Ctrl+S` / `Ctrl+Enter` saves. Tests:
  model update handlers against a fake `Editor` (the picker's `model_test.go` is
  the pattern).
- **E4 — pickers**: the folder picker over `Folders()`, with `parent/child`
  labels built from `ParentID` (there is no `path` field); the tag input with
  suggestions from `Tags()`; the language list. All in `tui.Theme`, so the panel
  and the picker read as one tool.
- **E5 — the chooser for `snp edit`** (`internal/pick`): `Choose(ctx,
  lib) (Snippet, error)` runs the existing list model in a select-only mode (no
  template form, no stdout) and returns the highlighted row.
- **E6 — CLI and docs** (`cmd/snp`): `runAdd` and `runEdit`. Also deletes the
  `Reveal` alias if no caller remains once the panel and chooser use `Get`
  (`pick`'s model calls it today).
  - `snp add [--url URL | --local] [--title T] [--language L] [--folder PATH|ID]
    [--tags a,b] [--sensitive] [--pin]` opens the panel, prefilled. On save it
    exits 0 and prints nothing (like `pick`; a `--print-id` is a later add).
  - `snp edit [--url URL | --local] [id | query]` opens the panel on that row;
    with no argument it runs `Choose`. A query runs `Search`: one hit edits it,
    several open the chooser.
  - `openEditorLibrary`: like `openPickLibrary` but it may **create** the
    database, and it attaches the key **lazily** — `LoadOrCreateKey` runs only
    when a sensitive body must be read or written, so a plain create or a
    metadata-only edit never writes a key file. This is the one place the
    picker's "never create" rule does not carry over.
  - Docs: README ("From the shell" gains an add/edit part, noting the panel
    needs no shell widget and works in any shell), spec §6, and the AGENTS /
    CLAUDE subcommand lists.

**Done when**: `snp add` creates and `snp edit` updates a snippet from the
terminal in both `--url` and local modes, with every field above; a full-replace
update preserves `folder_id`, `pinned`, and `var_defaults`; a sensitive body
round-trips encrypted and is never written empty; the template flag and
`var_defaults` pruning match the GUI; validation errors (tag charset, folder
sibling/collision, unknown id) surface inline and never lose the draft; local
mode writes no key file unless a sensitive snippet needs one; `make test` green
and `go vet` clean.