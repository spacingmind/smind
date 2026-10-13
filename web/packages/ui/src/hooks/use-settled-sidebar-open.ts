import { useEffect, useState, type RefObject } from "react";

/** The longest transition duration on `el`, in ms (0 when none or when `getComputedStyle` has no value, e.g. jsdom). */
function transitionMs(el: Element): number {
  const raw = getComputedStyle(el).transitionDuration;
  if (!raw) return 0;
  return Math.max(0, ...raw.split(",").map((part) => (parseFloat(part) || 0) * (part.trim().endsWith("ms") ? 1 : 1000)));
}

/**
 * The sidebar's *layout* state, which trails its requested `open` state until
 * the panel's width transition has finished. The panel animates its own
 * width the moment `open` flips, but the sidebar inside (expanded rows vs the
 * icon rail) is laid out for one fixed width, so flipping it at the start of
 * the animation shows it clipped, sliced and shifted mid-flight. The shell
 * hides the sidebar's content while `open !== settled` and only then lets the
 * new layout fade in. Resolves at once when nothing animates (reduced motion,
 * a drag-resize, no `enabled`, tests).
 */
export function useSettledSidebarOpen(open: boolean, panelRef: RefObject<HTMLElement | null>, enabled: boolean): boolean {
  const [settled, setSettled] = useState(open);
  useEffect(() => {
    if (settled === open) return;
    const el = panelRef.current;
    const ms = enabled && el ? transitionMs(el) : 0;
    if (!el || ms === 0) {
      setSettled(open);
      return;
    }
    const done = () => setSettled(open);
    const onEnd = (e: TransitionEvent) => {
      if (e.target === el && e.propertyName === "width") done();
    };
    el.addEventListener("transitionend", onEnd);
    // transitionend does not fire if the transition is interrupted or never starts.
    const timer = setTimeout(done, ms + 150);
    return () => {
      el.removeEventListener("transitionend", onEnd);
      clearTimeout(timer);
    };
  }, [open, settled, panelRef, enabled]);
  return settled;
}
