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
  previews. (Office mode and doc/PDF previews are a separate Human-approved
  epic, ADR-0049 / `office-mode.md`; this rule only scopes the visual-parity work.) No ZCode/Zai logos, product names or branding.
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
| 5 | Desktop titlebar chrome, ZCode style (back/forward) | own PR | done |
| 6 | Welcome / empty (landing greeting, illustration, send on Enter, chips; empty sidebar) | own PR | this PR |

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

Audit frames (`zc_{light,dark}_1280_{palette,modemenu,projmenu}`) and ZCode `DESIGN.md` (Dialogs, Menus and selection overlays radius rules); sources named by the audit: `command-center/CommandCenterDialog.tsx`, `components/ui/command.tsx`, `components/ui/dropdown-menu.tsx`.

### Design decisions

- Groups stay smind's own (Tasks, Workspaces, Open, Chats, Files, Actions, Commands, Settings, Agents). Chips are derived by a pure `paletteCategory(group)`: **All**, **Tasks** (Tasks, Workspaces, Chats), **Actions** (everything that is not Tasks/Files), **Files** (Files). A chip with no commands is not shown (so Files is absent without a task). No ZCode "Suggested/Panels/Configure" groups are invented.
- Leading icon per row comes from an optional `Command.icon` (registry key) falling back to a per-group icon in `palette/categories.ts`; every glyph through `lib/icons.ts`.
- Kbd hints stay `formatCombo` of the live binding, rendered by one shared `ui/kbd.tsx` (also used by the sidebar quick action and the shortcuts dialog).
- Radii follow ZCode `DESIGN.md` (read for this stage): dialog shells stay `rounded-2xl`, menu shells `rounded-lg`, menu items `rounded-md`, 2px gap between menu items. The first pass used `rounded-xl` for dialogs and menus and was corrected in review (smind's existing radii were already right). The palette keeps `rounded-xl` as the one exception, because the audit frame shows that radius.
- Dialog anatomy: `DialogContent` keeps its Radix focus trap/Escape/overlay; `ui/dialog.tsx` defines `DialogBody`, the header's close-button clearance, a 28px hover-filled close button and a semibold title once, and every dialog (`ConfirmDialog`, new workspace/task, archive, delete, rename) inherits it unchanged. The permission card is an inline card (not a modal) and already uses the dialog radius; it is left as is.
- Menu anatomy: `DropdownMenuItem`/`ContextMenuItem` get optional `icon`, `description`, `shortcut` through `MenuItemBody` (children render unchanged when none is given). The sidebar tree menus already carry icons, so no call site needed a change.

### Acceptance criteria

- **AC1 (item 5)** The palette has a pill search field with the registry search icon, filter chips (All/Tasks/Actions/Files, only non-empty categories, `role="tab"`-style buttons with `aria-pressed`), groups with sentence-case headings, a leading icon on every row, a right-aligned kbd hint (live shortcut), and a `rounded-xl` dialog. Choosing a chip narrows the list (and the search composes with it); an empty result shows the existing empty message. Arrow wrap, Enter, Escape, Mod+K toggle and focus restore are unchanged.
- **AC2** `ui/kbd.tsx` is the one kbd chip; palette, sidebar quick action and shortcuts dialog use it.
- **AC3 (generic dialogs)** `ui/dialog.tsx` defines the anatomy once (`rounded-2xl` shell, `DialogBody`, header clearance, close button hit area and focus ring); `ConfirmDialog`, new workspace, new task, archive, delete dialogs inherit it with no per-dialog overrides; the permission card keeps its behaviour.
- **AC4 (menus)** `DropdownMenuItem` and `ContextMenuItem` accept `icon`, `description`, `shortcut`; menu shells are `rounded-lg` with a 2px item gap and `rounded-md` items; no behaviour change.
- **AC5 (evidence)** Audit-format sheets (L = ZCode raw frame, R = smind after) in `/tmp/steward4/zcode-audit/after/dialogs-menus/`: palette (empty query, query, chip-filtered, empty result), each dialog, menus; 1280 light and dark, dialogs at 390; transition frames. All looked at.
- **AC6** `task lint` + `task test` green on the final head; one mutation per load-bearing test.

### Named tests

- `palette/categories.test.ts`: `paletteCategory maps groups to chips`, `unknown groups fall into Actions`, `iconForGroup returns a registry key for every known group`
- `components/ui/kbd.test.tsx`: `renders each key as a cap`, `renders a mac combo as one cap`
- `components/command-palette.test.tsx` (extended): `shows a leading icon on every row`, `chips filter by category`, `chips without commands are hidden`, `chip and query compose`, `kbd hint shows the live shortcut`
- `components/ui/menu-item.test.tsx`: `renders icon, title, description and shortcut`, `plain children render unchanged`
- `components/ui/dialog.test.tsx`: `content uses the shared anatomy classes`, `footer and body slots render`
- `components/ui/menu-item.test.tsx` (more): `item stack is spaced and items are rounded-md`; `components/command-palette.test.tsx` (more): `all chip is pressed when the selected category disappears`
- Existing dialog/menu suites (`confirm-dialog`, `crud-dialogs`, `tree-action-dialogs`, `archive-dialog`, `permission-card`) stay green unchanged.

### Ported files and sources

None: reference-only reimplementation.

## Stage 4 — mobile tokens

Port the Stage 1 table into the Expo app's theme (`mobile/`), own PR, after 1.

## Stage 5 — desktop titlebar chrome (audit 16)

One PR, reference-only (no ZCode source copied, no "Ported from" headers), web
only: no Rust, daemon or wsapi change. Prior art:
[completed/desktop-chrome-sidebar-toggle.md](../completed/desktop-chrome-sidebar-toggle.md)
already gives the desktop shell the overlay title bar (macOS traffic-light inset,
fullscreen shrink, drawn Windows/Linux caption buttons, `data-tauri-drag-region`
header, double-click maximize, one window-level sidebar toggle). What ZCode has
and smind lacks is the **back / forward arrow pair** in the top-left cluster. This
stage adds it, with real history behaviour.

### References (ZCode files consulted, read-only)

`DesktopTopOverlay.tsx` (cluster anatomy: toggle, back/forward, new task, at a fixed x
after the lights), `DesktopWindowFrame.tsx`; audit frames `zc_{light,dark}_1280_home`
(back/forward at the top-left of the sidebar). (The `refs/zcode` clone is not
checked out in this worktree; anatomy taken from the audit frames and the earlier
desktop-chrome plan's findings.)

### Design decisions (Supervisor default, Human may veto)

- **History model.** Back/forward walk an in-app history of *navigation
  entries*, not the browser history (the app uses no router; the hash only carries
  the selected task). An entry is `{ view: "workspace" | "settings" | "inbox", taskId }`
  (plugin pages are not recorded). A new entry is pushed when the selected task or
  the full-pane view changes, unless the change was itself a back/forward step; pushing
  truncates the forward tail; an entry equal to the current one is not duplicated;
  capped at 50. A step to a task that no longer exists (archived/deleted) is dropped
  and the step continues in the same direction.
- **Where.** Desktop build only, in the window-level top-left overlay next to the
  sidebar toggle (the web build keeps its header: no fake window chrome). The sidebar's
  top strip and the header pad around the widened zone through `toggleZoneEndPx`.
- **Shortcuts.** `Mod+[` / `Mod+]` already mean previous / next task in the list
  (Paseo's workspace-prev/next) and stay. New actions `nav.back` / `nav.forward`,
  desktop-only, `Alt+ArrowLeft` / `Alt+ArrowRight` (default focus rule: blocked while
  typing in a text field or a terminal, where Alt+Arrow is word movement). No clash
  with the existing table (checked by the table's conflict test). Tooltips show the chord.
- Disabled state: the button is `disabled` (muted, not focusable) when there is nothing
  in that direction.
- Windows/Linux keep the drawn caption buttons (already there); macOS keeps native
  lights. Not changed here.

### Acceptance criteria

- **AC1** `lib/nav-history.ts` pure model: push, dedupe, truncate-forward, cap 50, back, forward, can-back/can-forward.
- **AC2** `WindowNavButtons` render in the desktop shell's top-left overlay (all desktop OSes), not in the web build; aria labels "Back"/"Forward", tooltips with the chord, `disabled` when empty, click steps the history.
- **AC3** The overlay cluster's right edge feeds `toggleZoneEndPx`, so the sidebar strip and header never sit under the arrows (macOS lights + toggle + arrows; fullscreen shrinks; Windows rail).
- **AC4** `nav.back` / `nav.forward` registered in the shortcut registry (desktop-only rows), handled in the shell, listed in the shortcuts help; no combo conflicts.
- **AC5** The shell records task/view changes and steps through them without re-recording; stepping to a gone task skips it.
- **AC6** Drag regions: the cluster wrapper stays click-through (`pointer-events-none`), only buttons take events; fullscreen/double-click behaviour unchanged.
- **AC7** After sheets in `/tmp/steward4/zcode-audit/after/p5-titlebar/` (L = ZCode raw home frame, R = smind desktop-mode after), 1280 light+dark, narrow width, sidebar toggle transition frames; all looked at. State plainly whether native chrome was captured.
- **AC8** `task lint` + `task test` green; one mutation per load-bearing test.

### Named tests

- `lib/nav-history.test.ts`: `push appends and moves the cursor`, `push after back truncates the forward tail`, `equal consecutive entry is not duplicated`, `history is capped at 50 entries`, `back and forward clamp at the ends`, `can-back and can-forward reflect the cursor`
- `lib/window-chrome.test.ts` (extended): `toggle zone end includes the nav buttons`
- `components/window-nav-buttons.test.tsx`: `renders back and forward with labels`, `both disabled when history is empty`, `click calls the handlers`, `tooltip shows the chord`
- `components/desktop-chrome.test.tsx` (extended): `window nav buttons render in the desktop shell only`, `overlay wrapper stays click-through`
- `keyboard/shortcuts.test.ts` (extended): `nav back and forward are desktop-only rows`, no-conflict test keeps passing
- `hooks/use-nav-history.test.tsx`: `records a selection change`, `stepping does not re-record`, `skips a gone task`

### Ported files and sources

None: reference-only reimplementation.

## Stage 6 — Welcome / empty (home landing + empty sidebar)

Size: M (scope 2: web only; risk 2: the landing now sends; verification 3: light+dark frames at 1280 and 390, every greeting bucket, chips open, send in flight). Human decisions 2026-10-31; UI lane, after P5. One PR.

### References (ZCode files consulted, read-only)

- `refs/zcode/packages/web` and `packages/ui` empty-state / welcome screen (greeting, mark, composer card, project chip): structure, spacing and radii only. Copy, illustration and logic are our own.

### Design decisions

- **Greeting** (`lib/greeting.ts`, `hooks/use-greeting.ts`): bucket from the LOCAL clock (`new Date().getHours()`, never the daemon): late night 00-05, morning 05-12, afternoon 12-18, evening 18-24; 3 own lines per bucket. One line is picked per mount and held (stable across re-renders); a 60 s tick and `visibilitychange` re-read the bucket, and only a bucket change re-picks.
- **Illustration** (`components/home/home-illustration.tsx`): own inline SVG line drawing, `currentColor` + theme tokens, `aria-hidden`, replaces the logo image above the greeting.
- **Send on Enter from the landing**: the landing creates the task, writes the prompt as the task draft (as before) and records a *pending send* (`stores/pending-send-store.ts`: taskId + run config + timestamp, in memory, 30 s TTL). The task `Composer` of the new task's default chat takes it once (`take` is atomic, so StrictMode/re-render cannot send twice), applies the run config through the toolbar's new `applyState` action, and then calls its own existing `submit()` — same run config, `whenBusy: "queue"`, attachments path; there is no second send implementation. A failed send leaves the prompt in the draft with the composer's error; the task exists.
- **Chips on the landing**: the same `RunConfigToolbar` (`ModelPicker`, `Mode`) with `taskId`/`chatId` null (persists nothing, no chat id invented). The state is the pending run config; on send it is also saved to `stores/home-run-config-store.ts` (persisted preference, next landing's initial state via the toolbar's new `initialState` prop).
- **Empty sidebar**: with no workspace the tree shows a "Projects" section (add action, "No projects yet") and a "Tasks" section ("No tasks yet") instead of the welcome paragraph; header add button and group control stay as stage 3b built them.

### Acceptance criteria

1. Greeting H1 follows the local-time bucket; edges 04:59/05:00, 11:59/12:00, 17:59/18:00, 23:59/00:00.
2. The greeting does not change on re-render; it changes when the bucket changes while mounted.
3. The illustration renders above the greeting, aria-hidden, no external asset, light and dark.
4. Submitting on the landing creates the task and sends the first prompt once, no second Enter.
5. A failed first send keeps the prompt in the task draft with the error shown; the task exists. No double send on re-render or StrictMode.
6. The landing shows the same model and mode pickers; the choice is applied to the new task's first chat before the first send and remembered as the next default; no chat id is created early.
7. With no workspace the sidebar shows Projects / Tasks sections with the add action and "No tasks yet".
8. No backend change; mobile untouched.

### Named tests

- `lib/greeting.test.ts`: `bucket edges 04:59/05:00`, `bucket edges 11:59/12:00`, `bucket edges 17:59/18:00`, `bucket edges 23:59/00:00`, `every bucket has 3 lines`.
- `hooks/use-greeting.test.tsx`: `stable across re-render`, `updates on bucket change`, `updates on visibilitychange`.
- `stores/pending-send-store.test.ts`: `take is atomic`, `expires after the ttl`, `other task does not take`.
- `components/composer/composer-pending-send.test.tsx`: `pending send applies the config then submits once`, `no double send under StrictMode`, `send failure keeps the draft`, `non-default chat does not take it`.
- `components/composer/run-config-toolbar.test.tsx`: `applyState replaces the state and blocks the default-agent seed`, `initialState seeds a toolbar with no persisted state`.
- `components/home/home-landing.test.tsx`: `landing submit creates and sends once`, `send failure keeps the draft`, `landing chips apply to the created chat`, `chips default to the last used`, greeting and illustration render.
- `components/app-sidebar.test.tsx` / `app-sidebar-crud.test.tsx`: `empty sidebar shows Projects and Tasks sections`.

### Ported files and sources

None (reference-only; own code, copy and illustration).

### Progress / Validation

- [x] Stage 6 implemented (this PR): greeting + illustration, pending-send hand-off, landing chips, empty sidebar.

**Validation (2026-10-31)**

- `task test` 394 files / 4251 tests pass, `task lint` clean, `bunx tsc -b` clean on the final head.
- One mutation per load-bearing test (each failed the named test, file restored with `git checkout --`): `greeting.ts` `hour < 12` to `<= 12` (bucket edges 11:59/12:00); `use-greeting.ts` bucket compare removed (stable across re-render); `pending-send-store.ts` clear removed (take is atomic, expires after the ttl); `composer.tsx` hand-off `submit()` removed (pending send applies the config then submits once, no double send under StrictMode, send failure keeps the draft); `home-landing.tsx` pending-send `set` removed (landing submit creates and sends once, chips apply, chips default to last used); `app-sidebar.tsx` "No tasks yet" text changed (empty sidebar test).
- Found in review: the landing chips added a second `run.list` on connect and broke four App.test attention tests that index run.list requests; fixed by not reading the observed model without a chat id.
- Frames (isolated daemons, stub agent only, fake clock; L = ZCode raw, R = smind), `/tmp/steward4/zcode-audit/after/welcome/`: `welcome_light_1280.png`, `welcome_dark_1280.png` (four greeting buckets), `welcome_390.png`, `welcome_390_extra.png`, `welcome_chips_1280.png` (model picker + mode menu), `welcome_empty_sidebar.png`; singles `welcome_bucket-{before,after}_light_1280.png` (11:59 to 12:00 swap), `welcome_send-{0ms,600ms}_{light,dark}_1280.png` (send in flight: exactly one user prompt in the timeline, no second Enter).
- Looked at: buckets, chips, empty sidebar and send frames in light and dark. Observed, not changed: at 0 ms after Enter the landing still shows while task.create is in flight (send button busy); at 390 the chips are icon-only; the disabled empty-sidebar composer is low contrast by design; a Codex row hint is clipped in the (pre-existing) model picker.
- Not done: the landing does not show a "draft only" mode; mobile untouched (the landing is web only).
- Peer ledger: GLM 0 runs (2 sends refused, rate limited until 18:28, Steward switched to Sonnet); peer-sonnet 5 runs (A greeting/illustration/sidebar $0.23, B hand-off $0.46, C landing + fix run, frames), no failures; no wrong edits, one review finding (run.list ordering) fixed by Sonnet.

## Stage 7 — Automations page (own page, off-state explainer, task chip)

Size: L (6) — scope 2 (store + wsapi + config + web), PRs 1, risk 1, decisions 0 (ADR-0044 already keeps the daemon switch file/CLI-only), verify 2 (light+dark frames at 1280/390). Human-approved UI-lane task, after Welcome/empty, before UAT. One PR. Contract: `docs/plans/active/zcode-parity-automations.contract.md`.

### References (ZCode files consulted, read-only)

- `refs/zcode` sidebar quick actions ("Automations" row) and settings row-card anatomy: structure and spacing only. ZCode has no screen for this page: frames have no ZCode left side (the audit raw set has no Automations frame); the sheets pair smind before/after instead.

### Design decisions

- **Own page**: `activeView === "automations"` in `App.tsx`, exactly like the inbox (a full-pane view, back arrow, sidebar sheet closes first on mobile). Opened by the sidebar quick action (`onOpenAutomations`) and a palette command `open-automations`. Settings keeps only a link row (`Automations` section shows the daemon state read-only and an "Open Automations" button) and the settings-section id stays registered so deep links work.
- **No second implementation**: the list, switch, row, dialogs, history dialog (with memory panel) and form are the existing components, moved from `components/settings/automations-section.tsx` into `components/automations/automations-page.tsx` (+ `automation-list.tsx`); the form/history files keep their code. New: `formatLocal` next to `formatUtc` in `lib/automation-format.ts`; the next-run line reads `next run 2026-11-01 09:00 UTC (16:00 your time)`.
- **Off card** (`components/automations/automations-off-card.tsx`): what automations do (cron in UTC, CI failure -> task), the rails (budget cap, auto-pause on failures, never merges or releases), the exact command `smind config set automations.enabled true` with a copy button, then the line `smind stop && smind serve`. No UI switch for the daemon flag.
- **`smind config set automations.enabled true`**: a new `kindBool` key in `internal/config/edit.go` (`true|false` only, field-named error, comments kept); `automations.enabled` is the only bool key added. Docs via `task docs:cli`.
- **Task chip**: no field on `tasks`; the link is `automation_audit.task_id` (decision `fire`). `task.list`/`task.get` gain read-only `automationId` and `automationName` (omitempty) on `taskView`, filled by one batched store query (`AutomationLinksForTasks`). Web: `AutomationChip` in the sidebar task row, the task header and the inbox row; filter "Started by automation" in the sidebar task-list header (persisted preference store like the group mode).

### Acceptance criteria

1. Automations is a top-level page opened from the sidebar quick action and the palette; Settings no longer renders the list.
2. The page lists automations with on/off switches, the next run in UTC with the local time alongside, per-automation history (audit + memory when enabled) and a "New automation" button opening the existing form.
3. With automations off the page shows the explainer card with the exact CLI, a copy button and the restart line; nothing in the UI flips the daemon flag.
4. `smind config set automations.enabled true|false` works, rejects other values naming the key, keeps comments; `smind config get automations.enabled` reads it.
5. `task.list` and `task.get` expose `automationId`/`automationName` for a task an automation fired, and nothing for any other task.
6. A task started by an automation shows an Automation chip (name on hover) in the sidebar row, the task header and inbox rows; the sidebar filter "Started by automation" shows only those.
7. Light+dark frames at 1280 and 390 (off, empty-on, list, history, chip + filter); transitions reviewed. Mobile app untouched (follow-up: mobile shares `automation.list`, no chip there yet).

### Named tests

- Go: `TestConfigSetAutomationsEnabled`, `TestConfigSetAutomationsEnabledRejectsNonBool`, `TestAutomationLinksForTasks`, `TestTaskListExposesAutomationLink`.
- Web: `automations-off-card.test.tsx` (`shows the exact command and copies it`, `shows the restart line and the rails`), `automations-page.test.tsx` (`lists rows with the switch, UTC and local next run`, `off state shows the card, not the list`, `new automation opens the form`), `app-automations-page.test.tsx` (`sidebar quick action opens the page`, `palette command opens the page`, `settings section only links to the page`), `automation-chip.test.tsx` (`renders the name on hover`, `absent without a link`), `app-sidebar-automation-filter.test.tsx` (`filter shows only automation tasks`), `automation-format.test.ts` (`formatLocal`).

### Ported files and sources

None (reference-only).

### Progress / Validation

- [ ] Stage 7 in progress (this PR).

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
- [x] Stage 3d dialogs + menus (this PR): palette (chips, icons, Kbd), `Kbd`, `MenuItemBody`, dialog/menu anatomy.
- [ ] Stage 3 PR (timeline).
- [ ] Stage 4.
- [ ] Stage 5 (this PR): back/forward history in the desktop top-left overlay.

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


### Stage 3d validation (dialogs + menus)

- AC1: `command-palette.tsx` (pill search with registry search icon, `command-palette-chips` with All/Actions/Tasks/Files only for non-empty categories, leading icon per row, `Kbd` hint from the live binding, sentence-case group headings, `rounded-xl`), `palette/categories.ts`, optional `Command.icon`; specific icons set on the shell and sidebar commands (`App.tsx`, `app-sidebar.tsx`: one `icon:` line each). Keyboard behaviour untouched (all existing palette tests green). Tests: `shows a leading icon on every row`, `chips filter by category`, `chips without commands are hidden`, `chip and query compose`, `kbd hint shows the live shortcut`, `all chip is pressed when the selected category disappears`, `categories.test.ts` (3).
- AC2: `ui/kbd.tsx` used by the palette, `sidebar-quick-actions.tsx` and `shortcuts-dialog.tsx` (local `KeyCaps` removed). Tests: `renders each key as a cap`, `renders a mac combo as one cap`.
- AC3/AC4: `ui/dialog.tsx` (`DialogBody`, close button, header clearance, semibold title; shell stays `rounded-2xl`), `ui/menu-item-body.tsx` + `DropdownMenuItem`/`ContextMenuItem` props, menu shells `rounded-lg` with `gap-0.5`, items `rounded-md`. Tests: `content uses the shared anatomy classes`, `footer and body slots render`, `renders icon, title, description and shortcut`, `plain children render unchanged`, `item stack is spaced and items are rounded-md`. Existing `confirm-dialog`, `crud-dialogs`, `tree-action-dialogs`, `archive-dialog`, `permission-card` suites unchanged and green.
- AC5: sheets (L = ZCode raw frame, R = smind after; ZCode has no dialog or task page, so its palette frame is the left side of the palette/dialog rows and its mode/project menu frames of the menu rows) in `/tmp/steward4/zcode-audit/after/dialogs-menus/`: `dialogs-menus_{light,dark}_1280_a.png` (palette: empty, query, Actions chip, empty result), `..._b.png` (palette kbd hint, new workspace, new task, archive confirm), `..._c.png` (sidebar context menu, task "more" menu, permission request card), `dialogs-menus_390_after.png` (palette + chip at 390, smind only), `dialogs-menus_390_dialogs.png` (new workspace and archive at 390), `transitions_light_1280_palette.png` (palette open 4 frames, close 3, animations slowed 12x), `transitions_dark_1280_dialog.png` (new workspace open/close). All looked at. Open frames show the clean overlay fade plus zoom; the dialog close frames were captured after the exit animation finished (screenshot latency) so mid-close was not seen for the dialog; the animation classes are unchanged by this PR.
- Not done / known: ZCode's "Suggested/Panels/Configure" groups and Open workspace/Toggle terminal entries are not invented (smind's own groups are kept); the palette's "New workspace" and "New task in ..." commands do nothing at 390 while the sidebar drawer is closed (the dialogs live in the sidebar component; pre-existing, not touched); the permission card (inline, not a modal) is unchanged.
- AC6: `task lint` clean; `task test` green on `75d4e696` (web 365 files / 4033 tests, Go, desktop/docs-site node tests, install scripts), run with a temp HOME and shared caches. Mutations (all failed the named test, files restored): `kbd.tsx` drop the `+` split (`renders each key as a cap`), `categories.ts` drop Chats from tasks (`paletteCategory maps groups to chips`), `command-palette.tsx` ignore the category filter (`chips filter by category`, `chip and query compose`), drop the present filter (`chips without commands are hidden`), pressed uses the raw category (`all chip is pressed when the selected category disappears`), `menu-item-body.tsx` drop the description (`renders icon, title, description and shortcut`), `dialog.tsx` `rounded-2xl` to `rounded-xl` (`content uses the shared anatomy classes`), `dropdown-menu.tsx` drop `gap-0.5` (`item stack is spaced and items are rounded-md`).
- Peer ledger: GLM 6 runs (task 165: steps 1-3, review fix, step 4 icons; task 166: menu item and dialog anatomy, radius correction), 0 Sonnet, no rate limit, no turn cap (`glm-acp-agent --max-turns 500` seen). Failures: the first radii (rounded-xl dialogs and menus) were my brief's mistake, corrected after reading ZCode `DESIGN.md`; GLM left the chip's pressed state on the raw category (caught in review, fixed by GLM first time).


## Stage 3e — Timeline (audit 13, 14, 15)

One PR, reference-only (no ZCode source copied, no "Ported from" headers). Web only: no daemon/wsapi change, because the daemon already forwards an ACP `plan` update as a `raw` run event (`rawKind: "plan"`, `rawPayload` = the ACP update JSON; ADR-0010). The gap is only in the web timeline model.

### References (ZCode files consulted, read-only)

`refs/zcode/DESIGN.md` ("Chat, Tooling" and the Markdown type scale); audit frames `zc_{light,dark}_1280_home` (ZCode has no task page without a model, so tool cards and the plan card are read from DESIGN.md).

### Design decisions

- Supervisor default 2026-10-31, Human may veto: expanded tool calls get the bordered, rounded card look (`rounded-lg` card, monospace output wells `bg-muted/50`-style via tokens); collapsed tool rows stay compact one-liners. Grouped runs, Detailed/Overview, copy buttons, diff/file chips, run footer, whenBusy markers and the error box are untouched.
- Plan updates: a new timeline item kind `plan` built from `raw` events with `rawKind === "plan"`. Each ACP plan update carries the whole plan, so a new update replaces the earlier plan item **in place** (same id, same position) within a run. Malformed entries are dropped; a plan with no valid entries falls back to the neutral row. Shown in Detailed and Overview.
- Every other unknown event kind degrades to a neutral collapsed row ("Event: <kind>", details closed, payload inside) instead of "Unrecognised event"; Detailed only, as today.
- Mobile: its timeline is a separate line-based model (`mobile/src/runTimeline.ts`) that already drops `raw`; a plan card there is a **follow-up** (not in this PR).
- Markdown: inline-code chip, heading rhythm, code block, table and list spacing adjusted by token classes where the zoomed pass shows a visible difference.

### Acceptance criteria

- **AC1 (item 14)** An ACP `plan` raw event renders a plan card: header "Plan" with "n of m done", entries with a status icon (pending / in progress / completed) and a priority marker, collapsed by default when every entry is done and open otherwise, `aria` labelled; a later update replaces it in place.
- **AC2** Unknown raw kinds and unknown wire types render a neutral collapsed row, never the text "Unrecognised event".
- **AC3 (item 13)** Expanded tool calls render as a bordered rounded card with a status icon in the header (check / x / spinner), monospace wells for input/output; collapsed rows unchanged in shape; all existing tool-call tests keep passing.
- **AC4** User bubble `rounded-lg bg-secondary`-style per DESIGN.md using tokens.
- **AC5 (item 15)** Assistant markdown: inline-code chip, heading and block spacing, code block, table, list, link differences fixed.
- **AC6** After sheets (L = ZCode raw, R = smind after) in `/tmp/steward4/zcode-audit/after/timeline/`: seeded run (thinking, plan, read/search/edit/execute with one failure, markdown with headings/code/table), 1280 light+dark, 390, expanded and collapsed, Detailed vs Overview, expand/collapse transition frames. All looked at.
- **AC7** `task lint` + `task test` green on the final head; one mutation per load-bearing test.

### Named tests

- `hooks/use-run-timeline.test.ts`: `plan raw event becomes a plan item`, `a later plan update replaces the plan in place`, `invalid plan entries are dropped`, `plan with no valid entries falls back to unknown`, `other raw kinds stay unknown`
- `components/timeline/plan-card.test.tsx`: `shows n of m done`, `renders status and priority per entry`, `collapsed when all done and open otherwise`
- `components/timeline/timeline-row.test.tsx`: `plan item renders a plan card`, `unknown raw kind renders a neutral row without "Unrecognised"`
- `components/timeline/detail-level.test.ts`: `overview keeps plan items`
- `components/timeline/tool-call.test.tsx` (extended): `expanded card is bordered with a status icon`, `failed call shows the failure icon`
- `components/timeline/timeline-markdown.test.tsx`: `inline code is a chip`, `headings keep the type scale`

### Ported files and sources

None: reference-only reimplementation.

### Progress / Validation

Status: shipped in this PR (stage 3e). Mobile plan card is a follow-up (separate line-based model).

- AC1: `hooks/use-run-timeline.ts` (`TimelinePlanItem`, defensive `planEntries`, in-place replace), `components/timeline/plan-card.tsx`, `timeline-row.tsx` `case "plan"`; icons `circle`, `listChecks` in `lib/icons.ts`; copy-as-plain-text handles a plan item (`timeline-text.ts`). Overview keeps the plan (`detail-level.test.ts`).
- AC2: unknown raw kinds and wire types read "Event: <kind>" in a collapsed row (no "Unrecognised" anywhere in the web source).
- AC3: `tool-call-card.tsx` expanded frame (`rounded-lg border`), status check / x / spinner icon, `border-t` detail body; terminal/diff/payload wells share `rounded-md bg-surface`; collapsed row unchanged. The borderless-chrome allow-list in `run-timeline.test.tsx` names the open tool-call frame.
- AC4: user bubble `rounded-lg bg-secondary`.
- AC5: markdown inline-code chip `rounded-md px-1.5`, code well bordered `p-3`, table header fill, h1/h2 spacing, links `text-primary`.
- AC6 sheets (L = ZCode raw `zc_*_1280_home`, since ZCode has no task page without a model; R = smind after) in `/tmp/steward4/zcode-audit/after/timeline/`: `timeline_{light,dark}_1280_a.png` (plan + collapsed rows, expanded cards, markdown), `timeline_{light,dark}_1280_overview.png` (Overview view; before vs after expanded), `timeline_{light,dark}_390.png` (before | after pairs: collapsed, markdown, expanded, Overview), `transitions_light_1280_toolcard.png` (open 0/60/260 ms, close 0/60 ms: the card swaps without animation, only the chevron rotates; no glitch frame). All looked at. Seeded from a scripted ACP stub (thought, plan, read/search/edit/execute with one failure, markdown with heading/code/table/link).
- Known / not done: the edit card shows the raw ACP diff payload as JSON for this stub's `diff` content shape (pre-existing renderer behaviour, untouched); mobile plan card; ZCode's code-block copy bar not added (smind's copy buttons stay on tool output).
- Mutations (all failed the named test, files restored): `findIndex` -> -1 (`a later plan update replaces the plan in place`), `initiallyOpen` negation dropped (`collapsed when all done and open otherwise`), `Event:` -> `Unrecognised event:` (3 timeline-row tests), drop the open border (`expanded card is bordered with a status icon`), chip `rounded-md` -> `rounded-sm` (`inline code is a chip`).
- Peer ledger: GLM 3 runs done or started (task 198 plan card: done, 5 commits; task 199 tool cards + markdown: 2 runs died on `429 Rate limit reached for requests` before any edit, the third finished), 0 Sonnet. Plan-quota rate limit 12:50-14:20 cost a 1h30 wait. `glm-acp-agent` ran with `--max-turns 500`. Review: no findings needing a fix.

### Stage 5 progress / validation

Status: shipped in this PR (stage 5, web only; no Rust/daemon change, so no cargo run).

- AC1: `lib/nav-history.ts` (+ test), AC2: `components/window-nav-buttons.tsx` hosted by `window-sidebar-toggle.tsx` (`nav` prop), AC3: `lib/window-chrome.ts` (`NAV_*` constants, `navLeftPx`, `toggleZoneEndPx` now spans toggle + arrows, which the sidebar strip and header already pad by), AC4: `nav.back`/`nav.forward` rows (`Alt+ArrowLeft`/`Alt+ArrowRight`, desktop-only), handled in `App.tsx` and allowed while Settings/Inbox cover the shell, AC5: `hooks/use-nav-history.ts` wired in `App.tsx` (`applyNavEntry` drops a gone or archived task and keeps walking), AC6: overlay wrapper stays `pointer-events-none`, only the buttons take events.
- Not covered by a vitest: the `App.tsx` wiring itself (`applyNavEntry`, handler enablement); covered by the hook tests plus a scripted Playwright run on the desktop-mode build (`~/.smind/dogfood/p5/nav.cjs`: click back/forward and Alt+Arrow step through task selections, buttons disable at the ends).
- AC7 evidence: native chrome (real traffic lights) was **not** captured; the desktop-mode web bundle (`VITE_SMIND_DESKTOP=1`) ran in Chromium with a stubbed Tauri bridge and a macOS/Windows user agent, against an isolated daemon seeded through a scripted ACP stub. The macOS inset (x < 84) is blank space in the frames. Sheets (L = ZCode raw `zc_*_1280_home`, R = smind after) in `/tmp/steward4/zcode-audit/after/p5-titlebar/`: `p5_light_1280_macos.png`, `p5_dark_1280_macos.png` (home + collapsed), `p5_1280_windows.png`, `p5_800_narrow.png` (before | after), `p5_transition_light_1280.png` (sidebar-toggle strip; the cluster keeps one x in every frame), `p5_before_after_macos_light.png`. All looked at. Measured rects: toggle x 84..112, back 116..144, forward 148..176 on macOS.
- Scripted Playwright run: task1 -> task2 -> task3, Back click -> task2, Alt+Left -> task1, Alt+Right -> task2, Forward click -> task3 (Forward disabled at the end), tooltip `Back (⌥←)`. The history also keeps the initial no-task home entry, so Back from the first task lands on the empty home.
- Decisions: `Mod+[`/`Mod+]` keep meaning previous/next task; history chords are `Alt+Arrow` (Supervisor default, Human may veto). On a collapsed Windows rail the header now starts after the arrows. The wordmark in the expanded strip is dropped at the minimum sidebar width on macOS (existing container-query behaviour).
- Mutations (all failed the named test; files restored): slice(0, index+1)->slice(0) (`push after back truncates the forward tail`), cap 50->500 (`history is capped at 50 entries`), drop `NAV_BUTTONS_WIDTH_PX` from the zone (`toggle zone end includes the nav buttons` + 2 more), remove `desktop: true` (`nav back and forward are desktop-only rows`), `disabled={false}` (`both disabled when history is empty`), drop `pointer-events-auto` (`overlay wrapper stays click-through`), `dropEntry` -> no-op (infinite loop, worker OOM: the run fails, not a clean assertion). `pending && sameEntry` guard in `useNavHistory`: first survived (test changed `current` only after the cursor moved); its test ("stepping does not re-record") must make `apply` change `current` before resolving; that rewrite was handed to GLM but hit the GLM plan rate limit (reset 2026-10-31 11:28Z), so it lands as a follow-up commit on this PR.
- References consulted: `DesktopTopOverlay.tsx`, `DesktopWindowFrame.tsx` (anatomy only, via the earlier desktop-chrome plan and audit frames).
