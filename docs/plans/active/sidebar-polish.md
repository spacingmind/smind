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
- SP-S7-AC2: nesting indentation is per-row padding (12px per depth level) — nested lists carry no margin/padding — so hover/selected highlight spans the whole sidebar width.
- SP-S7-AC3: at most one thin guide line per expanded parent; DOM/ARIA nesting unchanged.
- SP-S7-AC4: measured targets (screenshots/probe): depth-0 title starts at most 32px from its row's own padding edge (excluding container indent); at a 192px sidebar a depth-2 title has ~100px of width. Actuals are recorded in Validation.

### S8 — persisted expansion
- SP-S8-AC1: workspace expansion, space open, Pinned section and status-group collapse persist across a remount via `STORAGE_KEYS`.
- SP-S8-AC2: a newly created workspace is expanded even if a stale id for it was persisted as collapsed.
- SP-S8-AC3: searching never writes the stored state, and clearing the query restores the tree as it was.
- SP-S8-AC4: corrupt/absent storage falls back to "everything expanded".

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
- Visual tokens: existing classes only (`bg-hover`, `bg-selected`, `text-ui-base`, `text-foreground-subtle(st)`, `bg-tag`); no new color values.

## Progress

- [ ] Plan committed
- [ ] S1 scoped actions
- [ ] S2 keyboard
- [ ] S3 single status slot
- [ ] S4 single-line row
- [ ] S5 container rows
- [ ] S6 task context menu
- [ ] S7 fade + per-row indent
- [ ] S8 persisted expansion
- [ ] Screenshots light/dark 256/192 + hover + focus + D1 probe
- [ ] `task test`, `task lint`, tsc
- [ ] PR opened

## Validation

(filled in as items land — AC → test name / screenshot path)
