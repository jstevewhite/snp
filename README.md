# snp

A personal snippet manager. One Go binary on your tailnet; every machine on
the tailnet reaches it through a browser or an installed PWA. Fast full-text
search over everything from a one-liner to a whole script, nested folders,
tags, Markdown notes, `{{template}}` variables, and offline read via PWA.

- Design: [docs/snp-design.md](docs/snp-design.md)
- Implementation plan: [docs/snp-implementation-plan.md](docs/snp-implementation-plan.md)
- Work log: [docs/work-log.md](docs/work-log.md)

## How it works

One binary, `snp`:

- `snp serve` — joins your tailnet as its own node (embedded tsnet; no
  Tailscale daemon needed on the host), gets an HTTPS certificate from
  Tailscale, and serves the web app plus the JSON API on port 443. Access is
  limited to a single configured owner, identified by Tailscale whois — no
  passwords, no reverse proxy, no containers.
- `snp backup <dest>` — consistent database copy (`VACUUM INTO` + integrity
  check), cron-able.
- `snp export [-o file]` / `snp import <file>` — full JSON export/import
  (plaintext bodies).
- `snp seed` — add the bundled starter snippets (a `Starter` folder with a
  copy-pasteable `snp config.toml`, a template example, and a few everyday
  commands). Nothing seeds by itself; it is also a button in the app's
  settings panel.
- `snp key show-path` — prints the encryption key path, for backup scripts.

Storage is SQLite with FTS5 under `state_dir` (default
`~/.local/share/snp`): `snp.db` (the database), `key` (AES-256-GCM key for
sensitive snippets), `tsnet/` (tailscale node state). Sensitive snippets are
encrypted at rest; their bodies are never indexed or stored in the PWA's
local cache.

## Requirements

- A Tailscale tailnet with **MagicDNS** and **HTTPS certificates** enabled
  (admin panel). The Tailscale certificate only validates for the tailnet
  DNS name, so the app must be reached by `https://snp.<your-tailnet>.ts.net`
  — never by a `100.x` IP.
- A systemd Linux host on the tailnet — an always-on machine that stays
  up (this is the design's deployment target).
- To build: Go ≥ 1.24 and Node ≥ 20. Or build once anywhere and ship the
  binary (see below).

## Building

```sh
make build      # builds web/dist (Svelte + Vite), then bin/snp
make test       # go test + Vitest + svelte-check
```

The web app is embedded into the binary with `go:embed`, so `web/dist` must
exist when the Go binary is built; `make build` handles that. To build on
one machine and deploy on another (e.g. an x86_64 server), build the web app
anywhere, then cross-compile:

```sh
cd web && npm ci && npm run build
GOOS=linux GOARCH=amd64 go build -o bin/snp ./cmd/snp
```

## Installing (one command)

On the target host, from a checkout of this repo:

```sh
sudo TS_AUTHKEY=tskey-... ./deploy/install.sh -o your-tailnet-login
```

- `-o` your Tailscale login (e.g. `you@github`) — the only identity allowed
  in. Find it in the admin panel or via `tailscale whois <a-tailnet-ip>` on
  any tailnet machine.
- `TS_AUTHKEY` joins the node to your tailnet the first time (admin panel,
  or `tailscale authkeys`). It is written to
  `/var/lib/snp/.config/snp/authkey` and **kept** there: after the first
  join it is only needed again if the node state is wiped.
- `-n` sets the node name (default `snp`); `-b` points at a prebuilt binary
  (default `./bin/snp`).

The script creates the `snp` user (home `/var/lib/snp`), installs
`/usr/local/bin/snp`, writes the starter config to
`/var/lib/snp/.config/snp/config.toml` (an existing config is kept),
installs and enables the `snp.service` systemd unit, and sets up a daily
backup (cron, 03:17) into `/var/lib/snp/backups` via
`/usr/local/sbin/snp-backup`.

Then check:

```sh
systemctl status snp
journalctl -u snp -f        # watch the first join
```

From any other tailnet machine:

```sh
curl https://snp.<your-tailnet>.ts.net/api/me
# → {"login":"you@github","display_name":"..."}
```

Open `https://snp.<your-tailnet>.ts.net` in a browser.

## Using the PWA

Install it so it works offline:

- **macOS (Chrome/Edge):** toolbar / menu → “Install snp” (or “Add to
  Dock”). Safari: Share → “Add to Home Screen”.
- **iOS:** Safari → Share → “Add to Home Screen”.
- **Android:** Chrome → menu → “Install app”.

Offline: browsing and search work from the local cache; creating, editing,
and deleting are disabled (nothing is queued), and revealing a sensitive
snippet requires a connection. When you come back online the app syncs
automatically. If the server was ever restored from an older backup, use
**Settings → Full resync** to re-cache from scratch.

## Desktop app (macOS & Linux, Wails)

snp also runs as a native desktop app (design §12): the same store and
the same UI in a Wails window, local-only — no tailnet, no HTTP server.
It shares the state dir with the CLI and server variants, so your
snippets, `snp export`, and `snp backup` all see the same data.

```sh
make run-desktop          # builds web + binary, opens the window
make app                  # macOS: builds + signs + notarizes build/snp.app
make run-app              # macOS: opens build/snp.app from Finder/Launchpad
make desktop-install      # Linux: installs to ~/.local (override PREFIX=)
```

- **macOS** needs a CGO toolchain (Xcode CLT) and the web app built
  (`make desktop` does both). The raw binary is `bin/snp-desktop`.
- **Linux** needs the GTK/WebKit dev packages —
  `build-essential pkg-config libgtk-3-dev` plus `libwebkit2gtk-4.1-dev`
  (Ubuntu 24.04+, Debian 13, Fedora 40+) or `libwebkit2gtk-4.0-dev`
  (Debian 12, Ubuntu 22.04). `make desktop` picks the matching wails
  build tag from pkg-config; force it with `make desktop WEBKIT2=` (4.0)
  or `WEBKIT2=webkit2_41` (4.1). `make desktop-install` then puts the
  binary in `~/.local/bin`, the launcher in
  `~/.local/share/applications`, and icons in the hicolor theme — no
  root, and `~/.local/bin` must be on your `PATH`. At runtime the binary
  needs the same GTK/WebKit shared libraries.
- The desktop binary must be built on the OS it runs on: wails links
  against the platform's WebKit/GTK, so it does not cross-compile from
  macOS.
- `make app` (spec §12) packages that binary into `build/snp.app` with
  an Info.plist, your app icon (deploy/appicon.png, falling back to the
  PWA icon), and hardened-runtime codesigning (secure timestamp +
  network-client entitlement, so Ask-AI works and Apple accepts it).
  It can then **notarize and staple** the bundle using a notarytool
  keychain profile (`NOTARY_PROFILE`) and write a distribution-ready
  `build/snp.zip`. Both `SIGN_IDENTITY` and `NOTARY_PROFILE` default to
  empty, so `make app` produces an ad-hoc, un-notarized bundle that runs
  locally with no Apple credentials. To ship it to other machines, create
  the profile once:

  ```sh
  xcrun notarytool store-credentials snp-notary \
    --apple-id <your-apple-id> --team-id <your-team-id> \
    --password <app-specific-password>
  ```

  then build with your own Developer ID:

  ```sh
  make app SIGN_IDENTITY="Developer ID Application: Your Name (TEAMID)" \
           NOTARY_PROFILE=snp-notary
  ```

  Use `SIGN_IDENTITY=-` for an explicit ad-hoc bundle when iterating.
- Config resolution matches the CLI (`--state-dir`, `SNP_STATE_DIR`,
  `--config`, …) except that no `owner` is needed.
- The SPA's API calls run in-process through a bridge
  (`internal/desktop`), so nothing listens on a port.
- On Linux the app is the binary plus a `.desktop` entry — there is no
  bundle to sign. Building the desktop app does not
  affect the server binary — wails stays out of `cmd/snp`, so the
  headless server still cross-compiles for Linux. A Windows port is the
  remaining follow-on.

## Configuration

TOML file, discovered in order: `--config` flag → `$SNP_CONFIG` →
`$XDG_CONFIG_HOME/snp/config.toml` → `~/.config/snp/config.toml`. Every key
also has a flag and an `SNP_*` environment variable; flag beats env, env
beats file:

| Key | Flag | Env | Default |
|---|---|---|---|
| `hostname` | `--hostname` | `SNP_HOSTNAME` | `snp` |
| `owner` | `--owner` | `SNP_OWNER` | (required unless dev mode) |
| `state_dir` | `--state-dir` | `SNP_STATE_DIR` | `~/.local/share/snp` |
| `log_level` | `--log-level` | `SNP_LOG_LEVEL` | `info` |
| `ai_endpoint` | `--ai-endpoint` | `SNP_AI_ENDPOINT` | `https://api.openai.com/v1` |
| `ai_model` | `--ai-model` | `SNP_AI_MODEL` | `gpt-4o-mini` |
| `ai_key` | `--ai-key` | `SNP_AI_KEY` | (AI disabled until set) |

For the systemd service, `HOME=/var/lib/snp`, so the defaults resolve to
`/var/lib/snp/.config/snp/config.toml` and `/var/lib/snp/.local/share/snp`.

## AI snippet generation (optional)

With `ai_key` set (design §13), a **one-shot** "Ask AI…" control appears
in the snippet form: type something like *"give me a command to copy a
file to my home dir"* and it fills the form with a templatized snippet
(`cp -rf {{file}} ~/`) in Body plus a short plain-text explanation of
what it does in Notes, for you to review, tweak, and save like any
other snippet. No chat history, and nothing is stored by the feature
itself.

The control's **Output** selector picks what the model should produce:
**Command** (the default — one executable line), **Script** (a complete
multi-line program, with a shebang for shell scripts and how-to-run
notes), or **Function** (one named function definition, with its
parameters and a call example in Notes). The reply lands in the same
fields either way.

Any OpenAI-compatible endpoint works — OpenAI, or a self-hosted one
(ollama, llama.cpp, …) via `ai_endpoint`:

```toml
ai_endpoint = "http://127.0.0.1:11434/v1"   # e.g. ollama
ai_model    = "llama3.2"
ai_key      = "ollama"                       # any non-empty value
```

When unconfigured the control is hidden; `GET /api/ai/status` reports
whether it is on, and the model name — never the endpoint key.

## Backups

Daily at 03:17, cron runs `/usr/local/sbin/snp-backup` (the installed
`deploy/backup.sh`) as the `snp` user:

```sh
snp-backup DEST_DIR [retention_days]
```

- writes `DEST_DIR/snp-YYYYmmdd-HHMMSS.db` — a consistent `VACUUM INTO`
  copy, verified with an integrity check before the script finishes;
- copies the encryption key alongside as `snp.key` (overwritten each run);
- deletes `snp-*.db` files older than `retention_days` (default 7).

Copying backups offsite (rsync/rclone) is the operator's job, by design.

### Restoring

```sh
sudo systemctl stop snp
sudo cp /path/to/snp-YYYYmmdd-HHMMSS.db /var/lib/snp/.local/share/snp/snp.db
sudo rm -f /var/lib/snp/.local/share/snp/snp.db-wal \
           /var/lib/snp/.local/share/snp/snp.db-shm
# The key must be the one that went with this backup:
sudo cp /path/to/snp.key /var/lib/snp/.local/share/snp/key
sudo chown -R snp:snp /var/lib/snp/.local/share/snp
sudo systemctl start snp
```

After restoring an *older* backup, PWA clients that cached a newer state
should use **Settings → Full resync**.

## Warnings, in plain language

- **The key file is required to read sensitive snippets from a backup.** A
  database backup without its `snp.key` still gives you everything
  non-sensitive, but sensitive bodies are AES-256-GCM ciphertext and are
  unrecoverable without the matching key. Back them up together (the backup
  script does this) and keep them equally safe.
- **Export files are as sensitive as the database plus key.** `snp export`
  and `GET /api/export` write every body in plaintext, including sensitive
  ones. Store and transfer them accordingly.
- **Don't delete `state_dir/tsnet` casually.** It holds the node's tailnet
  membership. Deleting it forces the next start to re-join the tailnet,
  which needs `TS_AUTHKEY` again.
- **`--dev-listen` is unauthenticated.** It serves plain HTTP with no tsnet
  and no auth, for local development only. Never expose it beyond the
  machine.
- **Anyone with the key file and the database can read everything.** The key
  is not derived from a passphrase; that is the accepted threat model.
  Protect `/var/lib/snp/.local/share/snp` (0700, owned by `snp`) and the
  backups.
- **AI requests leave your machine.** Every "Ask AI…" prompt is sent to
  the configured provider (`ai_endpoint`, keyed by `ai_key`). Only the
  prompt you type goes out — no existing snippets, nothing sensitive,
  no history — but pick a provider you trust, or run a self-hosted
  OpenAI-compatible endpoint, and review generated snippets before
  running them.

## Logs

`snp` logs structured lines to stdout; journald captures them:

```sh
journalctl -u snp -f
journalctl -u snp --since "1 hour ago"
```

Request logs include method, path, status, and duration — never query
strings or bodies.

## Development

```sh
make dev        # build + serve on 127.0.0.1:8080 (no tsnet, no auth)
make test       # go test + web tests
```

The API is plain JSON under `/api` (spec §5);
`GET /api/snippets/{id}/raw` returns the decrypted body as `text/plain`,
for `curl | sh`-style use.

## Repository layout

```
cmd/snp/                 main, subcommand wiring
internal/config/         toml + flags + env resolution
internal/store/          sqlite open/migrate, queries, fts, crypto
internal/store/migrations/*.sql
internal/server/         router, middleware, handlers, embedded static
internal/tsauth/         tsnet listener and whois identity
web/                     svelte app; web/dist is embedded
deploy/                  snp.service, install.sh, backup.sh
docs/                    design, plan, work log
Makefile                 build web, build binary, test
```
