import { create } from "zustand";

/** One pending "focus this card" request, keyed by requestId. */
export interface CardFocusRequest {
  /** Distinguishes two requests for the same card, so a repeat re-triggers. */
  nonce: number;
  /** When it was queued: a request whose card never appears (already resolved) goes stale instead of grabbing focus later. */
  at: number;
}

/** How long a queued request stays valid. */
const REQUEST_TTL_MS = 15_000;

let nextNonce = 0;

/**
 * Pending focus requests for the permission/question cards (DT5-A AC3):
 * opening a `chat/<chatId>?card=<requestId>` route queues a request, and
 * the card whose requestId matches takes focus and consumes it once --
 * unlike the card's own mount focus, this runs even while a text field
 * has focus, because a notification click is an explicit action. Same
 * shape of latch as stores/file-reveal-store.ts.
 */
export const useCardFocusStore = create<{
  requests: Readonly<Record<string, CardFocusRequest>>;
  request: (requestId: string) => void;
  consume: (requestId: string) => boolean;
}>()((set, get) => ({
  requests: {},
  request: (requestId) =>
    set((state) => ({ requests: { ...state.requests, [requestId]: { nonce: ++nextNonce, at: Date.now() } } })),
  consume: (requestId) => {
    const pending = get().requests[requestId];
    if (!pending) return false;
    set((state) => {
      const next = { ...state.requests };
      delete next[requestId];
      return { requests: next };
    });
    return Date.now() - pending.at <= REQUEST_TTL_MS;
  },
}));
