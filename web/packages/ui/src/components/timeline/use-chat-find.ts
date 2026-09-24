import { useCallback, useEffect, useRef, useState, type RefObject } from "react";

import type { FindBarHandle } from "@/components/find/find-bar";
import { usePaneFocusWithin } from "@/components/find/use-pane-focus-within";
import {
  applyChatHighlights,
  clearChatHighlights,
  findChatMatches,
  restyleChatHighlights,
} from "@/components/timeline/chat-find-dom";
import { nextMatchIndex, previousMatchIndex } from "@/components/timeline/chat-find-text";
import { useActionHandler } from "@/keyboard/keyboard-provider";

/**
 * How long a keystroke waits before chat Find re-walks the transcript DOM --
 * cheap at smind's scale (one task's rendered transcript, not a large
 * file), but per `docs/plans/active/web-find.md`'s Decisions, debounced
 * anyway so a fast typist doesn't re-walk on every keystroke.
 */
const FIND_DEBOUNCE_MS = 120;

export interface ChatFindState {
  open: boolean;
  query: string;
  setQuery(query: string): void;
  /** "i/N", "No matches", or "" for a closed/empty query. */
  status: string;
  count: number;
  next(): void;
  previous(): void;
  close(): void;
  barRef: RefObject<FindBarHandle | null>;
  /** Spread onto whatever element should count as "this pane is focused" for `Mod+F` to claim -- see `usePaneFocusWithin`. */
  onFocus: ReturnType<typeof usePaneFocusWithin>["onFocus"];
  onBlur: ReturnType<typeof usePaneFocusWithin>["onBlur"];
}

/**
 * Chat Find's React half: owns open/query/active-match state, claims the
 * `pane.find` action while this pane has focus, and re-walks
 * `containerRef`'s rendered DOM (via `chat-find-dom.ts`) to find and
 * highlight matches. Client-side only -- no host search RPC -- because
 * smind loads a task's whole run history into the DOM up front
 * (`hooks/use-run-timeline.ts`), unlike Paseo's windowed history.
 *
 * `revision` is whatever value changes whenever the searchable transcript
 * content changes -- item count alone misses a streamed chunk appended to
 * the last item, so callers pass something that also grows with streamed
 * text (see `task-detail.tsx`). Re-walking on every such change (rather
 * than patching incrementally) is what makes "matches inside streamed-in
 * assistant text are found" (AC1) hold without any special-casing of the
 * streaming case.
 */
export function useChatFind(containerRef: RefObject<HTMLElement | null>, revision: unknown): ChatFindState {
  const [open, setOpen] = useState(false);
  const [query, setQueryState] = useState("");
  const [activeIndex, setActiveIndex] = useState(0);
  const [count, setCount] = useState(0);
  const marksRef = useRef<HTMLElement[]>([]);
  const barRef = useRef<FindBarHandle>(null);
  const { focused, onFocus, onBlur } = usePaneFocusWithin();

  const clearMarks = useCallback(() => {
    clearChatHighlights(containerRef.current);
    marksRef.current = [];
  }, [containerRef]);

  const openBar = useCallback(() => setOpen(true), []);
  useActionHandler("pane.find", openBar, { enabled: focused });

  useEffect(() => {
    if (open) barRef.current?.focus();
  }, [open]);

  const close = useCallback(() => {
    setOpen(false);
    clearMarks();
    setCount(0);
    setActiveIndex(0);
    setQueryState("");
  }, [clearMarks]);

  const setQuery = useCallback((next: string) => setQueryState(next), []);

  // Re-walk and re-highlight whenever the bar is open and either the query
  // or the transcript content changes. Debounced: this touches the DOM
  // directly (never React state that would re-render TimelineRow), so it
  // can't break that memo, but it's still an O(rendered text) walk that
  // shouldn't run on every keystroke of a fast typist.
  useEffect(() => {
    if (!open) return;
    const timer = window.setTimeout(() => {
      clearMarks();
      const matches = findChatMatches(containerRef.current, query);
      const marks = applyChatHighlights(matches, 0);
      marksRef.current = marks;
      setCount(matches.length);
      setActiveIndex(0);
      marks[0]?.scrollIntoView({ block: "center" });
    }, FIND_DEBOUNCE_MS);
    return () => window.clearTimeout(timer);
  }, [open, query, revision, containerRef, clearMarks]);

  // Clears any highlight left in the DOM when this pane (or the whole app)
  // unmounts while Find was open.
  useEffect(() => clearMarks, [clearMarks]);

  const move = useCallback(
    (direction: (current: number, count: number) => number) => {
      setActiveIndex((current) => {
        const next = direction(current, count);
        restyleChatHighlights(marksRef.current, next);
        marksRef.current[next]?.scrollIntoView({ block: "center" });
        return next;
      });
    },
    [count],
  );
  const next = useCallback(() => move(nextMatchIndex), [move]);
  const previous = useCallback(() => move(previousMatchIndex), [move]);

  let status = "";
  if (query.trim()) status = count ? `${activeIndex + 1}/${count}` : "No matches";

  return { open, query, setQuery, status, count, next, previous, close, barRef, onFocus, onBlur };
}
