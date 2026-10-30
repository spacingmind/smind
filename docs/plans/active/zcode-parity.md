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

Run by a separate Lead. Its ranked diff list is pasted here when handed over.

<!-- ranked audit list: pending -->

## Stage 3 — component ports

One PR per area, in audit-rank order: shell+sidebar, timeline, composer,
settings, dialogs/menus. Each: ACs + named tests per PR, light/dark frames at
1280 and 390 including transition frames, shared stores/pickers and the icon
registry reused. Filled once Stage 2 lands.

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
- [ ] Stage 2 audit handed over.
- [ ] Stage 3 PRs.
- [ ] Stage 4.
- [ ] Stage 5.

## Validation

Filled per stage as PRs land.
