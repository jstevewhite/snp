# Code review — snp (2026-09-13)

Scope: whole repo at `main` `5888e27` (working tree clean). Reviewed by
reading the spec, the store/server/SPA/desktop/deploy paths, and the
2026-09-09 review in `docs/qwen-review.md`. The suite was not re-run in
this session.

**Verdict:** this is an unusually disciplined single-binary app. The hard
invariants (FTS write order, sync boundary, CSRF, sensitive-body
handling, no wails in `cmd/snp`) are real and mostly implemented. The
holes that remain are concentrated in **import/purge**, **desktop shell
injection**, and a few **offline/sync UX** paths — not in auth or crypto.

---

## What is working well

- **FTS5 external-content discipline is correct** at every write site
  (create insert-only, replace/import-update delete-then-insert, purge
  delete-before-row). Sensitive bodies are indexed as `''` and omitted
  from list/sync.
- **Sync boundary is right:** `server_time` is captured before the read,
  filters use `>=`, client merge is idempotent. Fake-clock tests pin this.
- **CSRF model matches the WhoIs auth:** POST/PUT/DELETE require
  `Content-Type: application/json`, no CORS headers, desktop `CallAPI`
  reproduces the same rule instead of bypassing it.
- **Encryption is the boring correct shape:** AES-256-GCM, random nonce,
  32-byte key at 0600, AAD = snippet ULID.
- **Frontend XSS boundary is real:** notes go through `marked` + DOMPurify;
  highlighted bodies allow only `span`/`class`. Sensitive bodies are not
  written to IndexedDB on the create/replace/sync paths, because those API
  responses omit `body`.
- **Deploy hygiene is above average:** `ProtectSystem=strict`, pre-deploy
  backup with the *previous* binary, schema-aware rollback, architecture
  check before enabling the unit.

The 2026-09-09 review already named the two import bugs. They are still in
the tree. That is the highest-leverage fix.

---

## Bugs

### 1. Nested-folder export → import fails the foreign key

`Export` orders folders by name (`internal/store/export.go:27-28`).
`Import` inserts `doc.Folders` in document order
(`internal/store/import.go:90-111`) against `parent_id REFERENCES
folders(id)`. A child whose name sorts before its parent (`Work` /
`bash`) is inserted first and SQLite returns `FOREIGN KEY constraint
failed`. That error is not wrapped as `ErrImport`, so the API maps it to
500 `"internal error"`.

This is the primary portability path (`snp export` / `snp import` /
`POST /api/import`). Hand-written docs with arbitrary folder order hit
the same hole. `import_test.go` has no nested-folder case, which is why
it survived.

**Fix:** topologically sort, or multi-pass insert (only upsert a folder
whose parent is NULL or already present). Add a regression test whose
child name sorts before the parent.

### 2. Replace-import can orphan a live folder under a tombstone, which
blocks purge forever

Replace mode un-soft-deletes only folders *directly* referenced by live
snippets (`import.go:141-147`). Ancestors stay tombstoned. `UpdateFolder`
already refuses this state (`folders.go:129-133`); Import does not.

`Purge` then does `DELETE FROM folders WHERE deleted_at IS NOT NULL AND
deleted_at <= ?` (`purge.go:63-64`) with undefined row order against the
FK. The whole purge transaction rolls back — snippet trash stops draining
too. `purge_test.go` never creates folders.

Same-second nested deletes can hit the same FK: `DeleteFolder` does not
write `updated_at`, timestamps are whole seconds, so child-then-parent in
one second share `deleted_at`.

**Fix:** revive the full ancestor chain (or reject a missing/deleted
parent, same as `liveFolderTx`). In `Purge`, `PRAGMA
defer_foreign_keys=ON` and/or delete leaves first. Test: nested
export/import; replace-import of a nested folder omitting the parent;
purge after child+parent deleted in the same second.

### 3. Desktop marker injection is dead code

Confirmed against the embed FS: `ReadFile(webembed.FS, "index.html")` is
`file does not exist`; the file is `dist/index.html`.

```
cmd/snp-desktop/main.go:216-219
raw, err := fs.ReadFile(webembed.FS, "index.html")
if err != nil {
    next.ServeHTTP(w, r)
    return
```

Wails `Sub`s `dist/` for the asset handler, but this middleware still
reads the unstripped embed. `window.__SNP_DESKTOP__` is never stamped and
`registerSW.js` is never stripped. `resolveDesktopApp` only waits for the
wails bind when the marker is set (`web/src/lib/desktop.ts:49-51`);
without it, `/api` calls `fetch` the asset server (404), and SW
registration rejects on `wails://` — the failure this code comments as
already seen.

The HTTP bridge itself is fine and tested. The shell never uses it.

**Fix:** `fs.ReadFile(webembed.FS, "dist/index.html")`, or `fs.Sub` first
(same as `server.NewWithAI`). Add a unit test for the rewrite; hoist
`injectDesktopMarker` into `internal/desktop` so it is testable without
wails.

---

## Important

**Import also skips folder invariants** (`import.go:90-111`). No
`uniqueSiblingTx`, no cycle check, no “parent must be live.” Merge can
create duplicate siblings (the invariant `folder_path` relies on) and
A↔B cycles. `UpdateFolder`’s ancestor walk (`folders.go:140-161`) then
has no visited set, so cyclic data loops until `busy_timeout`.

**Import stores `created_at` / `updated_at` verbatim**
(`import.go:211-222`). Sync’s lexicographic `>=` requires RFC3339 UTC
`Z`. A `2021-01-01 00:00:00` or `+00:00` offset can drop a row from
incremental sync or re-send it forever. Parse with the same layout
`now()` emits, or reject.

**After a failed API call, reconnect does not sync.**
`OnlineTracker.online` is `browserOnline && !lastFetchFailed`
(`web/src/lib/online.ts:20-22`). The browser `online` event only sets
`browserOnline`; `lastFetchFailed` stays true, so App’s `if (o) tick()`
never runs and Resync is `disabled={!online}` (`App.svelte:462-465`,
`1062`). Spec §6 says sync on reconnect. Clear `lastFetchFailed` on the
`online` event, and keep Resync clickable when `ready`.

**Sensitive reveal does not restore `var_defaults`.** Reveal stores only
`body` (`App.svelte:746-754`). List/sync rows keep `var_defaults: null`
for sensitive snippets, so the variables panel stays empty after Show
body. After save of a new secret, `revealed[id]` is not updated, so
detail still shows “Show body.” Keep `{ body, var_defaults }` in memory;
never write it to IndexedDB. Also make `putSnippet` / `mergeSyncResponse`
*force* `body` and `var_defaults` to null when `is_sensitive` — today
that invariant is accidental (API omits the fields) and the db test only
inserts an already-null row.

**Offline search can disagree with FTS5** (`web/src/lib/search.ts`). The
server drops terms with no letters/digits and keeps `_` inside tokens;
MiniSearch’s default tokenizer splits on `_` and does not drop
punctuation-only terms. Spec §6: both engines must match the same set.
Mirror `prefixExpr` and add the shared fixture from
`internal/store/search_test.go`.

**`PUT /api/folders/{id}` cannot move a folder to the root.** `parent_id`
is `*string` (`handlers.go:224-227`); JSON `null` and an omitted field
both become `nil`, and the store treats nil as “no change.”

**`snp export -o` writes plaintext sensitive bodies at `0666 & umask`**
(`cmd/snp/main.go:327`). The key file is 0600. Use `O_EXCL` + `0600`.

**`update.sh` has no trap after the fast-forward.** If `make build`
fails, HEAD already equals `origin/main`, so the next timer run sees
`head == remote` and exits 0 (`deploy/update.sh:136-149`,
`autoupdate.sh:27-28`). Trap: `git reset --keep "$prev"` and restore
`bin/snp` from `snp.prev`.

**Linux `.desktop` `Exec=snp-desktop` is not an absolute path**
(`deploy/snp.desktop:7`). GUI sessions often do not have `~/.local/bin`
on `PATH`. Rewrite `Exec=` at install time.

**AI `/tags` and `/explain` do not cap body size** (only generate’s
prompt is 4000 chars). Those fields can be 10 MiB and are forwarded
upstream.

---

## Minor / hardening

- `UpdateFolder` mutates the caller’s `*name` (`folders.go:164`). Trim
  into a local.
- `search.go:187` wraps the SQLite error in `ErrFTS`; 4xx returns
  `err.Error()`, so clients can see engine text.
- `Purge`’s snippet scan never checks `rows.Err()`.
- No `WriteTimeout` on `http.Server` (`cmd/snp/main.go:248-252`).
  `ReadHeaderTimeout` and `IdleTimeout` are set; a stalled write can
  still hold the connection.
- `--ai-key` as a CLI flag (both binaries) puts the key in the process
  table; file/env already exist.
- `handleRawSnippet` has no `X-Content-Type-Options: nosniff`.
- `make test` compiles `cmd/snp-desktop` without `production` / Darwin
  `UniformTypeIdentifiers` / Linux `webkit2_41`. There is no PR test
  workflow, only release builds.
- `install.sh` overwrites the binary and `enable --now` without
  restarting an already-running unit.
- Autoupdate health URL requires the Tailscale CLI unless `-U` is set,
  which contradicts “host does not need a Tailscale daemon.”
- Desktop first-call log includes the raw path+query
  (`internal/desktop/desktop.go:148-151`); the HTTP logger deliberately
  omits query strings.
- Folder delete has no confirm; snippet delete does.
- Modal has no focus trap.
- `LoadOrCreateKey` is not `O_EXCL`; two processes racing on first run
  can write different keys.

---

## Spec notes (not implementation bugs)

- Spec §12 says `make app` always notarizes with default profile
  `snp-notary`. The Makefile defaults it empty and skips notarization.
  The code is the safer behavior; the spec is stale.
- Create/Replace of sensitive snippets omit `body` / `var_defaults`. Spec
  §5 says those fields are null in *list and sync*; GET decrypts. The
  client happens to tolerate POST/PUT omitting them.
- Incremental sync after the 30-day purge cannot emit tombstones for rows
  that no longer exist. A client offline longer than that window keeps
  stale cache until a full resync (already noted in the work log).

---

## Suggested order

1. Fix import parent ordering + ancestor liveness, and make `Purge`
   FK-safe, with the three regression tests above. This is the same B1/B2
   from `docs/qwen-review.md`, still open.
2. Fix `injectDesktopMarker` to read `dist/index.html` and test the
   rewrite. Until that lands, treat the desktop app’s API path as
   unverified.
3. Reconnect-after-fetch-failure, sensitive reveal/`var_defaults`, and
   the MiniSearch tokenizer.
4. Autoupdate rollback trap, export `0600`, folder-to-root, Linux
   `Exec=` path.

I would not treat this as “not ready.” The serving/PWA path is in good
shape. I would not cut another release that advertises nested-folder
import or a working desktop bridge until 1 and 2 are fixed.
