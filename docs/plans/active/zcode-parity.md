# ZCode visual parity, second pass (tokens → audit → component ports)

Size: L — scope 3 (web tokens, web components, desktop chrome), PRs ~10, risk 2
(visual regressions only; no behaviour), decisions 0, verification 3 (light+dark
frames at 1280 and 390 per UI PR).

Human directive (2026-10-30, "cu lam di"): ZCode (zai-org/ZCode, Apache-2.0,
read-only clone at `refs/zcode`) is the **visual** north star; Paseo
(`refs/paseo`) stays the **interaction** model (Find, chord shortcuts, split
panes, attention/unread, whenBusy queue).

This is a second pass. P1–P4 of the first epic
([completed/zcode-visual-parity.md](../completed/zcode-visual-parity.md)) already ported
the Zai Light/Dark palettes, the `text-ui-*` scale and primitives into
`web/packages/ui/src/index.css`. Nothing below assumes a blank slate: each stage
starts by diffing what is there against ZCode and changes only what differs.

## Hard rules

- No backend / daemon / wsapi / auth changes. Web UI (and later mobile tokens,
  desktop titlebar chrome) only.
- Not cloned: workflows, Goal Mode, plugin store, bots, Office mode, doc/PDF
  previews. No ZCode/Zai logos, product names or branding.
- **ZCode is a visual spec, not a source.** Reimplement in smind's own code
  (our components, stores, icon registry `lib/icons.ts`, Tailwind tokens) from
  `DESIGN.md`, the token values and the screens. No "Ported from ZCode" headers,
  no NOTICE/README work, no verbatim copying of source files or large blocks.
  Token values (colors, sizes, radii) may match.
- Isolated daemons only (own `SMIND_HOME`, free port), seeded data through the
  fake agent / devharness / `internal/qaframes`; no live model calls.
- One area per PR, from `origin/develop`, never stacked.

## References (ZCode files consulted, read-only)

- `refs/zcode/DESIGN.md` — typography (`text-ui-*`), spacing (4px unit), radius
  hierarchy, elevation, motion, workspace layout.
- `refs/zcode/packages/ui/src/styles.css` — `@theme` block, `.theme-zai-light`,
  `.theme-zai-dark` (the active palettes).
- Stage 3+ will add the component files each PR consults here.

## Stages

| # | Stage | PR | State |
|---|-------|----|-------|
| 0 | This plan | docs PR | this PR |
| 1 | Tokens | own PR | planned |
| 2 | Side-by-side audit → ranked diff list | separate Lead | in flight |
| 3 | Component ports, one PR per area (shell+sidebar, timeline, composer, settings, dialogs/menus) | ≥5 PRs | blocked on 2 |
| 4 | Mobile tokens | own PR | after 1 |
| 5 | Desktop titlebar chrome, ZCode style | own PR | never started |

## Stage 1 — tokens

### Inventory (2026-10-30, `refs/zcode` a60ba6a7 vs develop 6f4eb702)

A name-by-name diff of ZCode's `.theme-zai-light` / `.theme-zai-dark` against
`index.css` `:root` / `.dark`:

- **Already ported and equal**: 68 light / 69 dark colour tokens (background,
  foreground, card, popover, primary, secondary, accent, border family, input
  family, surface, menu, header, panel, sidebar, foreground-subtle/subtlest/inverse,
  success/warning/destructive, diff-added/removed, git-*, find-highlight, hover,
  selected, interaction-confirmation, toast, tooltip, tag, terminal-*).
- **Differs only by alias form** (13/12): e.g. ZCode `card-border: var(--color-border)`,
  smind `card-border: var(--border)`. Same resolved value; recorded in the table,
  not changed. `--color-input` differs on purpose (smind kept shadcn `--input`
  for `border-input`; the ZCode field background lives in `--input-bg`).
- **Missing and in scope** (to add): composer shadow, background-alt /
  background-win-alt, brand, user-message-bubble, tab / tab-active / tab-border,
  interaction-ask-{surface,foreground,fill}, success / warning / destructive /
  progress `-surface` + `-surface-foreground`, idle-task(+surface),
  trajectory-{user,assistant,reasoning,tool-call,tool-result} (the "timeline"
  colours), usage-chart-1..6, context-breakdown-1..7, usage-heatmap-0..4,
  `--lucide-subtlest-opacity`, `text-ui-settings-title(-lg)`.
- **Missing and out of scope** (no smind consumer / banned feature): workflow-*,
  remote-ssh/wsl/docker/continuity, file/skill/command/subagent/session/plugin
  node colours, plugin-paid-plan-badge, animated-gradient-text, `text-ui-2xs`,
  `text-mobile-input-safe`, feedback-privacy-hint, remote-foreground-muted.
- **Radius, spacing, motion, shadow**: ZCode defines no radius/spacing/motion
  tokens (it uses Tailwind defaults plus the DESIGN.md hierarchy rules); smind
  already runs on Tailwind defaults and keeps its own shadow tiers
  (`--elevation-shadow-*`) and motion durations. Nothing to port beyond the
  composer shadow. Applying the radius hierarchy to components is Stage 3.

### Name mapping rule

ZCode `--color-<name>` (Tailwind `@theme` token) ↔ smind raw var `--<name>` in
`:root` / `.dark`, re-exported as `--color-<name>` in `@theme inline`. Two other
families keep their ZCode name (`--text-ui-*`, `--shadow-composer-soft`,
`--lucide-subtlest-opacity`). The full table is a typed constant in
`web/packages/ui/src/lib/zcode-tokens.ts` (ZCode name, smind var, light value,
dark value, status `ported` | `alias` | `added`); the Stage 1 PR pastes it here.

### Acceptance criteria

- **AC1** Every token in the table exists in `index.css` with the table's light
  value in `:root` and dark value in `.dark` (exact string, or, for `alias`
  rows, the smind-side expression recorded in the table).
- **AC2** Every colour row is exported as `--color-<name>` in `@theme inline`,
  so `bg-<name>` / `text-<name>` utilities exist.
- **AC3** No existing token value changes except where the table says
  `alias`→`ported` is value-equal; visible diffs come only from the new tokens
  and their consumers.
- **AC4** The table is the only place smind↔ZCode names are mapped; the plan
  carries the same table.
- **AC5** Frames of 6 key screens (sidebar+task list, chat timeline, composer,
  a Settings page, a dialog, a menu), light and dark, at 1280 and 390, before
  and after; every image looked at; regressions fixed are listed.
- **AC6** `task lint` and `task test` green on the final head; one mutation per
  load-bearing test, listed in the PR.

### Named tests (web/packages/ui/src/lib/zcode-tokens.test.ts)

- `zcode token drift: every light value in the table matches index.css :root`
- `zcode token drift: every dark value in the table matches index.css .dark`
- `zcode token drift: every colour row is exported in @theme inline`
- `zcode token drift: table has no duplicate zcode or smind names`

Mutations (commit first, `git checkout --` the file after): change one value in
`index.css` `:root` (test 1 must fail); change one in `.dark` (test 2); delete
one `--color-*` line from `@theme inline` (test 3); duplicate a row in the table
(test 4).

## Stage 2 — audit

Audit run 2026-10-30 (ZCode web build ran for real, no model/account so no task
page; smind from an isolated daemon with a scripted ACP stub). ZCode is only
reachable at ≥1024px (its web build is not responsive), so 390 gives no parity
targets. Ranked, most visible first (Size S/M/L; Kind C = component structure,
T = token/class usage):

| # | Screen | What differs | Size | Area | Kind |
|---|---|---|---|---|---|
| 1 | Settings (every page) | ZCode: big H1 + subtitle, centred ~510px column on the shell background, rows are rounded-xl cards grouped with hairlines (label+desc left, control right). smind: tiny title in a header bar, flat rows, left-aligned in a wide bordered panel | L | settings | C |
| 2 | Settings nav | ZCode: "Back to workspace" pill, grouped labels, selected = rounded-lg pill. smind: close, density/radius differ | S | settings | C/T |
| 3 | Home / new-task landing | ZCode: greeting, project picker, big composer card. smind: blank "Select a task" | L | shell/composer | C |
| 4 | Light shell frame | ZCode: grey shell, main pane a floating white rounded panel with soft shadow. smind: flush | M | shell+sidebar | T+C |
| 5 | Command palette | ZCode: icon search, filter chips, groups, kbd hints. smind: flat lists | M | dialogs/menus | C |
| 6 | Composer menus | ZCode: icon+title+description+check items. smind: dense, no descriptions | M | composer/menus | C |
| 7 | Composer shell | similar card; toolbar IA differs | M | composer | C |
| 8 | Sidebar structure | different IA by design; density and bottom bar deltas | L | shell+sidebar | C |
| 9 | Shortcuts page | ZCode: table (Command / Keybinding chips / Scope / Actions), search with icon, Reset all. smind: list with text buttons | M | settings | C |
| 10 | Empty states (MCP etc.) | ZCode: scope selector + search, dashed card with two buttons. smind: centred text + one button | S | settings | C |
| 11 | Usage page | ZCode: stat strip in a rounded tray. smind: richer but flat | M | settings | C |
| 12 | Settings form controls | ZCode: select with icon+chevron, number with suffix, switch, segmented only for small sets | S | settings | T/C |
| 13 | Timeline tool cards | ZCode: bordered tool cards; smind: dense unbordered rows (needs a decision) | L | timeline | C |
| 14 | Timeline plan update | smind shows "Unrecognised event: plan" (functional gap) | M | timeline | feature |
| 15 | Assistant markdown | heading/inline-code spacing | S | timeline | T |
| 16 | Window controls | P5 desktop chrome | M | other | C |
| 17 | Banners / notices | different style | S | dialogs/menus | C |
| 18 | Task header | not captured | ? | shell | ? |

Suggested stage split: (1) settings: 1, 2, 9–12; (2) shell+sidebar: 4, 8, 16;
(3) composer + home: 3, 6, 7, 17; (4) dialogs/menus: 5, 6; (5) timeline: 13–15.

## Stage 3 — component ports

One PR per area, in audit-rank order: shell+sidebar, timeline, composer,
settings, dialogs/menus. Each: ACs + named tests per PR, light/dark frames at
1280 and 390 including transition frames, shared stores/pickers and the icon
registry reused. Filled once Stage 2 lands.

## Stage 3a — Settings (audit 1, 2, 9–12)

One PR. Reference-only (no ZCode source copied): a shared settings page shell
and row-card primitives, applied to every settings page with no behaviour
change (all `data-testid`s, store wiring and RPCs untouched).

### References (ZCode files consulted, read-only)

`refs/zcode/packages/ui/src/SettingsPage.tsx`, `settingsPageHelpers.tsx`,
`settings/ShortcutSettingsSection.tsx`, `settings/McpSettingsSection.tsx`,
`settings/UsageStatsSection.tsx`, `components/ui/{select,switch,input-group,kbd}.tsx`.

### Acceptance criteria

- **AC1** Settings content shows a large H1 (section label) with an optional
  subtitle (registry `description`), in a centred column (`max-w-[510px]`
  default, a `wide` opt-in for table-like pages), directly on the shell background (no
  inner bordered frame). The header strip keeps the window drag region and
  caption controls.
- **AC2** `SettingsGroup` (rounded-xl bordered card, hairline between rows) and
  `SettingsRow` (label + description left, control right; stacks below `sm`)
  primitives exist and every settings page uses them for its controls.
- **AC3** Nav: back-to-app pill, grouped labels, selected row = rounded-lg
  pill; behaviour (aria-current, testids, narrow list/detail) unchanged.
- **AC4** Shortcuts renders a table: Command / Keybinding (kbd chips) / Scope /
  Actions, search with icon, "Reset all to defaults" when overrides exist; Change,
  Record chord, Reset, conflict and capture behaviour unchanged.
- **AC5** Empty states (MCP servers, plugins, skills, automations) use a dashed
  `SettingsEmptyCard` with the existing actions.
- **AC6** Usage stat tiles sit in a rounded tray; plan usage and token table stay
  as is but card-wrapped.
- **AC7** Controls: `Switch` primitive (36×20 pill) replaces bare checkboxes where
  a boolean is toggled; select = 32px bordered with chevron; segmented
  control kept for ≤3-option sets (Theme, font size).
- **AC8** Light + dark frames at 1280 (and 390 where relevant), after-sheets in
  the audit's pair format at `/tmp/steward4/zcode-audit/after/settings/`.
- **AC9** `task lint` and `task test` green; one mutation per load-bearing test.

### Named tests

- `settings-layout.test.tsx`: `SettingsGroup renders rows with hairlines`,
  `SettingsRow shows label, description and control`, `SettingsEmptyCard shows
  actions`, `SettingsStatStrip renders every tile`
- `switch.test.tsx`: `Switch toggles and reports checked state`, `Switch is
  keyboard operable and disabled when asked`
- `settings-screen.test.tsx`: `shows the section label as the page H1`,
  `shows the registry description as a subtitle`
- `shortcuts-section.test.tsx`: `renders Command, Keybinding, Scope and Actions
  column headers`, `filters rows by search`, `Reset all appears only with
  overrides`

### Ported files and sources

None: reference-only reimplementation.

## Stage 4 — mobile tokens

Port the Stage 1 table into the Expo app's theme (`mobile/`), own PR, after 1.

## Stage 5 — desktop titlebar chrome

ZCode-style top overlay in the Tauri shell (traffic-light inset, caption
buttons, sidebar toggle position), own PR. Prior art:
[completed/desktop-chrome-sidebar-toggle.md](../completed/desktop-chrome-sidebar-toggle.md).

## Decisions

- Aliased-vs-resolved: where ZCode aliases (`var(--color-border)`) and smind
  aliases the raw var, the plan treats them as equal; the table records both.
- No ZCode token is added without a smind consumer or an explicit Stage 3/4
  need (see "out of scope" above).

## Progress

- [x] Inventory of what is already ported (2026-10-30).
- [x] Plan written (this PR).
- [ ] Stage 1 tokens PR.
- [x] Stage 2 audit handed over (table above).
- [ ] Stage 3a settings (this PR: in progress).
- [ ] Stage 3 PRs.
- [ ] Stage 4.
- [ ] Stage 5.

## Validation

Filled per stage as PRs land.
