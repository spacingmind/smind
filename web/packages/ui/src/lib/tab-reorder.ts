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
