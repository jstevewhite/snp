# snp web

Svelte 5 + Vite + TypeScript single-page app for snp (spec §6). Built into
`dist/`, embedded in the Go binary via `web/embed.go` and served at `/`.

## Layout

- `src/lib/` — plain TypeScript modules (api client, query parsing, template
  rendering, IndexedDB store, sync merge, offline search, online status).
  Unit-tested with Vitest (`src/**/*.test.ts`).
- `src/lib/components/` — Svelte components (panes, view, editor, dialogs).
- `src/App.svelte` — three-pane layout (folders/tags, search + results,
  view/editor).

## Commands

Run from this directory:

- `npm run dev` — Vite dev server (for UI development; API calls hit the
  same-origin `/api`, so run the snp server too or proxy).
- `npm run build` — emit `dist/` (the Makefile `web` target does this).
- `npm test` — Vitest unit tests.
- `npm run check` — svelte-check + tsc.

From the repo root, `make test` runs the Go tests plus the web tests and
check; `make build` builds the web app and then the binary.

The checked-in `dist/index.html` is a stub so a bare `go build` works
without a web build; `make build` replaces it with the real app.
