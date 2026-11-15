# DT-UI Lane D, PR D2: drag a tab to reorder it within its strip

Part of the Lane D plan (`desktop-tabs.md`, in the D1 PR). Branch flow: fresh from `origin/wave/desktop`, targets `wave/desktop`, not stacked on D1 (the two touch the same strip and merge cleanly or are merged by hand).

## What exists
Tabs are draggable through one `DndContext` (`useDraggable` per tab, a `useDroppable` per pane): a center drop moves the tab to that pane, an edge drop splits. There is no reorder inside one strip, and a drag over a pane's own strip used to show the pane's drop preview.

## Acceptance criteria
- AC1 Dragging a tab along its OWN pane's strip reorders it: a 4 px drop indicator marks the insertion slot between two tabs (before the first, between, after the last); releasing moves the tab there. A tab's left half is "before it", its right half "after it".
- AC2 While a reorder is in progress the pane drop preview is off; leaving the strip (over the content) restores the existing move/split behaviour unchanged.
- AC3 Dropping where the tab already is, or cancelling with Escape, changes nothing.
- AC4 The new order persists with the task's layout (reload keeps it), the active tab and the focused pane are untouched, and other panes are untouched.
- AC5 A drag over ANOTHER pane's strip is not a reorder (it keeps the existing move behaviour). The compact (narrow) layout has no drag-and-drop and is unchanged.
- Named tests: `reorderTabInLayout` table (move to front, to end, one slot either way, onto itself before/after, clamping, unknown tab or pane, active tab and other panes kept), `reorderSlotAtX` and `reorderIndicatorX` tables, source pins on the App wiring, Playwright `tabs-reorder.spec.ts` (TR-01 drag shows the indicator at the right slot and reorders, the order survives a reload; TR-02 drop-in-place and Escape change nothing) with transition frames: drag started, over with the indicator, after the drop, light and dark at 1280 (the strip has no drag at 390 or in a desktop window that narrow).
- Mutations: the slot flipped to the right half, the removal shift missing in `reorderTabInLayout`, the drop not calling `reorderTab`, the indicator not rendered, the pane preview left on during a reorder.

## Not in this PR
Keyboard reorder, dragging a tab to a specific slot in ANOTHER pane's strip, drag reorder in the narrow layout.

## Validation
(filled below)
