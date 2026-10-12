import { useSyncExternalStore } from "react";

const QUERY = "(hover: none)";

function subscribe(listener: () => void): () => void {
  if (typeof window.matchMedia !== "function") return () => {};
  const mql = window.matchMedia(QUERY);
  mql.addEventListener("change", listener);
  return () => mql.removeEventListener("change", listener);
}

function getSnapshot(): boolean {
  return typeof window.matchMedia !== "function" || !window.matchMedia(QUERY).matches;
}

/** False on touch-only devices (`(hover: none)`), where a hover card has no way to open: they get a tap popover instead. */
export function useCanHover(): boolean {
  return useSyncExternalStore(subscribe, getSnapshot, () => true);
}
