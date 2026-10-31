import { useCallback, useLayoutEffect, useRef, useState, type RefObject } from "react";

/**
 * How close to the bottom still counts as "following". Not zero: a
 * fractional scrollTop (zoom, sub-pixel line heights) or a one-line
 * overshoot shouldn't be read as the user deliberately scrolling back.
 */
export const FOLLOW_THRESHOLD_PX = 32;

/** The "Jump to latest" pill: ~32px high, `bottom-4` (16px) over the log. */
export const PILL_HEIGHT_PX = 32;
export const PILL_BOTTOM_OFFSET_PX = 16;

/**
 * Permanent padding at the end of the log content. The pill shows once the
 * view is more than FOLLOW_THRESHOLD_PX from the tail, so the last line must
 * sit at least pill height + offset + threshold above the content end to never
 * be under it. Constant (not toggled with the pill) so nothing jumps.
 */
export const LOG_END_CLEARANCE_PX = PILL_HEIGHT_PX + PILL_BOTTOM_OFFSET_PX + FOLLOW_THRESHOLD_PX;

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
 *
 * The item count is not the only thing that grows the log: an attachment
 * image decoding, a markdown block settling or the composer's chip row
 * changing the viewport height all move the tail without adding an item.
 * Those used to leave a freshly opened chat a few dozen pixels short of the
 * bottom (hiding the last run's footer, and past FOLLOW_THRESHOLD_PX the
 * next scroll flipped `following` off and showed "Jump to latest"), so a
 * ResizeObserver on the container and its content re-pins while following.
 */
export function useAutoFollow<T extends HTMLElement>(revision: unknown): AutoFollow<T> {
  const ref = useRef<T | null>(null);
  const [following, setFollowing] = useState(true);
  const followingRef = useRef(true);
  followingRef.current = following;

  useLayoutEffect(() => {
    const el = ref.current;
    if (!el || !following) return;
    el.scrollTop = el.scrollHeight;
  }, [revision, following]);

  useLayoutEffect(() => {
    const el = ref.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(() => {
      if (followingRef.current) el.scrollTop = el.scrollHeight;
    });
    observer.observe(el);
    if (el.firstElementChild) observer.observe(el.firstElementChild);
    return () => observer.disconnect();
  }, []);

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
