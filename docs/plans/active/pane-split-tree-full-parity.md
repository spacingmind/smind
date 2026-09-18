# Pane split-tree, full parity pass: 4-direction split, drag-to-split, exact-pane "+"

## Context

`docs/plans/completed/pane-split-tree.md` shipped the split-tree data model (Item 1) and its wiring into `use-task-tabs.ts`/`App.tsx` (Items 2-6): arbitrary N-pane layouts, resize persistence, a button-driven "Split right/down" menu. Three gaps were called out there as deliberately deferred, and the user has now asked for "đầy đủ" (the complete/full thing) rather than leaving them open:

1. **Only 2 of 4 split directions.** `web/packages/ui/src/lib/split-tree.ts`'s `splitPaneInLayout`/`insertSplitInternal` already accept `"left" | "right" | "top" | "bottom"` (verified: `position` is typed as the full 4-value union at every call site in that file) — the restriction to right/down is purely in `use-task-tabs.ts`'s `SplitDirection` type and `App.tsx`'s `PaneTabStrip` menu, not the underlying model.
2. **No drag-a-tab-to-an-edge-to-split.** Splitting only happens via the "Split" dropdown menu; paseo's own equivalent (`refs/paseo/packages/app/src/components/split-drop-zone.tsx` + `split-container.tsx`, read in full this session) drives it from `@dnd-kit/core` — a draggable per tab, a droppable overlay per pane classifying the drop position into center/left/right/top/bottom via `resolveSplitDropPosition` (pure geometry, ports directly), then `moveTabToPaneInLayout` (center) or `splitPaneInLayout` (an edge).
3. **The "+" menu can't unambiguously target a 3rd+ pane.** `openTab`'s `placement` is still the 3-value `TabPlacement` (`"primary" | "side" | "prefer"`) from before the tree existed; `"side"` picks "the first non-default pane `collectAllPanes` finds," not necessarily the pane whose own "+" was clicked.

`@dnd-kit/core` is not currently a smind dependency (checked `web/package.json`, `web/packages/ui/package.json`, `web/bun.lock` — no match).

## Reference patterns (refs/paseo, read 2026-09-18)

- `components/split-drop-zone.tsx`: `resolveSplitDropPosition({width, height, x, y})` — pure function, ports near-verbatim. `EDGE_RATIO = 0.15` (outer 15% of each axis is an edge zone), `CENTER_RATIO = 0.4` (inner 40% is the "move here, don't split" zone), whichever edge is closest wins for a point that's in neither. The component itself (`SplitDropZone`) is React Native/Unistyles — only the geometry function ports; the overlay visuals get rebuilt in Tailwind.
- `components/split-container.tsx`: `DndContext` wraps the whole split tree, `useSensors(PointerSensor)` (an 8px `activationConstraint.distance` so a plain click doesn't register as a drag start). `handleDragStart`/`updateDropPreview` (on `onDragMove`/`onDragOver`)/`handleDragEnd` — the latter reads `event.over`'s droppable data to decide `applyPaneDropEnd` (center -> move, edge -> split) vs (paseo also has tab-onto-tab reordering, which smind doesn't have today and this pass doesn't add — out of scope, see Decisions).

## Acceptance Criteria

### Item 7 — 4-direction split
`use-task-tabs.ts`'s `SplitDirection` widens from `"right" | "down"` to `"left" | "right" | "up" | "down"` (keep smind's existing up/down naming rather than paseo's top/bottom, mapping to the tree's `"top"`/`"bottom"` at the `splitPaneInLayout` call site only). `PaneTabStrip`'s "Split" dropdown offers all 4 as menu items. No other behavior changes — `splitPaneInLayout` already handles all 4 positions correctly (verified by Item 1's own tests covering the underlying function, just never exercised via right's siblings from the UI layer).

### Item 8 — drag-a-tab-to-an-edge-to-split
Dragging a tab's strip entry (any tab, not just movable kinds — dropping a Chat/Files tab onto another pane's center still just moves-or-focuses it there, matching what clicking it there would do; only *splitting* is gated to `isMovableKind`, same as today's button) and releasing it:
- Over the **center 40%** of any pane (including its own, a no-op) moves the tab there via the existing `moveTab`, exactly like a click-driven move.
- Over one of the four **outer 15% edges** of any pane splits that pane in the corresponding direction via `splitTab`, carrying the dragged tab into the new pane — same operation Item 3's menu already performs, now reachable by drag as well as by menu (the menu is not removed; both remain valid ways to split).
- A visible preview (highlighted overlay + border matching the eventual split shape) tracks the pointer during the drag, so the user sees which zone they're about to drop into before releasing.
- Dragging outside any pane (drop cancelled, or dropped somewhere with no droppable) is a no-op — nothing moves.

### Item 9 — the "+" menu targets its own pane exactly, regardless of pane count
`openTab`'s `placement` gains a 4th form that names an exact pane id (e.g. `{ paneId: string }` alongside the existing 3 string literals, or widen `TabPlacement` to `"primary" | "side" | "prefer" | string` — implementation's call, state which in Decisions). `PaneTabStrip`'s own "+" button and empty-state buttons pass their own `paneId` directly instead of guessing `"primary"` vs `"side"`. The 3 legacy string placements keep their exact existing meaning for every other caller (command palette, file-tree "open"/"open to side", route restore) — this item only changes what the tab strip's own local affordances pass.

## Test Scenarios

**Item 7 (`use-task-tabs.test.ts` / `App.test.tsx`):**
- `splitTab(..., "left")` and `splitTab(..., "up")` each produce the correct group direction/child order (mirroring the existing right/down test shapes).
- `PaneTabStrip`'s "Split" menu lists 4 items; each fires the correct `SplitDirection`.

**Item 8 (new, e.g. `split-drop-zone.test.ts` for the pure geometry, `App.test.tsx` for the integrated flow):**
- `resolveSplitDropPosition` unit tests ported from paseo's own coverage: a point in the exact center returns `"center"`; a point in the outer-left 15% returns `"left"` even if it's also within the vertical center band; a point in neither the center band nor within any edge threshold (the geometrically ambiguous ring in between) returns whichever edge is nearest by distance.
- Dragging a movable tab and dropping on another pane's left-edge zone calls `splitTab(..., targetPaneId, "left")` with the dragged tab's key.
- Dragging and dropping on a pane's center zone calls `moveTab`, not `splitTab`.
- Dragging and releasing with no `over` droppable (dropped outside any pane) changes nothing.
- The existing "Split" menu (Item 3/7) still works unmodified — drag-to-split is additive, not a replacement.

**Item 9:**
- With 3 panes open (two splits deep), each pane's own "+" -> "Open Diff" lands the new Diff tab in *that* pane specifically, not whichever non-default pane `collectAllPanes` happens to list first.
- Existing placement tests (`"prefer"`/`"side"`/`"primary"` from file-tree opens, route restore, command palette) unaffected.

## Decisions

- **`@dnd-kit/core` is the new dependency** (not `@dnd-kit/sortable` — smind doesn't need paseo's tab-reordering-within-a-pane feature, which is the only thing that package's `sortableKeyboardCoordinates`/`arrayMove` helpers are for; a plain `PointerSensor` is enough for pane-to-pane drag). If keyboard-only drag accessibility turns out to matter, that's a follow-up, not blocking this pass — dropping a tab by mouse/touch plus the existing menu (unchanged, still keyboard-operable) together already cover both an accessible path and the requested drag interaction.
- **Tab-onto-tab reordering within or across a strip is explicitly out of scope.** Paseo's `split-container.tsx` handles both "drop on a pane" (split/move) and "drop on another tab" (reorder/insert-at-position) via two different droppable "kind"s; smind has no tab-reordering feature today (tabs are appended in open order) and this pass doesn't add one — only the pane-level drop zone is built. Dropping a tab anywhere within a pane's strip (not specifically on another tab) still counts as "the center of that pane" for this pass's purposes.
- **Visual preview styling**: reuse the codebase's existing accent/border tokens (whatever `docs/design.md` names for the "action" accent) rather than inventing new ones — this is a UI-polish detail for the implementer to match to the existing visual-identity pass (#152), not a new design decision.

## Progress

- [ ] Item 7 — 4-direction split
- [ ] Item 8 — drag-to-split
- [ ] Item 9 — exact-pane "+" targeting
- [ ] `task test` / `task lint` green
- [ ] Manual dogfood pass (drag a tab to each of the 4 edges of a pane, drag onto center, drop outside any pane, 3-pane "+" targeting)

## Validation

To be filled in as each item lands.
