# Pane split tree: arbitrary VSCode-style layouts (port from paseo, button-driven)

## Context

Today a task's content area supports exactly **one** split: `use-task-tabs.ts`'s `PaneId = "primary" | "side"`, `TaskTabsState { primary: PaneState; side: PaneState | null }`. `App.tsx` (lines 461-534) renders this as a single `ResizablePanelGroup` with up to 2 hardcoded `ResizablePanel`s, each its own `PaneTabStrip` (a Radix `<Tabs>` root). Moving a tab between the two panes is a button (`onMove`, "Open X to the side" / "Move X to the primary pane"), gated to `isMovableKind` (file/diff/terminal — Chat and Files stay pinned to primary). This is not a general layout: no arbitrary N-way splits, no choice of orientation beyond the one hardcoded horizontal split, no nesting.

The user asked (2026-09-18) for VSCode-style custom layouts ("chia nửa là chat, nửa là terminal... giống chia layout window/layout vscode ấy"). A research pass this session found that **paseo has already built exactly this** — a general split-tree model with pure, well-tested tree-manipulation functions — and confirmed (after pulling `refs/paseo` from stale 2026-08-27 to current `origin/main` @ `3cc4ae2`, 2026-09-17) that this code is present and structurally unchanged at that commit. Per this project's standing preference to port working UI/data-model patterns from paseo rather than invent new ones, this plan ports paseo's tree model and pure functions, while deferring paseo's drag-to-split UI (see Decisions).

## Reference patterns (refs/paseo @ 3cc4ae2, read 2026-09-18)

All citations are `packages/app/src/stores/workspace-layout-actions.ts` unless noted.

**Types (lines 15-35):**
```ts
export interface SplitPane { id: string; tabIds: string[]; focusedTabId: string | null; hidden?: boolean }
export interface SplitGroup { id: string; direction: "horizontal" | "vertical"; children: SplitNode[]; sizes: number[] }
export type SplitNode = { kind: "pane"; pane: SplitPane } | { kind: "group"; group: SplitGroup }
export interface WorkspaceLayout { root: SplitNode; focusedPaneId: string | null; parentTabIdByTabId?: Record<string, string> }
```

**Pure tree functions worth porting almost verbatim** (verified read at this commit):
- `normalizeSizes` / `clampNormalizedSizes` (413-485) — rebalances a group's `sizes` array to sum to 1 and respect `MIN_SPLIT_SIZE`, used both when a child is inserted/removed and when a resize handle reports a raw drag.
- `findPanePathById` / `findPanePathContainingTab` / `findGroupPathById` / `findParentGroup` / `getNodeAtPath` / `replaceNodeAtPath` / `insertChildIntoGroup` / `listPaneIds` / `findNearestSiblingPaneId` (498-656) — the tree-navigation primitives everything else is built from.
- `removePaneByPath` / `detachTabFromTree` / `insertTabIntoPane` / `focusTabInPane` (772-888) — tab-level mutation.
- `updateGroupSizesInTree` / `updatePaneInTree` (923-960).
- `insertSplitInternal` (962-1022) — the core "split" operation: detaches a tab, then either inserts a new sibling pane into an existing same-direction group (halving the target's size) or wraps the target pane in a brand-new group with the new pane as its sibling. This is the one function that makes "split right/down" work correctly regardless of existing tree shape, and is the highest-value single port.
- `normalizeNode` / `normalizePaneNode` / `normalizeGroupNode` / `normalizeLayout` (674-741, 1024-1050) — persisted-layout validation/repair (drops dangling ids, collapses a group left with one child, falls back to a default layout if the root is unsalvageable). Directly analogous to `use-task-tabs.ts`'s existing `readPersisted`/`isTaskTabsState`, and replaces it.
- `findPaneById` / `findPaneContainingTab` / `getTreeDepth` / `collectAllTabs` / `collectAllPanes` (1052-1105).
- `insertSplit` / `removePaneFromTree` / `removeTabFromTree` (1276-1308) — the public wrappers around the internal versions above.
- `closePaneInLayout` / `canDismissPaneInLayout` / `isLastVisibleOrdinaryPane` (1575-1641) — ported as pure functions for future use; no UI affordance calls them yet (see Decisions).
- `splitPaneInLayout` / `splitPaneEmptyInLayout` (1915-2001) — the public "split with an existing tab" / "split with a fresh empty pane" entry points, each capping the result with `getTreeDepth(...) > maxTreeDepth` guard.
- `moveTabToPaneInLayout` / `focusPaneInLayout` / `resizeSplitInLayout` (2026 onward) — generalize today's `moveTab`/`activate`/resize-persist to an arbitrary pane id instead of the hardcoded `"primary" | "side"` pair.

**Explicitly not ported** (paseo-specific, no smind equivalent): `WorkspaceTabPlacement` modes (`pane`/`prefer`/`focused`/`ambient`) and `resolvePlacementPane`; the Explorer-sidebar pane concept (`EXPLORER_SIDEBAR_PANE_ID`, `createWorkspaceLayoutWithExplorerSidebar`); ephemeral-tab stripping (`stripEphemeralTabsFromLayout`, paseo's commit-diff/new-tab tabs don't exist in smind's model); draft/agent/PR tab reconciliation (`reconcileWorkspaceTabs` and everything under it); `parentTabIdByTabId` parent-tracking (subagent-tab parenting, not a smind concept); `createNewWorkspaceTab`/`ensureRetainedPaneHasTab`/`isSoleNewTabPane` (paseo always keeps a pane populated with an auto-created draft tab — smind's tabs are already user-owned/closable per the shipped "flexible tabs" item, so a genuinely empty pane is a valid state that already has a UI, `TabsEmptyState`).

## Acceptance Criteria

### Item 1 — pure split-tree module
A new module (`web/packages/ui/src/lib/split-tree.ts` or similar) exports the smind-adapted tree types and pure functions: `SplitPane`, `SplitGroup`, `SplitNode`, `TaskLayout` (renamed from `WorkspaceLayout` — no `parentTabIdByTabId`), plus `createDefaultLayout`, `findPaneById`, `findPaneContainingTab`, `findPanePathById`, `getTreeDepth`, `collectAllTabs`, `collectAllPanes`, `insertSplit`/`splitPaneInLayout`, `splitPaneEmptyInLayout`, `moveTabToPaneInLayout`, `detachTabFromTree`/`removeTabFromTree`, `removePaneFromTree`, `closePaneInLayout`/`canDismissPaneInLayout` (ported, unused by UI yet), `focusTabInLayout`/`focusPaneInLayout`, `resizeSplitInLayout`/`clampNormalizedSizes`, `normalizeLayout`. No React, no App.tsx/use-task-tabs.ts imports — this module is pure data transformation, independently unit-testable exactly like paseo's own coverage of these functions.

**The pane-removal rule (smind-specific, differs from paseo):** closing a leaf pane's last tab removes that pane from the tree, collapsing its parent group (single remaining sibling replaces the group) exactly as `detachTabFromTree`'s non-preserved branch + `removePaneByPath` already do — **unless** the pane is the tree's sole root pane (no parent group / `findPanePathById` returns `[]`), in which case it's left in place with zero tabs (renders `TabsEmptyState`, unchanged from today). This generalizes today's `withPane`'s `side: next.tabs.length === 0 ? null : next`.

### Item 2 — `use-task-tabs.ts` runs on the tree
`PaneId`/`TaskTabsState`/`PaneState` are replaced by `TaskLayout`/`SplitNode` under the hood. The hook's existing public API (`tabsByTask`, `ensureTask`, `openTab`, `closeTab`, `activate`, `moveTab`) keeps its current call signatures where possible; `moveTab`'s `target: PaneId` parameter widens from the 2-value union to `target: string` (any live pane id) — the only breaking signature change, and it's additive (existing "primary"/"side" callers still work since those remain valid pane ids for a task with one split). Persistence (`readPersisted`/`writePersisted`, localStorage key, `MAX_PERSISTED_TASKS` eviction) is unchanged in mechanism, just now round-trips a `TaskLayout` through `normalizeLayout` instead of the old ad hoc `isTaskTabsState` shape-check.

### Item 3 — "Split" affordance per movable tab
Today's binary "Open to side" / "Move to primary pane" button (file/diff/terminal only, per `isMovableKind`) becomes a **2-direction split** action: "Split right" and "Split down" (not the full 4-direction `left`/`right`/`top`/`bottom` paseo exposes — see Decisions for why 2 is enough for v1). Each movable tab's strip entry gets a small menu/pair of buttons wired to `splitPaneInLayout({ tabId, targetPaneId: thisPane.id, position: "right" | "bottom", maxTreeDepth })`. Splitting caps at a fixed `maxTreeDepth` (reuse paseo's judgment call — 5 — unless testing shows a tighter number reads better in a browser window); attempting to split past the cap is a no-op (button stays visible but does nothing harmful, or is disabled — implementation's call, state which in Progress).

### Item 4 — recursive `App.tsx` renderer
The hardcoded 2-pane JSX (lines 461-534) is replaced by a recursive `<SplitTree node={taskState.root} .../>` component: a `{ kind: "pane" }` node renders today's `PaneTabStrip` (generalized to take a real `paneId: string` and an `onSplit` callback instead of the old fixed `onMove`); a `{ kind: "group" }` node renders a `ResizablePanelGroup` (orientation from `group.direction`) wrapping one `ResizablePanel` per child (recursing) separated by `ResizableHandle`s, with `onResize`/`onLayout` writing back through `resizeSplitInLayout`. `react-resizable-panels` v4.12.3 (already a dependency) nests `ResizablePanelGroup`s arbitrarily — this is its own documented pattern, not new capability being added.

Must not regress, and each has an existing test proving it today that must still pass:
- Force-mounted terminal tabs (Item 20 — a backgrounded terminal keeps streaming; `forceMount={entry.kind === "terminal"}` logic in `PaneTabStrip`).
- Per-pane "+"/empty-state (`NewTabButton`/`TabsEmptyState`).
- Command-palette "Open `<tab>`" entries (`defaultTabsForTask`-driven, pane-agnostic already).
- URL routing (routes encode `taskId` + active tab kind/path only, never pane layout — unaffected by this change).
- Existing keyboard shortcuts: `Ctrl+Alt+<digit>` tab-position activation, `Ctrl+W` close active tab, `Ctrl+]`/`Ctrl+[` task stepping.

### Item 5 — responsive/mobile collapse for an arbitrary tree
Below the mobile breakpoint, today's compact mode flattens primary+side into one merged strip (`compactTabs`/`compactActiveKey`, lines 433-454) since there's nowhere to put a second pane on a phone. For an arbitrary-depth tree, compact mode collapses the **whole tree** into one strip: `collectAllTabs(root)` for the merged tab list, with the currently-focused pane's `activeKey` winning (falls back to root's, same "most recently interacted with" reasoning as today's side-wins-primary rule). No split UI (no "split" buttons) renders in compact mode, matching today's `showMoveAffordance={false}` pattern.

### Item 6 — resize persistence
Each group's `sizes` array persists through the same localStorage mechanism as the rest of `TaskLayout` (no separate storage key) — a resize handle drag calls `resizeSplitInLayout`, which the existing `setTabsByTask` write-through already covers once `use-task-tabs.ts` is on the tree (Item 2). No new acceptance criterion beyond "drag a handle, reload, sizes hold" — this is largely a consequence of Items 1+2+4 done correctly, not standalone work.

## Test Scenarios

**Item 1 (`split-tree.test.ts`, adapted from paseo's own test shapes for these functions):**
- `splitPaneInLayout` on a single-pane tree with position `"right"` produces a 2-child horizontal group, `[0.5, 0.5]` sizes, the moved tab in the new pane, new pane focused.
- `splitPaneInLayout` twice in the same direction against the same target inserts into the existing group (3-way split) rather than nesting a redundant group — mirrors paseo's `findParentGroup` same-direction-reuse doc comment.
- `splitPaneInLayout` returns `null` when it would exceed `maxTreeDepth`.
- Closing the only tab in a split-created (non-root) pane removes that pane and collapses its parent group back to the surviving sibling (single node, not a 1-child group).
- Closing the only tab in the tree's sole root pane leaves the pane in place with an empty tab list (does *not* collapse to nothing).
- `moveTabToPaneInLayout` moves a tab across two arbitrary (non-primary/side-named) pane ids, updates `focusedPaneId`, leaves everything else untouched.
- `resizeSplitInLayout`/`clampNormalizedSizes`: sizes always sum to 1 after resize; no child can be driven below `MIN_SPLIT_SIZE` (reuse paseo's clamp algorithm, so its existing edge-case coverage — e.g. can't shrink 3 panes to 3× below-minimum — transfers).
- `normalizeLayout` on a corrupted/hand-edited persisted blob: dangling pane id in `focusedPaneId` gets repaired to a real pane, not left dangling; a group with one child collapses to that child; a totally unsalvageable root falls back to `createDefaultLayout()`.
- `getTreeDepth` on a 5-level nested fixture returns 5.

**Item 2 (`use-task-tabs.test.ts`, extending the existing suite rather than rewriting it):**
- Every existing test in the current file (seed-only-Chat, open/close/activate, `moveTab` to/from `"side"`, persistence round-trip, malformed-storage fallback, task-count eviction) still passes unmodified where it names `"primary"`/`"side"` literally — those remain valid pane ids for a not-yet-split task.
- New: `moveTab` to a third pane id created via a split.
- New: persisted `TaskLayout` from a previous (pre-tree) session — i.e. today's `{primary, side}` shape sitting in a real user's localStorage — degrades gracefully (either a one-time migration to the new shape, or `normalizeLayout` treats unrecognized shape as corrupt and reseeds; implementation picks one and states which in Decisions, but silent data loss without a stated reason is not acceptable).

**Item 3 (`tab-registry.test.tsx` / `App.test.tsx`):**
- "Split right"/"Split down" appears only for `isMovableKind` tabs, same gate as today's move button.
- Clicking it creates a new pane rendering with the moved tab, in the correct orientation (right → horizontal group, down → vertical group).
- At `maxTreeDepth`, the affordance's behavior (disabled vs silent no-op — per what Item 3's Acceptance Criteria settles on) is asserted.

**Item 4 (`App.test.tsx`, extending `describe("App splits (Item 6)")`):**
- Existing side-dock tests (file "Open to side", tool-call file click-through preferring an open side pane, terminal move-to-side without `terminal.close`/second `terminal.create`) still pass against the new recursive renderer — these are the regression backstop that the port didn't change pane-content mount/unmount semantics.
- A 3-pane layout (two splits deep) renders three independent `PaneTabStrip`s, each retaining its own tab state.
- `Ctrl+Alt+<digit>` still activates by strip position within whichever pane currently owns keyboard focus (or root pane if that's the existing scoping rule — verify against current behavior, don't silently change it).

**Item 5 (`App.responsive.test.tsx`):**
- A 3-pane desktop layout collapses to one merged strip below the breakpoint, containing every tab from every pane.
- Resizing back up restores the full tree (same test shape as the existing "Item 21" resize-back-up assertion, generalized from 2 panes to N).

**Item 6:**
- Covered by Item 1's `resizeSplitInLayout` unit tests plus one `App.test.tsx`/`use-task-tabs.test.ts` integration case: resize a group, remount (simulating reload), sizes match.

## Decisions

- **2-direction split (right/down), not paseo's 4-direction, and no drag-to-split.** Today's UI already has exactly one binary move affordance; extending it to right/down covers "half chat, half terminal" (the user's literal example) and its vertical analogue without introducing left/top's redundant-with-right/down-plus-swap complexity or a drag-and-drop dependency (`@dnd-kit/core`, not currently in smind's `package.json`). Left/top and drag-to-split are explicitly the option-3 follow-up if this ships and more is wanted.
- **No explicit "close this whole pane" affordance.** `closePaneInLayout`/`canDismissPaneInLayout` are ported as pure functions (cheap, and future UI can call them for free) but nothing wires them to a button in this pass — the auto-collapse-on-last-tab-close rule (Item 1) already gets you "close a split" via the existing per-tab close button, which is the more common path anyway.
- **`splitPaneEmptyInLayout` ported but not wired to UI in v1** — splitting always carries an existing tab across (matches today's "Open to side" UX, where you're always moving *something*); a bare "split with nothing in it yet" affordance is deferred unless dogfooding surfaces a need for it.
- **Pre-tree persisted state migration strategy — resolved (Item 2, 2026-09-18).** A legacy `{primary, side}` blob has no `root` key, so `normalizeLayout` (already merged in Item 1) treats it as unrecognized and falls back to `createDefaultLayout()` — an *empty* default pane, not the seeded-Chat state `seedState()` produces for a task with no entry at all. Confirmed by a `use-task-tabs.test.ts` case that seeds a real legacy blob into `localStorage` and asserts the read-back layout is a single empty `DEFAULT_PANE_ID` pane. This is silent-but-explained data loss (the pane layout, not the user's actual work — files/terminals reopen from the daemon, nothing server-side is lost) and needed no special-casing in `readPersisted` beyond the cross-task-taskId guard it already had.
- **`maxTreeDepth` value** — start at paseo's 5, revisit only if a manual pass in a real browser window shows panes becoming unusably thin before hitting it.
- **`detachTabFromTree`'s `preserveEmptyPaneId` must be computed dynamically at close time, not hardcoded (Item 2/Part A, 2026-09-18).** The previously-exported `removeTabFromTree` wrapper hardcodes `preserveEmptyPaneId: DEFAULT_PANE_ID`, which is wrong once a task has been split: it would keep the pane literally named `"primary"` alive-but-empty even when a sibling pane exists next to it, instead of collapsing it into that sibling per Item 1's actual rule ("...unless the pane is the tree's sole root pane"). `detachTabFromTree` is now exported directly; `use-task-tabs.ts`'s `closeTab` computes `isSolePane = collectAllPanes(layout.root).length === 1` at call time and only preserves the closing pane when that's true. `removeTabFromTree` is left in place (still exported, still useful for a caller that genuinely always wants the default pane preserved) but nothing in this codebase calls it anymore.
- **The URL only ever reflected `primary`'s active tab; it now reflects whichever pane is currently *focused* (Item 4, 2026-09-18).** With an arbitrary tree of panes there's no single "primary" story to lean on any more for what the address bar should show. `focusedPaneId` already tracks exactly "the pane the user was last looking at/interacting with" (every split/open/activate/move sets it), which is what the old hardcoded "always primary" rule was really approximating for the one-split case. `Ctrl+W`/`Ctrl+Alt+<digit>` deliberately keep their existing "default pane only" scoping instead (that comment predates this plan and is an intentional design choice, not something this port changed).
- **Item 3's "split past `maxTreeDepth`" behavior: silent no-op, not a disabled button (Item 3, 2026-09-18).** `splitTab` calls `splitPaneInLayout`, which returns `null` past the depth cap; the hook's setter just returns `prev` unchanged in that case. The "Split" menu itself stays enabled and clickable regardless of depth — simpler than threading a live depth check into every tab strip's render just to grey out a button that, past 5 levels, nobody is realistically clicking anyway.
- **The tab strip's own "+" button can't always target the exact pane it was clicked from, for a 3rd+ pane (Item 2/3, smallest-reasonable-call, 2026-09-18).** `openTab`'s `placement` parameter is still the 3-value `TabPlacement` (`"primary" | "side" | "prefer"`) per this plan's explicit choice to keep that signature unchanged — it was never widened to take an arbitrary pane id. `PaneTabStrip`'s "+" menu maps its own pane to a placement via `paneId === DEFAULT_PANE_ID ? "primary" : "side"`, same as before this port. For a task with only 0 or 1 splits this is exactly correct (there's at most one non-default pane, so "side" is unambiguous); for a 3rd+ pane, `"side"` placement lands in *whichever* non-default pane `collectAllPanes` finds first, not necessarily the one whose "+" was clicked. The tab strip's own "Split" affordance (Item 3) has no such ambiguity, since `splitTab` always takes the exact `targetPaneId` the click happened in — only the "+" menu's base-tab-kind opens are affected. Left as-is rather than widening `TabPlacement` to an arbitrary pane id, which would ripple through every `openTab`/`onOpenBase` call site for a gap that only shows up with 3+ panes open at once; worth revisiting if dogfooding surfaces it as an actual annoyance.
- **`hooks/use-side-pane-width.ts` is now unused but left in place (Item 4, 2026-09-18).** It persisted the old fixed side-dock's pixel width, keyed per task; Item 6's resize model persists a group's fractional `sizes` through `TaskLayout` itself instead (no separate storage key), so `App.tsx` no longer imports it. Deleting the hook, its test file, and its `STORAGE_KEYS.sidePaneWidth` entry is out of scope for this port and left for a follow-up cleanup pass.

## Progress

- [x] Item 1 — pure split-tree module + unit tests
- [x] Item 2 — `use-task-tabs.ts` on the tree
- [x] Item 3 — split affordance (right/down) — a "Split" dropdown per movable tab, disabled-vs-no-op decided as silent no-op past `maxTreeDepth` (see Decisions)
- [x] Item 4 — recursive `App.tsx` renderer (`SplitTreeView`/`SplitGroupView`)
- [x] Item 5 — responsive collapse for arbitrary tree (compact mode already generalized via `collectAllTabs`/`focusedPane`, covered by the existing `App.responsive.test.tsx` suite)
- [x] Item 6 — resize persistence (`resizeGroup` -> `resizeSplitInLayout`, no separate storage key)
- [x] `bun run test` / `bun run typecheck` green (see Validation)
- [ ] Manual dogfood pass (split chat+terminal side by side, resize, reload, close panes down to one)

## Validation

Item 1, 2026-09-18:

- `web/packages/ui/src/lib/split-tree.ts` — pure port (no React, no imports from `use-task-tabs.ts`/`App.tsx`), types renamed per plan (`TaskLayout`, `SplitPane` holds `TabEntry[]` + `activeKey` directly). `removePaneByPath`'s root case and `createDefaultLayout` both confirmed to leave/seed an empty pane rather than paseo's fake-draft-tab fallback, matching the smind-specific pane-removal rule.
- `web/packages/ui/src/lib/split-tree.test.ts` — all 9 Item 1 Test Scenarios present and passing: split-right creates a 2-child horizontal group with correct sizes/focus; splitting twice same-direction reuses the group (3-way, not nested); split rejected past `maxTreeDepth=5`; closing a split-created pane's last tab collapses it into its sibling; closing the sole root pane's last tab leaves it in place, empty; `moveTabToPaneInLayout` across two arbitrary pane ids; `clampNormalizedSizes` sums to 1 and respects `MIN_SPLIT_SIZE`; `normalizeLayout` repairs a dangling `focusedPaneId`, collapses a single-child group, and falls back to `createDefaultLayout()` for a garbage root; `getTreeDepth` on a 5-level fixture returns 5.
- Verified independently (not just trusting the implementing agent's report): `cd web && bun run --filter '@smind/ui' test -- split-tree.test.ts` — 13/13 passing. `bun run --filter '@smind/ui' typecheck` — clean.

Items 2-4, 2026-09-18:

- Part A: `detachTabFromTree` exported from `split-tree.ts`; one new unit test in `split-tree.test.ts` (a tab closing in a pane with a non-empty sibling collapses away rather than staying alive-but-empty). `split-tree.test.ts` — 14/14 passing.
- Part B: `use-task-tabs.ts` rewritten on top of `TaskLayout`/`SplitNode` -- `PaneId` widened to `string`, `SplitDirection` added, `openTab`/`closeTab`/`activate`/`moveTab` rebuilt as thin wrappers over the Item 1 module (`closeTab` uses `detachTabFromTree` directly, per Part A/Decisions), and `splitTab`/`resizeGroup` added for Items 3/6. Persistence (`readPersisted`/`writePersisted`) now round-trips through `normalizeLayout`, keeping only the cross-task-taskId guard from the old validator.
- Part C: `use-task-tabs.test.ts` rewritten -- every existing test's behavioral intent preserved, assertions reshaped onto `findPaneById`/`collectAllPanes`. Added: `splitTab` creating a pane and landing the tab there, `splitTab` no-op past `maxTreeDepth`, `resizeGroup` persisting across a remount, a 3-pane scenario via two splits, and the legacy-blob-reseeds-to-empty-default case. `use-task-tabs.test.ts` — 24/24 passing.
- Part D: `App.tsx`'s hardcoded 2-pane block replaced by `SplitTreeView`/`SplitGroupView` (recursive renderer over `SplitNode`, `ResizablePanelGroup`'s `defaultLayout`/`onLayoutChange` for group sizing, frozen via `useInitialValue` per group id the same way the sidebar's own width is). `PaneTabStrip`'s old single move button became a 2-item "Split" dropdown (`workspace-tab-split`/`-right`/`-down` testids); its root gained a `data-pane-id` attribute alongside the existing conditional `primary-pane`/`side-pane` testid. `openTerminalTab`, the URL-writing effect, `Ctrl+W`/`Ctrl+Alt+<digit>`, compact-mode's merged strip, and `ShellCommands`' tab list all regrepped off `.primary`/`.side` onto the tree (see Decisions for the URL-focus-pane generalization and the "+"-button pane-targeting limitation for 3+ panes).
- Part E: `App.test.tsx`/`App.responsive.test.tsx` updated for the new split-menu interaction; added the Item 4 "two splits -> three independent panes" scenario to `App.test.tsx`'s `describe("App splits (Item 6)")` block.

Verified independently: `cd web && bun run --filter '@smind/ui' test -- split-tree.test.ts use-task-tabs.test.ts` — 38/38 passing. `bun run --filter '@smind/ui' test` (whole suite) — 790/790 passing. `bun run --filter '@smind/ui' typecheck` — clean.
