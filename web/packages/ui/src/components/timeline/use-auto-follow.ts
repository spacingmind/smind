import { useCallback, useLayoutEffect, useRef, useState, type RefObject } from "react";

/**
 * How close to the bottom still counts as "following". Not zero: a
 * fractional scrollTop (zoom, sub-pixel line heights) or a one-line
 * overshoot shouldn't be read as the user deliberately scrolling back.
 */
export const FOLLOW_THRESHOLD_PX = 32;

export interface AutoFollow<T extends HTMLElement> {
  ref: RefObject<T | null>;
  /** True while the view is pinned to the tail; false once the user scrolls up. */
  following: boolean;
  /** Attach to the scroll container's onScroll. */
  onScroll: () => void;
  /** Scroll to the tail and resume following (the "jump to latest" affordance). */
  jumpToLatest: () => void;
}

function distanceFromBottom(el: HTMLElement): number {
  return el.scrollHeight - el.scrollTop - el.clientHeight;
}

/**
 * Keeps a scroll container pinned to its tail while content streams in,
 * and lets go the moment the user scrolls up — the behaviour
 * `audit-smind-current.md` §2 records as entirely missing today (the run
 * log has no scroll handling at all, so a long run silently scrolls its
 * own tail out of view).
 *
 * `revision` is whatever value changes when new content is appended (the
 * transcript's total item count works); the effect re-pins on every
 * change to it, and only while `following`.
 *
 * The scroll is applied in a layout effect so it lands in the same frame
 * the new content does — an ordinary effect lets the browser paint the
 * grown, unscrolled container first, which reads as a visible jolt.
 */
export function useAutoFollow<T extends HTMLElement>(revision: unknown): AutoFollow<T> {
  const ref = useRef<T | null>(null);
  const [following, setFollowing] = useState(true);

  useLayoutEffect(() => {
    const el = ref.current;
    if (!el || !following) return;
    el.scrollTop = el.scrollHeight;
  }, [revision, following]);

  const onScroll = useCallback(() => {
    const el = ref.current;
    if (!el) return;
    setFollowing(distanceFromBottom(el) <= FOLLOW_THRESHOLD_PX);
  }, []);

  const jumpToLatest = useCallback(() => {
    const el = ref.current;
    if (el) el.scrollTop = el.scrollHeight;
    setFollowing(true);
  }, []);

  return { ref, following, onScroll, jumpToLatest };
}
