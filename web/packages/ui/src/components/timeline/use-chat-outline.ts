import { useCallback, useEffect, useRef, useState, type RefObject } from "react";

import {
  ACTIVE_ANCHOR_PX,
  buildOutlinePrompts,
  createActivePromptPublisher,
  resolveActiveRunIndex,
  stabilizeOutlinePrompts,
  type ActivePromptSource,
  type OutlinePrompt,
} from "./chat-outline";

/**
 * Reading column is `max-w-3xl` (768px); the rail plus its gutter needs ~44px each side, so
 * narrower panes (mobile, split panes) go without it.
 */
export const MIN_RAIL_PANE_WIDTH_PX = 860;

export interface ChatOutline {
  prompts: OutlinePrompt[];
  activePrompt: ActivePromptSource;
  /** Rail is worth showing: 2+ runs, content overflows, pane wide enough. */
  visible: boolean;
  jump: (runId: string) => void;
  scrub: (runId: string) => void;
}

function runElements(scroll: HTMLElement): HTMLElement[] {
  return Array.from(scroll.querySelectorAll<HTMLElement>("[data-run-id]"));
}

/** Top of `el` in the scroll element's content coordinates. */
function contentTop(scroll: HTMLElement, el: HTMLElement): number {
  return el.getBoundingClientRect().top - scroll.getBoundingClientRect().top + scroll.scrollTop;
}

function prefersReducedMotion(): boolean {
  return (
    typeof window !== "undefined" &&
    typeof window.matchMedia === "function" &&
    window.matchMedia("(prefers-reduced-motion: reduce)").matches
  );
}

/**
 * Drives the chat timeline rail. The active run lives in an external store the rail alone
 * subscribes to, so scrolling never re-renders the pane or any message; only the (rarely
 * flipping) `visible` flag is React state.
 */
export function useChatOutline<T extends HTMLElement>(
  scrollRef: RefObject<T | null>,
  runs: readonly { id: string; prompt: string }[] | null | undefined,
): ChatOutline {
  const [activePrompt] = useState(createActivePromptPublisher);
  const [fits, setFits] = useState({ overflow: false, wide: false });

  // Streaming makes a new `runs` array per chunk; keep the prompts identity while ids and
  // previews are unchanged so nothing downstream re-renders.
  const promptsRef = useRef<OutlinePrompt[]>([]);
  promptsRef.current = stabilizeOutlinePrompts(
    promptsRef.current,
    buildOutlinePrompts(runs ?? []),
  );
  const prompts = promptsRef.current;

  const updateActive = useCallback(() => {
    const scroll = scrollRef.current;
    if (!scroll) return;
    const elements = runElements(scroll);
    if (elements.length === 0) {
      activePrompt.publish(null);
      return;
    }
    const tops = elements.map((el) => contentTop(scroll, el));
    const atBottom = scroll.scrollHeight - scroll.scrollTop - scroll.clientHeight <= 1;
    const index = resolveActiveRunIndex(tops, scroll.scrollTop + ACTIVE_ANCHOR_PX, atBottom);
    activePrompt.publish(elements[index]?.dataset.runId ?? null);
  }, [activePrompt, scrollRef]);

  const measure = useCallback(() => {
    const scroll = scrollRef.current;
    if (!scroll) return;
    const overflow = scroll.scrollHeight > scroll.clientHeight;
    const wide = scroll.clientWidth >= MIN_RAIL_PANE_WIDTH_PX;
    setFits((prev) => (prev.overflow === overflow && prev.wide === wide ? prev : { overflow, wide }));
  }, [scrollRef]);

  // Scroll: rAF-throttled, passive.
  useEffect(() => {
    const scroll = scrollRef.current;
    if (!scroll) return;
    let frame: number | null = null;
    const onScroll = () => {
      if (frame !== null) return;
      frame = window.requestAnimationFrame(() => {
        frame = null;
        updateActive();
      });
    };
    scroll.addEventListener("scroll", onScroll, { passive: true });
    return () => {
      scroll.removeEventListener("scroll", onScroll);
      if (frame !== null) window.cancelAnimationFrame(frame);
    };
  }, [scrollRef, updateActive]);

  // Size: overflow and width, from the scroll element and its content.
  useEffect(() => {
    const scroll = scrollRef.current;
    if (!scroll) return;
    measure();
    if (typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(() => {
      measure();
      updateActive();
    });
    observer.observe(scroll);
    if (scroll.firstElementChild) observer.observe(scroll.firstElementChild);
    return () => observer.disconnect();
  }, [scrollRef, measure, updateActive]);

  // The run list changed (not merely its contents): re-resolve position and overflow.
  useEffect(() => {
    measure();
    updateActive();
  }, [prompts, measure, updateActive]);

  const scrollToRun = useCallback(
    (runId: string, behavior: ScrollBehavior) => {
      const scroll = scrollRef.current;
      if (!scroll) return;
      const target = runElements(scroll).find((el) => el.dataset.runId === runId);
      if (!target) return;
      const top = contentTop(scroll, target);
      if (typeof scroll.scrollTo === "function") scroll.scrollTo({ top, behavior });
      else scroll.scrollTop = top;
      activePrompt.publish(runId);
    },
    [activePrompt, scrollRef],
  );

  const jump = useCallback(
    (runId: string) => scrollToRun(runId, prefersReducedMotion() ? "instant" : "smooth"),
    [scrollToRun],
  );
  const scrub = useCallback((runId: string) => scrollToRun(runId, "instant"), [scrollToRun]);

  const visible = prompts.length >= 2 && fits.overflow && fits.wide;
  return { prompts, activePrompt, visible, jump, scrub };
}
