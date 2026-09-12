# snp — Multi-user Plan (database per tailnet login)

Date: 2026-09-12
Status: proposed, not started
Spec: `docs/snp-design.md` (section refs like "spec §3" point there)
Plan: `docs/snp-implementation-plan.md` (phases 0–10; this document adds
phases M0–M6, which follow the same conventions)

## Purpose

Let more than one tailnet user run against a single `snp serve` node, each
with their own snippets, folders, tags, and encryption key, isolated from
every other user. Identity is the Tailscale login that `WhoIs` already
returns; there is no new credential and no new login flow.

This is **isolation only**. Sharing snippets between users, shared
folders, and version history stay out of scope (spec §11). A user who
visits the node sees exactly the store they would see if they ran their
own single-user node.

## Decisions

- **One SQLite database per login, not one database with an owner
  column.** Spec §3 currently sketches the column approach ("add an
  `owner` column on snippets, replace `owner` in config with an allowlist,
  and filter queries by owner"). This plan chooses separate files
  instead, and phase M6 rewrites that spec paragraph. Reasons:
  - The store (`internal/store`) stays untouched. Every query, the FTS5
    write-ordering invariants, the sync boundary, purge, import/export,
    and backup keep working per file with no `WHERE owner = ?` to forget.
    A missed filter in the column design is a cross-user data leak; the
    file design cannot have that bug.
  - One key per user (`spec §4 Encryption`). A leaked key file exposes one
    user, and a user can be handed their own key with their own backup.
  - `snp backup`, `snp export`, and `snp import` already operate on a
    whole database; per-user files make "back up Alice" a path choice
    rather than a new filtered export.
  - Cost: N open SQLite handles and N purger goroutines, which is fine
    for the tens of users a tailnet has, and one more directory level.
- **Layout.** Under `state_dir` (spec §2):

  ```
  tsnet/                     tailscale node state (unchanged)
  users/<login-dir>/snp.db   that user's database (+ -wal, -shm)
  users/<login-dir>/key      that user's 32-byte key, mode 0600
  ```

  `<login-dir>` is the login lower-cased, with every byte outside
  `[a-z0-9._@-]` replaced by `_`, and truncated to 200 bytes. Logins are
  emails or `name@github`-style strings, so this is almost always the login
  verbatim. Two logins that sanitize to the same directory are refused
  at open time (see M1) rather than silently merged.
- **Lazy open with an allowlist.** A user's store is opened on their first
  authenticated request, not at startup. This means the allowlist is the
  only thing standing between "any tailnet peer" and "a new directory on
  disk", so the allowlist is required in multi-user mode and there is no
  "anyone on the tailnet" mode in this phase. A future `users = ["*"]`
  is a one-line change once someone wants it.
- **Config.** `owner` (string) stays and keeps meaning exactly what it
  means today, so every existing config file, `install.sh` invocation, and
  README instruction still works. A new `users` list turns on multi-user
  mode:

  ```toml
  owner = "alice@example.com"                  # single-user (unchanged)
  # or
  users = ["alice@example.com", "bob@example.com"]  # multi-user
  ```

  Setting both is an error. Setting neither is an error outside dev mode,
  as today. Flag `--users a,b` and env `SNP_USERS=a,b` follow the
  spec §2 rule that every key has both.
- **Single-user mode keeps its layout.** With `owner` set, the database
  stays at `state_dir/snp.db` and the key at `state_dir/key`. Nothing
  moves for existing installs, and the desktop app (spec §12) and dev
  mode keep the single-store path. Migrating an existing single-user
  install to multi-user is `mkdir -p users/<login> && mv snp.db key
  users/<login>/`, documented in the README (M6); there is no automatic
  migration, because guessing which login owns the old file is exactly
  the kind of thing that should be a deliberate operator step.
- **The SPA needs no change for isolation.** IndexedDB, the MiniSearch
  index, and `server_time` live in the user's own browser and are fed
  from the user's own database. Two users on the same browser profile
  would share a cache, which is the same situation as two people sharing
  a logged-in Tailscale device and not something the server can fix.
- **Login is the key.** If a user's Tailscale login changes (a new
  identity provider, an org rename), their directory does not follow.
  The operator renames the directory. This is a known one-way door and is
  documented rather than engineered around.

## What changes, by package

| Package | Change | Size |
|---|---|---|
| `internal/config` | `Users []string`; flag/env/file plumbing; mutual-exclusion validation | small |
| `internal/store` | nothing in the store itself; a new `store.Dir` helper for the per-user path layout | tiny |
| `internal/server` | a `Stores` provider interface replacing the single `*store.Store`; `authMW` does allowlist + store lookup; handlers read the store from the context | medium, mechanical |
| `cmd/snp` serve | builds the provider, starts/stops purgers through it | small |
| `cmd/snp` backup/export/import/seed | `--user <login>` flag; backup also gets `--all` | small |
| `internal/desktop` | passes a single-store provider; behavior unchanged | tiny |
| `deploy/backup.sh`, `install.sh` | multi-user backup loop; `-u user,...` install option | small |
| `docs/` | spec §2, §3, §7, §11; README; work log | small |

Nothing in `web/` changes.

## Phase M0 — Config

Goal: the server can be told who is allowed in, either one owner or a list.

- `Config.Users []string`, TOML key `users`, flag `--users` (comma-
  separated), env `SNP_USERS` (comma-separated). Trim whitespace, drop
  empties, reject duplicates.
- Validation in `load`: `owner` and `users` both set → error
  `"config: set owner or users, not both"`. Neither set with
  `requireOwner` → the existing "owner is required" error, reworded to
  mention `users`.
- `Config.MultiUser() bool` returns `len(Users) > 0`.
- `LoadDesktop` ignores `users` the way it ignores `owner`; a desktop
  config with `users` set logs a warning and runs single-user.
- Tests: each source (flag, env, file) for `users`; precedence; the
  mutual-exclusion error; the desktop warning path.

**Done when**: `go test ./internal/config/` covers the new key and
`snp serve --users a,b` gets past config loading.

## Phase M1 — Per-user store paths (`internal/store`)

Goal: one place decides where a user's files live, and refuses
collisions.

- `store.UserDir(stateDir, login string) (string, error)` applies the
  sanitizer from the Decisions section and returns
  `state_dir/users/<login-dir>`. It also writes (or verifies) a
  `login` file inside that directory holding the raw login; a mismatch
  returns `ErrLoginCollision`, which the server maps to 500 and logs at
  error level with both logins. This is the only new store error.
- `store.OpenUser(stateDir, login string) (*Store, error)` is
  `MkdirAll(0700)` + `UserDir` + `Open` + `LoadOrCreateKey` + `SetKey`,
  so every caller (serve, CLI, tests) opens a user the same way. It is
  the per-user twin of `cmd/snp`'s `openStore`, which stays for the
  single-user layout.
- No schema or migration changes. `migrate.go` runs per file as it does
  now, so a new user's database gets the full migration chain on first
  open.
- Tests: sanitizer table (mixed case, `/`, spaces, unicode, a 300-byte
  login); collision detection; `OpenUser` twice returns the same key
  bytes and the same rows.

**Done when**: two `OpenUser` calls for different logins under one temp
dir produce two independent databases and two different keys.

## Phase M2 — Store provider in the server (`internal/server`)

Goal: handlers stop holding a single store and ask the request context
instead. This is the bulk of the work and it is mechanical.

- New interface in `internal/server`:

  ```go
  // Stores maps an authenticated identity to its store. Single-user
  // and desktop deployments return the same store for every identity.
  type Stores interface {
      For(ctx context.Context, id tsauth.Identity) (*store.Store, error)
      // Each calls fn for every currently open store (purger start,
      // shutdown). Order is unspecified.
      Each(fn func(login string, st *store.Store))
      Close() error
  }
  ```

- `SingleStore{st}` implements it by returning `st` always. `New` and
  `NewWithAI` keep their signatures and wrap the given `*store.Store` in
  a `SingleStore`, so `internal/desktop`, the dev-mode path, and every
  existing test compile unchanged. A new `NewMulti(stores Stores,
  resolver, allow []string, log, ai)` constructor is the multi-user entry.
- `MultiStore` (in `internal/server/stores.go`) lazily opens with
  `store.OpenUser`, under a `sync.Mutex` guarding a `map[login]*Store`.
  Open happens with the mutex held: a concurrent first request from the
  same user must not open the file twice (two `Open`s on one SQLite
  file work, but two `LoadOrCreateKey` races could generate two keys,
  with one overwriting the other and the first's ciphertext lost). The
  mutex serializes first-open per node, which is fine because open is
  milliseconds and happens once per user per process. `Close` closes
  every store and clears the map.
- `authMW`: replace the `s.owner` comparison with an `allowed(login)`
  check over a `map[string]bool` built from either `[owner]` or `users`
  (an empty set means the check is disabled, as `owner=""` does today).
  After the check, call `stores.For` and put both the identity and the
  store in the context. A `For` error is logged and answered 500 with
  the generic body, matching the existing `whois failed` branch.
- `storeFrom(ctx) *store.Store` next to `identityFrom`. Every `s.store.`
  in `handlers.go` (16 call sites, listed below) becomes
  `storeFrom(r.Context()).`. A nil store in a handler is a programming
  error and panics into `recoverMW`, which is the right failure mode.

  ```
  handleListSnippets   handleCreateSnippet   handleGetSnippet
  handleReplaceSnippet handleDeleteSnippet   handleRawSnippet
  handleListFolders    handleCreateFolder    handleUpdateFolder
  handleDeleteFolder   handleListTags        handleSync
  handleExport         handleImport          handleSeed
  handleAISuggestTags (ListTags for the tag vocabulary)
  ```

- `loggingMW` already logs the login; no change.
- Tests (`server_test.go`):
  - `newTestServer` keeps its shape (it builds a `SingleStore`), so the
    existing suite is the regression net for the refactor.
  - `newMultiTestServer(t, users []string)` with a `switchableResolver`
    whose identity the test changes between requests.
  - Alice creates a snippet; Bob lists and gets `[]`; Bob gets Alice's id
    and receives 404; Alice's `/api/sync` `server_time` and Bob's are
    independent.
  - A login not in `users` gets 403 with the existing JSON body.
  - Alice's sensitive snippet raw body is readable by Alice and 404 for
    Bob (exercises per-user keys, not just per-user rows).
  - Concurrency: 50 goroutines hit `/api/me` as a fresh login at once;
    exactly one database and one key file exist afterwards.
  - Collision: two logins that sanitize identically; the second gets 500
    and the log line names both.

**Done when**: `go test ./internal/server/` green, including the
multi-user cases, and `go vet ./...` clean.

## Phase M3 — Serve wiring and purgers (`cmd/snp`)

Goal: `snp serve --users a,b` works end to end on a tailnet.

- `runServe` builds `SingleStore` or `MultiStore` from `cfg.MultiUser()`.
  Dev mode (`--dev-listen`) stays single-store with the dev identity,
  because dev mode has no real logins to key on.
- Purgers. `StartPurger` is per store today and started once in
  `runServe`. In multi-user mode the `MultiStore` starts a purger when it
  opens a store, holding the serve `ctx` and logger given at
  construction, and records each purger's done channel. `Close` cancels
  nothing itself: `runServe` calls `stop()` first (as now), then
  `stores.Wait()` (a new method on `MultiStore`; `SingleStore` returns
  the single done channel's close) and only then `stores.Close()`. The
  ordering comment in `runServe` about "database is closed" mid-purge
  applies to every store and moves onto `Wait`.
- Log lines: `store opened` with `login` on first open (info); the
  `listening` line adds `mode=single|multi` and `users=<count>`.
- `/api/me` is unchanged. A follow-on could add `"multi_user": true` so
  the SPA can show the login in the header, but nothing depends on it.
- Tests: `cmd/snp/main_test.go` gets a serve-flag test that a config with
  `users` builds a `MultiStore` (via a small `buildStores(cfg)` helper
  that is the testable core) and that dev mode ignores `users`.

**Done when**: on a real tailnet, two logins in `users` each see their own
empty store, and `state_dir/users/` holds one directory per login that
has connected; shutdown under load logs no `database is closed`.

## Phase M4 — CLI subcommands

Goal: backup, export, import, and seed work per user.

- `--user <login>` on `backup`, `export`, `import`, and `seed`. Required
  when the config is multi-user; an error when the config is single-user
  ("--user is only valid with users = [...]"). `openStore` grows a
  `login` argument and dispatches to `store.OpenUser` or the single-user
  path. The Go `flag` positional rule still holds: `snp backup --user
  alice@example.com <dest>`.
- `snp backup --all <dest-dir>` writes `<dest-dir>/<login-dir>/snp.db`
  for every directory under `users/` and copies each key alongside as
  `key`, running `quick_check` on each. It iterates the filesystem, not
  the allowlist, so a user removed from `users` still gets backed up
  until the operator deletes their directory.
- `snp users` (new, tiny): lists `users/*/login` contents with database
  size and last-modified time, so an operator can see who has connected.
  This is the only new subcommand.
- `snp key show-path` takes `--user` the same way and prints that user's
  key path.
- Tests: `backupCmd` with `--all` over a temp state dir holding two users
  produces two verified copies; `--user` on a single-user config errors.

**Done when**: `snp backup --all` and `snp export --user` round-trip a
two-user state dir through `snp import --user`.

## Phase M5 — Deploy scripts

Goal: the systemd deployment supports multi-user without hand edits.

- `deploy/install.sh`: `-u login,login,...` writes `users = [...]` instead
  of `owner`. `-o` and `-u` are mutually exclusive; one is required.
- `deploy/backup.sh`: detect `users/` under the state dir. If present,
  run `snp backup --all` into `DEST_DIR/snp-YYYYmmdd-HHMMSS/` and prune
  directories by age; otherwise keep the current single-file behavior
  byte for byte. The retention logic moves into a function used by both.
- `deploy/snp.service`: no change; `StateDirectory` already covers the
  new subtree.
- Tests: there are no shell tests today; `bash -n` on both scripts plus a
  manual run against a two-user state dir, recorded in the work log.

**Done when**: a fresh `install.sh -u a,b` boots, and a cron `backup.sh`
run produces per-user copies.

## Phase M6 — Docs

Goal: the spec describes what the code does.

- Spec §2: the `users` key and the `users/<login-dir>/` layout.
- Spec §3: replace the "Multi-user later" paragraph with the file-per-user
  design and the allowlist rule. Keep the CSRF rules verbatim.
- Spec §7: backup and install changes; the login-rename caveat.
- Spec §11: remove "multi-user" from out of scope; keep "sharing".
- README: a "Multiple users" section with the config example, the
  migration `mv`, and the backup change. Update the config table.
- `CLAUDE.md`: "What this is" drops "single-user"; the Auth surface
  invariant mentions the allowlist; a new invariant: "**Store per
  request.** Handlers must take the store from the request context, never
  from a field. A handler that reaches a package-level or struct-level
  store in multi-user mode serves the wrong user's data."
- Work log entry with gotchas found.

**Done when**: `grep -n "single-user" docs/ README.md CLAUDE.md` names only
the desktop app and dev mode.

## Order and estimate

The phases are ordered so that each commit leaves the single-user server
working. M0 and M1 are independent and can land first in either order;
M2 depends on both; M3 on M2; M4 and M5 on M1 and M3; M6 last.

| Phase | Effort |
|---|---|
| M0 config | 1–2 h |
| M1 store paths | 1–2 h |
| M2 server provider + tests | 4–6 h |
| M3 serve wiring + purgers | 2 h |
| M4 CLI | 2–3 h |
| M5 deploy | 1–2 h |
| M6 docs | 1–2 h |

Roughly two working days including the tailnet smoke test.

## Risks and how the plan handles them

- **Wrong store served to a user.** The only way this happens is a
  handler holding a store outside the request. M2 removes the `store`
  field from `Server` entirely, so the compiler catches a stray
  reference, and the CLAUDE.md invariant catches future ones.
- **Two keys for one user.** The first-open race in M2 is serialized
  under the provider mutex, and the concurrency test proves it.
- **Purge across close.** Every store's purger is waited on before any
  close, extending the existing single-store rule.
- **Unbounded state on disk.** Lazy open only after the allowlist, and
  the allowlist is mandatory in multi-user mode.
- **Login collisions after sanitizing.** The `login` marker file refuses
  the second login rather than merging two users.
- **Existing installs.** `owner` is untouched and the single-user layout
  is untouched, so nothing changes for anyone who does not set `users`.

## Explicitly not in this plan

- Sharing, shared folders, or an admin who can read every store.
- Any tailnet-wide "everyone allowed" mode (one line later, if wanted).
- Automatic migration of a single-user install into `users/`.
- Per-user AI configuration; the one `ai_key` serves every user, which
  matches the one-shot, no-history rule in spec §13 since no user content
  crosses between users through it.
- Windows or desktop multi-user; the desktop app stays single-user by
  design (spec §12).
