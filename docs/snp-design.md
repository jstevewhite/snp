# snp — Snippet Manager Design

Date: 2026-09-02
Revised: 2026-09-04 (per-snippet `uses_variables` template flag, §4/§5/§6/§9)
Revised: 2026-09-06 (§6 reconciled with the implemented v1 frontend; §12 desktop app added)
Revised: 2026-09-08 (§13 AI snippet generation added)
Revised: 2026-09-08 (§6 left-pane tag filter implemented)
Revised: 2026-09-09 (§6 notes Markdown rendering; §13 explain emits Markdown)
Revised: 2026-09-09 (§6 read-view syntax highlighting)
Revised: 2026-09-10 (§3 SPA auth exception; §5 sync `>=` boundary; §2/§4/§9/§10 reconciled with the implementation)
Revised: 2026-09-11 (§13 Ask-AI output kind; §6 read-view long-body collapse; §12 Linux desktop build; §5 starter pack)
Revised: 2026-09-11 (§6 draggable pane dividers)
Revised: 2026-09-11 (§13 Explain replaces Notes instead of appending, with undo)
Revised: 2026-09-11 (§4/§5 `pinned`; §6 Favorites, explicit copy actions, the search keyboard workflow, simplified timestamps, two-line titles)
Revised: 2026-09-12 (§6 service-worker update check, so a stale precached shell cannot linger)
Revised: 2026-09-12 (§5/§6 search: prefix terms, terms ANDed, operators literal — online and offline now match the same set)
Revised: 2026-09-12 (§6 compact layout: the single-pane phone/narrow-window mode and the Layout setting — built, Phase 11; verified in headless Chromium, on-device checklist pending)
Status: approved design, revised after review, implemented

> Revision note (2026-09-06): §6 originally specified a CodeMirror 6
> editor/read view with lazy-loaded syntax highlighting, Markdown notes
> rendered through `marked` + DOMPurify, an API-backed online search,
> global keyboard shortcuts, a tags pane, and responsive pane stacking.
> The 2026-09-06 rewrite dropped CodeMirror and the Markdown path and
> shipped plain-text view/edit, escaped-plain notes, search against the
> locally synced index in both states, and a folder-only left pane. Later
> revisions (2026-09-09) restored Markdown notes (`marked` + DOMPurify)
> and lazy per-language highlighting (highlight.js) and added the
> left-pane tag list, so the section below again describes what is built.
> CodeMirror and the API-backed online search remain unbuilt; `marked`,
> `dompurify`, and `highlight.js` are dependencies. "Responsive pane
> stacking" was replaced on 2026-09-12 by the compact layout below
> (§6 "Compact layout"), a stacked navigation model rather than stacked
> panes; built in Phase 11 (`web/src/lib/layout.ts`). Global keyboard shortcuts arrived with the 2026-09-11
> refinement, scoped to the search workflow (§6 "Keyboard").

## Purpose

A personal snippet manager that stores anything from a one-liner with notes
to a whole script, is fast to search, and runs as one copy on a host that every
machine on the tailnet can use through a browser or an installed PWA.

## Decisions already made

| Topic | Decision |
|---|---|
| Language | Go |
| Tailnet access | tsnet embedded in the binary; the app is its own tailnet node |
| Auth | Tailscale identity via whois; single configured owner in v1 |
| Users | Single user in v1; schema leaves room for more |
| Deploy | systemd unit + binary; cron-able backup script |
| Frontend | Svelte 5 + Vite + TypeScript SPA, embedded via go:embed |
| Storage | SQLite (modernc.org/sqlite, no cgo) with FTS5 |
| Snippet shape | title, body, language, notes, folder, tags, timestamps, `uses_variables` flag; one body per snippet |
| Organization | nested folders plus free tags |
| History | none in v1 |
| Notes | Markdown — edited as plain text, rendered sanitized in the view |
| Templates | `{{var}}` / `{{var|default}}` placeholders, filled in at copy time |
| Search | FTS5 over title, notes, body, tags with `tag:` and `lang:` filters |
| Editor | read view highlights the body (highlight.js, by language); the editor stays a plain textarea |
| Offline | installable PWA; offline read of everything; writes online only |
| Sensitive snippets | flag + AES-256-GCM encryption at rest with a server-held key |
| Other clients | JSON API from day one; CLI is a follow-on |
| Import | JSON document matching the export format, plus a bundled starter pack (`snp seed`); SnippetsLab converter later |
| Sharing | none |
| Config | on-disk config lives under `~/.config/snp/` |

## 1. Architecture

One Go binary, `snp`. Under `snp serve` it:

1. Loads config.
2. Opens (or creates and migrates) the SQLite database.
3. Loads (or generates) the encryption key.
4. Starts a tsnet node named by config (default `snp`), obtains an HTTPS
   cert from Tailscale, and listens on 443.
5. Serves `/api/*` from Go handlers and everything else from the embedded
   SPA (with SPA fallback to `index.html`).

`snp serve --dev-listen` skips tsnet and auth and serves plain HTTP for
local development. It binds to `127.0.0.1` by default and must never be
exposed beyond the local machine — it performs no authentication at all.
The identity in dev mode is a fixed fake login.

Other subcommands:

- `snp backup <dest>` — consistent copy of the database via `VACUUM INTO`.
- `snp export [-o file]` — full JSON export (plaintext bodies).
- `snp import <file>` — import the same JSON shape.
- `snp seed` — add the bundled starter snippets (explicit; nothing seeds
  by itself).
- `snp key show-path` — prints the key file path, for backup scripts.

No reverse proxy, container, or external services.

Build note: the SPA is embedded with `go:embed`, so `web/dist` must exist
when the Go binary is built. The Makefile builds the web app first, and the
repo keeps a placeholder so a bare `go build` outside the Makefile does not
fail on a missing directory.

## 2. Configuration and paths

Config is TOML. Resolution order:

1. `--config <path>`
2. `$SNP_CONFIG`
3. `$XDG_CONFIG_HOME/snp/config.toml`
4. `~/.config/snp/config.toml`

Discoveries 3–4 are used only when the file exists; an explicit
`--config` or `$SNP_CONFIG` path that cannot be read is an error.

Every key has a flag and an `SNP_*` env override. Env beats file, flag beats
env.

```toml
hostname  = "snp"                      # tailnet node name
owner     = "jstevewhite@github"       # Tailscale login allowed in
state_dir = "~/.local/share/snp"       # tsnet state, db, key
log_level = "info"
```

Inside `state_dir`:

```
tsnet/        tailscale node state
snp.db        sqlite database (+ -wal, -shm)
key           32 random bytes, mode 0600, created on first run
```

`TS_AUTHKEY` in the environment is needed on first run only, to join the
tailnet.

For the systemd deployment the service runs as a dedicated `snp` user whose
home is `/var/lib/snp`, so the same defaults resolve to
`/var/lib/snp/.config/snp/config.toml` and `/var/lib/snp/.local/share/snp`.
The install script writes the config file there.

## 3. Auth

Every `/api/*` request passes through middleware that calls tsnet's
`WhoIs` on the remote address. The result's user login is compared to
`owner`. Mismatch returns 403 with a JSON error. Matching requests carry
the identity in the request context; handlers log it.

Only `/api/*` is authenticated. The embedded SPA — the app shell, its
assets, and the `index.html` fallback for client-side routes — is served
without auth so the shell loads for any tailnet peer; the owner rule
still protects every data endpoint, so an unauthorized peer loads the
shell and then gets 403 from its API calls. The identity check guards
data, not the app bundle.

Because identity is derived from the source address rather than from
credentials, CSRF is a live concern: a cross-origin request issued by a
malicious page in the owner's browser would pass `WhoIs`. Two rules close
this:

- Every state-changing endpoint (`POST`/`PUT`/`DELETE`) requires
  `Content-Type: application/json`; anything else is rejected with 415.
  A "simple" cross-origin POST cannot set that header without a CORS
  preflight, which the server never grants.
- The server sends no CORS headers at all; the SPA is same-origin.

`GET /api/me` returns `{ "login": ..., "display_name": ... }`.

`GET /api/version` returns `{ "version": ... }` — the build's stamped
version, or `"dev"` — which the SPA shows in the header beside the
wordmark. It is the only endpoint that reports build metadata, so a
client can tell which release it is talking to.

Multi-user later: add an `owner` column on snippets, replace `owner` in
config with an allowlist, and filter queries by owner. No structural change.

## 4. Data model

SQLite, migrations applied at startup from embedded SQL files with a
`schema_version` table. The database runs in WAL mode with a `busy_timeout`
set, so the live server and `snp backup`'s `VACUUM INTO` can contend
without spurious lock errors.

```sql
folders (
  id         TEXT PRIMARY KEY,          -- ULID
  parent_id  TEXT REFERENCES folders(id),
  name       TEXT NOT NULL,
  created_at TEXT NOT NULL,             -- RFC3339 UTC
  updated_at TEXT NOT NULL,
  deleted_at TEXT
)

snippets (
  rowid          INTEGER PRIMARY KEY,     -- surrogate; FTS rowid, not exposed
  id             TEXT NOT NULL UNIQUE,    -- ULID, the API identity
  title          TEXT NOT NULL,
  body           BLOB NOT NULL,           -- plaintext, or nonce||ciphertext when sensitive
  body_text      TEXT NOT NULL DEFAULT '',-- plaintext body, for FTS; '' when sensitive
  language       TEXT NOT NULL DEFAULT '',
  notes          TEXT NOT NULL DEFAULT '',
  tags           TEXT NOT NULL DEFAULT '',-- space-joined tag names, for FTS
  folder_id      TEXT REFERENCES folders(id),
  is_sensitive   INTEGER NOT NULL DEFAULT 0,
  uses_variables INTEGER NOT NULL DEFAULT 0,
  pinned         INTEGER NOT NULL DEFAULT 0,-- 1 = favorite; plain, not FTS-indexed
  var_defaults   TEXT NOT NULL DEFAULT '',-- JSON {"name": value} map; '' when sensitive
  var_defaults_enc BLOB,                   -- sealed map when sensitive; NULL when empty
  created_at     TEXT NOT NULL,
  updated_at     TEXT NOT NULL,
  deleted_at     TEXT
)

tags (
  id   INTEGER PRIMARY KEY,
  name TEXT NOT NULL UNIQUE             -- lowercase, trimmed
)

snippet_tags (
  snippet_id TEXT NOT NULL REFERENCES snippets(id),
  tag_id     INTEGER NOT NULL REFERENCES tags(id),
  PRIMARY KEY (snippet_id, tag_id)
)

snippets_fts USING fts5 (
  title, notes, body_text, tags,
  content='snippets', content_rowid='rowid',   -- external content; index only
  tokenize='unicode61'
)
```

Rules:

- `snippets.rowid` exists so FTS5 can key its rows to the main table:
  FTS5 ties rows together via INTEGER rowids, and the API identity is a
  TEXT ULID. External-content FTS5 stores only the index, not the content,
  so bodies are not duplicated on disk.
- `snippets.body_text` and `snippets.tags` are plaintext mirrors of the
  body and the space-joined tag names. They exist because external-content
  FTS5 reads *every* FTS column back from the main table on delete and on
  `rebuild`, and it cannot index the BLOB `body` column (which holds
  ciphertext for sensitive snippets). `body_text` is the plaintext body for
  non-sensitive snippets and `''` for sensitive ones; `tags` is the
  space-joined tag names. The authoritative tag list still lives in
  `snippet_tags`; the `tags` column is only for FTS.
- The store keeps the FTS index in lockstep with the main row, using the
  ordering that keeps external-content FTS5 consistent:
  - **Create:** the rowid is new and not yet in the index, so the store
    inserts the FTS row only (no delete).
  - **Replace:** the store deletes the FTS row *first* — while the main row
    still holds the old values, which is what the external-content delete
    reads — then updates the main row, then inserts the new FTS row.
  - **Remove / purge:** the FTS row is deleted *before* the main row, in
    the same transaction.
  Deleting an FTS rowid that was never inserted, or deleting it after the
  main row's values have already changed, corrupts the index and surfaces
  as `SQLITE_CORRUPT` ("database disk image is malformed"). If the index
  ever falls out of sync anyway,
  `INSERT INTO snippets_fts(snippets_fts) VALUES('rebuild')` reconstructs
  it from the main table.
- `snippets.var_defaults` holds the per-variable default map as JSON
  (`{"name": value}`) for non-sensitive rows and `''` for sensitive rows,
  where the same map is sealed under the snippet id into
  `snippets.var_defaults_enc` (NULL when a sensitive snippet has no
  defaults). Neither column is FTS-indexed: defaults are never searched.
- For sensitive snippets `body_text` is written `''`, so sensitive bodies
  are never indexed. Title, notes, and tags remain searchable.
- The FTS `tags` column holds the snippet's tag names joined by spaces.
  This is safe because tag names match `[a-z0-9][a-z0-9-]{0,63}` (no
  spaces, at most 64 characters);
  the store rejects anything else with 400. `tag:` filters are applied as
  SQL joins over `snippet_tags`, not through FTS.
- All `*_at` columns and all API timestamps are RFC3339 UTC with second
  precision, no fractional seconds, always `Z`
  (e.g. `2026-09-02T10:00:00Z`). The fixed format keeps lexicographic TEXT
  comparison — used for sync and ordering — correct.
- `deleted_at` is a soft delete. Deleted rows are excluded from all normal
  queries, returned by sync as tombstones, and hard-purged after 30 days by
  a job that runs at startup and daily.
- Tags with no live snippets are pruned by the same job.
- Folder invariants:
  - `name` is unique per parent (409 on violation at create or rename).
    This also keeps the import `folder_path` unambiguous.
  - Moving a folder under its own descendant is refused (the store walks
    the `parent_id` chain; 400).
  - Deleting a folder is refused if it has live children or snippets (409).

### Encryption

Key: 32 bytes from `crypto/rand`, stored in `state_dir/key`. Cipher:
AES-256-GCM, fresh 12-byte nonce per write, stored as `nonce || ciphertext`
in `body`. The `id` is used as additional authenticated data so a ciphertext
cannot be moved between rows. Toggling `is_sensitive` re-encrypts or
decrypts the body and rewrites the FTS row.

The per-variable default map is protected the same way for sensitive
snippets: it is sealed under the snippet id into `var_defaults_enc`
while the plain `var_defaults` column stays `''`, and the toggle
re-seals or clears that column alongside the body. A saved default can
be as sensitive as the body itself (a token, a URL with credentials).

The key is not derived from a passphrase. Anyone with the key and the
database can read everything; that is the accepted threat model.

### Templates

The `snippets.uses_variables` flag marks a body as a template. The server
treats the body as opaque text; the flag only drives frontend behavior
(the variables panel in the detail view and the rendered copy). A body
containing `{{name}}` or `{{name|default}}` is a template; the edit form
keeps the flag in sync with the body's placeholders. Variable names match
`[A-Za-z_][A-Za-z0-9_]*`. Parsing lives in the frontend (and later the
CLI). A variable without a default that is left blank in the copy panel
renders as an empty string; in the panel's live preview it stays visible
as `{{name}}`.

A template body can also carry a persisted per-variable default map, the
`var_defaults` field of the snippet JSON (the `snippets.var_defaults`
column): a value saved per `{{name}}` by the user through the variables
panel. Precedence when copying: the value typed in the panel, then the
saved default, then the inline `{{name|default}}` text, then an empty
string (or the literal placeholder in the live preview). The server
treats the map as opaque; the client owns the keys and prunes them to
the body's current variables on save. For sensitive snippets the map is
encrypted at rest with the body (see "Encryption") and is omitted from
list and sync responses, like the body.

### Favorites

The `snippets.pinned` flag marks a snippet as a favorite. Like
`uses_variables`, it is a flag the server never interprets: it is not
FTS-indexed, and it is stored plainly even for sensitive snippets — whether
a snippet is a favorite says nothing about its body, so it is not
encrypted alongside `body` and `var_defaults`. It rides the snippet JSON as
`pinned`, so it syncs with the row and travels through export and import
(an older export document without the field imports as unpinned). The
frontend surfaces pinned snippets in a Favorites list above the folder tree
(spec §6).

## 5. API

All endpoints are under `/api`, JSON in and out, `Content-Type:
application/json` except `/raw`. State-changing requests must carry
`Content-Type: application/json` (415 otherwise); the server sends no CORS
headers. Request bodies over 10 MiB are rejected with 413. Errors are
`{ "error": "message" }` with an appropriate 4xx/5xx status. Timestamps are
RFC3339 UTC strings, second precision, no fractional seconds.

### Snippet JSON

```json
{
  "id": "01J...",
  "title": "restart caddy",
  "body": "sudo systemctl restart caddy",
  "language": "bash",
  "notes": "Only needed after editing the Caddyfile.",
  "folder_id": "01J...",
  "tags": ["ops", "caddy"],
  "is_sensitive": false,
  "uses_variables": false,
  "pinned": false,
  "var_defaults": {},
  "created_at": "2026-09-02T10:00:00Z",
  "updated_at": "2026-09-02T10:00:00Z"
}
```

In list and sync responses, sensitive snippets have `body` set to
`null`; `var_defaults` is `null` for them as well. On input (POST/PUT),
an omitted `var_defaults` decodes to an empty map; the server stores
the keys it is given (the client prunes them to the body's variables).
`pinned` is always present on output and defaults to `false` on input, so
a PUT that omits it clears the pin — the same full-replace contract the
other fields have.

### Endpoints

| Method | Path | Notes |
|---|---|---|
| GET | `/api/me` | identity |
| GET | `/api/version` | `{ "version": ... }` — the release version stamped into the binary (`internal/buildinfo`), or `"dev"` for an unstamped build; the SPA header shows it next to the wordmark |
| GET | `/api/snippets?q=&tag=&lang=&folder=&limit=&offset=` | search/list; ordering `updated_at DESC, id DESC`; default `limit` 50, max 200; `q` may contain `tag:x lang:y` filters mixed with search terms (prefix-matched, ANDed, see "Query syntax"); `tag:`/`lang:` tokens in `q` AND with the separate `tag=`/`lang=` params; if nothing is left after filter extraction, no FTS MATCH is issued and all rows matching the filters are returned |
| POST | `/api/snippets` | create; server assigns id and timestamps |
| GET | `/api/snippets/{id}` | full snippet, body decrypted |
| PUT | `/api/snippets/{id}` | full replace; `var_defaults` carries the per-variable default map (keys owned by the client, pruned on save) and `pinned` the favorite flag — omitting either clears it |
| DELETE | `/api/snippets/{id}` | soft delete |
| GET | `/api/snippets/{id}/raw` | `text/plain` body, decrypted; for `curl \| sh` |
| GET | `/api/folders` | full tree as a flat list with `parent_id` |
| POST | `/api/folders` | create; 409 on name collision with a sibling |
| PUT | `/api/folders/{id}` | rename / move; 409 on name collision; 400 if the move would create a cycle |
| DELETE | `/api/folders/{id}` | refused with 409 if non-empty |
| GET | `/api/tags` | `[{ "name": "ops", "count": 12 }]` over live snippets |
| GET | `/api/sync?since=<ts>` | all folders and snippets with `updated_at >= since` or `deleted_at >= since`, tombstones as `{ "id", "deleted_at" }`; sensitive bodies omitted; omit `since` for a full sync (first run or manual resync); response carries `server_time`, captured *before* the read, to use as the next `since` |
| GET | `/api/export` | full export document |
| POST | `/api/import` | import document; `?mode=merge` (default, upsert by id) or `?mode=replace` (upsert everything in the document and delete live rows not in it; one transaction) |
| POST | `/api/seed` | apply the bundled starter pack (`internal/starter`, import merge, no body); returns `{created, updated}` |

Sync boundary: `server_time` is captured *before* the read, and the
`since` filters are `>=`, not `>` — for both `updated_at` and
`deleted_at`. Stored timestamps are whole seconds, so a row written in
the same second as the previous snapshot has `updated_at == since`; a
strict `>` would skip it forever, because its timestamp never changes
again (`internal/store/sync_test.go`, `TestSyncSameSecondBoundary`).
With `>=` that row is re-sent, which is harmless because the client
merge is idempotent: the same row may appear in two consecutive syncs
and duplicates are ignored. A row written just after the snapshot is
likewise never lost — it either lands in this response or has
`updated_at >= server_time` and is picked up next time.

Import: duplicate ids within one document are a 400. `created_at` /
`updated_at` are preserved when present in the document, assigned otherwise.

### Query syntax

`q` is split on whitespace. Tokens of the form `tag:foo` and `lang:foo`
become filters (repeatable, ANDed). Everything else is a search term, and
every term must match (terms are ANDed). A term matches when it is a
*prefix* of a token in the snippet's title, notes, body or tags —
case-insensitively, so `zeb` finds `Zebra deployment` while `estart` finds
nothing. Each term reaches FTS5 as a quoted prefix query (`"term"*`), which
makes FTS5 syntax in user input literal text: `AND`, `OR`, `NOT`, `NEAR` and
column filters like `title:x` are ordinary words. That is deliberate — the
offline engine has no operators, so quoting the terms is what keeps the two
agreeing (spec §6). A term containing no letters or digits is dropped. If
FTS5 nonetheless rejects the generated expression, the server retries the
terms as a single quoted phrase; if that fails too it returns 400.

Search ranking uses `bm25()` with title weighted highest. Ranking is
per-engine, so online and offline return the same *set* of snippets but may
order it differently.

### Export / import document

```json
{
  "version": 1,
  "exported_at": "...",
  "folders":  [ { "id", "parent_id", "name" } ],
  "snippets": [ { ...snippet JSON with plaintext body... } ]
}
```

Import accepts snippets without `id` (one is assigned) and folders referenced
by `folder_path` (`"shell/deploy"`) instead of `folder_id`, creating the
path as needed. This is the mass-import format; anything can be munged into
it. Snippet entries carry `var_defaults`, decrypted for sensitive
snippets just like the body, and `pinned`; export documents older than
either field import with an empty map and no pin respectively. An export file contains plaintext
sensitive bodies and is as sensitive as the database plus key file;
treat it accordingly.

### Starter pack

Added 2026-09-11. `internal/starter` holds a small curated pack — a
copy-pasteable `snp config.toml`, a template example, and a couple of
everyday commands — as an embedded import document (`pack.json`,
`go:embed`), so seeding reuses the import path above: no second format and
no new store code. Snippets ship in a `Starter` folder via `folder_path`,
and the ids are pinned, so applying the pack twice updates those rows
instead of duplicating them.

- **Explicit only.** Nothing seeds itself. Import's merge mode clears
  `deleted_at`, so an automatic apply would resurrect snippets the user
  deleted and undo a deliberate cleanup; the pack is written only when
  asked for — `snp seed`, `POST /api/seed`, or the SPA's settings panel
  ("Add starter snippets").
- **API**: `POST /api/seed` takes no body and returns
  `{"created", "updated"}` like `/api/import`. `snp seed` prints the same
  counts.
- **Scope**: packs are data, not code. Editing `pack.json` and keeping
  the pinned ids is enough to change what ships; a pack that should not
  overwrite an edited snippet later needs new ids rather than a re-seed.

## 6. Frontend

Svelte 5, Vite, TypeScript, in `web/`. `npm run build` emits `web/dist`,
which is embedded with `go:embed` and served by the binary. A Makefile
target builds both.

### Layout

Three panes: favorites and folders with a wrapping tag pill cloud below
(left), search box and result cards (middle), snippet view card or editor
(right). The list offers Compact, Regular (default), and Large sizes,
remembered locally independently of the layout and title-wrapping settings.
Compact uses tighter spacing; Large adds up to three lines of non-sensitive
body preview. Snippet tags are
also shown as chips in the detail pane. The favorites list sits above the
folder tree and stays visible when empty, with a line saying how to add to
it.

The two dividers between the panes are draggable: dragging the folders
divider trades width with the list pane, and dragging the list divider
is absorbed by the detail pane, which is the flexible one and always
takes what the other two leave. A divider also takes focus, so the
arrow keys resize by 16px (1px with Shift) and Home or a double-click
restores the default layout. Widths persist in localStorage
(`snp.paneWidths`, see `web/src/lib/panes.ts`); a window whose dividers
were never dragged keeps the stylesheet's own flexible proportions and
so still adapts to its size. This is the **wide** layout; below the
compact breakpoint, or when the Layout setting says so, the same
components are arranged as one screen at a time instead (next section).

### Compact layout

Designed and built 2026-09-12 (Phase 11; `web/src/lib/layout.ts`,
`.app.compact` rules in `app.css`). The three panes
do not fit a phone, and stacking them vertically would put the search
box and the result list on different scroll positions from the body the
user actually wants, so narrow windows get a **stacked navigation
model** instead: one screen at a time, with the list as the root.

- **Layout setting.** A three-way *Layout* select in the settings panel
  next to *Theme*: **Auto** (default), **Wide**, **Compact**. It persists
  in localStorage (`snp.layout`; Auto is the absence of the key, like
  `snp.theme`) and is re-applied before first paint. Auto resolves to
  compact when the viewport is narrower than **720px**
  (`(max-width: 719px)`, tracked live with `matchMedia` and re-read on
  every window `resize` and root-element size change, since an embedded
  webview does not always deliver the media-query change event, so a
  resize or a rotation switches without a reload) and to wide otherwise. Wide and
  Compact are plain overrides that ignore the viewport entirely; there is
  no "compact unless the window is huge" middle ground. The wails desktop
  window's minimum width is 400px — below the breakpoint — so narrowing
  it switches to compact under Auto, and a manual Compact applies to it
  like anywhere else — same SPA, same code, no per-platform special case.
- **Screens.** The **list** is the root: the search box and *New snippet*
  at the top, the result list below, full width. Tapping a result pushes
  the **detail** screen — the same snippet view, or the editor for
  *New snippet* / *Edit* — with a **Back** control at the left of the top
  bar. Back returns to the list with the selection and scroll position
  kept (the list stays mounted while detail is shown, and the existing
  scroll-selected-row-into-view behavior restores the place). Saving a
  new snippet lands on its detail screen, as selecting it would; Cancel
  in the editor returns to where the editor was opened from (detail for
  Edit, the list for New).
- **Drawer.** Favorites, the folder tree, the tag list and *New folder*
  — the whole left pane — live in a **drawer** that slides in over the
  list from the left, opened by a ☰ control at the left of the top bar
  or by the *Folders* affordance beside the search box, and closed by its
  own close control, a tap on the scrim, Back, or Escape. Choosing a
  folder (including *All*) closes the drawer, since it is a filter on the
  list behind it. Toggling a tag does not, because tags combine with AND
  and the user may want several; the counts and the highlighted list
  behind the scrim show the effect as they go. The drawer is an overlay,
  not a push, so the list never reflows under it.
- **Top bar.** In compact mode the top bar holds only the ☰ / Back
  control, the `snp` wordmark, and the settings gear. The version chip,
  the connection pill, the *Synced …* age and *Resync* move into the
  settings panel (which becomes a full-width sheet under the bar instead
  of an anchored popover); the offline banner already announces the
  offline state on its own line. The gear stays reachable on every
  screen, so a Layout override chosen by mistake can always be undone.
- **History.** Every push — opening the drawer, opening detail — pushes
  one `history` entry tagged as ours, so the browser's or the OS's back
  gesture pops exactly one level and only leaves the app once the list is
  showing with the drawer closed. The in-app Back and the close controls
  go through `history.back()` too, so the stack and the history never
  disagree. Wide mode pushes nothing. Switching from wide to compact with
  a snippet selected or the editor open starts on the detail screen (so a
  window dragged narrower keeps what was being read); switching back to
  wide discards the stack and shows all three panes. Reload starts at the
  root: the stack is not persisted, and an entry left over from a
  previous page load is ignored.
- **Keyboard in compact mode.** Escape closes the drawer if it is open,
  otherwise leaves the detail screen for the list — except while the
  editor is open, where Escape does nothing and *Cancel* is explicit, the
  same as wide mode. `Cmd/Ctrl+K` first pops back to the list, then
  focuses the search box, so the search workflow (arrows, Enter to copy)
  works from anywhere. The arrow keys in the search box move the
  selection without pushing detail; Enter copies, as in wide mode.
- **Not changed by compact mode.** Everything the screens contain: the
  detail view, the editor, the copy actions, the variables panel, the
  reveal flow, offline behavior. The pane dividers and the persisted
  pane widths are simply inert in compact mode — the grid is one column
  and the dividers are not rendered — so a width dragged on a desktop
  never leaks into the phone layout, and it is still there when the
  window is wide again.
- **Editor and Back.** Back while editing behaves as Cancel, with an
  in-app discard confirmation when fields have changed. Keep editing restores
  the detail history entry and preserves the draft. The same guard protects
  Cancel, selection (including Favorites and keyboard selection), New,
  compact Focus search, full resync, and native desktop close. Browser
  unload uses the browser's confirmation. Navigation waits while a snippet
  write is pending; failed writes leave the draft and an inline error.
- **Focus.** Dialogs, the command palette, and the compact folder drawer
  contain keyboard focus and restore it on close. Dialog backgrounds are
  inert; destructive confirmations initially focus the cancel control.

### Behaviors

- **Save lifecycle.** One snippet write at a time; an in-flight sync finishes
  before a save, and new syncs wait until the write finishes. Offline saves
  are disabled without clearing the editor. Active search results derive
  from the current cached rows and refresh after local writes and sync.
- **Sensitive detail lifecycle.** Reveals keep the body and variable defaults
  together in view memory, update after saving, and clear on selection or
  sync. Late reveal responses cannot restore an abandoned selection. Cache
  writes and local indexing strip sensitive body/default content defensively.
- **Folder editing.** An existing unfiled snippet stays unfiled regardless
  of the active folder filter. New snippets inherit that filter. Pickers and
  detail labels show full ancestor paths.

- **Tags** (left column, under the folders): every tag with its count,
  most-used first. Clicking a tag filters the snippet list to snippets
  carrying it; several tags combine with AND (all must be present), the
  same rule as typing `tag:a tag:b`. Clicking an active tag clears it.
  Active tags AND with the folder selection and the search box. The
  list is derived from the local cache, so filtering works offline.
- **Favorites** (left column, above the folders): the pinned snippets,
  newest first, with the flag toggled from the detail header. Clicking a
  row selects that snippet, and a row can be unpinned from the list itself.
  It is derived from the local cache, so it renders offline; pinning and
  unpinning are server writes and are disabled offline, like the others.
- **Search** runs against the locally synced index (kept current by the
  sync merge below) in both the online and offline states, and the two
  engines are held to the same rule: every whitespace-separated term must
  prefix a token (case-insensitively) in the title, notes, body or tags,
  with all terms required. Neither engine offers FTS5 operators — the
  server quotes each term, so `AND`/`OR`/`NOT`/`NEAR` and `title:x` are
  literal words — which is what makes the matched *set* identical online
  and offline. Ranking stays per-engine (`bm25` against MiniSearch), so
  equally-matching results can be ordered differently. v1 does not query
  `/api/snippets` per keystroke, so online results are as fresh as the last
  sync rather than hitting the server's FTS5 ranking live.
- **View** shows title, language badge, tags, folder path, notes rendered
  as sanitized Markdown, and the body with read-only syntax highlighting
  when its `language` matches a known grammar (highlight.js, loaded per
  language; plain escaped text otherwise). A body longer than ten lines
  (scripts especially) is capped to about ten lines with an inner scroll,
  with a Show all / Show less button toggling the full height. The
  template Rendered preview collapses on the same rule but independently
  — a short template can render long, when a variable holds many lines —
  so each box has its own toggle. The editor stays uncapped, and a
  sensitive body that has not been revealed offers no toggle. The detail
  footer dates the snippet as "Updated Sep 11" (with the year when it is not
  the current one) and keeps the precise local timestamp in its tooltip.
- **Edit** uses a plain textarea for the body, a plain textarea for notes,
  a language text input, a folder picker, a comma-separated tag input with
  a sensitive checkbox, and a template checkbox that stays in sync with
  the body's `{{var}}` placeholders. Saving a snippet carries its
  `var_defaults` forward, pruned to the variables the body still uses,
  so an edit never wipes saved defaults. Editing a sensitive snippet
  fetches its decrypted body first (sensitive bodies are never cached
  locally), so saving never replaces it with an empty body.
- **Copy** places each action next to what it copies, so the template and
  its rendered form cannot be confused, and each box's copy button is the
  pane's accented primary action (the footer holds only Edit). A plain
  snippet's body box carries **Copy snippet** (the body). A snippet with
  `uses_variables` set shows a variables panel: the Template box carries
  **Copy template** (the body with its `{{var}}` placeholders intact, for
  reusing the shape) and the Rendered box **Copy rendered** (filled in).
  A sensitive body that has not been revealed shows **Copy snippet**
  disabled. Every copy control briefly reports its own outcome — "Copied.",
  or "Copy failed" when the write is rejected — so a blocked clipboard is
  visible rather than silent. The inputs are pre-filled from the snippet's
  saved defaults (`var_defaults`, spec §4); precedence when copying is the
  value typed in the panel, then the saved default, then the inline
  `{{name|default}}` text, then an empty string. In the live preview a
  variable with no value stays visible as `{{name}}`; the copied rendered
  text renders it empty.
- **Save defaults** (variables panel) is inert until the inputs differ from
  what the server holds — judged on the same pruned map the save writes, so
  retyping the stored value is not a change and clearing an input is. A
  successful save confirms with a brief "Defaults saved" and moves the
  baseline; a failure reads "Save failed" and keeps the same write on
  offer. A blank input clears that default, and keys for variables no
  longer in the body are dropped on save.
- **Sensitive** snippets show a masked body with a reveal button. The body
  is fetched on reveal and held only in memory for that view. Offline, the
  reveal button is disabled (the body is never stored locally) and a hint
  says an online connection is required; metadata stays visible.
- **Keyboard**: `Cmd/Ctrl+K` focuses the search box (selecting the current
  query, so typing replaces it) and is hinted inside the field. While the
  search box has focus, `Up`/`Down` move the selection through the visible
  results with wrapping at both ends, `Enter` copies the selected snippet,
  and `Escape` clears the query and then leaves the field. Only the focus
  shortcut is global: the arrows and `Enter` are scoped to the search box,
  so the snippet editor keeps its own keys. The copy shortcut writes exactly
  what the detail pane's copy buttons would (typed variable values included)
  and flashes "Copied." in the header, since there is no button under the
  cursor to change its label. The folder-rename inline input answers Enter
  (commit) and Esc (cancel), the pane dividers take focus and answer the
  arrow keys, and forms save through their buttons.
- **Command palette**: `Cmd/Ctrl+Shift+P` (matched on the physical P key,
  so Shift-altered layouts still work) toggles a commands-only palette
  over the app; snippet *finding* stays in the search box. It is a
  filter field over a list: arrows move with wrapping, `Enter` runs,
  `Escape` or the scrim closes. The list is built from the app's existing
  actions — New / Edit / Copy / Favorite / Delete / Show body for the
  selected snippet, New folder, Focus search, Resync, Full resync, Add
  starter snippets, and one entry per Layout mode. Every command stays
  listed; one that cannot run now is greyed with the reason where its
  shortcut hint would be (`Offline`, `Select a snippet first`, `Body
  hidden`, `Current`), so the palette also explains itself. Snippet
  commands are dropped while the editor is open so nothing can clobber an
  unsaved edit from the keyboard. Delete and Full resync go through the
  usual confirm dialogs. The shortcut is ignored while a dialog is open,
  and while the palette is open the global handler sees no keys, so
  Escape there never reaches the search workflow. `lib/commands.ts`
  holds the `Command` shape and the pure filter (every token must match,
  word-prefix matches rank first); `lib/CommandPalette.svelte` is the
  overlay; `App.svelte` assembles the list.
- **Timestamps** are simplified, with the exact value on hover: the header
  reads "Synced 2 minutes ago" and each list row and the detail footer a
  short calendar date ("Sep 11", with the year when it is not the current
  one), the precise local date and time behind a tooltip. The sync age is
  measured from when this client observed the sync, not from the response's
  `server_time`: `server_time` is the sync cursor, and comparing it to the
  local clock would render a negative age on a client whose clock trails the
  server's, so a timestamp in the future reads "just now". The header label
  re-renders on a 30-second tick so the age does not go stale.
- **Appearance** (settings panel, 2026-09-08): a **Theme** select —
  auto (system), light, dark, Solarized Light, Solarized Dark, Kimbie
  Dark, Tokyo Night — and an **Interface text size** slider (75–150%).
  Themes remap the CSS palette variables via `:root[data-theme=…]`
  blocks (auto = no attribute, so `prefers-color-scheme` drives it);
  text size scales every `font-size` through the `--text-scale` custom
  property. A **Two-line titles in the list** checkbox wraps a long list
  title onto a second line instead of truncating it at one; every list
  title carries its full text as a tooltip either way. All three persist in
  localStorage and are applied before first paint
  (`web/src/lib/settings.ts`), in the browser and the desktop window alike.

### PWA and offline

- `manifest.webmanifest` with icons; installable on macOS, iOS, Android.
- Service worker precaches the built app shell and serves it cache-first.
  API requests are network-only.
- The precached shell can go stale: a browser only looks for a new worker on
  a navigation, so a window left open — or a PWA resumed from the background
  — can keep serving a bundle from an old deploy while every request still
  succeeds, which reads as "the app is broken" rather than "the app is old".
  The app therefore re-checks for an update whenever the window is focused or
  shown again, and reloads once when a new worker takes over
  (`web/src/lib/sw.ts`); the reload is skipped on a first install, where the
  worker claiming clients fires the same event. If a draft is dirty or a
  snippet write is pending, the reload waits until the draft is saved or
  discarded and the write finishes.
- On startup, on `visibilitychange`/`focus`, and every few minutes while
  open, the app calls `/api/sync?since=` and merges folders, snippets, and
  tombstones into IndexedDB (mobile OSes suspend background apps, so the
  timer alone is not enough). The stored `server_time` becomes the next
  `since`. The client-side search index (MiniSearch) is refreshed from the
  merged cache after each sync, so it tracks the cache without a separate
  rebuild step.
- Offline, the list and search read from IndexedDB. Offline search cannot
  match sensitive bodies, consistent with the server, which never indexes
  them.
- Offline, create/edit/delete are disabled and a banner says so. Nothing is
  queued. Saving variable defaults and pinning are server writes as well, so
  both are disabled offline; saved defaults still pre-fill the inputs and the
  Favorites list still renders, since both ride in the local cache (the
  defaults only for non-sensitive snippets).
- Sensitive bodies are never written to IndexedDB.
- A "full resync" action clears the local cache and re-syncs from scratch;
  this is the recovery path if the server is ever restored from an older
  backup and the stored `since` is too new.

## 7. Ops

### Access

The app must be reached by its tailnet DNS name (`snp.<tailnet>.ts.net` or
the magic-DNS name), not by a `100.x` IP: the Tailscale certificate only
validates for the DNS names.

### systemd

`deploy/snp.service` runs `snp serve` as user `snp`, `WorkingDirectory` and
`StateDirectory` under `/var/lib/snp`, `ProtectSystem=strict`,
`Restart=on-failure`. `deploy/install.sh` creates the user, copies the
binary to `/usr/local/bin/snp`, writes a starter config, and enables the
unit. `TS_AUTHKEY` is supplied via an `EnvironmentFile` for the first start
and can be removed afterwards.

Note: deleting `state_dir/tsnet` discards the node's tailnet membership;
the next start must join again and will need `TS_AUTHKEY` once more. The
README says not to delete it casually.

### Auto-deploy from a checkout

For a host that builds from a checkout, `deploy/install-autoupdate.sh`
renders `deploy/user/{snp.service,snp-update.service,snp-update.timer}`
into `~/.config/systemd/user/` with the checkout path baked in. The
server runs from `bin/snp` as a user unit; the timer runs
`deploy/autoupdate.sh`, which fetches and hands off to `deploy/update.sh`
only when `origin/main` is strictly ahead of a clean checkout on `main`.

`update.sh` takes the node name from the installed unit's `--hostname`
(a `-n` flag overrides; `snp` only when there is no unit), and before
touching anything probes the health URL against the server already
running — a URL that does not answer 200 then is a wrong URL, and the
script refuses rather than rolling a healthy server back later. It then
keeps the old binary, fast-forwards and builds first, backs up the
database **with the old binary** (a new binary migrates on open) into
`state_dir/pre-deploy/`, restarts the unit, and polls
`https://<node>.<tailnet>/api/me` for up to 60s from the owner's
identity. On failure it restores the old binary, restores the backup only
if `schema_version` changed (the failed database is kept alongside),
resets the checkout, restarts, and writes the bad commit to
`deploy/.last-failed`, which `autoupdate.sh` skips (exit 1, so the
oneshot stays failed and visible) until `origin/main` moves. Migrations
are forward-only, so a rolled-back binary would otherwise run against a
newer schema.

### Backup

`deploy/backup.sh DEST_DIR` runs `snp backup` into
`DEST_DIR/snp-YYYYmmdd-HHMMSS.db`, verifies the result by opening the new
file and running an integrity check, copies the key file alongside as
`snp.key` (overwriting), and deletes backups older than a configurable
number of days. Copying backups offsite (rsync/rclone) is the operator's
job. Intended for cron. The README states plainly that the key file is
required to read sensitive snippets from a backup.

### Logging

Structured logs to stdout via `log/slog`; journald captures them.

## 8. Error handling

- API: consistent JSON error body; 400 for validation (including folder
  move cycles and malformed tag names), 403 for identity mismatch, 404 for
  unknown ids, 409 for non-empty folder deletion and folder name
  collisions, 413 for oversized request bodies, 415 for missing JSON
  content type on state-changing requests, 500 for storage or crypto
  failures (logged with detail, returned without it).
- Store: all multi-row writes in a transaction; FTS row and main row are
  updated together or not at all, with the FTS row deleted before the main
  row on removal.
- Frontend: failed writes show an inline error and keep the editor state;
  failed sync is logged and retried on the next interval; offline state is
  detected via fetch failure plus `navigator.onLine`.

## 9. Testing

- **Go store tests** against a temporary on-disk database
  (`t.TempDir()`; `newTestStore` opens its own DB and key and installs a
  `fakeClock`): CRUD, tag
  normalization, FTS rowid mapping across create/update/delete (external
  content), FTS matching and the fallback path, filters, sensitive
  exclusion from FTS, encrypt/decrypt round trip including AAD mismatch,
  tombstone sync, the sync `since`/`server_time` boundary (same-second
  rows are re-sent, not lost), purge, folder delete refusal,
  folder move cycle refusal, sibling name collision, import merge and
  replace including `folder_path` creation.
- **Go handler tests** with `httptest` and an injected fake identity
  resolver covering auth accept/reject, content-type rejection (415) on
  `POST`/`PUT`/`DELETE`, and every endpoint's happy path plus validation
  errors.
- **Frontend unit tests** (Vitest): template variable parsing, rendering,
  and live preview, query string parsing, sync merge into the local store,
  offline search, and component behavior for the detail view's variables
  panel and the form's template flag.
- No browser end-to-end tests in v1. Manual smoke on `--dev-listen`.

## 10. Repository layout

```
cmd/snp/                 main, subcommand wiring
cmd/snp-desktop/         wails desktop binary (darwin/linux; spec §12)
                         plus platform_{darwin,linux}.go options
internal/config/         toml + flags + env resolution
internal/store/          sqlite open/migrate, queries, fts, crypto
internal/store/migrations/*.sql
internal/server/         router, middleware, handlers, embedded static
internal/tsauth/         tsnet listener and whois identity
internal/ai/             one-shot OpenAI-compatible generator (spec §13)
internal/starter/        bundled starter snippet pack (spec §5)
internal/desktop/        desktop API bridge (no wails import; spec §12)
web/                     svelte app; web/dist is embedded
deploy/                  snp.service, install.sh, backup.sh, make-app.sh,
                         snp.desktop + install-desktop.sh (Linux)
                         (plus Info.plist, snp.entitlements, appicon.png)
docs/                    this document
Makefile                 build/test/dev plus desktop/app targets
```

## 11. Out of scope for v1

Version history, multi-user, sharing, offline writes, CLI client,
SnippetsLab converter, semantic search, encryption with a user passphrase.
The CLI and the SnippetsLab converter are the first follow-ons.

## 12. Desktop app (Wails)

A local desktop variant of snp, added 2026-09-06: `snp-desktop` runs the
same store and the same SPA in a native Wails window, on macOS and Linux
(Linux added 2026-09-11; Windows remains a follow-on).
It is a *local instance* — no tailnet, no HTTP listener, no owner — and
coexists with the server variant unchanged.

- **Binary**: `cmd/snp-desktop` (main.go is tagged `darwin || linux`). It
  is a separate binary from `snp` because wails v2 is a heavy CGO
  dependency (WebKit on macOS, GTK3/WebKit2GTK on Linux); keeping it out
  of `cmd/snp` preserves the server binary's headless cross-builds. The
  file is platform-neutral: per-OS wails options live in
  `platform_darwin.go` (a deliberate no-op — the defaults are correct)
  and `platform_linux.go` (GPU policy, GTK program name, window icon),
  and per-OS link flags live in the Makefile. `make desktop` builds it,
  `make run-desktop` launches it, `make desktop-install` installs it for
  the current user on Linux.
- **Data**: the same state dir as every other variant (config default
  `~/.local/share/snp`), so the desktop app, the CLI, and backups share
  one store. Config resolution is `config.LoadDesktop`: identical to
  `Load` minus the owner-required rule. `snp-desktop` ignores the
  tailnet-ish keys (hostname, owner).
- **Transport**: the SPA normally fetches `/api/*` over HTTP; in the
  window there is no HTTP surface, so `internal/desktop.App.CallAPI`
  (bound as `window.go.desktop.App`) runs each request against the same
  in-process handler the serve subcommand serves, via httptest. The
  bridge applies the same JSON content-type rule (spec §3) and returns
  `{status, contentType, body}`. `web/src/lib/desktop.ts` detects the
  shell (the asset server stamps `window.__SNP_DESKTOP__` into
  `index.html`) and routes the existing fetch-based api client through
  the bridge; in a browser the module is inert and behavior is
  unchanged. `internal/desktop` deliberately does not import wails so
  its tests run on any platform.
- **Store lifetime**: the desktop process opens/migrates the store,
  loads or creates the key, and runs the same daily purge job as
  `serve`; the purger is drained before the store closes on quit.
- **Frontend notes**: the offline/PWA machinery is inert in the window
  (the local bridge never fails at the network layer); the cache/sync
  loop still runs against the local store and is harmless.
- **Packaging**: `make app` packages `bin/snp-desktop` into a signed
  macOS bundle, `build/snp.app` (`deploy/make-app.sh`): Contents/
  Info.plist (`deploy/Info.plist`, bundle id `com.jstevewhite.snp`),
  an AppIcon.icns from deploy/appicon.png (falling back to the PWA icon), hardened-runtime
  codesigning with the network-client entitlement
  (`deploy/snp.entitlements` — Ask-AI calls the provider in-process),
  verified with `codesign --verify --deep --strict`. The identity comes
  from `SIGN_IDENTITY` (Makefile default: empty, i.e. ad-hoc; set it to
  your Developer ID for distribution).
  `make app` also notarizes and staples the bundle with `notarytool`
  (`NOTARY_PROFILE`, default `snp-notary`) and writes `build/snp.zip`,
  so it passes Gatekeeper on other machines; the signature carries a
  secure timestamp as notarization requires. `make run-app` launches it
  via `open`.
- **Packaging (Linux)**: there is no bundle to sign — the binary is the
  app. `deploy/snp.desktop` is the launcher (`Exec=snp-desktop`,
  `Icon=snp`, `StartupWMClass=snp` matching wails' linux `ProgramName`,
  so the running window groups with its launcher icon), and
  `deploy/install-desktop.sh` installs the binary to `<prefix>/bin`, the
  entry to `<prefix>/share/applications`, and 192/512px icons into
  `<prefix>/share/icons/hicolor`, defaulting to `$HOME/.local` (no
  root); `make desktop-install` runs it, `PREFIX=` overrides. There is
  no notarization equivalent and no app-sandbox format involved.
- **Cross-platform build**: the desktop binary must be built on the
  target OS — wails needs CGO against that platform's WebKit/GTK
  headers, so it does not cross-compile from macOS; a Linux host or a
  Linux container is the build path. Linux needs `build-essential
  pkg-config libgtk-3-dev` plus `libwebkit2gtk-4.1-dev` (Ubuntu 24.04+,
  Debian 13, Fedora 40+) or `libwebkit2gtk-4.0-dev` (Debian 12, Ubuntu
  22.04); the Makefile derives the matching wails `webkit2_41` tag from
  pkg-config (`WEBKIT2=` forces 4.0). The resulting binary links the
  same GTK/WebKit shared libraries at runtime. A real 1024px marketing
  icon and the Windows port remain follow-ons; the wails CLI is not
  required on any platform.


## 13. AI snippet generation (OpenAI-compatible)

Added 2026-09-08. An optional, one-shot generator: the user types a
request ("give me a command to copy a file to my home dir") and the
reply is a templatized snippet — `cp -rf {{file}} ~/` — ready to review
and save. There is no chat history and nothing is stored by the feature
itself; the reply fills the snippet editor and saving goes through the
normal snippet endpoints, so AI output never lands in the store
un-reviewed.

- **Configuration** (flag > env > file, like every key): `ai_endpoint`
  (`SNP_AI_ENDPOINT`; default `https://api.openai.com/v1` — any
  OpenAI-compatible base URL works), `ai_model` (`SNP_AI_MODEL`;
  default `gpt-4o-mini`), `ai_key` (`SNP_AI_KEY`). The feature is
  enabled when a key is set. The key is never exposed over the API.
- **API**: `GET /api/ai/status` → `{"enabled", "model"}` (no endpoint
  or key); `POST /api/ai/generate` with `{"prompt", "language"?,
  "kind"?}` → `{"title", "language", "body", "notes",
  "uses_variables"}` — the bare command/fragment in `body`, its
  plain-text explanation in `notes` for the snippet's Notes field.
  `kind` selects the shape of the reply: `command` (the default when
  omitted or empty), `script`, or `function`; matching is
  case-insensitive and surrounding whitespace is ignored, and any
  other value is a 400 rather than a silent command. `POST /api/ai/tags`
  with
  `{"body", "title"?, "language"?}` → `{"tags": [...]}` (2-3 relevant
  tags; the server sends the user's existing tag vocabulary so the model
  can reuse or extend it). Suggested tags are filtered to the store's
  tag grammar before being returned. `POST /api/ai/explain` with
  `{"body"}` → `{"notes": "…"}` — a Markdown explanation (what it
  does, gotchas, important info) for the snippet's Notes field; the
  prompt asks for a reply under 500 tokens rather than a hard API cap,
  so reasoning tokens are not truncated. Not configured → status `enabled:false`
  and the endpoints return 503; missing/oversized prompt or body → 400;
  provider/parse failures → 502 (detail logged, never returned).
- **Statelessness**: every call is one system+user message pair sent to
  the configured endpoint; nothing about prior conversations, other
  snippets, or sensitive content is ever included. Prompts and bodies
  are not logged.
- **Output contract**: the system prompt demands a bare snippet in
  snp's `{{name}}` / `{{name|default}}` template syntax inside a JSON
  envelope. The `kind` picks the body rule: `command` must be exactly
  one executable command on a single line — no shell comments, no second
  or alternative command, no prose (extras, flags and caveats go to
  `notes`); `script` is a complete runnable multi-line program (a shell
  script starts with a shebang, comments and argument handling are
  expected) whose `notes` say what it does and how to run it; `function`
  is one complete named function definition in the requested language
  (imports may precede it) whose `notes` state the parameters, return
  value, and a one-line call example. Command `notes` stay a single
  plain-text line; script and function `notes` may use short Markdown
  lines, since Notes are Markdown-rendered (§6). All three share the
  template and envelope rules and forbid markdown code fences in
  `body`. The
  one-line rule is a prompt rule, not a parser rule: parsing accepts
  multi-line bodies and tolerates fenced or prose-wrapped replies
  defensively. `uses_variables` is set when the body contains
  placeholders, so the variables panel works unchanged.
- **Frontend**: the snippet form's "Ask AI…" control queries the status
  endpoint (hidden when unconfigured) and fills
  title/language/body/notes for review — snippet in Body, explanation
  in Notes; the template checkbox syncs from the body as usual. An
  "Output" selector (Command / Script / Function) inside the control
  picks the `kind` sent with the request; it defaults to Command and
  covers the whole form's session, so a script can be regenerated
  without re-selecting. A
  "Suggest tags" control next to the Tags field asks for 2-3 relevant
  tags (new ones allowed) and merges them into the field without
  duplicating or clobbering what is already typed. An "Explain" button
  next to Notes asks for a short explanation of the body (gotchas +
  important info) and replaces the Notes field with it. The text Explain
  replaced is kept behind an "Undo" button beside it, so an unwanted
  explanation is one click to back out of rather than something to delete
  by hand; editing Notes by hand drops that undo point, so Undo can never
  discard typing that came after the overwrite.
- **Privacy**: requests leave the machine for the configured provider;
  a self-hosted OpenAI-compatible endpoint (e.g. ollama) is supported
  by pointing `ai_endpoint` at it. Treat generated snippets like any
  other snippet — review before running.
