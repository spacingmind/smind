/** Sub-pixel layout leaves scroll positions a fraction off their ends; within this many pixels counts as "at the end". */
const EDGE_EPSILON_PX = 1;

export interface TabScrollEdges {
  /** More tabs lie beyond the left edge (scrolled away from the start). */
  start: boolean;
  /** More tabs lie beyond the right edge. */
  end: boolean;
}

/** Which edges of a horizontally scrolling tab strip have more content beyond them. Both are false when it does not scroll (nothing is beyond either edge). */
export function tabScrollEdges(scrollLeft: number, clientWidth: number, scrollWidth: number): TabScrollEdges {
  return {
    start: scrollLeft > EDGE_EPSILON_PX,
    end: scrollLeft + clientWidth < scrollWidth - EDGE_EPSILON_PX,
  };
}

/** Clear space kept between a revealed tab and the strip's edge: the width of an edge fade (16px) plus a little air, so a fade never sits over the chip or its close button. */
export const TAB_REVEAL_PADDING_PX = 20;

/**
 * The `scrollLeft` that shows a tab FULLY inside a scrolling strip, with `padding` of clear space on each side (the
 * edge fades), changed as little as possible and clamped to what the strip can scroll. `itemLeft`/`itemRight` are in
 * the strip's scroll coordinates (its left edge at 0 when scrolled to the start).
 */
export function revealScrollLeft(input: {
  scrollLeft: number;
  clientWidth: number;
  scrollWidth: number;
  itemLeft: number;
  itemRight: number;
  padding?: number;
}): number {
  const padding = input.padding ?? TAB_REVEAL_PADDING_PX;
  const max = Math.max(0, input.scrollWidth - input.clientWidth);
  let next = input.scrollLeft;
  if (input.itemLeft < next + padding) next = input.itemLeft - padding;
  else if (input.itemRight > next + input.clientWidth - padding) next = input.itemRight - input.clientWidth + padding;
  return Math.min(max, Math.max(0, next));
}

/** Titles for a menu where two tabs share one: the first keeps its title, later ones get " 2", " 3", ... in tab order (the strip itself is unchanged). */
export function disambiguatedTitles(entries: ReadonlyArray<{ key: string; title: string }>): Map<string, string> {
  const seen = new Map<string, number>();
  const out = new Map<string, string>();
  for (const { key, title } of entries) {
    const n = (seen.get(title) ?? 0) + 1;
    seen.set(title, n);
    out.set(key, n === 1 ? title : `${title} ${n}`);
  }
  return out;
}

