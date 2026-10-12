# Sidebar polish (ZCode visual + Paseo interaction)

Size: M (5) — scope 1, PRs 1, risk 1, decisions 0 (all approved by the Supervisor), verification 2 (screenshots).

Goal: bring the task sidebar to "clean like ZCode": single-line 32px task rows,
row actions scoped to their own row, keyboard reachable, one status slot,
consistent container-row chevrons, right-click menu, fade (not ellipsis) title
overflow with per-row indentation, and persisted expansion state. Web UI only;
no daemon/Go change.

Sources (local-only, gitignored): `docs/research/local/2026-10-12-sidebar-polish-lead-report.md`;
refs `refs/zcode/packages/ui/src/{TaskListItem.tsx,WorkspaceSidebarItem.tsx,TaskTitleOverflowText.tsx,workspace-grouped-tasks/}`
and `refs/paseo/packages/app/src/components/sidebar/`.

Out of scope: S9 scroll-edge fade / show-more, S10 drag reorder (needs a user
decision on persisted order), S11 filter chips and ⌘1-9 badges. Do not touch
`components/composer/*`.

## Acceptance Criteria

### S1 — row actions scoped to their own row
- SP-S1-AC1: hovering one task row mounts the actions (⋯) trigger for that row only; every other task row (ancestors, descendants, siblings) has none.
- SP-S1-AC2: a row's actions are mounted when the row is hovered, keyboard-focused (focus-visible within the row) or its menu is open, and unmounted otherwise.
- SP-S1-AC3: with a menu open, moving the pointer away keeps that row's trigger mounted until the menu closes.
- SP-S1-AC4: a parent row's hover does not mount its children's triggers and a child's hover does not mount its parent's.

### S2 — keyboard
- SP-S2-AC1: a task row is a real focusable button (Tab reaches it) with a visible focus-visible ring.
- SP-S2-AC2: Enter and Space on a focused row select the task (native button semantics).
- SP-S2-AC3: ArrowDown/ArrowUp move focus to the next/previous visible task row (rows under a collapsed parent are skipped, they are not in the DOM).
- SP-S2-AC4: ArrowLeft collapses an expanded parent (or moves focus to the parent row from a child); ArrowRight expands a collapsed parent (or moves into its first child).
- SP-S2-AC5: nested list/ARIA structure is intact (`ul > li > ul`, `aria-expanded` on the collapse toggle).

### S3 — one leading status slot
- SP-S3-AC1: exactly one status slot per task row, always in the DOM at a constant width, with or without a signal.
- SP-S3-AC2: priority attention(permission|error) > run error > running > finished attention > unread > none; exactly one indicator is rendered in the slot.
- SP-S3-AC3: the slot exposes `data-attention-reason`, `data-run-status` and `data-unread` for the winning/known signals so tests and tools can query them.
- SP-S3-AC4: slot width is identical whether empty or populated (layout stability).

### S4 — single-line row
- SP-S4-AC1: a task row is one 32px line: title, then a right cluster; no second meta line is rendered.
- SP-S4-AC2: the right cluster shows the diffstat (`+n −n`) only when files changed, and a compact relative time from `UpdatedAt` (`now`, `49m`, `3h`, `2d`, `1mo`, `1y`).
- SP-S4-AC3: on hover / focus / menu-open the ⋯ replaces the time in the same fixed-width slot (no layout shift).
- SP-S4-AC4: the branch appears only in the hover card (and the row's aria-label); tier is not visible in the row but is in the hover card and the aria-label.
- SP-S4-AC5: parents keep a child-count chip and the collapsed-parent attention rollup.

### S5 — container rows
- SP-S5-AC1: the space ⋯ never overlaps its chevron; workspace and space ⋯ are not mounted at idle and mount on hover / focus-within / menu-open.
- SP-S5-AC2: workspace, space, pinned and status-group headers use one chevron (same side — right after the title, as in ZCode group headers —, same size, muted).
- SP-S5-AC3: a status-group count renders as a small pill.

### S6 — task context menu
- SP-S6-AC1: right-click on a task row opens a context menu with the same items (Move to space, Pin/Unpin, Mark unread, Archive task) and the same handlers as the ⋯ menu, built from one shared definition.

### S7 — title overflow and indentation
- SP-S7-AC1: an overflowing title fades out at its right edge (mask) instead of showing an ellipsis; a title that fits has no mask.
- SP-S7-AC2: nesting indentation inside the task tree is per-row padding (12px per depth level) -- task-tree nested lists carry no margin/padding -- so hover/selected highlight spans its container's content box. (Round 1: container content is inset behind a guide line, see SP-R1-AC1.)
- SP-S7-AC3: no guide lines inside the task tree; DOM/ARIA nesting unchanged. (Round 1 replaces the original "one thin guide line per expanded parent" with container guides, SP-R1-AC1.)
- SP-S7-AC4: measured targets (screenshots/probe): every depth-0 title in a container starts at the same x regardless of siblings (SP-R1-AC2). Actuals are recorded in Validation.

### S8 — persisted expansion
- SP-S8-AC1: workspace expansion, space open, Pinned section and status-group collapse persist across a remount via `STORAGE_KEYS`.
- SP-S8-AC2: a newly created workspace is expanded even if a stale id for it was persisted as collapsed.
- SP-S8-AC3: searching never writes the stored state, and clearing the query restores the tree as it was.
- SP-S8-AC4: corrupt/absent storage falls back to "everything expanded".

### Review round 1 (PR #300 review: hierarchy, alignment, narrow titles, typography)
- SP-R1-AC1: workspace content (spaces / Ungrouped / flat tasks), Pinned and status-group content sit behind one subtle guide line under the header's icon (`ml-4 border-l pl-1`, ZCode's `ml-4 border-l pl-2` narrowed); a space's / Ungrouped content carries the same guide only on a wide sidebar (>= 14rem container query) so a 192px sidebar keeps its title width. Spaces therefore read as nested under their workspace, not as siblings of the next workspace.
- SP-R1-AC2: depth-0 task titles share one x within a container whether or not the list contains a parent: every root row reserves the chevron gutter (`ROOT_PAD_PX` = 4px inset + 12px gutter); a task row sits at the same x in tree, search, Pinned and status-group lists.
- SP-R1-AC3: container titles (workspace, space, Ungrouped, Pinned, status group) use `FadeText` (no ellipsis) and the chevron sits right after the title; the right padding for the ⋯ box (`pr-7`) and the hidden aggregate dot apply only while the row is hovered / focused / has its menu open.
- SP-R1-AC4: typography is muted and graded: task titles `text-ui-base` at .76 opacity (1 on hover / selected); workspace header `text-ui-base font-medium`; space / Ungrouped / Pinned / status-group headers `text-ui-sm` + `text-foreground-subtle` (-> foreground on hover); relative time `text-ui-xs text-foreground-subtlest`.

## Test Scenarios

Named to match the tests (`web/packages/ui/src`):

- `components/app-sidebar.test.tsx` › "task row actions (S1)": `hovering a task row mounts only that row's actions`, `hovering a child row does not mount its parent's or siblings' actions`, `focus-visible within a row mounts its actions`, `an open menu keeps its row's actions mounted after the pointer leaves`.
- "task row keyboard (S2)": `a task row is a focusable button and Enter selects it`, `ArrowDown and ArrowUp move focus between visible task rows`, `ArrowLeft collapses an expanded parent and ArrowRight expands it`, `ArrowLeft on a child moves focus to its parent`.
- "task row status slot (S3)": `renders exactly one status slot with constant width`, `attention outranks running outranks unread` (table-driven), `slot exposes data-attention-reason, data-run-status and data-unread`; the old three-slot layout-stability tests are re-baselined to the single slot.
- "task row (S4)": `is a single line: no meta row`, `shows diffstat only when files changed and a compact relative time`, `the actions replace the time in the same slot on hover`, `branch and tier live in the hover card and the aria-label`, `parents keep the child-count chip`.
- `lib/relative-time.test.ts` › `formatRelativeTimeShort` buckets.
- "container rows (S5)": `workspace and space actions are absent at idle and mount on hover`, `space actions do not sit on the chevron`, `all container headers share one chevron class`, `status-group count is a pill`.
- "task context menu (S6)": `right-click offers the same items as the actions menu`, `context menu Archive and Pin call the same handlers`, `context menu offers Move to space targets`.
- "title overflow and indentation (S7)": `the title fade mask applies only when the title overflows` (mocked scrollWidth), `indentation is per-row padding by depth and nested lists add no indent`.
- "expansion persistence (S8)": `collapsed workspace persists across a remount`, `collapsed space persists`, `collapsed Pinned section persists`, `collapsed status group persists`, `a created workspace is expanded even if its id was persisted collapsed`, `search does not touch stored state`, `corrupt stored value falls back to expanded`; `lib/sidebar-preferences.test.ts` for the node-set reader/writer.
- Review round 1 (`components/app-sidebar-rows.test.tsx`, `app-sidebar.test.tsx`): `task-tree nested lists add no indent; container content wrappers carry the guide`, `workspace content and space content each carry one guide-line wrapper`, `depth-0 rows have identical padding with and without a parent sibling`, `flat views use the same root padding`, `container titles render via FadeText and the mask applies only when overflowing`, `the workspace header reserves its right padding only while active`, `workspace title is the loud top-level heading; space titles are the quieter sub-heading`, `the task title is muted at idle and full opacity when hovered or selected`, `a selected task's title is at full opacity`, `the relative time is the quietest element on the row`.
- Live probes (screenshots): D1 probe (hover one nested row → exactly one ⋯), keyboard-focus state, light+dark at 256px and 192px.

## Decisions

Supervisor-approved (2026-10-12), implemented as written unless a deviation is listed:

- One PR, one commit per item S1..S8.
- S1: per-row wrapper (not the `li` containing descendants) owns hover/focus/menu-open state in JS; actions are conditionally mounted so tests are deterministic (jsdom cannot evaluate CSS `group-hover`).
- S3: re-baselining `task-run-status-slot` / `task-unread-slot` / `task-attention-slot` into one slot is accepted; priority rules keep test coverage.
  - Deviation/clarification: the latest run being `done` or `stopped` (no unread, no attention) renders no dot — ZCode's idle is empty. `error` and `running` stay. Container aggregates are unchanged.
- S4 supersedes the "tier on the meta line" part of task-hierarchy-tiers U1-AC3 (tier is now in the hover card and the aria-label; child-count chip and rollup stay on the row).
- S5: chevron goes immediately after the title (ZCode group header, `workspace-grouped-tasks/types.ts`), `size-3.5`, `text-foreground-subtlest`.
- S6: `ContextMenuSub*` wrappers are added to `components/ui/context-menu.tsx` so the Move-to-space submenu can be shared.
- S7: ZCode's marquee is not ported — only the right-fade mask. Container levels (workspace/space) add no extra indentation to task rows; only task depth does, so rows at the same task depth align across containers.
- S8: one `STORAGE_KEYS.collapsedSidebarNodes` key holds a string-id set (`ws:<id>`, `space:<id>`, `ungrouped:<wsId>`, `pinned`, `status:<key>`) of *collapsed* nodes, so anything new (a new workspace, a new space) defaults expanded without a write.
Review round 1 (Supervisor findings 1-4, human asked to fix before merge):

- Hierarchy: ZCode's group content `ml-4 border-l pl-2` is ported as `ml-4 border-l pl-1` (20px per level instead of 25px). Two guide levels plus the always-reserved chevron gutter left ~60px of title at 192px, so a space's guide is wide-only (container query `@min-[14rem]/sb`); below that, tasks stay flush and their titles still line up with the space's label. This supersedes the S7 "container levels add no indentation / no guide line" decision; highlight of rows inside a container now spans from the guide line to the sidebar edge (what ZCode does), workspace headers stay full-width.
- Alignment: the chevron gutter is always reserved for root rows (`ROOT_PAD_PX`), costing 12px on lists with no parents; chosen over a per-list conditional because the Supervisor asked for one stable column. Row inset 8 -> 4px (the container guide's `pl-1` supplies the rest).
- Relative time (refs checked): ZCode shows a time on every row (`text-ui-sm text-foreground-subtle`), Paseo's rows show none; neither hides it by recency. Kept on every row, quieter (`text-ui-xs text-foreground-subtlest`); no recency logic. To drop it entirely later, remove the `task-row-time` span.
- Muting: Paseo's title opacity .76 -> 1 is applied to task titles only (workspace/space headers are graded by size/weight/colour, not opacity).
- Visual tokens: existing classes only (`bg-hover`, `bg-selected`, `text-ui-base`, `text-foreground-subtle(st)`, `bg-tag`); no new color values.

## Progress

- [x] Plan committed
- [x] S1 scoped actions (6ebcbc2)
- [x] S2 keyboard: focusable rows, Enter/Space, arrow keys (all four arrows landed)
- [x] S3 single status slot
- [x] S4 single-line row
- [x] S5 container rows
- [x] S6 task context menu
- [x] S7 fade + per-row indent (+ geometry follow-up after the screenshot pass)
- [x] S8 persisted expansion
- [x] Screenshots light/dark 256/192 + hover + focus + D1 probe (local: `/private/tmp/sidebar-polish-shots/`)
- [x] `task test`, `task lint`, tsc (see Validation)
- [x] PR opened
- [x] Review round 1 fixes (findings 1-4): `3594811`/`77e4b054` (hierarchy + alignment), then fade/typography commit; screenshots in `tmp/before` / `tmp/after` (local)
- [ ] Merged (Lead never merges)

Tests live in `components/app-sidebar-rows.test.tsx` (new, S1-S8) with the
older `app-sidebar*.test.tsx` / `open-in-editor.test.tsx` / `App.test.tsx`
re-baselined where the row's contract changed (status slots, meta line, tier,
hover-only triggers). Shared helpers: `test/sidebar-helpers.ts`.

Not done / follow-ups: S9 (scroll-edge fade, show-more), S10 (drag reorder:
needs a decision on persisted order), S11 (filter chips, ⌘1-9 badges). Touch
devices have no hover, so the ⋯ is reachable there only through the row's
long-press context menu (S6) -- unverified on a real touch device. Pre-existing,
untouched: the "reveal the selected task's collapsed ancestors" effect in
`AppSidebar` re-expands immediately, so a parent of the selected task cannot be
collapsed.

## Validation

Spec scenarios in the list above are the test names in
`components/app-sidebar-rows.test.tsx` unless a file is named.

- SP-S1-AC1..AC4: "task row actions (S1)" -- `hovering a task row mounts only that row's actions`, `hovering a child row does not mount its parent's or siblings' actions`, `hovering a parent row does not mount its descendants' actions`, `leaving a row unmounts its actions`, `keyboard focus within a row mounts its actions, and blur unmounts them`, `an open menu keeps its row's actions mounted after the pointer leaves`. Live D1 probe against a sandbox daemon: hovering the nested row of task 9 left exactly `["9"]` triggers in all four theme x width runs (`/private/tmp/sidebar-polish-scripts/report.json`).
- SP-S2-AC1..AC5: "task row keyboard (S2)" -- focusable button, Enter/Space are native button activation (jsdom cannot synthesize the click, so the test asserts `<button type=button>` + click selects), ArrowDown/Up, skipping collapsed rows, ArrowLeft/Right collapse/expand and parent/first-child hops, modified arrows ignored, nested `ul > li > ul` + `aria-expanded`. Screenshot: `sidebar-*-focus.png` (ring wraps the whole row), `states3-*-collapsed-keyboard.png`.
- SP-S3-AC1..AC4: "task row status slot (S3)" -- single slot at constant width, 8-case priority table (`priority: ...`), data attributes, idle slot carries none; the old three-slot layout-stability tests were re-baselined in `app-sidebar.test.tsx`. Screenshot: `states-*-unread.png`.
- SP-S4-AC1..AC5: "task row, single line (S4)" + `lib/relative-time.test.ts` (`formatRelativeTimeShort`) + the re-baselined signal/tier tests in `app-sidebar.test.tsx`. Measured row height 32px in every run (`report.json`).
- SP-S5-AC1..AC3: "container rows (S5)". Screenshots: `states-*-spacehover.png` (⋯ clear of the chevron, highlight spans the row), `states2-dark-status-groups.png` (count pill).
- SP-S6-AC1: "task context menu (S6)". Screenshot: `states-*-contextmenu.png`.
- SP-S7-AC1..AC3: "title overflow and indentation (S7)". Screenshots at 256/192 px light and dark (`sidebar-*-wide.png`, `sidebar-*-narrow.png`).
- SP-S7-AC4 (original measurement, superseded by SP-R1-AC2 below; `report.json`): depth-0 title starts 30px from the row's left edge in a list with no parents and 42px where the list contains a parent (the 12px gutter for the chevron) -- the 32px target holds only for the first case. At a 192px sidebar the depth-2 title is 101px wide (89px for the depth-1 title that sits next to a child-count chip); at 256px it is 135px.
- SP-S8-AC1..AC4: "expansion persistence (S8)" + `lib/sidebar-preferences.test.ts`. Live: after collapsing a space and a workspace and reloading, both stayed collapsed (`states2-*-after-reload.png`, stored `["space:2","ws:2"]`).
- SP-R1-AC1..AC4 (measured against an isolated sandbox daemon, 1440x900, `tmp/after/report.json` vs `tmp/before/report.json`, local-only): at 256px depth-0 titles start at x=88 (inside a space) / x=67 (flat workspace) for every row of the container -- before: 50 or 38 (and 62/74 for children) depending on whether the list had a parent; at 192px x=67 in every container. Container titles: `smind-core` fits in full at 192px (before: "smind-co…" ellipsis, 4px short of the 73px it needed); "Sidebar polish" overflows by 6px and fades (before: "Sidebar …"). Hover on a nested task still mounts exactly one ⋯ (`["9"]`). Costs: depth-2 title width at 192px is 84px (was 101px) and at 256px 97px (was 135px) -- the price of the guide indent + reserved gutter; a space's guide is hidden below 14rem to bound it.
- Screenshots (light + dark x 256/192): `{light,dark}-{256,192}-{full,taskhover,spacehover,workspacehover,focus,statusgroups}.png` in `tmp/before/` and `tmp/after/` (gitignored).
- Commands: see the PR description for the exact `tsc`/vitest/`task test`/`task lint` runs and results.
