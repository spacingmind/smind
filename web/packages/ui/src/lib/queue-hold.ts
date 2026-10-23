/** store.ChatQueueSourceChildEvent: the daemon's coalesced child-update item (ADR-0041). */
export const CHILD_EVENT_SOURCE = "child_event";

export const CHILD_UPDATES_LABEL = "Child updates";

/**
 * "starts in 18s" (under a minute) or "starts 14:05" (local time) for a
 * queue item held until `notBefore`; null when it has no hold, the value is
 * unparseable, or the hold has already passed.
 */
export function queueStartsLabel(notBefore: string | null | undefined, now: number): string | null {
  if (!notBefore) return null;
  const at = Date.parse(notBefore);
  if (Number.isNaN(at) || at <= now) return null;
  const secs = Math.ceil((at - now) / 1000);
  if (secs < 60) return `starts in ${secs}s`;
  return `starts ${new Date(at).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", hour12: false })}`;
}
