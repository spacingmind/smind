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
- AC1-AC5: `reorderTabInLayout` table (13 cases), `reorderSlotAtX`/`reorderIndicatorX` tables, `tab-reorder-wiring.test.tsx` (4 pins), Playwright `tabs-reorder.spec.ts` TR-01/TR-02 on light and dark 1280 (the narrow runs are skipped by design: the compact layout has no drag-and-drop) with `tabs.spec.ts`: 20 passed, 4 skipped. Transition frames, light and dark, every pair opened: drag started, over with the indicator, after the drop.
- Mutations (committed code, each failed a named test): the slot flipped to the right half, the removal shift missing, the drop not reordering, the pane preview left on during a reorder, and the indicator not rendered (this one first survived the unit tests, which now pin the render; the e2e spec also kills it).
- Not verified: real-window frames (screen locked); a drag across panes or onto another pane's strip (unchanged code, existing behaviour); touch drag; Windows and Linux.
- Review round 2 (drag feedback): AC6 the held chip dims (opacity 50%) with the grabbing cursor for the whole drag (a root flag read by `index.css`, since the pointer is over other elements); AC7 while any tab drag is active no chip reveals its hover controls (hidden at once, no transition), so the hovered target's label stays readable and the insertion bar is the only cue; AC8 no indicator at the slots beside the held tab (they would not move it). Tests: `tabDragClasses` table, wiring pins, and the e2e assertions (held chip opacity 0.5, root flag, every affordance at opacity 0, the hovered label not clipped and not covered, flag and opacity restored after the drop). Mutations (each failed): dragged chip not dimmed, hover controls shown during a drag, cursor flag not set, indicator beside the held chip, chip classes not applied. Frames re-shot, light and dark, drag started / over / after drop, every pair opened; the drag exists only in the desktop (non-compact) layout, so there is no 390 pair.

