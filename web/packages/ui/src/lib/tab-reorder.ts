/** A tab's horizontal extent in viewport pixels. */
export interface TabSpan {
  left: number;
  right: number;
}

/**
 * The insertion slot a dragged tab's pointer is over: 0 = before the first tab ... `spans.length` = after the last. A
 * tab's left half is "before it", its right half "after it". `spans` must be in strip order.
 */
export function reorderSlotAtX(spans: readonly TabSpan[], x: number): number {
  for (let i = 0; i < spans.length; i++) {
    const span = spans[i]!;
    if (x < (span.left + span.right) / 2) return i;
  }
  return spans.length;
}

/** The x (viewport pixels) where the drop indicator for `slot` is drawn: the boundary between the neighbouring tabs. */
export function reorderIndicatorX(spans: readonly TabSpan[], slot: number): number | null {
  if (spans.length === 0) return null;
  if (slot <= 0) return spans[0]!.left;
  if (slot >= spans.length) return spans[spans.length - 1]!.right;
  return (spans[slot - 1]!.right + spans[slot]!.left) / 2;
}

/**
 * Classes for a tab chip while tabs are being dragged. The chip being dragged dims (so the drag has feedback on the
 * thing held) and shows the grabbing cursor; while ANY tab drag is active no chip reveals its hover controls (split,
 * close), so the label stays readable and the insertion bar is the only cue at the drop target.
 */
export function tabDragClasses(state: { isDragging: boolean; dragActive: boolean }): { chip: string; affordances: string | null } {
  return {
    chip: state.isDragging ? "opacity-50 cursor-grabbing" : "",
    // null = use the normal reveal-on-hover classes.
    affordances: state.dragActive ? "pointer-events-none opacity-0 transition-none" : null,
  };
}

