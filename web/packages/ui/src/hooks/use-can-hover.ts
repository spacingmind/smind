import { useSyncExternalStore } from "react";

const QUERY = "(hover: none)";

function subscribe(listener: () => void): () => void {
  if (typeof window.matchMedia !== "function") return () => {};
  const mql = window.matchMedia(QUERY);
  // Safari < 14 only has the deprecated addListener.
  if (typeof mql.addEventListener === "function") {
    mql.addEventListener("change", listener);
    return () => mql.removeEventListener("change", listener);
  }
  mql.addListener(listener);
  return () => mql.removeListener(listener);
}

function getSnapshot(): boolean {
  return typeof window.matchMedia !== "function" || !window.matchMedia(QUERY).matches;
}

/** False on touch-only devices (`(hover: none)`), where there is no hover: the card then opens on tap only. */
export function useCanHover(): boolean {
  return useSyncExternalStore(subscribe, getSnapshot, () => true);
}
