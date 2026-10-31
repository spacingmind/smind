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

## Stage 2 audit: ranked diff list (handed over 2026-10-30)

ZCode ran for real (empty state only: no model, so no task page, tool cards or dialogs on its side; those are read from DESIGN.md and component structure). Sheets L = ZCode, R = smind. 390 gives no ZCode parity target (its web build is not responsive).

| # | Screen | What differs | Size | Stage |
|---|---|---|---|---|
| 1 | Settings (every page) | H1 + subtitle, centred ~510px column, rounded-xl row cards | L | 3a (done) |
| 2 | Settings nav | grouped sections, rounded-lg selected pill, back pill | S | 3a (done) |
| 3 | Home / new-task landing | brand mark, greeting, project chip, big composer card | L | 3c (done) |
| 4 | Light shell frame | floating white rounded panel, gap, soft shadow | M | 3b (done) |
| 5 | Command palette | icon search field, filter chips, grouped rows with leading icon and kbd hints, rounded-xl | M | 3d (this PR) |
| 6 | Composer / generic menus | item = icon + title + description + check, rounded-xl popover | M | 3c + 3d |
| 7 | Composer shell | card, bottom bar, circular send | M | 3c (done) |
| 8 | Sidebar structure | quick actions, "Task list" header | L | 3b (done) |
| 9 | Shortcuts page | table with kbd chips, scope, actions | M | 3a (done) |
| 10-12 | Empty states, usage, form controls | cards, stat strip, switch/select | S-M | 3a (done) |
| 13 | Timeline tool cards | bordered rounded-xl cards vs smind dense rows (needs a Human decision) | L | timeline stage |
| 14 | Timeline plan update | "Unrecognised event: plan" (feature gap) | M | timeline stage |
| 15 | Assistant markdown | heading/inline-code spacing | S | timeline stage |
| 16 | Window controls | back/forward, overlay titlebar | M | Stage 5 |
| 17 | Banners / notices | notice banner row | S | 3c (done) |
| 18 | Task header | not captured | ? | timeline stage |

Not captured by the audit: dialogs (new workspace, confirm, permission) on either side; transitions. Stage 3d captures them on the smind side.

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

## Stage 3b — Shell + sidebar (audit 4, 8; 16 is Stage 5)

One PR, reference-only (no ZCode source copied; `refs/zcode` was not on this machine,
so the shape comes from the audit's ZCode frames). No behaviour change: workspace
tree, status/stage views, unread/attention dots, drag, context menus, shortcuts,
collapsed rail and the mobile drawer are untouched.

### References (ZCode files consulted, read-only)

Audit frames `zc_{light,dark}_1280_home` only (sources: `WorkspaceSidebar.tsx`,
`WorkspaceSidebarFooter.tsx`, `project-sidebar/` as named in the audit).

### Acceptance criteria

- **AC1** Light theme: the content panel is a framed rounded panel (gap to the
  window edge, 1px border, soft shadow) over the grey shell; dark theme and the
  mobile/lights-bar layouts keep today's flush inset. The frame class set is one
  exported constant (`lib/shell-frame.ts`).
- **AC2** Sidebar gets a quick-actions block under the brand row: `New task`
  (icon + label), `Search` (opens the palette, shows the palette's live shortcut
  as a kbd), `Automations` (opens Settings -> Automations). Hidden in the collapsed rail.
  `New task` opens the new-task dialog for the selected task's workspace, or the
  only workspace; with several and no selection it opens the palette (which
  lists "New task in <workspace>"). Hidden while there are no workspaces.
- **AC3** The tree header reads "Task list" (was "Workspaces") with the same
  group-mode switch, search and add controls.
- **AC4** New icon `newTask` goes through `lib/icons.ts` (drift test green).
- **AC5** Frames: audit-format pair sheets (L = ZCode raw, R = smind after) for
  home/empty, tree with tasks, status view, collapsed rail at 1280 light+dark,
  390 smind-only, plus sidebar collapse/expand transition frames, in
  `/tmp/steward4/zcode-audit/after/shell-sidebar/`.
- **AC6** `task lint` + `task test` green; one mutation per load-bearing test.

### Named tests

- `lib/shell-frame.test.ts`: `light frame carries gap, border, rounded and shadow`,
  `dark frame resets the gap and shadow`
- `components/sidebar-quick-actions.test.tsx`: `renders New task, Search and Automations`,
  `Search shows the palette shortcut`, `rows call their handlers`
- `components/app-sidebar-quick-actions.test.tsx`: `New task opens the task dialog for the only workspace`,
  `New task with several workspaces and no selection opens the palette`,
  `quick actions are absent with no workspaces`, `tree header reads Task list`

### Ported files and sources

None: reference-only reimplementation.

## Stage 3c — Composer + home landing (audit 3, 6, 7, 17)

One PR, reference-only (no ZCode source copied, no "Ported from" headers; the shape comes
from the audit frames `zc_{light,dark}_1280_{home,modemenu,projmenu}`). No behaviour change
to the task composer: profiles, models, thinking, attachments, @file/PR/issue attach,
drafts, Cmd+L focus, queue-vs-interrupt (`whenBusy`), Esc-stop and Shift+Tab mode cycle
stay as they are. No daemon/wsapi change; the mobile app is untouched.

### References (ZCode files consulted, read-only)

Audit frames only (ZCode sources named by the audit: `WelcomeScreen.tsx`,
`NewTaskButtonGroup.tsx`, `LexicalChatInput.tsx`, `composer-toolbar-layout/`,
`ModelConfigSelect.tsx`, `ChatErrorBanner.tsx`).

### Design decisions

- The landing composer is **not** a second composer implementation of run logic: it shares
  the card shell constants (`lib/composer-shell.ts`) and `PromptTextarea` with the task
  composer, and creates the task through the same `task.create` RPC as the new-task dialog.
  The first prompt is carried into the new task as the composer draft (the mechanism the
  dialog already uses to carry a picked issue), so run config (profile/model/mode), the
  `whenBusy` choice and sending stay in the task composer, which owns them. Consequence: a
  landing submit creates the task and opens it with the prompt ready; one more Enter sends.
  Recommendation to the Human: keep it (auto-send would need a pending-send hand-off into
  the task composer's submit path); say so if you want it.
- Model/mode chips are not on the landing: they are per-chat run config keyed by a chat id
  that does not exist yet (inventing them would be a fake affordance).
- The notices row on the landing is the existing global `DesktopDaemonBanner` (rendered above
  every screen); the untrusted-source banner appears in the task view. Both adopt the new
  `NoticeBanner` primitive.

### Acceptance criteria

- **AC1 (item 3)** With no task selected the main pane shows a landing: smind's own mark,
  a short neutral greeting H1, a project chip (workspace picker, remembered in a preference
  store) and a composer card (shared shell, `PromptTextarea`, circular send). Submitting
  creates a task in the chosen project (title from the prompt's first line, max 60 chars)
  via `task.create`, seeds the draft with the full prompt, and selects the task. Disabled
  with a stated reason while disconnected, with no projects, or with an empty prompt.
- **AC2 (item 7)** The task composer and the landing share `composer-shell.ts` (card,
  send button classes); the send button is circular, and when disabled stays clearly a
  button (dimmed, not invisible). Textarea min height unchanged behaviour (2 rows).
- **AC3 (item 6)** The permission-mode menu items show icon + label + one-line description
  + check, in a `rounded-xl` ~220px popover; the project menu has a search field (shown
  from 6 projects up) and a footer action "Add project" that opens the existing
  new-workspace dialog.
- **AC4 (item 17)** `NoticeBanner` (info icon, text, up to two pill actions, dismiss `x`,
  tones info/warning/error) is used by `DesktopDaemonBanner` and `UntrustedBanner`; all
  existing test ids and behaviours stay.
- **AC5** Frames: audit-format sheets (L = ZCode raw, R = smind after) for home/empty,
  composer states (empty, with attachment, mode menu open, project menu open), notices;
  1280 light + dark, 390 smind-only, plus menu open/close transition frames, in
  `/tmp/steward4/zcode-audit/after/composer-home/`. All looked at.
- **AC6** `task lint` + `task test` green on the final head; one mutation per
  load-bearing test.

### Named tests

- `components/ui/notice-banner.test.tsx`: `renders icon, text and tone`,
  `renders primary and secondary actions`, `dismiss calls onDismiss and is absent without it`
- `components/pickers/pickers.test.tsx`: `mode items show description and check`
- `lib/composer-shell.test.ts`: `card and send classes are shared constants` (pins
  `rounded-2xl` card and `rounded-full` send)
- `components/home/home-landing.test.tsx`: `renders mark, greeting, project chip and composer`,
  `submit creates a task in the chosen project and selects it`, `submit seeds the composer draft with the prompt`,
  `title is the first line truncated to 60 chars`, `send is disabled when empty, offline or without projects`,
  `project menu filters by search and picks a project`, `project choice is remembered`,
  `Add project calls the handler`
- `stores/home-project-store.test.ts`: `persists the chosen workspace id`
- `App.test.tsx`: `shows the landing when no task is selected` (replaces the old text assertion)

### Ported files and sources

None: reference-only reimplementation.

## Stage 3d — Dialogs + menus (audit 5 and the generic part of 6)

One PR, reference-only (shape from the audit frames `zc_{light,dark}_1280_palette`; no ZCode source copied, no "Ported from" headers). No daemon/wsapi change, no new commands, no new shortcuts. Palette behaviour (Mod+K toggle with the modal keyboard lock, arrow/Enter/Escape, pointer-follows-highlight, focus restore, registry contributions) stays.

### References (ZCode files consulted, read-only)

Audit frames only; ZCode sources named by the audit: `command-center/CommandCenterDialog.tsx`, `components/ui/command.tsx`, `components/ui/dropdown-menu.tsx`.

### Design decisions

- Groups stay smind's own (Tasks, Workspaces, Open, Chats, Files, Actions, Commands, Settings, Agents). Chips are derived by a pure `paletteCategory(group)`: **All**, **Tasks** (Tasks, Workspaces, Chats), **Actions** (everything that is not Tasks/Files), **Files** (Files). A chip with no commands is not shown (so Files is absent without a task). No ZCode "Suggested/Panels/Configure" groups are invented.
- Leading icon per row comes from an optional `Command.icon` (registry key) falling back to a per-group icon in `palette/categories.ts`; every glyph through `lib/icons.ts`.
- Kbd hints stay `formatCombo` of the live binding, rendered by one shared `ui/kbd.tsx` (also used by the sidebar quick action and the shortcuts dialog).
- Dialog anatomy: `DialogContent` keeps its Radix focus trap/Escape/overlay; header, `DialogBody`, footer spacing and close affordance are defined once in `ui/dialog.tsx` and `confirm-dialog.tsx` follows. Menu anatomy: `DropdownMenuItem`/`ContextMenuItem` get optional `icon`, `description`, `shortcut` (children unchanged when absent) and `rounded-lg`; content `rounded-xl`.

### Acceptance criteria

- **AC1 (item 5)** The palette has a pill search field with the registry search icon, filter chips (All/Tasks/Actions/Files, only non-empty categories, `role="tab"`-style buttons with `aria-pressed`), groups with sentence-case headings, a leading icon on every row, a right-aligned kbd hint (live shortcut), and a `rounded-xl` dialog. Choosing a chip narrows the list (and the search composes with it); an empty result shows the existing empty message. Arrow wrap, Enter, Escape, Mod+K toggle and focus restore are unchanged.
- **AC2** `ui/kbd.tsx` is the one kbd chip; palette, sidebar quick action and shortcuts dialog use it.
- **AC3 (generic dialogs)** `ui/dialog.tsx` defines the anatomy once (`rounded-xl`, header/body/footer padding, close button hit area and focus ring); `ConfirmDialog`, new workspace, new task, archive, rename dialogs inherit it with no per-dialog padding overrides; permission request card keeps its behaviour and adopts the same radius and spacing tokens.
- **AC4 (menus)** `DropdownMenuItem` and `ContextMenuItem` accept `icon`, `description`, `shortcut`; the sidebar tree context menu and the task/workspace "more" menus use icons; no behaviour change.
- **AC5 (evidence)** Audit-format sheets (L = ZCode raw frame, R = smind after) in `/tmp/steward4/zcode-audit/after/dialogs-menus/`: palette (empty query, query, chip-filtered, empty result), each dialog, menus; 1280 light and dark, dialogs at 390; transition frames. All looked at.
- **AC6** `task lint` + `task test` green on the final head; one mutation per load-bearing test.

### Named tests

- `palette/categories.test.ts`: `paletteCategory maps groups to chips`, `unknown groups fall into Actions`, `iconForGroup returns a registry key for every known group`
- `components/ui/kbd.test.tsx`: `renders each key as a cap`, `renders a mac combo as one cap`
- `components/command-palette.test.tsx` (extended): `shows a leading icon on every row`, `chips filter by category`, `chips without commands are hidden`, `chip and query compose`, `kbd hint shows the live shortcut`
- `components/ui/menu-item.test.tsx`: `renders icon, title, description and shortcut`, `plain children render unchanged`
- `components/ui/dialog.test.tsx`: `content uses the shared anatomy classes`, `footer and body slots render`
- Existing dialog/menu suites (`confirm-dialog`, `crud-dialogs`, `tree-action-dialogs`, `archive-dialog`, `permission-card`) stay green unchanged.

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
- [x] Stage 3a settings (this PR): shell, nav, row-card primitives, Switch, Shortcuts table, empty cards, usage tray applied to General, Appearance, Notifications, Shortcuts, GitHub, Agents, MCP, Skills, Plugins, Automations, Providers, Usage, Connection.
- [x] Stage 3b shell + sidebar (this PR): light framed content panel, sidebar quick actions, "Task list" header, `newTask` icon.
- [x] Stage 3c composer + home landing (this PR): `NoticeBanner`, rich select items for the mode menu, shared composer shell, home landing with project chip.
- [~] Stage 3d dialogs + menus (this PR): in progress.
- [ ] Stage 3 PR (timeline).
- [ ] Stage 4.
- [ ] Stage 5.

## Validation

Filled per stage as PRs land.
### Stage 3a validation (settings)

- AC1/AC3: `settings-screen.tsx` — H1 + subtitle in a centred column (510px; 3xl for
  table/card pages, 5xl for Shortcuts), nav items `rounded-lg`, back pill.
  Tests: `shows the section label as the page H1`, `shows the registry description as a subtitle`.
- AC2/AC5/AC6/AC7: `settings-layout.tsx` (`SettingsGroup` with `actions` slot,
  `SettingsRow`, `SettingsEmptyCard`, `SettingsStatStrip`) and `ui/switch.tsx`, applied across all
  sections. Notifications' sound toggle is now the shared Switch.
- AC4: Shortcuts table (Command / Keybinding / Scope / Actions); rows stack below `sm`.
  Scope shows "Global" (every binding is app-level today, no per-binding scope data).
- AC8: sheets (L = ZCode raw frame, R = smind after) in
  `/tmp/steward4/zcode-audit/after/settings/`:
  `settings_{dark,light}_1280_{a,b,c}.png` (general+appearance, shortcuts+MCP, usage+skills) and
  `settings_{dark,light}_390_a.png` (smind after only: ZCode's web build is not responsive, so
  390 has no ZCode side). All looked at.
- Not visually verified: the Connection and daemon pages (not in the nav of the isolated
  daemon's browser build; covered by tests only), and the Plugins/Automations non-empty states.
- Behaviour unchanged: every `data-testid`/aria-label kept; full `task test` green.
- GLM ledger: 12 peer-glm runs (tasks 70, 71, 73, 74, 75, 76 x3, 77, 82 x3), 0 Sonnet; one
  rate-limit stall (5h window) on task 76, resumed after the Human's quota reset. Failure
  modes seen: wrong layout fix twice (shortcuts clipping), stray re-indent, button inside a title prop.


### Stage 3b validation (shell + sidebar)

- AC1: `lib/shell-frame.ts` (`SHELL_FRAME_INSET_CLASS`, `SHELL_FRAME_PANEL_CLASS`) used in
  `App.tsx` for the inset and content panel when not mobile and not under the
  traffic-lights bar. Light shows the gap, border, rounded-xl and soft shadow; dark keeps the
  flush inset (`dark:` resets). Tests: `light frame carries gap, border, rounded and shadow`,
  `dark frame resets the gap and shadow`; `desktop-chrome.test.tsx` now asserts the dark classes.
- AC2/AC3: `sidebar-quick-actions.tsx` wired in `app-sidebar.tsx`; tests in
  `sidebar-quick-actions.test.tsx` and `app-sidebar-quick-actions.test.tsx`.
  No new keyboard binding was invented: New task shows no kbd (ZCode's would need a new
  shortcut), Search shows the live palette binding.
- AC4: `Icons.newTask` (SquarePen); icon drift test updated.
- AC5: sheets (L = ZCode raw home frame, R = smind after; ZCode has no tasks, so its home
  frame is the left side of every row) in `/tmp/steward4/zcode-audit/after/shell-sidebar/`:
  `shell-sidebar_{light,dark}_1280_a.png` (home, tree), `..._b.png` (status view, collapsed
  rail), `transitions_light_1280.png` (collapse/expand frames), `sheet390.png` (390 home and
  drawer, light+dark, smind only). All looked at. The screenshot latency (> 200 ms animation)
  caught only settled collapse frames, not mid-width ones; the width/opacity animation code is
  unchanged by this PR.
- Not changed: footer (usage, inbox, host, help, theme, settings stay), row density, the
  workspace tree, Projects/Tasks sections, back/forward arrows (Stage 5).
- AC6: `task lint` clean; web vitest 3968 tests (2 `App.responsive` timeouts under load,
  7/7 green alone); Go `cmd/smind` daemon-log tests only pass with a temp HOME (they read the
  real daemon log), 95 s green that way. Mutations (all failed the named test, files restored):
  drop the shadow token (`shell-frame.test.ts`), disable the kbd
  (`Search shows the palette shortcut`), `list.length === 1` -> `7` (`New task opens the task
  dialog for the only workspace`), drop the `!empty` guard (`quick actions are absent with no
  workspaces`), rename the label back (`tree header reads Task list`).
- GLM ledger: 3 peer-glm runs (tasks 109 frame, 110 component + icon, 111 wiring + tests),
  0 Sonnet, no rate limit, no turn-cap or wrong-edit failures; `glm-acp-agent` ran with
  `--max-turns 500`.

### Stage 3c validation (composer + home)

- AC1: `components/home/home-landing.tsx` (+ `stores/home-project-store.ts`, wired in `App.tsx`; "Add project" via `lib/app-events.ts` -> sidebar dialog). Tests in `home-landing.test.tsx`, `home-project-store.test.ts`, `app-sidebar-crud.test.tsx`, `App.test.tsx`.
- AC2: `lib/composer-shell.ts` shared by `composer.tsx` and the landing; send/stop/send-now are `rounded-full`. Disabled send keeps its muted fill (unchanged `execute` variant).
- AC3: `SelectItem` gained optional `icon`/`description` (label stays the option's accessible name); `ModeSelect` pill uses them in a `rounded-xl` 220px menu; model popover `rounded-xl`. Project menu: search from 6 projects, "Add project" footer.
- AC4: `ui/notice-banner.tsx` (`NoticeBanner`, `NoticeAction`) used by `DesktopDaemonBanner` and `UntrustedBanner` (frames show the untrusted one; the daemon banner is desktop-only and was not rendered).
- AC5: sheets (L = ZCode raw, R = smind after) in `/tmp/steward4/zcode-audit/after/composer-home/`:
  `composer-home_{light,dark}_1280_{a,b,c}.png` (home + project menu; mode menu + attachment; model popover + notice),
  `composer-home_before-after_{light_1280,390}.png` (smind before | after), `transitions_light_1280_mode.png`,
  `transitions_dark_1280_proj.png` (open 0/40/80/160 ms, close 0/40 ms: clean fade/zoom, no jump). All looked at; ZCode has no task page, so its home frame is the left side for task-composer rows.
- Not done / differences kept: no model/mode chips on the landing (per-chat run config), no auto-send (the prompt arrives as the task's draft), project chip strip is subtle, the daemon update banner not visually verified, ZCode's "+" attach and "Manage models" entries are not on the landing.
- Mutations (all failed the named test, files restored): `rounded-full`->`rounded-xl` (composer-shell), error role (notice-banner), drop select `aria-label` (2 composer tests), title cap 60->80, drop draft seed, search threshold off (home-landing), drop mode description (pickers).
- Peer ledger: GLM 2 runs done (task 143 style strip, 142 model popover), 5 runs hit the 5h rate limit (07:23, resets 10:42, cancelled and resent), Sonnet 5 runs done (tasks 142 and 143, steps A1, A2, B1, B2, B3). Review found 3 regressions Sonnet left (composer/toolbar tests keyed on option text, a Workspace fixture type error that broke `bun run build`), fixed by the Lead.

