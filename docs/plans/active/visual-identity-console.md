# Web UI visual identity: "calm technical console"

## Context

`docs/plans/completed/ui-redesign-parity.md` shipped structural parity with
paseo/deepseek-harness/cliproxyapi: tokens (static + a first semantic
layer), dark mode, shared primitives (`PaneHeader`, `StatusDot`,
`StatusBadge`, `Alert`, `EmptyState`), keyboard registry, command palette,
resizable panes, structured timeline + a render-intent tool-call system
(terminal/read/edit/search/fetch/generic in
`components/timeline/tool-renderers.tsx`), permission UX, settings,
accounts v2, responsive/compact layout. That work is done and merged.

Two research passes done today (2026-09-17) on top of that foundation
independently converged on the same diagnosis: **the architecture is
right; what's missing is a deliberate visual-identity layer on top of it.**
`docs/research/ui-ux-polish-2026.md` (`pplx -m best`, 3 queries): shadcn's
defaults *are* the generic look, and breaking out requires a product
visual thesis + semantic tokens beyond `background`/`card`/`muted` +
a real type-role scale + a spacing rhythm + product-meaning component
variants + an elevation vocabulary + a motion vocabulary — not new
features. `docs/research/uiux-audit.md`: catalogued concrete P0-P2 gaps
live-tested in browser; re-verified against current code before writing
this plan — **P0 crashes, dark-mode wiring, the stray token, workspace/
space creation UI, and task-archive UI are already fixed/shipped** since
that audit was written (confirmed by direct grep, see Decisions). The one
P2 item still genuinely open is a "Suggested commit message" affordance,
folded into this plan below since it's small and token-adjacent.

This session additionally read `refs/paseo`'s and `refs/deepseek-harness`'s
actual source (not just their READMEs) for concrete, portable patterns —
paseo's `docs/design.md` (character → component-reuse rule → per-primitive
rules → forbidden list → canonical-surfaces table) and its
`styles/theme.ts` token model; dsh's `docs/web-styling.md` +
`ui-theme`/`ui-layout`/`ui-tool` packages (static→alias→specific token
layering, render-intent tool cards, two-tier permission UX). Full findings
are in this session's transcript, not duplicated here — this plan cites
the specific patterns being ported at each Item below.

**Not a rewrite.** Every item is additive/refinement on top of the
existing, working architecture — no component tree restructuring beyond
Item 6's tool-card visual upgrade (which itself extends the existing
render-intent registry rather than replacing it).

## Decisions

- **Visual thesis: "calm technical console"** (maintainer decision,
  2026-09-17) — paseo's own character statement (`refs/paseo/docs/design.md`
  §1): minimal, spacious, quiet, confident; every visual choice serves
  *act on this* or *understand this*, never *look at this*; consistency
  from component reuse, not hand-matched per-surface styling. Rejected
  alternatives: "instrument panel" (denser, chase-animation status dots —
  more distinctive but more discipline-risk), "code laboratory"
  (mono/dark-first — furthest from the current shadcn baseline, most
  migration cost).
- **Spacing/typography get a real numeric token scale**, matching paseo's
  `SPACING`/`FONT_SIZE` model rather than dsh's deliberately-untokenized
  px-per-component approach (maintainer decision). **Scope note that
  shrinks this materially**: Tailwind's default spacing scale
  (`1=4px, 2=8px, 3=12px, 4=16px, 6=24px, 8=32px, 12=48px, 16=64px`) is
  *already numerically identical* to paseo's `SPACING` table — so this is
  a **documentation-and-enforcement** task (name the scale as the one
  legal vocabulary, lint out arbitrary values), not a mechanical rename
  migration across the ~95 existing `.tsx` files. Typography is the part
  that needs real new tokens (Tailwind's default `text-*` scale does not
  match paseo's `FONT_SIZE`).
- **No new display/brand font.** Consistent with "calm technical console"
  — paseo itself uses one interface font (system-ui stack) + one mono
  stack, differentiated by a 3-tier *weight* system and a content-vs-
  interface *size* split, not a second typeface. Avoids font-loading work
  entirely.
- **Tool-call render-intent visual upgrade is in scope for this plan**
  (maintainer decision, despite touching timeline component internals) —
  because the intent *registry* (`tool-renderers.tsx`) already exists from
  Item 9 of the parity plan; this is a visual/detail-level upgrade of an
  existing extensible system (dsh's dedicated-block-per-intent pattern),
  not introducing the registry itself.
- **Prerequisite, not part of this plan's scope**: `docs/plans/active/
  web-ui-fixes.md`'s three bug fixes (shadcn `Select` for composer
  dropdowns, resizable-panel drag fix, sidebar truncation fix) are
  implemented on an unmerged branch
  (`smind/task-web-ui-fixes-dropdown-theme-resize-drag-sidebar-truncation-dlgyo422xbb0`,
  679/679 tests green) and touch the same files Items 1/3/6 below will
  touch (`composer.tsx`, `resizable.tsx`, `scroll-area.tsx`). Merging it
  first avoids two independent agents fighting over the same lines — done
  as the first action under Progress, not a numbered Item here.
- **uiux-audit.md re-verification (2026-09-17, before writing this plan)**:
  nil-slice list bug — fixed (`grep` for `var .*\[\]Workspace\|Space\|Task`
  in `internal/store/*.go` finds nothing); dark mode — wired
  (`lib/theme.ts` calls `classList.toggle("dark", ...)`); `PaneHeader` —
  shipped and consumed by all four panes the audit named; stray
  `--sidebar-primary` chromatic token — resolved achromatic per
  `docs/design.md` §11; `workspace.create`/`space.create` — wired in
  `components/crud-dialogs.tsx`; `task.archive` — wired, same file;
  `StatusBadge`/`toast()` — now consumed (`components/timeline/
  timeline-row.tsx`, `permission-reason.ts`), no longer dead primitives.
  Only genuinely-open item from that audit folded in here: P2.12,
  "Suggested commit message" (Item 8 below).
- **Item 8 uses a one-click affordance, not placeholder text**
  (`diff-viewer-pane.tsx`) — a ghost "Use "<message>"" button next to the
  Commit button, shown only while the message box is empty and a
  suggestion exists, and it fills-and-retires on click. Placeholder text
  was rejected: a placeholder vanishes the moment the field is focused,
  so there'd be nothing left to click or read back once the user starts
  typing, and it can't be "accepted" without being retyped verbatim.

## Acceptance Criteria

### Item 1 — `docs/design.md` restructured as a real design-system doc

- Adds, in paseo's `docs/design.md` order: a **Character** section (the
  "calm technical console" statement, 2-3 sentences, in this repo's own
  words — not copied verbatim from paseo); an explicit statement that
  consistency comes from component reuse, not per-surface hand-styling.
- Adds a **Forbidden** list: enumerable anti-patterns callable out in
  review (no hardcoded colors — already lint-enforced, cite the existing
  test; no `shadow-*` utility on more than one elevation tier per Item 3;
  no color-only disabled state — opacity per paseo §11; no ad-hoc pane
  header padding outside `PaneHeader`; no arbitrary Tailwind spacing
  value outside the documented scale; no font-weight `medium`/`semibold`
  on body text/button labels/badge text, only on structural labels per
  Item 2).
- Adds a **canonical-surfaces table**: UI pattern → one reference file,
  covering at minimum list+detail, form dialog, destructive confirm,
  sidebar list row, pane header, composer, alert/banner, empty state,
  tool-call card, permission card.
- Existing sections (token layers, theming, primitives, density, copy,
  state, keyboard, palette, persistence, responsive) are kept, updated
  only where Items 2-7 change their content (e.g. the density section
  gains the formalized spacing-scale statement).

### Item 2 — Typography token layer

- New CSS custom properties in `index.css`'s `@theme` block (or a
  dedicated layer) for a **type-role scale**, not raw sizes: at minimum
  `--text-workspace-title`, `--text-section-title`, `--text-panel-title`,
  `--text-metadata-label`, `--text-code-annotation`, plus paseo's
  content-vs-interface split (`--text-interface` = the existing
  UI-chrome ramp `12/13/14/16px`-ish already used by dense components,
  `--text-content` = one fixed size for message bodies/composer/Markdown/
  PR prose, matching paseo's fixed-15px rule so prose doesn't rescale
  with the interface-density controls).
- Each role token is documented in `docs/design.md` with its one
  canonical consumer (mirrors the Forbidden-list discipline of Item 1).
- Font-weight collapses to exactly 3 legal tiers app-wide (paseo §3):
  screen/workspace titles get one fixed weight, structural labels
  (section headers, modal titles, form-field labels, dense metadata
  emphasis) get `font-medium`, everything else is `font-normal` — audit
  and fix any current component using `font-semibold`/`font-bold`
  outside those two tiers.
- `src/test/` gains a token-presence test (mirrors the existing
  `no-hardcoded-colors.test.ts` shape) asserting every new `--text-*`
  role token exists in both `:root` and `.dark` (values may be identical
  across themes, but the token must exist in both so nothing silently
  falls back to browser default).

### Item 3 — Elevation, shadow, and motion vocabulary

- `surface-0..3` (existing) gains a documented elevation *vocabulary*
  layered on top of the raw tokens — flat/raised/floating/focused/
  embedded, per `ui-ux-polish-2026.md` §1 — mapped explicitly to which
  `surface-N` + which shadow tier + which border each uses, in
  `docs/design.md`, so "floating" always means the same recipe everywhere
  (popovers, dropdowns, dialogs) rather than each component picking its
  own shadow.
- Shadow tiers added as tokens (`--shadow-sm/md/lg`) with paseo's
  light/dark asymmetry (`refs/paseo/packages/app/src/styles/theme.ts`):
  light-mode shadows very soft (`alpha ~0.02-0.08`), dark-mode shadows
  harder (`alpha ~0.20-0.40`) — defined once per theme in `index.css`,
  consumed via `shadow-[var(--shadow-md)]` or an equivalent Tailwind
  utility, not per-component hex/rgba literals (covered by extending
  `no-hardcoded-colors.test.ts` or a sibling test).
- Motion vocabulary: three duration tokens (`--duration-hover` 120-160ms,
  `--duration-menu` 180-240ms, `--duration-panel` 250-400ms) plus the
  existing easing, applied to hover states, Radix popover/dropdown/
  dialog open-close (already CSS-var-driven by Radix's own
  `--radix-*-content-transform-origin` pattern — confirm before adding a
  second mechanism), and the resizable-panel highlight-delay timer
  (`resizable.tsx`'s existing 150ms hover-highlight, from
  `web-ui-fixes.md`, gets pointed at `--duration-hover` instead of a
  literal `150`).
- At least one state-transition animation exists for an agent-specific
  moment named in `ui-ux-polish-2026.md` §2 (queued→running,
  permission-card entering foreground) — not decorative entrance
  animation on every mount.

### Item 4 — Semantic CVA component variants

- `components/ui/button.tsx`'s `buttonVariants` CVA gains additive
  variant options with product meaning (`execute`, `approval`,
  `quiet` at minimum) alongside the existing `default`/`secondary`/
  `destructive`/`outline`/`ghost`/`link` — additive, not a rename, so no
  existing call site breaks.
- At least three real call sites adopt the new variants where they add
  actual meaning over the generic ones: the task detail "Run"/"Stop"
  action (`execute`), the permission approve/reject buttons
  (`permission-option-button.tsx`, `approval`), and a clearly
  non-committal/structural action (e.g. a dialog's Cancel button,
  `quiet`) — chosen to demonstrate the pattern, not to convert every
  button in the app in this pass.
- `docs/design.md`'s primitives table documents which variant means what,
  so a future PR picks the right one instead of reaching for `default`
  out of habit.

### Item 5 — Status/diff color-family discipline (extend, don't rebuild)

- Confirm (or add if missing) a third, explicit color family for diff
  additions/deletions — `--diff-addition`/`--diff-deletion` — separate
  from `status-success`/`status-danger`, per paseo's three-family model
  (status text tier / status dot tier / diff tier all differ in
  chroma/lightness on purpose). Today's `diff2html` override
  (`docs/design.md` §2) points at the library's own variable names
  directly; add the smind-side named tokens as the source of truth those
  overrides reference, so a future non-diff2html diff surface (e.g. a
  future Item 6 sub-card) can reuse the same tokens.
- `no-hardcoded-colors.test.ts` (or a documented reason it's out of
  scope) covers the new tokens' usage sites.

### Item 6 — Tool-call card visual upgrade (per render-intent)

*Extends the existing registry in `tool-renderers.tsx` — does not
replace it.*

- `terminal` intent: monospace block visually consistent with the
  terminal pane's own xterm theme tokens (`lib/terminal-theme.ts`),
  collapsed-by-default output with a "Show output" affordance for long
  output, failed output expanded by default with the actionable line
  visible without scrolling (per `ui-ux-polish-2026.md` §2's tool-card
  anatomy rule).
- `read` intent: line-numbered window (reuse CodeMirror's read-only mode
  or an equivalent already in the app — do not add a new syntax
  highlighter dependency if one is already present; confirm first).
- `search` intent: match count in the summary line, expandable match
  list, bounded preview (never an unbounded dump — same rule as
  terminal).
- `generic`/unclassified fallback: dsh's IN/OUT two-section layout for
  plain text input/output (small labeled gutter, divider) instead of a
  raw JSON dump.
- Every card keeps a **stable row height while its content is still
  streaming** — no reflow of the whole timeline per token (verified by
  the streaming test scenario below, not just eyeballing).
- A tool card's semantic title (already partially true — confirm) reads
  as "Search for `X` in `src/`" rather than the raw tool/method name, for
  every intent, not just the ones that already do this.

### Item 7 — Permission card visual polish

- Apply the elevation vocabulary from Item 3 (a "floating"-tier card,
  not ad hoc) to the pending-permission card; confirm the existing
  structure (title/description, optional inline tool-call detail capped
  at a max height, action row) already matches dsh's `ApprovalPanel`
  shape from this session's research — if it already does (Item 11 of
  the parity plan shipped this), this Item is a token/elevation pass
  only, not new structure. Verify and record which before implementing.
- Action buttons use Item 4's `approval` CVA variant.

### Item 8 — Suggested commit message affordance

- The commit bar (`diff-viewer-pane.tsx`) shows a non-LLM heuristic
  suggested message (file count + dominant change kind — e.g. "Update 3
  files" / "Add feature.ts" for a single new file) as placeholder text
  or a one-click-to-fill affordance, per `uiux-audit.md` P2.12 and
  Conductor's "Suggested Git Actions" precedent. Small, independent of
  the token/visual work above — can run on its own track.

## Test Scenarios

- Vitest: token-presence test for every new `--text-*`/`--shadow-*`/
  `--duration-*`/`--diff-*` token exists in both `:root` and `.dark`
  (Item 2, Item 3, Item 5).
- Vitest: `no-hardcoded-colors.test.ts` (extended) still passes with zero
  exceptions after Items 3 and 5 add new color-bearing CSS.
- Vitest: `buttonVariants`'s new variants (`execute`/`approval`/`quiet`)
  render with the expected class output; the three adopted call sites
  (Item 4) render the new variant, not the old default.
- Vitest: each `tool-renderers.tsx` intent (terminal/read/search/generic)
  renders its upgraded card shape; a streaming-update simulation (append
  partial output across renders) asserts the card's outer height/layout
  key stays fixed while inner content grows (Item 6's stable-row-height
  requirement) — this is the one most likely to expose a regression if
  skipped.
- Vitest: failed terminal output renders expanded by default; successful
  long output renders collapsed with a working "Show output" toggle.
- Vitest: the commit bar's suggested-message heuristic produces the
  right string for at least three shapes (single new file, single
  modified file, multi-file mixed change) (Item 8).
- Manual, live smoke test (browser, light + dark, desktop + narrow
  width): visual read-through of every Item against the "calm technical
  console" thesis — no shadow-on-every-card, no color-only disabled
  states, motion durations feel like the documented tiers, not
  eyeballed. Bundle this with `web-ui-fixes.md`'s own still-pending live
  smoke test (same session, same four viewport widths) rather than
  running two separate manual passes.

## Progress

- [x] Merge `smind/task-web-ui-fixes-dropdown-theme-resize-drag-sidebar-truncation-dlgyo422xbb0`
      into the working branch first (prerequisite — see Decisions)
- [x] Item 1 — `docs/design.md` restructure (Character, Forbidden, canonical-surfaces table)
- [x] Item 2 — Typography token layer
- [x] Item 3 — Elevation/shadow/motion vocabulary
- [ ] Item 4 — Semantic CVA component variants
- [x] Item 5 — Status/diff color-family extension
- [ ] Item 6 — Tool-call card visual upgrade
- [ ] Item 7 — Permission card visual polish
- [x] Item 8 — Suggested commit message affordance
- [ ] Live smoke test (combined with web-ui-fixes.md's pending one)

## Tracks and dependencies

Sized for parallel Paseo-orchestrated agents (`glm-acp-agent`, per this
project's cost-conscious default; Sonnet 5 if a Claude agent is used
instead of Opus):

- **Track A — Tokens** (Items 1, 2, 3, 5). No dependencies beyond the
  web-ui-fixes merge. Everything else depends on this landing first.
- **Track B — Component variants + permission polish** (Items 4, 7).
  Depends on Track A's tokens (elevation vocabulary, `approval` variant
  needs Item 3's tiers defined).
- **Track C — Tool-call cards** (Item 6). Depends on Track A's tokens
  (terminal theme alignment, stable-height rule uses the motion tokens
  for any transition). Independent of Track B — can run in parallel with
  it once Track A lands.
- **Track D — Suggested commit message** (Item 8). No dependency on any
  other track — can run fully in parallel from the start.

## Validation

**Track A (Items 1, 2, 3, 5) — complete on `feat/visual-tokens-track-a`.**
Independently re-verified in a follow-up session (2026-09-17) after the
implementing agent hit a rate limit before confirming green: `task test`
and `task lint` re-run from a clean worktree, `index.css`'s `:root`/
`.dark`/`@theme inline` blocks read directly, `docs/design.md` §§1,
4, 6, 11-15 read directly (not just the commit message), and the diff
for all 11 touched component files re-read against the claims below —
all confirmed accurate, no gaps found in Items 1/2/3/5.

- `task test` green (712 web tests total = 669 pre-existing + 43 new
  token-presence tests, confirmed by direct count in
  `token-presence.test.ts`; full Go suite green), `task lint` green
  (this repo's `task lint` is Go-only — `go vet` + `gofmt` check; there
  is no web lint task in `Taskfile.yml`).
- **Item 1**: `docs/design.md` restructured — Character section,
  Forbidden list (§14, citing `no-hardcoded-colors.test.ts`), canonical-
  surfaces table (§15, all ten required patterns covered); existing
  sections kept, §4 gained the spacing-scale statement, §6 the
  state-transition rule. All new tokens documented with canonical
  consumers (§1, §12, §13).
- **Item 2**: seven `--text-*` role tokens in `:root` + `.dark` +
  `@theme inline` (paired line-heights; paired weights where fixed);
  five font-weight violations fixed (app-sidebar wordmark → the
  workspace-title token itself, file-status-marker → text-code-annotation,
  timeline-markdown h1-h3 → font-medium, quick-open match highlight →
  font-medium, dialog title → text-section-title, which also unified
  dialog 18/600 + sheet 16/500 to one 16/500 role); `text-content`
  adopted by `timeline-markdown.tsx`. Token-presence test asserts both
  themes carry every token.
- **Item 3**: `--elevation-shadow-sm/md/lg` tiers with paseo's light/dark
  asymmetry (light alpha 0.04/0.06/0.08, dark 0.24/0.32/0.40 — round
  values in paseo's bands, inspired not copied); exposed via `@theme`
  `--shadow-sm/md/lg` so existing `shadow-md/lg` call sites resolve to
  the tiers with zero churn. `--duration-hover/menu/panel` (150/200/300ms)
  consumed by dialog/sheet/dropdown-menu (`duration-(--duration-menu)`;
  Radix's own transform-origin mechanism confirmed untouched — no second
  mechanism added). State-transition animation: `StatusBadge` gains
  `transition-colors duration-(--duration-hover)` (queued→running).
  Elevation vocabulary table (flat/raised/floating/focused/embedded) in
  `docs/design.md` §13. Note: `resizable.tsx`'s 150ms hover-highlight
  timer does not exist post-merge (handle is pure CSS) — nothing to
  repoint, recorded in design.md Decisions.
- **Item 5**: `--diff-addition`/`--diff-deletion` third color family in
  both themes (paseo's light/dark diff tables); diff2html's ins/del
  overrides now reference the named tokens as the source of truth;
  `--color-diff-*` `@theme` re-exports. `no-hardcoded-colors.test.ts`
  still passes with zero exceptions (new color-bearing CSS lives only in
  index.css).
- Items 4/6/7 remain open for Tracks B/C; the combined live smoke
  test is still pending.

**Item 8 — Suggested commit message affordance** (2026-09-17, on
`feat/suggested-commit-message-track-d`): `task test` (677/677 web tests
green, including `commit-suggestion.test.ts`'s heuristic coverage for
single-add, single-modify, and mixed multi-file cases, plus
`diff-viewer-pane.test.tsx`'s two new commit-bar interaction tests) and
`task lint` (go vet + gofmt; this repo has no separate web lint task)
both pass. Manual smoke test not run this pass (no dev server
exercised) — flag if a live check is wanted before merge.
