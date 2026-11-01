/**
 * The scripted daemon's only source of time (ADR-0051 section 2): tests inject a fake one, the browser uses timers.
 * `after` returns a cancel function; `reducedMotion` collapses every delay to a few milliseconds.
 */
export interface DemoClock {
  /** Wall-clock "now" the scenario's timestamps are written relative to. */
  now(): number;
  after(ms: number, fn: () => void): () => void;
}

/** Delay used for every step when the visitor prefers reduced motion. */
export const REDUCED_MOTION_DELAY_MS = 5;

export function prefersReducedMotion(): boolean {
  return typeof window !== "undefined" && typeof window.matchMedia === "function" && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
}

export function realClock(): DemoClock {
  return {
    now: () => Date.now(),
    after(ms, fn) {
      const id = setTimeout(fn, ms);
      return () => clearTimeout(id);
    },
  };
}
