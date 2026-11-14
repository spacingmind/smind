import { create } from "zustand";

/** One pending "open this file at this position" request: line 1-based, col 1-based. */
export interface FileRevealRequest {
  line: number;
  col: number;
  /** Distinguishes two requests for the same tab, so a second click on an already-open tab re-triggers. */
  nonce: number;
  /** When it was queued: a request nobody consumed (read failed, tab closed) goes stale instead of jumping a later open to an old line. */
  at: number;
}

/** How long a queued request stays valid. */
const REQUEST_TTL_MS = 15_000;

let nextNonce = 0;

/**
 * Pending reveal-at-line requests for file tabs (DT3-A AC4): whoever opens
 * a file from outside the editor (the terminal's link handler) queues a
 * request against the tab key; the mounted editor pane consumes it once its
 * content has loaded. A store, not a URL route, so deep links are unchanged
 * -- same shape of latch as lib/diff-reveal.ts and focused-chat-store's
 * revealRequest.
 */
export const useFileRevealStore = create<{
  requests: Readonly<Record<string, FileRevealRequest>>;
  request: (tabKey: string, line: number, col?: number) => void;
  consume: (tabKey: string) => FileRevealRequest | undefined;
}>()((set, get) => ({
  requests: {},
  request: (tabKey, line, col = 1) =>
    set((state) => ({ requests: { ...state.requests, [tabKey]: { line, col, nonce: ++nextNonce, at: Date.now() } } })),
  consume: (tabKey) => {
    const pending = get().requests[tabKey];
    if (!pending) return undefined;
    set((state) => {
      const next = { ...state.requests };
      delete next[tabKey];
      return { requests: next };
    });
    return Date.now() - pending.at > REQUEST_TTL_MS ? undefined : pending;
  },
}));
