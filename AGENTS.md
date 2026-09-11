# AGENTS.md

`snp` — single-user snippet manager: one Go binary embedding a Svelte 5 SPA,
joins the tailnet as its own node via `tsnet`, stores everything in SQLite
with FTS5. No reverse proxy, no containers, no external services.

OpenCode reads this file and ignores `CLAUDE.md`; `CLAUDE.md` carries the
same guidance for Claude Code and is updated in the same docs commits —
change a rule here, update it there too.

## Working process

This repo is executed against a written spec and phased plan. Read before
non-trivial work:

- `docs/snp-design.md` — the spec; code comments cite it ("spec §4"). If
  behavior and spec disagree, one of them is a bug; say which.
- `docs/snp-implementation-plan.md` — phases 0–9, each with a "done when".
- `docs/work-log.md` — append-only, newest at bottom; "Current status" at
  top names the current phase. **Append an entry when finishing a phase or
  session**, including gotchas found; it is the resume point.

Commit granularity is one commit per plan task, with `make test` green.

- git: when you commit, suffix **harness**:**model** - e.g.
  - opencode:qwen3.8-next-flash
## Commands

```sh
make build        # npm ci && vite build → web/dist, then go build -o bin/snp
make test         # go vet → go test ./... → (cd web && npm test && npm run check)
make dev          # build, then ./bin/snp serve --dev-listen :8080
make desktop      # macOS/Linux wails app → bin/snp-desktop (CGO required)
make desktop-install # Linux: install binary + .desktop + icons to ~/.local
make app          # signed macOS bundle → build/snp.app (override: SIGN_IDENTITY=…)
```

Focused checks — Go from repo root, web from `web/`:

```sh
go test ./internal/store/ -run TestFTSMatch -v   # single Go test
go vet ./...                                     # expected clean
npm test -- src/lib/query.test.ts                # single Vitest file
npm run check                                    # svelte-check + tsc
```

Dev-server smoke (no tsnet, no auth, `dev@local` identity, 127.0.0.1 only —
never expose it): `./bin/snp serve --dev-listen :8080 && curl localhost:8080/api/me`

## Architecture

```
cmd/snp/          subcommands: serve, backup, export, import, seed, key
cmd/snp-desktop/  wails desktop binary (darwin/linux, spec §12) +
                  platform_{darwin,linux}.go per-OS options
internal/config/  TOML + flags + SNP_* env; precedence flag > env > file > default
internal/store/   SQLite: migrations, CRUD, FTS, crypto, sync, purge, import/export
internal/server/  ServeMux router, middleware, handlers, embedded-SPA handler
internal/tsauth/  tsnet node + WhoIs identity; Dev resolver for --dev-listen
internal/desktop/ desktop API bridge (spec §12; deliberately no wails import)
internal/ai/      one-shot OpenAI-compatible snippet generator (spec §13)
internal/starter/ bundled starter snippet pack, applied on demand (spec §5)
web/              Svelte 5 SPA; web/dist is go:embed-ed by web/embed.go
deploy/           systemd unit, install.sh, backup.sh, make-app.sh,
                  snp.desktop + install-desktop.sh (Linux desktop)
```

Dependency direction is one-way: `cmd` → `server` → {`store`, `tsauth`};
`cmd/snp-desktop` → `desktop` → {`server`, `store`, `tsauth`}. The store
knows nothing about HTTP; the server maps store errors via `statusCode()`
(`internal/server/server.go`) and never leaks 5xx detail to the client.

Tests inject dependencies: handler tests use a real temp-file store plus a
`fakeResolver` (`internal/server/server_test.go`); store tests inject a
`fakeClock` via `SetClock` for sync-boundary and purge tests. AI tests point
`ai.Endpoint`/`Model`/`APIKey` (exported fields) at an httptest provider.

### Build coupling: web/dist must exist

`web/embed.go` does `//go:embed all:dist`, so a missing `web/dist` breaks a
bare `go build`. A stub `web/dist/index.html` is checked in for that reason.
`make build` replaces it with the real bundle. `server.New` panics if the
embedded FS has no `dist` subtree.

## Invariants that bite

These have already caused bugs. Preserve them.

**FTS5 write ordering** (`internal/store/snippets.go`). `snippets_fts` is
external-content (`content='snippets', content_rowid='rowid'`), so a delete
reads the *current* main-row values back. Deleting a never-inserted FTS
rowid, or deleting after the main row changed, corrupts the index
(`SQLITE_CORRUPT`). Create: `insertFTSTx` only, never a delete first.
Replace: `deleteFTSTx` **before** the main-row UPDATE, then insert.
Remove/purge: `deleteFTSTx` before deleting the main row, same tx.
Recovery: `INSERT INTO snippets_fts(snippets_fts) VALUES('rebuild')`.

**Mirror columns.** `snippets.body_text`/`snippets.tags` exist only so
external-content FTS can read indexed columns back (`body` is a BLOB,
ciphertext for sensitive rows). `body_text` is `''` for sensitive snippets —
sensitive bodies are never indexed. `snippet_tags` is authoritative; the
`tags` column is a space-joined denormalization, safe only because tag names
match `[a-z0-9][a-z0-9-]*`.

**Timestamps.** Every stored/serialized time is RFC3339 UTC, second
precision, always `Z` (`2026-09-02T10:00:00Z`). Sync and ordering rely on
lexicographic comparison; any other format silently breaks both.

**Sync boundary** (`internal/store/sync.go`). `server_time` is captured
*before* the read so a row written during the read is re-sent next time.
Client merges are idempotent. Moving the capture fails the fake-clock test.

**CSRF rule** (`guardMW` in `internal/server/server.go`). Identity comes
from the source address via `WhoIs`, not a credential, so a cross-origin
POST from the owner's browser would otherwise authenticate. Every
POST/PUT/DELETE must carry `Content-Type: application/json` (415 otherwise)
and the server sends no CORS headers at all. Do not add CORS headers; do not
relax the content-type check.

**Auth surface.** `/api/*` requires the configured `owner` login; the SPA is
served unauthenticated so the shell loads for any tailnet peer. `owner=""`
disables the check and is dev-only.

**Encryption** (`internal/store/crypto.go`). AES-256-GCM, key = 32 raw bytes
at `state_dir/key` (0600), AAD = the snippet ULID so ciphertext cannot move
between rows. Toggling `is_sensitive` re-encrypts and rewrites the FTS row.
Exports contain plaintext sensitive bodies; backups are unreadable without
the key file.

**AI rule** (`internal/ai`). Generation is strictly one-shot: never
accumulate history, never include existing snippet content, never log
prompts, responses, or the API key. Enabled only when `ai_key` is set.

**Go `flag` positional args.** Flags must precede positional args
(`snp backup [flags] <dest>`) — parsing stops at the first non-flag arg.

**Desktop rule: `cmd/snp` must never import wails.** Wails v2 is heavy CGO;
importing it into `cmd/snp` would break headless Linux cross-builds of the
server binary. `internal/desktop` carries no wails import either (wiring
lives only in `cmd/snp-desktop/main.go`, which is `darwin || linux`).
Platform-specific wails options live in `cmd/snp-desktop/platform_*.go`
(darwin is a no-op; linux sets GPU policy, GTK program name and window
icon) and platform link flags in the Makefile — do not put `uname` or
OS conditionals in `main.go`. `make desktop` needs the wails `production`
tag on every platform (already in the Makefile; without it wails runs a
stub that refuses to start); on macOS it also needs
`CGO_LDFLAGS="-framework UniformTypeIdentifiers"` (already in the
Makefile, darwin-only — wails v2.15.0 references UTType without linking
it against the macOS 26 SDK) and on Linux the GTK/WebKit dev packages
plus the `webkit2_41` tag when pkg-config finds webkit2gtk-4.1 (the
Makefile derives both). The desktop binary must be built on its target OS
— wails does not cross-compile. `./bin/snp-desktop --debug` logs startup
phase timing and forwards page errors. Kill stale instances with
`pkill -x snp-desktop` (exact name — `-f` matches any command containing
the substring); a stale instance holding the state dir stalls the store
open for ~40s.

## Frontend notes

`web/src/lib/` holds plain TS modules with colocated `*.test.ts` (Vitest +
jsdom): `api.ts` (typed client; switches to the wails bridge via
`desktop.ts` when present; throws `ApiError`), `query.ts` (`tag:`/`lang:`
syntax shared by online and offline search), `templates.ts`
(`{{var}}`/`{{var|default}}`), `db.ts` (IndexedDB via `idb`), `sync.ts`
(idempotent merge + `server_time`), `search.ts` (MiniSearch, incremental on
merge), `online.ts`, `desktop.ts` (desktop-shell detection; inert in
browsers).

Vitest needs `resolve.conditions: ['browser']` in `vite.config.ts` or
`svelte` resolves to its server entry and component tests lose `mount`.
In `@testing-library/svelte`, `getByText` THROWS when the element is absent —
use `queryByText` for negative assertions.

Offline is read-only by design: writes are disabled with a banner, nothing
is queued; sensitive bodies are never written to IndexedDB, so reveal is
disabled offline.

`npm run dev` (Vite) proxies `/api` to `http://localhost:8080` (override
`VITE_API_PROXY`) — run `make dev` alongside for live API calls.
