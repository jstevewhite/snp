# Code review — snp (2026-09-09)

Scope: whole repo (~8.1k lines Go, ~5.0k lines web TS/Svelte), reviewed by
reading the store, server, tsauth, config, ai, desktop, cmd, web `lib/` +
`App.svelte`, Makefile, and deploy scripts, and by executing the suites
plus throwaway repro tests (since deleted).

Baseline verified green at HEAD (`7014ab5`):

- `go vet ./...` — clean
- `go test ./...` — all packages pass
- `npm test` — 173/173 Vitest pass
- `npm run check` — svelte-check 0 errors, 0 warnings

Overall: this is an unusually well-disciplined codebase. The invariants
documented in AGENTS.md/CLAUDE.md match the code, the FTS write discipline
is correct everywhere I traced, and the crypto/sync/CSRF designs are sound.
I found **two real bugs, both in `Import`**, both confirmed by execution.

---

## Bugs

### B1 (High): export → import round-trip fails on folder foreign keys

`Export` orders folders by name (`ORDER BY name COLLATE NOCASE`,
export.go:28), but `Import` inserts `doc.Folders` **in document order**
(import.go:86-108) and `folders.parent_id` is a plain `REFERENCES folders(id)`
(0001_init.sql). When a child's name sorts before its parent's, the child
row is inserted first and the FK fails.

Confirmed by executing against the real store (then removed):

```
export lists child "bash" BEFORE parent "Work"
ROUND-TRIP IMPORT FAILED: FOREIGN KEY constraint failed (787)
```

Any nested-folder database whose child sorts alphabetically before its
parent cannot be restored via `snp import` or `POST /api/import` — the
primary data-portability path. It is not just an export bug: hand-written
or third-party docs have arbitrary order too, so the fix belongs in
`Import`, not only in `Export`:

- simplest: two-phase or repeated-pass insert (loop inserting folders whose
  parent is already live/NULL until no progress), or
- topologically sort `doc.Folders` before the upsert loop.

`import_test.go` has no nested-folder case — that is why this survived.

### B2 (Medium-High): replace-import can orphan a folder under a tombstone, which blocks the daily purge forever

The replace-mode restore (import.go:136-143) un-soft-deletes only folders
*directly referenced by live snippets* — one level. Its ancestors are left
tombstoned, producing a live folder whose parent is soft-deleted (and in
merge mode, INSERT/UPDATE of a folder doesn't require the parent to be
live either — FK is satisfied by mere row existence).

The consequence is worse than a broken tree: the expired tombstone parent
is purged while the live child still references it → FK violation → the
**whole `Purge()` transaction rolls back and keeps failing every day** —
snippet trash never purges again either. This is exactly the hazard the
comment at folders.go:129-133 in `UpdateFolder` warns about; `Import`
doesn't enforce the same rule.

Confirmed by executing (a replace doc containing a snippet in nested
folder `Work/bash` but omitting `Work`):

```
live: bash parent=0x…   (Work soft-deleted, bash live under it)
PURGE BLOCKED BY ORPHAN: FOREIGN KEY constraint failed (787)
```

Fix: during import folder upserts, either walk and revive the full parent
chain, or reject folders whose (new) parent is missing/deleted — the rule
`liveFolderTx` already applies to snippet references.

### Notes on both

- The purge ordering that *looks* risky is actually safe today:
  `DeleteFolder` refuses when live children/snippets exist, so tombstones
  always expire child/trash first, and `Purge` deletes snippets before
  folders. B2 is what breaks that invariant; a regression test for
  "purge after nested-folder lifecycle" would lock it in.
- Suggested permanent tests: import with child-before-parent order;
  replace-import restoring a nested folder then `Purge` with a fake clock.

---

## Smaller issues / hardening notes

- **`UpdateFolder` upward walk (folders.go:141-161)** loops `for cur :=
  *newParent` and only exits on `ErrNoRows`, `NULL` parent, or hitting
  `id`. That's fine for data it created, but pre-existing cyclic data
  (corruption, or B2-class states after a future fix) would loop forever
  instead of erroring. A visited-set or depth cap would make it total.
- **`UpdateFolder` mutates its caller's `*name`** (folders.go:164,
  `*name = strings.TrimSpace(*name)`) — a visible side effect on a handler
  input. Trim into a local instead.
- **No `WriteTimeout` on `http.Server`** (main.go:240): `MaxBytesReader`
  caps request size but not duration; a stalled client can hold a
  connection open indefinitely. Tailnet-only exposure makes this minor,
  but `WriteTimeout` (or per-route deadlines) would close the loop on the
  otherwise careful slow-loris hygiene.
- **`--ai-key` as a CLI flag** (both binaries) puts the API key in shell
  history and the process table. File/env paths already exist; consider
  noting in README that the flag is the least safe of the three sources.
- **`cmd/snp-desktop` is the only untested package** — understandable
  (wails), but `injectDesktopMarker` is pure string logic behind the
  darwin tag; hoisting it into `internal/desktop` would let the marker /
  registerSW-strip behavior be tested cross-platform.
- **Untracked `snp.bundle`** (1.3 MB) sitting in the repo root and a
  modified `deploy/appicon.png` — housekeeping before the next commit.
- **README says "Go ≥ 1.24"** but `go.mod` declares `go 1.26.6`; the tool
  chain must actually be ≥ 1.26.6.

## Doc fixes applied during this review

- `AGENTS.md` and `CLAUDE.md` claimed `npm run dev` has *no* `/api` proxy;
  `vite.config.ts` has proxied `/api` → `localhost:8080` (override
  `VITE_API_PROXY`) since phase 7. Both files updated to match.

---

## Strengths worth preserving

- **FTS5 external-content discipline** is correctly implemented at all
  five write sites (create/replace/remove/purge/import) with the
  delete-before-update rule documented at each site, plus a rebuild
  recovery path.
- **Timestamp + sync boundary design**: whole-second RFC3339 UTC,
  `server_time` captured before the read, `>=` filters, idempotent client
  merge — subtle, correct, and pinned by fake-clock tests.
- **The CSRF/content-type guard** is the right model for address-based
  identity, and the desktop bridge deliberately reproduces it instead of
  bypassing it.
- **Shutdown discipline**: purger drained before store close in both
  `serve` and the desktop app; busy_timeout-before-WAL pragma ordering.
- **`devListenAddr` refusing non-loopback binds** is exactly the kind of
  footgun-prevention that belongs in code, not docs.
- **Test architecture** (injected `Clock`, resolver interfaces, httptest
  AI provider) makes the tricky parts testable everywhere; 173 web tests
  including `@testing-library/svelte` component tests.
- **Deploy hygiene**: `install.sh` checks architecture before enabling,
  unit has `TimeoutStartSec=600` for the interactive first join, backups
  copy the key only after a verified DB copy.

## Suggested next steps (priority order)

1. Fix B1/B2 in `Import` (parent ordering + ancestor liveness), add the
   two regression tests noted above.
2. Consider `defer_foreign_keys` + child-first folder deletion in
   `Purge` as belt-and-braces for tombstone-tie cases.
3. Harden `UpdateFolder` walk termination; drop the `*name` mutation.
4. Add `WriteTimeout`; move `injectDesktopMarker` to a testable package.
5. README Go-version line and repo-root cleanup (`snp.bundle`).
