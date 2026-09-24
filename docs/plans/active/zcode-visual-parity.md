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

- [ ] P1 — design system: tokens, `text-ui-*` scale, guards, NOTICE
- [ ] P2 — app shell + sidebar
- [ ] P3 — chat timeline + composer + tool/permission/question cards
- [ ] P4 — panes: diff/git, terminal, files, settings
- [ ] P5 — desktop-only chrome (blocked on ADR-0013)

## Validation

(empty — fill per phase as acceptance criteria are confirmed)
