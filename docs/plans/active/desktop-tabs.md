# DT-UI Lane D: tab strip (design review rank 6)

Source: the desktop design review, section 2.6 and rank 6 (local-only research note, not committed). Reference behaviour: chips 64 to 160 px that shrink then scroll with edge fades, an overflow menu, drag reorder with a 4 px drop indicator, split ratios persisted per workspace. Branch flow: every PR targets `wave/desktop`, fresh from `origin/wave/desktop`, never stacked; small PRs, width and overflow first.

## Checked against origin/wave/desktop (what exists)

- `PaneTabStrip` (`App.tsx`): a 36 px (`h-9`) row, a horizontally scrolling `tab-scroller` (scrollbar hidden), tabs sized to their content (`TabsList w-max`, so they never shrink), the active tab is scrolled into view when it changes, and a "+" button (`NewTabButton`, the same icon-button box, not a tab-sized chip).
- Each tab (`DraggableTabTrigger`): icon, title, split and close affordances revealed on hover/focus/active, rename in place for terminal and chat tabs, and the full context menu from DT2a (native in the desktop build, Radix in the browser; close items, rename, archive chat, copy path, split left/right/up/down; enabled state from DT2b).
- Drag: a tab can be dragged to another pane (center drop moves it) or to a pane edge (splits), through one `DndContext` with `useDraggable` on tabs and `useDroppable` per pane; a `SplitDropPreview` shows the target zone. There is NO reorder inside one strip.
- Splits: the split tree (`lib/split-tree.ts`) stores `sizes` per group with a 10% minimum (`MIN_SPLIT_SIZE`); resizing writes them back through `onLayoutChange`. The whole layout, sizes included, persists per TASK in `localStorage` (`smind:task-tabs`), not per workspace, and a new task starts from the default 50/50.
- Missing, therefore the gaps: tab widths that shrink, edge fades, an overflow menu, drag reorder, split ratios remembered per workspace.

## Plan: three small PRs

### PR D1: width, edge fades, overflow menu (this PR)

- AC1 Tabs size as chips: each tab is `flex: 0 1 160px` with a 64 px minimum (the active tab keeps at least 96 px), so with few tabs they sit at their natural width up to 160 and with many they shrink together to 64 and only then does the strip scroll. A title that no longer fits is truncated; the close and split affordances keep their place.
- AC2 Edge fades: when the strip can scroll, a 16 px fade in the panel colour shows on the side that has more tabs beyond it (left when scrolled away from the start, right when more remain), and disappears at that end. The fades never intercept the pointer. They toggle instantly (no transition), so there is no mid-fade frame.
- AC3 Overflow menu: when the tabs overflow (the strip scrolls), a "More tabs" button before "+" opens a menu listing every tab in the pane (icon, title, the active one marked); choosing one activates it and scrolls it into view. With no overflow the button is absent. Keyboard: the button is a normal menu trigger.
- AC4 The "+" button keeps its place at the end and its behaviour; the 36 px rail height is unchanged.
- AC5 Nothing else changes: drag between panes, the context menu (DT2a/b), rename, close and split behave as before.
- Named tests: pure `tabScrollEdges` (start, middle, end, not scrollable, sub-pixel rounding), `tab chip width classes` (source pin on the chip classes), component tests with mocked layout for the fades (left/right shown or hidden per scroll position, never both when not scrollable, `pointer-events-none`), the overflow button (absent without overflow, lists every tab with the active marked, selecting activates), and a Playwright spec with many tabs (frames light/dark at 1280 and 390: not overflowing, overflowing at the start, in the middle with both fades, at the end, the overflow menu open).
- Mutations: width classes back to `w-max`, a fade shown when not scrollable, the right fade not hidden at the end, the overflow button always shown, a menu choice not activating.

### PR D2: drag reorder (later PR)

Reorder a tab within its own strip by dragging it over another tab, with a 4 px drop indicator at the insertion point, through the same `DndContext` (a drop on the strip itself reorders; the existing pane-center and pane-edge drops are unchanged); a pure `reorderTabInPane` in `split-tree.ts` (index math, no-ops, clamping), persisted with the layout; keyboard reorder is out of scope here. Frames include transition frames of the drag (start, over, drop).

### PR D3: split ratios per workspace (later PR)

Remember the last ratio a person set for a split in a workspace and use it as the starting ratio of the next split created in that workspace (the per-task layout stays authoritative once it exists). Storage: one small `localStorage` map keyed by workspace id (pure read/validate/write like `lib/storage.ts`, clamped to `MIN_SPLIT_SIZE`), written on `onLayoutChange` of a two-pane group, read when a split is created. No daemon or Rust change.

## Not in this plan

Terminal colour (rank 12), tab drag between windows (multi-window Part 2), a tab-width setting.

## Progress / Validation

### PR D1

- AC1-AC5: `tab-scroll-edges.test.ts` (8 cases), `tab-strip-overflow.test.tsx` (fades per scroll position, the overflow menu lists every tab, marks and activates, brings the current tab into view, the chip classes as source pins), the existing App tab tests (TB-AC3 updated for the wrapper), and Playwright `tabs-overflow.spec.ts` TO-01/TO-02 (real built daemon; chips 64-160 px with the active at least 96, the strip scrolls only after that, fades at start/middle/end, the menu activates a tab) on light/dark at 1280 and 390, plus `tabs`, `narrow`, `routing` and `context-menus` specs unchanged and green (72).
- Frames, light and dark, 1280 and 390, every pair opened: a few tabs, overflowing at the start, in the middle, at the end, the overflow menu. The fades toggle instantly (no transition), so there is no mid-fade frame to capture.
- Mutations (committed code, each failed its named test): the list back to `w-max`, the end fade never hiding, the overflow button always shown, a menu choice doing nothing, the current-tab marker removed. One mutation (an early return in `tabScrollEdges`) survived because it was redundant; the code was simplified instead.
- Not verified: real-window frames (screen locked); a drag or the tab context menu while the strip scrolls (D2 and unchanged here); Windows/Linux.
