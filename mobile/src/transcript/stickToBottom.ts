// stickToBottom.ts is the pure auto-scroll policy for the transcript list:
// while `following` the list sticks to the end as content grows; scrolling
// up past the threshold releases it, reaching the bottom again re-engages it.
// Content growth alone never changes the state (only a scroll event does).

export interface ScrollMetrics {
  contentOffsetY: number;
  contentHeight: number;
  layoutHeight: number;
}

export interface FollowState {
  following: boolean;
}

export const BOTTOM_THRESHOLD = 24;

/** True when the viewport's bottom edge is within `threshold` px of the content's end (content shorter than the viewport is always at the bottom). */
export function isAtBottom(m: ScrollMetrics, threshold: number = BOTTOM_THRESHOLD): boolean {
  return m.contentHeight - (m.contentOffsetY + m.layoutHeight) <= threshold;
}

export function nextFollowState(_prev: FollowState, ev: ScrollMetrics): FollowState {
  return { following: isAtBottom(ev) };
}
