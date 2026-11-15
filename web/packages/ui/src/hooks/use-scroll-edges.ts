import { useEffect, useState, type RefObject } from "react";

import { tabScrollEdges, type TabScrollEdges } from "@/lib/tab-scroll-edges";

const NONE: TabScrollEdges = { start: false, end: false };

/**
 * Tracks which edges of a horizontally scrolling element have more content beyond them: updated on scroll and whenever
 * the element or its first child (the tab list, whose width follows the tab count) changes size. Without
 * ResizeObserver (jsdom) it reads once and on scroll.
 */
export function useScrollEdges(ref: RefObject<HTMLElement | null>): TabScrollEdges {
  const [edges, setEdges] = useState<TabScrollEdges>(NONE);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const read = () => {
      const next = tabScrollEdges(el.scrollLeft, el.clientWidth, el.scrollWidth);
      setEdges((prev) => (prev.start === next.start && prev.end === next.end ? prev : next));
    };
    read();
    el.addEventListener("scroll", read, { passive: true });
    let observer: ResizeObserver | undefined;
    if (typeof ResizeObserver !== "undefined") {
      observer = new ResizeObserver(read);
      observer.observe(el);
      if (el.firstElementChild) observer.observe(el.firstElementChild);
    }
    return () => {
      el.removeEventListener("scroll", read);
      observer?.disconnect();
    };
  });
  return edges;
}
