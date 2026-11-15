import { useEffect, type RefObject } from "react";

import { revealScrollLeft } from "@/lib/tab-scroll-edges";

/**
 * Keeps the active tab fully visible in its strip whenever it changes: scrolls just far enough that the chip, its close
 * button and the edge fade's clear space are inside the visible area (`scrollIntoView` aligns the chip to the very edge,
 * where the fade covers it). Measured from the elements' rects at the moment of the change.
 */
export function useRevealActiveTab(scrollerRef: RefObject<HTMLElement | null>, activeKey: string | null): void {
  useEffect(() => {
    const scroller = scrollerRef.current;
    const active = scroller?.querySelector<HTMLElement>('[role="tab"][aria-selected="true"]');
    if (!scroller || !active) return;
    const box = scroller.getBoundingClientRect();
    const rect = active.getBoundingClientRect();
    const next = revealScrollLeft({
      scrollLeft: scroller.scrollLeft,
      clientWidth: scroller.clientWidth,
      scrollWidth: scroller.scrollWidth,
      itemLeft: rect.left - box.left + scroller.scrollLeft,
      itemRight: rect.right - box.left + scroller.scrollLeft,
    });
    if (next !== scroller.scrollLeft) scroller.scrollLeft = next;
  }, [scrollerRef, activeKey]);
}
