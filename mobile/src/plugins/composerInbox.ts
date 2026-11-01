// composerInbox.ts: the one-slot hand-off between a plugin panel's prompt
// action and the composer. A prompt action from a plugin panel only ever
// PUTS text here; the composer consumes it as a draft the Human must press
// Send on. Nothing here can send: there is no connection and no method
// call, only bounded plain text (same limits as slashRpc's answer).

/** Most text a plugin may hand the composer; larger is refused, not cut. */
export const MAX_INBOX_CHARS = 16 * 1024;

let pending: string | null = null;
const subscribers = new Set<(text: string | null) => void>();

function notify(): void {
  for (const fn of subscribers) fn(pending);
}

/** Puts text in the slot (replacing anything unconsumed). False, no change, for empty/whitespace-only, oversize or NUL-bearing text. */
export function setPendingInsert(text: string): boolean {
  if (text.trim() === '' || text.length > MAX_INBOX_CHARS || text.includes('\u0000')) return false;
  pending = text;
  notify();
  return true;
}

/** Hands the waiting text to its consumer and clears the slot; null when nothing waits. */
export function takePendingInsert(): string | null {
  if (pending === null) return null;
  const text = pending;
  pending = null;
  notify();
  return text;
}

/** The waiting text without consuming it; null when nothing waits. */
export function peekPendingInsert(): string | null {
  return pending;
}

/** Called with the slot's current value (null = empty) after every set/take that changes it; the return stops it. */
export function subscribePendingInsert(fn: (text: string | null) => void): () => void {
  subscribers.add(fn);
  return () => {
    subscribers.delete(fn);
  };
}
