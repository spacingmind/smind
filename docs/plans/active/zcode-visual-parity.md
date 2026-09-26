# ZCode visual parity (phased)

Make smind's web UI (`web/packages/ui`) visually 1:1 with ZCode — Z.ai's
Apache-2.0 AI coding workbench, cloned read-only at `refs/zcode`
(`packages/ui` is the UI package). Visual parity only: features stay on
smind's own roadmap; no backend/daemon/wsapi changes.

## Context

User decisions (2026-09-25, not open for re-litigation):

- **Visual 1:1 with ZCode.** ZCode sets the look — tokens, typography,
  spacing, radii, shadows, component proportions, light/dark palettes.
  ZCode is Apache-2.0; `refs/zcode/LICENSE` and `refs/zcode/NOTICE.md`
  are the attribution sources.
- **Features stay smind's.** Explicitly out of scope, not to be planned:
  workflows, Goal Mode, plugin store, bots, Office mode, doc/PDF
  previews, billing.
- **Existing Paseo-derived interaction UX stays working:** Find
  (Mod+F), chord shortcuts, split panes, attention/unread/pins. ZCode
  sets the *look* of these affordances; smind keeps the *behavior*.
- **No ZCode/Zai logos, names, or branding** anywhere in smind's UI,
  code identifiers, or comments beyond license-required attribution.
- **NOTICE attribution** for any code ported from ZCode.
- **Never run ZCode** in any form — its indexing feature uploaded user
  workspaces. Source is read only. Visual verification therefore never
  screenshots ZCode; it verifies against ZCode's checked-in source and
  token values (see Test Scenarios).

Primary ZCode references (all under `refs/zcode/packages/ui/src/`):

- `DESIGN.md` — the design system: `text-ui-*` scale, `--ui-font-size`,
  semantic color tokens, radius nesting rules, spacing, elevation,
  motion. The main source of truth.
- `styles.css` — the token implementations: `@theme` block with
  `--text-ui-*` formulas off `--ui-font-size` (default 14px), the
  `:root`/`.dark` fallback palettes, and `.theme-zai-light` /
  `.theme-zai-dark` (the active light/dark experiences per DESIGN.md's
  Theme Modes section).
- Shell: `App.tsx`, `Root.tsx`, `app-shell/WorkspaceShellLayout.tsx`
  (independent conversation/terminal/side-pane frames with 4px resizable
  gaps), `WorkspaceHeader.tsx`.
- Sidebar: `WorkspaceSidebar.tsx`, `WorkspaceSidebarItem.tsx`,
  `TaskList.tsx`/`TaskListItem.tsx`,
  `WorkspacePinnedTasksSection.tsx`, `WorkspaceSidebarFooter.tsx`.
- Chat: `v4/ConversationTimeline.tsx`, `ConversationTurnRow.tsx`,
  `ConversationRowView.tsx`, `ConversationComposer.tsx`.
- Composer: `LexicalChatInput.tsx`, `prompt-editor/ChatPromptEditor.tsx`.
- Tool cards: `ToolCallBlocks/` (`ToolLayout.tsx`, `ToolSummaryRow.tsx`,
  `renderers/`).
- Permission/question cards: `PermissionDialog.tsx`,
  `ElicitationDialog.tsx`, `TaskInteractionBadge.tsx`.
- Panes: `GitPane.tsx`/`GitPaneChangeCard.tsx`, `Terminal.tsx` +
  `terminal/terminalTheme.ts`, `workspace-file-tree/`,
  `SettingsPage.tsx`, `components/ui/` primitives (button, card,
  dropdown-menu, dialog, …).
- A full component-by-component mapping lives in
  `docs/research/local/zcode-component-map.md` (local-only, gitignored).

smind's current system is documented in `docs/design.md` (token layers,
type roles, elevation recipes, forbidden list) — this plan supersedes
the *visual values* there; `docs/design.md` gets a rewrite pass in P1's
PR to match what lands.

## Decisions

- **Adopt ZCode's token vocabulary wholesale.** Port the `@theme`
  semantic tokens from `refs/zcode/packages/ui/src/styles.css` under
  their ZCode names (`--color-background`, `--color-card`,
  `--color-foreground-subtle`, `--text-ui-*`, …) so ported component
  code compiles against the same classes it was written for. smind's
  existing semantic tokens (`surface-0..3`, `status-*`,
  `diff-addition/deletion`, type-role `--text-*`) are migrated onto the
  ZCode names (aliased during a phase, removed at its end) rather than
  kept as a parallel vocabulary. The static shadcn oklch layer stays
  only where some ZCode token doesn't cover it.
- **Zai Light / Zai Dark are the palettes.** smind's light theme maps to
  `.theme-zai-light` values, dark to `.theme-zai-dark`, applied via the
  existing `dark`-class mechanism (`hooks/use-theme.tsx`,
  `lib/theme.ts`). ZCode's `:root`/`.dark` fallback palettes are not
  ported — DESIGN.md names Zai as the active experiences. The
  `.theme-zai-*` class *names* are not shipped; only their values.
- **The `text-ui-*` scale replaces smind's type-role tokens and all
  stock text-size utilities in app UI.** `--ui-font-size` (default
  14px) is the one interface-scaling variable, exactly as DESIGN.md
  prescribes; smind's `--font-scale-content` system is retired. Code,
  diff, and terminal content keep independent numeric sizes per
  DESIGN.md's exceptions; smind's Find/chord/split/attention behavior is
  untouched while their chrome moves to `text-ui-*`.
- **The one smind-branded element is the wordmark.** ZCode's palettes
  make `--color-primary` neutral (black/white); smind follows that for
  all primary actions. smind's own logo mark in the sidebar header keeps
  its identity hue — it is smind's mark, the anti-Zai-branding rule
  cuts the other way. No other surface gets a brand hue.
- **Nothing beyond `web/packages/ui` changes.** No daemon, wsapi, or
  protocol work in any phase.
- **Ported code keeps Apache-2.0 attribution.** smind is AGPL (ADR-0003)
  and can incorporate Apache-2.0 code; every file containing ported
  ZCode code gets a header comment, and a top-level `NOTICE` file (P1)
  lists the derivation per Apache-2.0 §4(d).
- **No ZCode execution.** Parity verification = checked-in token
  fixtures + Playwright screenshots of smind + source-reading
  checklists (Test Scenarios below). Never build, launch, or screenshot
  ZCode.

## Skills to use

The repo already ships these agent skills under `.agents/skills/`, with symlinks in `.claude/skills/`. Agents that don't load skills automatically (for example GLM over ACP) should read the `SKILL.md` directly:

- `.agents/skills/web-design-guidelines/SKILL.md`: the review checklist at the end of **every phase**, covering spacing, typography, interaction states, focus and a11y. Run it against the changed components before marking a phase done, and record the findings in Validation.
- `.agents/skills/vercel-composition-patterns/SKILL.md`: use while porting ZCode components (P2–P4), to keep compound components, slots and variants consistent instead of boolean-prop sprawl.
- `.claude/skills/vercel-react-best-practices/SKILL.md`: use in P2–P4 to avoid re-render regressions. The timeline rows are memoized; keep them that way.
- `.agents/skills/frontend-design/SKILL.md`: only for surfaces ZCode has no equivalent for, i.e. smind-only features. **Do not** use it to restyle anything ZCode already defines; `refs/zcode/DESIGN.md` wins.
- Not used here: `sleek-design-mobile-apps` (mobile only) and `ui-ux-pro-max` (not installed; it proposes new palettes/styles, which conflicts with 1:1 parity).

## Acceptance Criteria

Each phase is one PR. Phases are ordered; a phase may start only when
the previous is merged.

### P1 — Design system: tokens, type scale, guards

- **`web/packages/ui/src/index.css` carries ZCode's `@theme` semantic
  token set** ported from `refs/zcode/packages/ui/src/styles.css`:
  background/surface/card/popover/menu/input families, foreground
  hierarchy (`-subtle`, `-subtlest`, `-inverse`), border families,
  success/warning/destructive, diff added/removed, find-highlight,
  hover/selected/primary/secondary, toast/tooltip/tag, and the terminal
  16-color set. Zai Light values in `:root`, Zai Dark values in
  `.dark`.
- **The `text-ui-*` scale exists and is the legal interface scale:**
  `--ui-font-size: 14px` plus `--text-ui-xl/lg/base/caption/sm/xs` with
  ZCode's exact `calc()` formulas. `--text-ui-2xs` and
  `text-mobile-input-safe` are not ported (no consumers: workflows and
  mobile-Web iOS are out of scope; revisit if either changes).
- **A guard test (like ZCode's DESIGN.md constraint, and sibling to
  `src/test/no-hardcoded-colors.test.ts`) fails the build on:** any
  `text-sm`/`text-xs`/`text-base` (Tailwind built-ins), arbitrary
  `text-[…px]`, or inline `font-size` in app-UI source. Exemptions
  mirror DESIGN.md: code/diff/terminal content sizes, and the test
  files themselves.
- **All existing components migrate to the new tokens** — no component
  still references a retired smind semantic token (`surface-*`,
  `status-*` text-tier names, type-role `--text-*`) by the end of P1;
  aliases added at the start of P1 are removed in the same PR.
- **The existing `src/test/no-hardcoded-colors.test.ts` still passes
  unchanged** (it may grow exempt paths, e.g. the token file).
- **Radii, spacing, shadows follow DESIGN.md rules** in the primitives
  (`components/ui/`): radius nesting `rounded-xl → lg → md → sm` with
  `rounded-2xl` only for the approved exceptions (main composer shell,
  dialogs, toasts — smind has no workflow/status panels); spacing
  stays on the 4px Tailwind numeric scale; `shadow-md`/`shadow-lg` map
  to ZCode's overlay/attention tiers.
- **`NOTICE` exists at the repo root** with the Apache-2.0 attribution
  for ZCode-derived tokens/CSS, modeled on `refs/zcode/NOTICE.md`'s
  own format.
- **`docs/design.md` is rewritten** to describe the ZCode-derived system
  (it currently documents the superseded token vocabulary).
- **A token fixture is checked in** (see Test Scenarios) and a test
  asserts every token in it resolves in `index.css` for both themes.

### P2 — App shell + sidebar

- **The shell layout matches `app-shell/WorkspaceShellLayout.tsx`:**
  independent conversation / bottom-terminal / side-pane frames, each
  with its own background (`--color-panel`) and border, separated by
  4px resizable gaps; resize handles: transparent 4px hit area, 2px
  `foreground-subtlest/50` indicator on hover/focus/drag, rounded
  ends, inset by panel radius. smind's existing split-pane behavior
  (side dock tabs, resizable panels) keeps working.
- **Sidebar matches `WorkspaceSidebar.tsx` + `WorkspaceSidebarItem.tsx`:**
  `--color-sidebar` background, item density, hover/selected states
  (`bg-surface-hover` / `bg-selected`), `text-ui-base` row titles,
  `min-w-0` truncation. smind's attention/unread dot and pinned-tasks
  section keep their behavior, restyled onto ZCode's tokens
  (`WorkspacePinnedTasksSection.tsx` is the visual reference for
  pins).
- **The workspace header matches `WorkspaceHeader.tsx`:** height,
  padding, title treatment (`text-ui-base` + weight per DESIGN.md),
  border, and the theme toggle relocated if ZCode's placement differs.
- **ZCode branding is absent:** no Zai logos, icons, or names; smind's
  wordmark is the only mark. No billing/plan/usage footer widgets are
  added (ZCode's `WorkspaceSidebarFooter` plan/usage surfaces are
  explicitly excluded).

### P3 — Chat timeline + composer + cards

- **Timeline rows match `v4/ConversationTimeline.tsx` /
  `ConversationTurnRow.tsx` / `ConversationRowView.tsx`:** message
  containers `text-ui-base` at ZCode's max-width and padding; markdown
  type scale per DESIGN.md (h1 `text-ui-xl`, h2 `text-ui-lg`,
  h3–h6 `text-ui-base` with the prescribed weights, inline code
  `font-mono text-ui-sm`); chat bubbles follow the radius container
  hierarchy starting `rounded-xl`.
- **The composer matches `prompt-editor/ChatPromptEditor.tsx` +
  `v4/ConversationComposer.tsx`:** input shell is the approved
  `rounded-2xl` exception, `bg-input`/`border-input-border` with
  hover/focus border tokens, `text-ui-base`; send/stop controls keep
  smind's variants restyled onto `--color-primary` treatment.
  smind's provider/approval selects keep working, restyled as ZCode
  Select triggers (`rounded-lg`, menu surface `bg-menu`).
- **Tool-call cards match `ToolCallBlocks/`** (`ToolLayout.tsx`,
  `ToolSummaryRow.tsx`, per-tool `renderers/`): `rounded-xl` container,
  `text-ui-sm` summaries, `font-mono` for paths/commands, ZCode's
  collapsed/expanded treatments. smind's tool-renderer registry
  structure stays; each renderer gets the visual pass.
- **Permission and question cards match `PermissionDialog.tsx` /
  `ElicitationDialog.tsx` / `TaskInteractionBadge.tsx`:** the green
  confirmation treatment
  (`--color-interaction-confirmation-surface/-foreground`) for all
  waiting badges; dialog shells `rounded-2xl`; smind's three
  card variants (options/plan-review/question) keep their behavior.
- **Find (Mod+F) keeps working** over the new timeline DOM; highlight
  uses `--color-find-highlight` / `-active`.

### P4 — Panes: diff/git, terminal, files, settings

- **Diff/git pane matches `GitPane.tsx` + `GitPaneChangeCard.tsx`:**
  change rows, status colors from ZCode's git status family
  (`--color-git-modified` etc. — ported as part of P1's token set),
  diff colors `--color-diff-added/-removed` (never generic
  success/destructive per DESIGN.md); diff2html variable overrides
  repointed at the new tokens. smind's per-file stage checkbox and
  commit flow keep working.
- **Terminal pane matches `Terminal.tsx` + `terminal/terminalTheme.ts`:**
  `--color-terminal-*` 16-color set wired through smind's existing
  `resolveTerminalTheme()` computed-style probe; xterm viewport
  scrollbar hidden per ZCode's rule; pane chrome `text-ui-*`.
- **File explorer matches `workspace-file-tree/`:** row height/indent
  density, `font-mono`-leaning file names, folder guides, git status
  markers using the git color family. No doc/PDF/office preview
  surfaces are added (excluded).
- **Settings matches `SettingsPage.tsx`'s visual system** for the
  settings smind actually has (theme, shortcuts, accounts): section
  layout, control styling, `text-ui-*` roles. No marketplace,
  automation/workflow, or billing sections are added (excluded).
- **File editor / preview pane chrome** (CodeMirror surfaces) follows
  ZCode's `PreviewPane`/`code-viewer` treatments where smind has an
  equivalent surface; syntax colors unchanged.

### P5 — Desktop-only chrome — **BLOCKED** on ADR-0013

**Blocked:** ADR-0013 (bundled desktop UI) is not yet written or
accepted (`docs/decisions/` currently ends at 0011). Do not start this
phase until it is accepted. Everything below is contingent on that
decision's outcome.

- Window frame / title-bar chrome matches `DesktopWindowFrame.tsx`,
  `DesktopTopOverlay.tsx`, `DesktopWindowControls.tsx` (frameless
  look, `bg-background-alt` handling, overlay safe areas) — minus all
  Zai branding.
- Linux-style shell radius rules (16px outer shell / 12px panels /
  4px inset) per DESIGN.md's Color Usage Rules, if ADR-0013 lands a
  desktop build with equivalent windowing.
- No web-UI behavioral changes in this phase; it is chrome only.

## Test Scenarios

Visual parity is verified **without ever running ZCode**, from three
independent angles:

1. **Token fixture comparison (P1, machine-checked).** A checked-in
   fixture (`web/packages/ui/src/test/zcode-tokens.fixture.json`, or
   `.md` table) lists every ported token with its Zai Light and Zai
   Dark values copied from `refs/zcode/packages/ui/src/styles.css`.
   A unit test parses smind's `index.css`, resolves each token in both
   themes, and fails on any mismatch. This is the 1:1 guarantee at the
   token layer and catches drift on every future PR.
2. **Playwright screenshots of smind (P2–P4, per phase).** Screenshot
   the affected surfaces in both themes at desktop width; assert
   against phase-checked-in baselines (generated from this phase's
   implementation, reviewed by a human against angle 3) so later phases
   can't regress earlier ones. Include: shell+sidebar, a seeded
   timeline (user/assistant/tool/permission rows), composer idle and
   focused, each pane with seeded content.
3. **Source side-by-side checklist (P2–P4, human/grepped).** For each
   surface in the component map
   (`docs/research/local/zcode-component-map.md`), a checklist row:
   ZCode file(s), smind file, and the concrete visual properties read
   from ZCode's source (padding, radius, tokens used, states). The
   phase PR's Validation section records each row checked off; the
   class-name parallels are also greppable (e.g. smind's sidebar item
   uses the same token utilities as `WorkspaceSidebarItem.tsx`).

Per-phase scenarios:

- **P1:** token fixture test green; `text-ui-*` guard test green on the
  migrated tree (and red on a deliberately-violating fixture in the
  test itself); `no-hardcoded-colors` green; both themes render every
  primitive (button variants, input states, menu, dialog, toast) with
  no unresolved `var()`s (a smoke render asserting computed styles are
  not empty/transparent defaults).
- **P2:** shell screenshot both themes — frames visible, 4px gaps,
  handle indicator shows on hover only; sidebar rows: hover, selected,
  attention dot, pinned section; keyboard: chord shortcuts and Mod+F
  still fire (existing keyboard tests untouched and green); split-pane
  resize drag works (existing resizable behavior test green).
- **P3:** seeded timeline screenshots (both themes) covering markdown
  heading scale, inline code, tool card collapsed/expanded, permission
  card each variant, question card; composer focused shows
  `border-input-border-focused`; waiting badge uses the confirmation
  green in both themes; Find highlights with `--color-find-highlight`
  and steps to `-active`.
- **P4:** git pane with a seeded repo state showing all status colors;
  diff view with +/- lines using diff tokens; terminal renders ANSI
  test output in ZCode's 16 colors both themes; file tree with nested
  folders + mixed git statuses; settings page sections in both themes.
- **P5 (when unblocked):** desktop window screenshots vs. the frame
  rules; web build unchanged (P1–P4 screenshot suites all green).

## Progress

- [x] P1 — design system: tokens, `text-ui-*` scale, guards, NOTICE
- [x] P2 — app shell + sidebar
- [ ] P3 — chat timeline + composer + tool/permission/question cards
- [ ] P4 — panes: diff/git, terminal, files, settings
- [ ] P5 — desktop-only chrome (blocked on ADR-0013)

## Validation

### P1 — design system: tokens, `text-ui-*` scale, guards

Baseline before P1: 94 test files / 1067 tests, typecheck green. After
P1: 96 test files / 1227 tests, typecheck green, `task lint` green (no
Go changes). Commits (`feat/zcode-visual-parity`, oldest first):
`8ee70c9` (tokens), `e52a9ce` + `961b4cd` + `c2b4ee0` (type scale +
migration, 4 commits — one per `MIGRATED_ROOTS` batch), `5610d0c`
(retired-token migration + alias deletion), `ac4ff69` (primitives),
`20c2d32` (NOTICE + docs/design.md).

- **index.css carries ZCode's `@theme` semantic token set** — ported in
  `8ee70c9`. `src/test/zcode-tokens.test.ts` + `zcode-tokens.fixture.json`
  resolve all 82 ported `--color-*` tokens (41 families × 2 themes)
  through `index.css`'s own var() chains and assert a match against
  values copied from `refs/zcode/packages/ui/src/styles.css`'s
  `.theme-zai-light`/`.theme-zai-dark` blocks — 164 assertions, all
  green. Tokens ZCode shares a name with smind's pre-existing static
  layer (background/foreground/card/popover/primary/secondary/accent/
  destructive/border/sidebar) are covered by the same test via their
  existing `--color-*` aliases, not duplicated in the fixture.
- **The `text-ui-*` scale exists** — `--ui-font-size` +
  `--text-ui-xl/lg/base/caption/sm/xs`, `8ee70c9`. `--text-ui-2xs`/
  `text-mobile-input-safe` deliberately not ported (no consumers).
- **Guard test fails the build on built-in text-size utilities,
  arbitrary `text-[…px]`, or inline `font-size`** —
  `src/test/text-ui-scale.test.ts` (`e52a9ce`), with 7 deliberately-
  violating fixtures (asserted red) and 4 legal-spelling fixtures
  (asserted not flagged) in the test itself, plus a real scan across
  every non-test `.ts(x)` file. Exemption: `code-mirror-editor.tsx`'s
  one `text-sm` (sets the CodeMirror container's ambient font-size,
  which the code content inherits — the code-content exception).
  Migrated one directory at a time (`components/ui` in `e52a9ce`;
  `composer`/`find`/`permission`/`settings`/`timeline` in `961b4cd`;
  the remaining top-level `components/*.tsx` + `App.tsx` in `c2b4ee0`,
  which also deletes the `MIGRATED_ROOTS` scaffolding); the guard now
  scans the whole tree unconditionally and is green.
- **All existing components migrated off retired tokens, aliases
  deleted** — `5610d0c`. `surface-1/2/3`, `status-{success,danger,
  warning,running}`, `status-dot-*`, and the type-role scale (minus
  `--text-content`) have zero remaining `.tsx`/`.ts` consumers
  (verified by repo-wide grep before committing) and their `index.css`
  aliases are deleted; `token-presence.test.ts` was updated to match.
  `--diff-addition`/`-deletion` are the one exception, left aliased —
  not required by this AC (P4 owns diff2html's own repointing) and
  still asserted present by `token-presence.test.ts`.
- **`no-hardcoded-colors.test.ts` passes unchanged** — the test file
  itself has zero diff across all of P1 (`git log -p` on that path is
  empty for this range); it passed at every commit.
- **Radii/spacing/shadows follow DESIGN.md rules in `components/ui/`**
  — `ac4ff69`. `--radius-*` deleted (Tailwind's own default scale
  applies); `button.tsx`'s per-size radius overrides removed (DESIGN.md:
  size doesn't independently change radius); `dialog.tsx` shell
  `rounded-2xl` (the dialog exception) with its close button dropped to
  the basic-control default `rounded-lg`; `toast.tsx` `rounded-2xl` (the
  toast exception) + `bg-toast` + `shadow-lg`; `dropdown-menu.tsx`/
  `context-menu.tsx`/`select.tsx` content shells `bg-menu`/`rounded-lg`,
  items `rounded-md`/`bg-menu-hover`; `dialog.tsx`/`sheet.tsx` shadow
  `shadow-md` (Overlay tier, not Attention). Spacing scale was already
  Tailwind's numeric scale pre-P1 (docs/design.md §4) and untouched.
  `input.tsx`/`select.tsx` also moved off the redirected `border-input`/
  `bg-input` names onto `border-input-border`/`bg-input`/
  `border-input-border-hover`/`border-input-border-focused`/
  `bg-input-focused` (promised in `8ee70c9`'s commit message).
- **`NOTICE` exists at the repo root** — `20c2d32`, Apache-2.0 §4(d)
  attribution for the ZCode-derived token values, listing
  `web/packages/ui/src/index.css` as the derived file.
- **`docs/design.md` is rewritten** — `20c2d32`; describes the
  ZCode-derived token/type-scale/radius/shadow system (§1, §12, §13)
  and an updated Forbidden list (§14); keyboard/palette/persistence/
  responsive sections (§7–10) untouched, no ZCode equivalent.
- **Token fixture checked in, test asserts resolution in both themes**
  — `zcode-tokens.fixture.json` + `zcode-tokens.test.ts`, `8ee70c9`
  (see above).

**Web Interface Guidelines review** (P1 Step 7, `.agents/skills/
web-design-guidelines/SKILL.md`, fetched fresh): reviewed every
`components/ui/` primitive changed in P1. Two real findings, both
fixed: `button.tsx` and `tabs.tsx` used `transition-all` (guideline:
"never use `transition: all` — list properties explicitly"); both now
use Tailwind's bare `transition` utility, which maps to its curated
safe property list (color/background/border/opacity/box-shadow/
transform/filter — not literally every property) rather than
`transition-property: all`, preserving the same animated properties.
One accepted trade-off, not fixed: `input.tsx`/`select.tsx`'s
`focus-visible:border-input-border-focused` replaces the old
`focus-visible:ring-*` (guideline: interactive elements need a visible
focus replacement for `outline-none`) — a border-color change is a
real, present replacement, just lower-contrast than a ring; this is
ZCode's own actual design (`refs/zcode/packages/ui/src/components/ui/
input.tsx`: inputs are "calm and integrated, not glowing by default"
per `refs/zcode/DESIGN.md`), so fixing it would mean deviating from the
visual-parity mandate rather than a real component bug.

**Not done in P1** (all explicitly deferred to a later phase by the
plan, not scope creep left behind): diff2html's own variable overrides
still read `--diff-addition`/`-deletion` rather than the ZCode-named
`--diff-added`/`-removed` (P4); the terminal's `resolveTerminalTheme()`
still only resolves chrome colors, not the ZCode 16-color ANSI set now
sitting unused in `index.css` (P4); markdown's heading hierarchy uses
its pre-existing pixel-preserving `text-ui-*` mapping, not yet
DESIGN.md's h1→`text-ui-xl`/h2→`text-ui-lg` scale (P3); most
`components/ui/` primitives outside the interactive-control/menu/
overlay family (`sidebar.tsx`, `tooltip.tsx`, `hover-card.tsx`,
`scroll-area.tsx`, `resizable.tsx`, `skeleton.tsx`) were left as-is —
no explicit DESIGN.md violation found in a light pass, and their
surfaces get pixel-matched against a specific ZCode source file in
P2–P4 rather than guessed at here.

### P1 follow-up — screenshot-review bug fixes

A screenshot pass over the committed P1 work (`bd9cfe2`, `da4abdd`,
after 086bddb) found four visual defects, all fixed and re-verified via
fresh Playwright screenshots (home/task/settings, light+dark,
1440×900) before landing:

1. **Selected/active items rendered much larger than siblings**
   (Settings → Appearance segmented controls, the settings nav, the
   "Chat" tab label, the sidebar's "Workspaces" heading). Root cause:
   tailwind-merge's default config doesn't know `text-ui-*` is a
   font-size scale, so it falls into the `text-color` group by
   default; any `cn()` call pairing a `text-ui-*` size with a later
   `text-{color}` class (the exact shape every selected/active variant
   produces) silently dropped the size, leaving the element to inherit
   ambient font-size. Fixed in `bd9cfe2` by registering the scale
   under tailwind-merge's `font-size` group via `extendTailwindMerge`
   (`lib/utils.ts`); `lib/utils.test.ts` pins the merge behavior, and
   `appearance-section.test.tsx` + a new `settings-screen.test.tsx`
   case render the real components and assert the selected and
   unselected items share one `text-ui-*` class.
2. **Overall text scale read too small (~11px body text).** The first
   migration pass ported the old Tailwind class name 1:1
   (`text-sm`→`text-ui-sm`, `text-xs`→`text-ui-xs`), but ZCode's roles
   sit one tier higher than those old names suggest. Fixed in `da4abdd`
   by moving every body/label/title site up one tier and reverting the
   genuine badge/counter/kbd sites back to `text-ui-xs` (see the role
   table in `docs/design.md` §12, "Role mapping", reproduced below).
3. **Dark-theme selected/hover state was a saturated navy; the dark
   Send button's disabled background was brownish.** The navy came
   from several components still using the pre-ZCode static
   `bg-accent`/`text-accent-foreground` pair (dark `--accent` is
   `#001d3d`, a leftover shadcn value) instead of the Zai-palette
   `bg-selected`/`bg-hover` tokens P1 already ported; every remaining
   application-UI use is now `bg-selected`/`hover:bg-hover` (`da4abdd`).
   The brownish Send button came from the "execute" button variant's
   already-translucent `bg-warning/10..20` compounding with the shared
   `disabled:opacity-50`, landing at an effective alpha low enough that
   the warning hue reads as brown rather than a dimmed orange; its
   disabled state now falls back to the flat neutral
   `muted`/`muted-foreground` pair (`da4abdd`). The `execute` variant
   itself, and its use for the idle Send button, are pinned by an
   existing `composer.test.tsx` case and were not changed.
4. **Sidebar task rows pushed the title ~32px right of its branch
   line.** The title row reserves a run-status-dot + unread-dot lead-in
   (`w-2.5` + `gap-2` + `w-1.5` + `gap-2` = 32px) so a status change
   never shifts the title sideways (pre-existing on `develop` too, not
   a P1 regression); the meta row underneath has no such slots, so it
   needs a matching `pl-8` to visually align under the title
   (`app-sidebar.tsx`'s `TaskMetaRow`, `da4abdd`).

**Role mapping** (also recorded in `docs/design.md` §12):

| Token | Role | smind examples |
| --- | --- | --- |
| `text-ui-xl` | Markdown h1 | `timeline-markdown.tsx`'s `h1` |
| `text-ui-lg` | Markdown h2 | `timeline-markdown.tsx`'s `h2` |
| `text-ui-base` | Markdown h3–h6, body copy, common buttons, titles/labels, primary UI text | dialog/section titles, form labels, timeline message bubbles, button labels, settings nav items, tab labels |
| `text-ui-caption` | Compact supporting copy one step below body | not yet consumed in smind |
| `text-ui-sm` | Secondary/supporting copy, helper text, inline code | sidebar task metadata (branch, diff stat, last-activity), hover-card detail rows, form helper/error captions, tooltip copy |
| `text-ui-xs` | Badges, counters, compact labels, keyboard-shortcut kbds, very weak metadata | `StatusBadge`, `SidebarMenuBadge`, `<kbd>` shortcut chips, account-provider pills, the folder picker's `git` indicator |

**Accepted trade-off, not fixed:** the button primitive's disabled
treatment is documented (`docs/design.md` §14 Forbidden: "Color-only
disabled state... a disabled control is the same control, dimmer, not
a recolored one"). The Send-button fix above technically recolors the
`execute` variant's disabled state rather than purely dimming it; the
alternative (raising the variant's base alpha enough that halving it
via `disabled:opacity-50` still reads as orange, not brown) would
visibly change the variant's normal, non-disabled appearance too, a
larger and unrelated change. `aria-invalid:*` overrides already
recolor this same variant string regardless of disabled state, so this
isn't a new pattern in this file.

**Verification:** `bun run --filter '@smind/ui' test` — 98 test files,
1235 tests, all green (up from 96/1227 at the end of P1 proper: +1 new
file `appearance-section.test.tsx` with 2 cases, +1 new case in
`settings-screen.test.tsx`, +5 in `lib/utils.test.ts`);
`bun run --filter '@smind/ui' typecheck` green; `task lint` green;
`task test` (Go suite) green, unaffected (web-only change). Playwright
screenshots (`home`/`task`/`settings` × light/dark, 1440×900) confirmed
all four defects gone before commit.

### P2 — app shell + sidebar

Commits (`feat/zcode-p2-shell`, oldest first): `9bb7d72` (shell frames:
panel bg/border, resize-handle restyle), `9abd692` (sidebar hover/
selected tokens + task-row over-indent fix), `66cf979` (sidebar footer
icons for the icon-collapsed rail).

- **The shell layout matches `app-shell/WorkspaceShellLayout.tsx`** —
  `9bb7d72`. Every split-tree pane (`PaneTabStrip`'s root, `App.tsx`) is
  now its own `bg-panel`/`border`/`rounded-lg` frame; `components/ui/
  resizable.tsx`'s `ResizableHandle` is a transparent 4px hit area
  (`w-1`) with a 2px `bg-foreground-subtlest/50` `::after` indicator,
  rounded (`rounded-full`), that only appears on hover (after the
  existing 150ms delay), focus-visible, or an active drag — never a
  permanent line. A `p-1` wrapper around the split-tree area gives every
  frame the same 4px gap against the header/sidebar/window edges that
  the handle already gives adjoining panes. smind has no fixed
  conversation/terminal/side-pane roles the way ZCode does (`side dock`
  placement can put any tab kind in either pane) — the frame treatment
  applies uniformly to whichever pane(s) the split tree currently holds,
  which is the equivalent for smind's architecture. The `withHandle`
  prop/nub-dot decoration is gone (ZCode's handle has no center grab
  dot); `ResizableHandle`'s two call sites (`App.tsx`) updated to match.
  Existing split/resize-persistence tests (`resizable.test.tsx`,
  `App.test.tsx`) are green unchanged — only classNames moved.
- **Sidebar matches `WorkspaceSidebar.tsx`/`WorkspaceSidebarItem.tsx`**
  — `9abd692`, `66cf979`. `--color-sidebar` background was already
  correct from P1 (no change needed); `sidebarMenuButtonVariants`/
  `SidebarMenuSubButton` (`components/ui/sidebar.tsx`) now hover/select
  onto ZCode's own `bg-hover`/`bg-selected` tokens instead of the static
  shadcn `sidebar-accent` pair, matching every other interactive surface
  in the app. Row titles were already `text-ui-base` with `min-w-0
  truncate` (pre-existing, P1-migrated). The footer's Providers/Agents
  rows (`#203`) gained `KeyRound`/`Bot` icons so the icon-collapsed rail
  shows a glyph instead of clipped text, without stripping the row's
  accessible name in that state (no `hidden` on the label span — it
  clips to icon-only the same way every other row already does, via
  `SidebarMenuButton`'s own `overflow-hidden` + fixed collapsed size).
- **Known P2 issue fixed: sidebar task rows indented far too deep
  relative to their group header** — `9abd692`. Root cause: a task row
  nested under a Space (or the "Ungrouped" bucket) picked up *two* full
  `SidebarMenuSub` indent steps (`mx-3.5` + `border-l` + `px-2.5`, each)
  because that list is the *second* `SidebarMenuSub` nesting level under
  its workspace (workspace → space → task), while the space's own row
  only ever gets one step. Fixed by flushing that inner list
  (`mx-0 border-l-0 px-1` override in `SpaceLikeItem`, `app-sidebar.tsx`)
  — task rows now align close under the space header they belong to,
  instead of a compounding second indent/guide-line. Pinned-section and
  status-grouped-view task lists (single nesting level) are unaffected.
- **ZCode branding absent** — unchanged; smind's own `logo.png` +
  "smind" wordmark are the only mark, no billing/plan/usage footer
  widgets were added.
- **Attention/unread/pins, keyboard/chords, Find, split panes, drag-to-
  split all keep working** — none of these behaviors were touched, only
  the classNames/structure around them; the full pre-existing test
  suite (below) is green with zero test edits required.

**Verification:** `bun run --filter '@smind/ui' typecheck` green;
`bun run --filter '@smind/ui' test` — 105 test files, 1316 tests, all
green (no test file added or edited — every P2 change was visual/
structural against already-passing behavior coverage). `go test ./...`
green (34 packages, unaffected — web-only change). `task lint` green
(`go vet` + `gofmt`, no Go changes).

Playwright screenshots (1440×900, light+dark) against a temp daemon
(port 4711, seeded with 2 workspaces/2 spaces/6 tasks/2 agent profiles/
1 pinned task, one task's real `claude-native` run left unread for an
attention dot) — `sidebar-states-{light,dark}.png` (selected + pinned +
hover + attention together), `shell-split-{light,dark}.png` (chat pane
split right into a terminal pane), `sidebar-collapsed-{light,dark}.png`
(icon rail) — saved to
`/mnt/c/Users/ADMIN/Downloads/smind-zcode-p2/`. Reviewed by hand;
one real defect found and fixed before the final pass (the footer icon/
a11y issue above, first found as clipped footer text, then a second
look at the fix itself caught the `hidden`-vs-accessible-name issue).

**Web Interface Guidelines review** (P2 Step 5): the guidelines
source's `command.md` returned only its own tool-usage blurb this run
(no fetchable rule list), so the pass fell back to applying the same
concrete rules P1's review already established for this codebase
(explicit-property transitions, not `transition-all`; a real focus-
visible replacement for `outline-none`) plus a fresh manual accessibility
check of every new/changed interactive element. One real finding, fixed
before commit: the footer icon fix's first draft hid the label span via
`hidden` (`display:none`) in icon-collapsed mode, which removes it from
the accessible tree rather than merely clipping it — changed to rely on
`SidebarMenuButton`'s existing `overflow-hidden`+fixed-size clip instead
(see above), matching how every pre-existing collapsed row already
avoids this.

**Not done / deferred, in scope for a later phase, not left behind**:
the workspace-header equivalent (task-detail.tsx's `PaneHeader` usage)
was left on its existing `px-4 py-2.5` padding scale rather than
ZCode's `h-12`/`p-2` — that scale is a P1-documented, cross-cutting
convention (`docs/design.md` §4) shared by every `PaneHeader` consumer,
including P4-owned panes (diff/terminal/file-editor); changing it here
would reach outside this phase's file ownership for a padding
difference of a few px, not a defect. The sidebar's theme toggle stays
in the sidebar header (not relocated to the main header's action
section) — smind has no billing/plan widget to make room for and no
UX problem with the current placement; moving it would be a judgment
call with test/behavior risk and no corresponding benefit the AC
requires.
