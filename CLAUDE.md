# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`snp` — a single-user snippet manager: one Go binary that embeds a Svelte 5
SPA, joins the tailnet as its own node via `tsnet`, and stores everything in
SQLite with FTS5. No reverse proxy, no container, no external services.

## Working process

This repo is executed against a written spec and phased plan. Read these
before non-trivial work:

- `docs/snp-design.md` — the spec. Code comments and log entries cite it as
  "spec §4" etc. If behavior and spec disagree, one of them is a bug; say
  which.
- `docs/snp-implementation-plan.md` — phases 0–10, each with a "done when".
- `docs/work-log.md` — append-only, newest entry at the bottom. "Current
  status" at the top names the current phase and what is next. **Append an
  entry when finishing a phase or a session**, including gotchas found; the
  log is the resume point after an interruption.

Commit granularity is one commit per plan task, with `make test` green.

## Commands

```sh
make build     # npm ci && vite build → web/dist, then go build -o bin/snp
make test      # go test ./... then (cd web && npm test && npm run check)
make dev       # build, then ./bin/snp serve --dev-listen :8080
```

Go, from the repo root:

```sh
go test ./internal/store/ -run TestFTSMatch -v   # single test
go vet ./...                                     # expected clean
```

Web, from `web/`:

```sh
npm test -- src/lib/query.test.ts   # single Vitest file
npm run check                       # svelte-check + tsc (part of make test)
npm run dev                         # Vite dev server; /api proxies to
                                    # http://localhost:8080 (override
                                    # VITE_API_PROXY), so run `make dev`
                                    # alongside for live API calls
```

Manual smoke against the dev server (no tsnet, no auth, `dev@local`
identity, 127.0.0.1 only — never expose it):

```sh
./bin/snp serve --dev-listen :8080
curl localhost:8080/api/me
```

## Architecture

```
cmd/snp/         subcommand dispatch: serve, backup, export, import, seed, key
cmd/snp-desktop/ wails desktop binary (darwin/linux; spec §12) +
                 platform_{darwin,linux}.go per-OS options
internal/config/ TOML + flags + SNP_* env; precedence flag > env > file > default
internal/store/  SQLite: schema/migrations, CRUD, FTS, crypto, sync, purge, import/export
internal/server/ ServeMux router, middleware chain, handlers, embedded-SPA handler
internal/tsauth/ tsnet node + WhoIs identity; Dev resolver for --dev-listen
internal/desktop/ desktop API bridge (spec §12; deliberately no wails import)
internal/ai/     one-shot OpenAI-compatible snippet generator (spec §13)
internal/starter/ bundled starter snippet pack, applied on demand (spec §5)
web/             Svelte 5 SPA; web/dist is go:embed-ed by web/embed.go
deploy/          systemd unit, install.sh, backup.sh (phase 8); user-unit
                 auto-deploy: update.sh, autoupdate.sh, install-autoupdate.sh,
                 user/*.{service,timer} templates (spec §7)
```

Dependency direction is one way: `cmd` → `server` → {`store`, `tsauth`};
`cmd/snp-desktop` → `desktop` → {`server`, `store`, `tsauth`}. The store
knows nothing about HTTP; the server maps store errors to status codes in
`statusCode()` (`internal/server/server.go`) and never leaks 5xx detail to
the client.

**AI rule** (`internal/ai`): generation is strictly one-shot — never
accumulate history, never include existing snippet content, and never
log prompts, responses, or the API key (the request-logging middleware
already omits bodies). The feature is enabled only when `ai_key` is set
(config); `ai.Endpoint`/`Model`/`APIKey` fields are exported so tests
can point the client at an httptest provider.

**Desktop rule: `cmd/snp` must never import wails.** The desktop app
(`cmd/snp-desktop`, darwin/linux) is a separate binary because wails v2
is a heavy CGO dependency; importing it into `cmd/snp` would break the
server binary's headless Linux cross-builds. `internal/desktop` carries
no wails import either, so its tests run anywhere — wails wiring lives
only in `cmd/snp-desktop/main.go`, which is platform-neutral; per-OS
wails options live in `cmd/snp-desktop/platform_{darwin,linux}.go`
(darwin is a deliberate no-op) and per-OS link flags in the Makefile's
`desktop` target. The `production` build tag is required on every
platform; `-framework UniformTypeIdentifiers` is darwin-only, and Linux
adds the `webkit2_41` tag when pkg-config finds webkit2gtk-4.1. The
desktop binary must be built on its target OS — wails does not
cross-compile.

Both `*store.Store` and the identity resolver are injected, so handler tests
use a real temp-file store plus a `fakeResolver` (`internal/server/server_test.go`),
and store tests inject a `fakeClock` via `SetClock` for sync-boundary and
purge tests.

### Build coupling: web/dist must exist

`web/embed.go` does `//go:embed all:dist`, so a missing `web/dist` breaks a
bare `go build`. A stub `web/dist/index.html` is checked in for that reason
(`.gitignore` ignores `web/dist/*` but negates `index.html`). `make build`
replaces it with the real bundle. `server.New` panics if the embedded FS has
no `dist` subtree.

**Every web build dirties that stub.** It is the only tracked file under
`web/dist`; the hashed assets it names are gitignored. `npm run build`
(hence `make web` and `make build`) rewrites its `index-<hash>.js` and
`index-<hash>.css` references, so committing it after a build points the
checked-in stub at bundle files no fresh clone will have — the SPA then
404s for anyone who runs a bare `go build`. Before committing, stage
source paths explicitly (`git add <paths>`) instead of reaching for
`git add -A`, or restore the stub with `git checkout -- web/dist/index.html`
once the build has done its job. `make test` does not build, so it leaves
the file alone.

## Invariants that bite

These are the things that have already caused bugs. Preserve them.

**FTS5 write ordering** (`internal/store/snippets.go`). `snippets_fts` is
external-content (`content='snippets', content_rowid='rowid'`), so a delete
reads the *current* main-row values back. Deleting an FTS rowid that was
never inserted, or deleting after the main row changed, corrupts the index
and surfaces as `SQLITE_CORRUPT` / "database disk image is malformed".

- Create: `insertFTSTx` only, never a delete first.
- Replace: `deleteFTSTx` **before** the main-row UPDATE, then insert.
- Remove/purge: `deleteFTSTx` before deleting the main row, same tx.
- Recovery: `INSERT INTO snippets_fts(snippets_fts) VALUES('rebuild')`.

**Mirror columns.** `snippets.body_text` and `snippets.tags` exist only so
external-content FTS can read every indexed column back from the main table
(`body` is a BLOB holding ciphertext for sensitive rows). `body_text` is `''`
for sensitive snippets — sensitive bodies are never indexed, online or
offline. `snippet_tags` remains the authoritative tag list; the `tags` column
is a space-joined denormalization, safe only because tag names match
`[a-z0-9][a-z0-9-]*`.

**Timestamps.** Every stored and serialized time is RFC3339 UTC, second
precision, no fractions, always `Z` (`2026-09-02T10:00:00Z`). Sync and
ordering rely on lexicographic TEXT comparison of these strings; any other
format silently breaks both.

**Sync boundary** (`internal/store/sync.go`). `server_time` is captured
*before* the read, so a row written during the read is re-sent next time
rather than lost. Client merges are idempotent, so duplicates are fine.
A refactor that moves the capture fails the fake-clock test.

**CSRF rule** (`guardMW` in `internal/server/server.go`). Identity comes from
the source address via `WhoIs`, not from a credential, so a cross-origin POST
from the owner's browser would otherwise authenticate. Every POST/PUT/DELETE
must carry `Content-Type: application/json` (415 otherwise) and the server
sends no CORS headers at all. Do not add CORS headers, and do not relax the
content-type check.

**Auth surface.** `/api/*` requires the configured `owner` login; the
embedded SPA is served unauthenticated so the shell loads for any tailnet
peer. `owner=""` disables the check and is only for dev mode.

**Encryption** (`internal/store/crypto.go`). AES-256-GCM, key = 32 raw bytes
at `state_dir/key` (0600), AAD = the snippet ULID so a ciphertext cannot be
moved between rows. Toggling `is_sensitive` re-encrypts/decrypts and rewrites
the FTS row. Export files contain plaintext sensitive bodies; a backup is
unreadable without the key file.

**Go `flag` positional args.** Flags must precede positional arguments
(`snp backup [flags] <dest>`) — parsing stops at the first non-flag arg.

## Frontend notes

`web/src/lib/` holds plain TypeScript modules — `api.ts` (typed API
client over fetch, or over the in-process wails desktop bridge when
present — see `desktop.ts`; throws `ApiError`), `query.ts` (the `tag:` /
`lang:` query syntax, shared by online and offline search),
`templates.ts` (`{{var}}` / `{{var|default}}`), `db.ts` (IndexedDB via
`idb`), `sync.ts` (idempotent merge + `server_time`), `search.ts`
(MiniSearch, updated incrementally on merge), `online.ts`, `desktop.ts`
(desktop-shell detection + bridge access; inert in browsers), `time.ts`
(display formatting for the stored timestamps), `keys.ts` (platform-aware
shortcut labels), `clipboard.ts` (clipboard write with a webview fallback),
`sw.ts` (service-worker update check).
Components are `.svelte` files alongside them. Each module has a colocated
`*.test.ts` run by Vitest under jsdom.

Vitest needs `resolve.conditions: ['browser']` in `vite.config.ts` or
`svelte` resolves to its server entry and component tests lose `mount`.

Offline is read-only by design: writes are disabled with a banner and nothing
is queued; sensitive bodies are never written to IndexedDB, so reveal is
disabled offline.
