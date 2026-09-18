# UI review + refresh — 2026-09-18

Scope: the whole SPA at `main` `18dd4b5`, on branch `ui-refresh`. Reviewed
twice over: once by reading `web/src/App.svelte`, the components and
`app.css` against spec §6, and once visually — the app built, served with
`serve --dev-listen` against a seeded store, and driven in headless
Chromium at 1440×900 / 900×700 / 390×844 in light and dark, with
screenshots plus computed-style/geometry measurement of every finding
(the measurements quoted below are from that pass, not estimates).

**Verdict:** the layout system was already disciplined — real token
palette, a consistent 14px rhythm, pixel-exact theme parity, and a
correctly built compact mode. The weaknesses were concentrated in five
places, all fixable without touching information architecture:

1. **No surface hierarchy.** Topbar, folders pane, list and detail all
   rendered on the same `--bg` (`#ffffff` light / `#16171d` dark);
   `--bg-alt` was defined and never used on a surface. Separation was
   carried entirely by 1px hairlines.
2. **Selection/active states below the perceptual floor, in three
   recipes.** Selected snippet rows: 8% accent tint; folder rows: 12%;
   tags: 14%. The command palette's active row — the target of Enter in
   the keyboard surface — was `--bg-alt` on `--bg`, ~1.07:1. A disabled
   command's reason rendered at 2.6:1.
3. **Two WCAG AA failures, theme-dependent.** White on the light
   accent `#aa3bff` = 4.39:1 (every filled primary action); white on
   the dark accent `#c084fc` = 2.64:1 (modal primary, active tag
   count). Light `--syn-comment` on `--code-bg` was 4.09:1.
4. **Touch targets stopped at the drawer's edge.** Compact sizing
   reached the list rows, topbar and copy buttons, but the drawer's
   folder/tag/favorite rows stayed at 26–29px, and the phone detail's
   Edit / Save defaults / pin / delete at ~30px — below the 44px
   standard the same screens apply elsewhere. The ⌘K hint rendered on
   touchscreens, consuming ~30px of a tight search field.
5. **Effectiveness gaps.** The detail pane's empty state floated
   top-left with a dead-end sentence. The settings popover closed only
   via its gear (no Escape, no outside click — found live while driving
   the UI: a click on the delete button behind it was intercepted by
   the panel). The editor's Save/Cancel sat below the fold behind a
   fixed-14-row textarea. Code wrapped mid-expression at narrow panes
   (`white-space: pre-wrap`). And a real layout bug: the section
   `pane.detail` and the read-view div both matched the `.detail`
   padding rule, double-padding the pane (36px of inset, and the sticky
   form bar pinned 18px off the pane's bottom).

Also noted: the 🔒 emoji was the app's only colored glyph amid flat
monochrome SVGs; left-column headings sat at three sizes and three
x-offsets (13/11/12px at x=13/7/12); the delete modal put the
destructive action in the position conventionally given to the safe
one.

## What changed (branch `ui-refresh`)

All CSS-level plus small markup changes; no information-architecture
or behavior changes beyond popover dismissal. `npm test` (368) and
`npm run check` green throughout.

- **Tokens.** `--accent` darkened to `#9a2fe6` in light themes (5.35:1
  on `--bg`, up from 4.39); new `--on-accent` / `--on-danger` name the
  text color for filled accent/danger surfaces per theme (`#fff` in
  light; the theme's own `--bg` where the accent is light — dark
  filled actions go from 2.64:1 to ~6.8:1). Light `--syn-comment` to
  `#63707d` (4.56:1 on `--code-bg`). Solarized-dark's accent/danger
  lifted to `#35a0e0` / `#ef6e88` and kimbie's danger to `#f2536e` so
  every theme clears AA both as text and as a fill.
- **Surfaces.** The topbar and the folders pane (wide and as the
  compact drawer) sit on `--bg-alt`; the content panes stay on `--bg`.
  One rule, all seven themes.
- **One selection recipe.** Selected rows everywhere (list, folder
  tree, favorites, tags) and the palette's active row: 12% accent tint
  + accent border + 2px inset accent edge on the leading side. Hover
  is the inverse surface of the pane the row sits on. Button/row state
  transitions at 120ms, all disabled under `prefers-reduced-motion`.
- **Palette.** Active row uses the selection recipe; a disabled
  command's reason is full-contrast (the row dims, not the reason);
  three presentation-only group headers (Snippet / Folder / App)
  appear where the group changes — `role="presentation"`, so
  `role=option` queries and screen readers see only the options.
- **Compact.** Touch sizing extends to the drawer (folder rows 44px;
  tag/favorite/controls 40px), the phone detail (Edit, Save defaults,
  Show all, Reveal, pin, delete 40px), and the settings sheet's
  actions. The ⌘K kbd hint hides under `(pointer: coarse)`, giving the
  search field its padding back.
- **Effectiveness.**
  - The detail empty state fills and centers, with a "Create a
    snippet" CTA (label chosen so `getByText('New snippet')` stays
    unambiguous).
  - The settings popover closes on Escape and on a pointer-down
    outside itself; the gear gets an accent-border open state (the
    `class:open` was already emitted and unconsumed).
  - The editor's Save/Cancel row is sticky at the pane's bottom edge;
    the pane's accidental double padding is removed
    (`.pane.detail { padding: 0 }` — the read view's own `.detail`
    padding remains, so detail content moves 18px closer to the pane
    edge, matching the toolbar columns).
  - Code boxes render `white-space: pre` + `overflow-x: auto`
    (Notes' `pre` already did); a 70-char line keeps its shape at
    900px-wide panes instead of folding mid-expression.
- **Polish.** The 🔒 emoji is now a flat SVG lock in both the list row
  and the sensitive notice (same stroke language as pin/trash); the
  sensitive notice drops its draft-suggesting dashed border for the
  quiet surface; Reveal ("Show body") carries the copy-button CTA
  treatment; left-column headings unify at 12px and align on one x;
  thin theme-aware scrollbars; one accent focus treatment (ring for
  buttons, border+ring for inputs); a small accent dot finishes the
  wordmark; modal/palette entrance animations and a 3px backdrop blur,
  all off under reduced motion; the modal's actions visually order
  safe-first (Cancel left, commit right) via `row-reverse` — DOM and
  Tab order, and the spec §6 "initially focus the cancel control"
  behavior, are untouched.

## Verification

- `npm test` 368 passed, `npm run check` 0 errors / 0 warnings.
- 24 live-DOM checks in headless Chromium against the rebuilt binary —
  surfaces, heading alignment, the selection recipe on every row type,
  palette tint/headers/option-count, modal button order and initial
  focus, gear open state, popover Escape/outside-click dismissal,
  `pre` + h-scroll at 900px, compact target heights (44/40/40/40px),
  hint hidden on touch, dark filled-action text, empty-state CTA
  centering, sticky-actions flush at the pane edge: **24/24**.
- An independent re-review of the refreshed app confirmed all ten
  headline changes present and a clean overflow/clipping sweep
  (0px horizontal overflow, no element beyond the viewport, no text
  escaping containers at 1440/900/390).

Not taken further (candidates for a later pass): a `max-width`/measure
cap for very wide detail panes, per-language template-variable
highlighting inside `{{var}}` bodies (highlight.js post-processing),
and a scroll affordance on the palette's inner fold.
