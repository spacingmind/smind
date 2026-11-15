/** Sub-pixel layout leaves scroll positions a fraction off their ends; within this many pixels counts as "at the end". */
const EDGE_EPSILON_PX = 1;

export interface TabScrollEdges {
  /** More tabs lie beyond the left edge (scrolled away from the start). */
  start: boolean;
  /** More tabs lie beyond the right edge. */
  end: boolean;
}

/** Which edges of a horizontally scrolling tab strip have more content beyond them. Both are false when it does not scroll. */
export function tabScrollEdges(scrollLeft: number, clientWidth: number, scrollWidth: number): TabScrollEdges {
  if (scrollWidth - clientWidth <= EDGE_EPSILON_PX) return { start: false, end: false };
  return {
    start: scrollLeft > EDGE_EPSILON_PX,
    end: scrollLeft + clientWidth < scrollWidth - EDGE_EPSILON_PX,
  };
}
