import { useEffect } from "react";

/**
 * Cmd+P (mac) / Ctrl+P (elsewhere) opens quick-open, matching Paseo
 * (`audit-paseo.md` §7). A **local** document-level listener, not a
 * registered action in a global keyboard registry -- Item 4's
 * `keyboard/actions.ts` doesn't exist yet, and Track C's scope doesn't
 * include building it. Track A should replace this call with a real
 * `{id: "quick-open", combo: "Mod+P", action: onTrigger}` entry once
 * Item 4 lands; until then this is a plain, swappable function rather
 * than a component so lifting it is a one-line change, not a rewrite.
 *
 * Browsers bind Ctrl/Cmd+P to Print by default -- preventDefault is load-
 * bearing here, not decorative.
 *
 * Does not fire while `enabled` is false, so a caller can suppress it
 * while, say, a modal that wants its own Ctrl+P (none exist yet) is open.
 */
export function useQuickOpenShortcut(onTrigger: () => void, enabled = true): void {
  useEffect(() => {
    if (!enabled) return;

    function handleKeyDown(e: KeyboardEvent): void {
      const modifier = navigator.platform.toUpperCase().includes("MAC") ? e.metaKey : e.ctrlKey;
      if (!modifier || e.key.toLowerCase() !== "p") return;
      e.preventDefault();
      onTrigger();
    }

    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [onTrigger, enabled]);
}
