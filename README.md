# snp

A personal snippet manager with a Go backend and an embedded Svelte 5 UI.
Run one server on your tailnet and use it through a browser or installed
PWA, or run a local desktop app on macOS/Linux.

Store anything from a one-liner to a whole script, with full-text search,
nested folders, tags, Markdown notes, `{{template}}` variables, and offline
read via PWA.

- Design: [docs/snp-design.md](docs/snp-design.md)
- Implementation plan: [docs/snp-implementation-plan.md](docs/snp-implementation-plan.md)
- Work log: [docs/work-log.md](docs/work-log.md)

![snp in the browser](docs/images/snp-snippet-manager-git-bundle-all-branches-template.png)

*Folders and tags on the left, full-text search over the list, and the open
snippet on the right — here a `git bundle` template with its variables and
the rendered command.*

![a template snippet open](docs/images/template-example.png)

*Templates: each `{{var}}` becomes a field, the Rendered preview fills in
(and saves) your values, and Notes can carry the gotchas.*

## How it works

The server is one binary, `snp`:

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

`cmd/snp-desktop` is a second binary: the same store and UI in a native
macOS/Linux window, local-only — see
[Desktop app](#desktop-app-macos--linux-wails).

Storage is SQLite with FTS5 under `state_dir` (default
`~/.local/share/snp`): `snp.db` (the database), `key` (AES-256-GCM key for
sensitive snippets), `tsnet/` (tailscale node state). Sensitive snippets are
encrypted at rest; their bodies are never indexed or stored in the PWA's
local cache.

## Requirements

### Building from source

- Go **1.26.6 or newer**, as declared in `go.mod`.
- Node.js **24.x** for the web build and tests. The locked test tools also
  support Node 22.12+ within 22.x, or 26+; Node 20 does not satisfy the
  full test toolchain.
- npm and make. Build commands below run from the repository root.

### Server requirements

- A Tailscale tailnet with **MagicDNS** and **HTTPS certificates** enabled
  (admin panel). The Tailscale certificate only validates for the tailnet
  DNS name, so the app must be reached by `https://snp.<your-tailnet>.ts.net`
  — never by a `100.x` IP.
- An always-on systemd Linux host with cron running for the installed
  daily backup. The binary joins the tailnet itself via tsnet.
- Client devices connected to the tailnet under the configured owner's
  identity. The host does not need a separate Tailscale daemon.

The server can be built elsewhere and shipped as a single binary; its
runtime needs neither Go nor Node.js.

### Desktop requirements

- macOS or Linux; no tailnet or configured owner is required.
- To build on macOS: Xcode Command Line Tools for CGO.
- To build on Linux: a C compiler, pkg-config, GTK3 and WebKitGTK development
  packages. The matching GTK/WebKit shared libraries are needed at runtime.
  See the [desktop build guide](docs/desktop.md) for package names and
  platform details.

## Quick start

### Tailnet server

From a checkout on the target Linux host, with the requirements above met:

```sh
make build
sudo TS_AUTHKEY=tskey-... ./deploy/install.sh -o your-tailnet-login
```

Replace the auth key and login with your own values, then open
`https://snp.<your-tailnet>.ts.net` from an owner device on your tailnet.
See [Installing](#installing-one-command) for configuration and verification.

### Local desktop

From a checkout on the Mac or Linux machine where the app will run:

```sh
make run-desktop
```

This builds the web app and desktop binary, then opens the local window.
See [Desktop app](#desktop-app-macos--linux-wails) for packaging and installation.

## Building

```sh
make build      # builds web/dist (Svelte + Vite), then bin/snp
make test       # go vet + Go tests + Vitest + svelte-check/TypeScript
```

The web app is embedded into the binary with `go:embed`, so `web/dist` must
exist when the Go binary is built; `make build` handles that. To build on
one machine and deploy on another (e.g. an x86_64 server), build the web app
anywhere, then cross-compile:

```sh
(cd web && npm ci && npm run build)
GOOS=linux GOARCH=amd64 go build -o bin/snp ./cmd/snp
```

## Installing (one command)

On the target host, from a checkout with `bin/snp` already built for that
host (see [Building](#building)):

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

## Everyday use

Create a snippet with a title and body, then add a language, folder, tags,
and Markdown notes as useful. Search combines text with filters:

| Search | Finds |
|---|---|
| `git bundle` | Snippets matching the search terms |
| `tag:ops` | Snippets tagged `ops` |
| `lang:bash` | Snippets with language `bash` |
| `backup tag:ops lang:bash` | Matches for `backup` with both filters applied |

For a template, put placeholders in the body:

```text
printf '%s\n' "Hello, {{name|world}}"
```

The form detects the template. After saving, fill in `name` in the
variables panel and use the Rendered preview to check the result before
copying. `{{name|world}}` defaults to `world`; `{{name}}` has no default.
A blank variable without a default is copied as an empty string.

**Settings → Add starter snippets** adds a sample config, a template, and
some everyday commands. Applying the starter pack again overwrites edits
to its bundled snippets and restores any you deleted.

## Using the PWA

Install it so it works offline:

- **macOS (Chrome/Edge):** toolbar / menu → “Install snp”.
- **macOS (Safari):** File or Share → “Add to Dock” (macOS Sonoma or
  later; [Apple instructions](https://support.apple.com/en-us/104996)).
- **iOS:** Safari → Share → “Add to Home Screen”.
- **Android:** Chrome → menu → “Install app”.

Offline: browsing and search work from the local cache; creating, editing,
and deleting are disabled (nothing is queued), and revealing a sensitive
snippet requires a connection. When you come back online the app syncs
automatically. If the server was ever restored from an older backup, use
**Settings → Full resync** to re-cache from scratch.

## Desktop app (macOS & Linux, Wails)

snp also runs as a native desktop app (design §12): `snp-desktop` is a
*local instance* — the same store and the same UI in a Wails window, with
no tailnet, no HTTP listener, and no owner.

Desktop and CLI/server instances share data only when they use the same
local `state_dir` with appropriate filesystem access. Your desktop defaults
to `~/.local/share/snp` under your own account; the installed systemd
service defaults to `/var/lib/snp/.local/share/snp` under the `snp` account.
The desktop app does not sync with a remote tailnet server.

- **No server, no port.** The SPA's `/api` calls run in-process through a
  bridge (`internal/desktop`) against the same handler `snp serve` uses,
  so core snippet features work offline. AI actions require access to the
  configured provider, which the desktop process calls directly.
- **No PWA machinery.** Nothing to install and nothing to cache: the
  service worker and offline banner are inert in the window, and
  **Settings → Full resync** is never needed.
- **Configuration matches the CLI** (`--config`, `--state-dir`,
  `--ai-key`, `SNP_*`, …), except that no `owner` is required;
  `--hostname` and `--owner` are accepted and ignored.
- **A plain native window.** 1150×760 by default (minimum 900×560),
  themed before the first paint, with in-app dialogs where the webview
  has no `prompt`/`confirm`.

Build and launch it:

```sh
make run-desktop          # builds web + binary, opens the window
make desktop             # builds web + bin/snp-desktop without launching
```

Install it:

```sh
make app                  # macOS: builds build/snp.app; ad-hoc signed by default
make run-app              # macOS: builds the bundle, then opens it
make desktop-install      # Linux: installs to ~/.local (override PREFIX=)
```

`make app` produces an ad-hoc, un-notarized bundle by default; it needs no
Apple signing credentials for local use. Distribution signing and
notarization require your own `SIGN_IDENTITY` and `NOTARY_PROFILE`.

The desktop binary must be built on its target OS. See the
[desktop build and packaging guide](docs/desktop.md) for Linux packages,
WebKit selection, macOS linking, signing, and notarization. Windows support
is a follow-on.

## Configuration

TOML file, discovered in order: `--config` flag → `$SNP_CONFIG` →
`$XDG_CONFIG_HOME/snp/config.toml` → `~/.config/snp/config.toml`. Every key
also has a flag and an `SNP_*` environment variable; flag beats env, env
beats file:

| Key | Flag | Env | Default |
|---|---|---|---|
| `hostname` | `--hostname` | `SNP_HOSTNAME` | `snp` |
| `owner` | `--owner` | `SNP_OWNER` | (required for server) |
| `state_dir` | `--state-dir` | `SNP_STATE_DIR` | `~/.local/share/snp` |
| `log_level` | `--log-level` | `SNP_LOG_LEVEL` | `info` |
| `ai_endpoint` | `--ai-endpoint` | `SNP_AI_ENDPOINT` | [OpenAI][ai-default] |
| `ai_model` | `--ai-model` | `SNP_AI_MODEL` | `gpt-4o-mini` |
| `ai_key` | `--ai-key` | `SNP_AI_KEY` | (AI disabled until set) |

[ai-default]: https://api.openai.com/v1

`owner` is optional for the desktop app and dev mode.

For the systemd service, `HOME=/var/lib/snp`, so the defaults resolve to
`/var/lib/snp/.config/snp/config.toml` and `/var/lib/snp/.local/share/snp`.

## AI features (optional)

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

Two additional controls work on the current form contents:

- **Suggest tags** sends the body, optional title and language, and the
  collection's existing tag vocabulary to the provider. Suggested tags
  merge into the Tags field.
- **Explain** sends the body to the provider and appends a Markdown
  explanation to Notes.

**Ask AI** sends your prompt, optional language, and the selected output
kind's instructions; it does not include the existing body. Each action
is a single request without chat history. Prompts and bodies are not
logged by snp, and form changes are stored only when you save.

The **Sensitive** flag protects bodies at rest and excludes them from
search indexing and offline caching. It does **not** block Explain or
Suggest tags from sending the current body to the provider, or redact
sensitive text you type into an Ask AI prompt.

![the Ask AI panel in the snippet form](docs/images/snippet-creation-form-ask-ai-go-caddy-ops.png)

*Ask AI… opens at the top of the new-snippet form: describe what you want,
choose an Output, and the reply lands in the fields below it.*

Any OpenAI-compatible endpoint works — OpenAI, or a self-hosted one
(ollama, llama.cpp, …) via `ai_endpoint`:

```toml
ai_endpoint = "http://127.0.0.1:11434/v1"   # e.g. ollama
ai_model    = "llama3.2"
ai_key      = "ollama"                       # any non-empty value
```

When unconfigured the AI controls are hidden; `GET /api/ai/status` reports
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
- **AI actions send content to the configured provider.** Ask AI sends
  your prompt; Explain and Suggest tags send the current snippet body,
  including sensitive bodies. Tag suggestions also send the title,
  language, and collection's tag vocabulary. Use a provider you trust or a
  local OpenAI-compatible endpoint, and review generated snippets before
  running them. See [AI features](#ai-features-optional) for details.

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

```text
cmd/snp/                 main, subcommand wiring
cmd/snp-desktop/         native wails window (macOS/Linux)
internal/config/         toml + flags + env resolution
internal/store/          sqlite open/migrate, queries, fts, crypto
internal/store/migrations/*.sql
internal/server/         router, middleware, handlers, embedded static
internal/tsauth/         tsnet listener and whois identity
internal/desktop/        in-process API bridge (no wails import)
internal/ai/             one-shot snippet generator
internal/starter/        bundled starter snippet pack
web/                     svelte app; web/dist is embedded
deploy/                  snp.service, install.sh, backup.sh
docs/                    design, plan, work log, screenshots
Makefile                 build web, build binary, test
```

## Notes on AI

This began as a project to compare various models and their ability to
produce useful code. I've been a user of Snippetlab, but they dropped out of
SetApp, so I needed a new snippet manager, and decided it was a good test.
So I gave the task to Opus 5, chat gpt Sol, qwen3.8-27b (initially). Opus
made the best out of the gate, but I was blown away by what qwen3.8-27b
produced, running locally, using the deepseek-harness and /goal. Sol's was
prettiest but had weird commentary all over the front page (every option had
an aphorism attached, like *saving your most valuable work*). It was also
enormous.

This program is the one I'm using, and it was initially created by
qwen3.8-27b running on my GX10 (DGX Spark clone), and then polished by
qwen3.8-flash-next (same box) and then deepseek-v4-flash-vision-exp, which
is insanely fast and at this level, incredibly functional. CLAUDE was used
to review code. The AGENTS file has a commit flag - to append harness and
model of all commits.
