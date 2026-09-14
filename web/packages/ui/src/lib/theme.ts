/**
 * Pure theme-preference logic (ui-redesign-parity plan, Item 1), factored
 * out of hooks/use-theme.tsx the same way lib/ws-client.ts factors the
 * WebSocket boundary out of its consumers: no React here, so this is
 * plain-unit-testable and is also the logic index.html's pre-paint
 * `<script>` mirrors by hand (see that file's comment) to avoid a flash
 * of the wrong theme before React mounts.
 */

export type ThemePreference = "light" | "dark" | "system";
export type ResolvedTheme = "light" | "dark";

/** Must match index.html's inline bootstrap script's STORAGE_KEY. */
export const THEME_STORAGE_KEY = "smind:theme";

/** `system` resolves via the caller-supplied `prefersDark` (usually a `matchMedia("(prefers-color-scheme: dark)").matches` read) rather than reading matchMedia itself, so this stays synchronous and trivially testable. */
export function resolveTheme(pref: ThemePreference, prefersDark: boolean): ResolvedTheme {
  if (pref === "system") return prefersDark ? "dark" : "light";
  return pref;
}

/** Reads the persisted preference, defaulting to `"system"` for anything absent, corrupt, or thrown (storage disabled, private-browsing quota) -- same defensive shape as use-sidebar-width.ts's readStored. */
export function readStoredThemePreference(): ThemePreference {
  if (typeof window === "undefined") return "system";
  try {
    const raw = window.localStorage.getItem(THEME_STORAGE_KEY);
    if (raw === "light" || raw === "dark" || raw === "system") return raw;
    return "system";
  } catch {
    return "system";
  }
}

/** Best-effort persistence -- a write failure (quota, disabled storage) shouldn't stop the preference from applying for the rest of this session. */
export function writeStoredThemePreference(pref: ThemePreference): void {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(THEME_STORAGE_KEY, pref);
  } catch {
    // Best-effort only.
  }
}

/** Applies a resolved theme to the document by toggling the `dark` class on `<html>` -- the single place any code in this app should touch that class, so it can never disagree with what's persisted. */
export function applyResolvedTheme(resolved: ResolvedTheme): void {
  document.documentElement.classList.toggle("dark", resolved === "dark");
}

/**
 * The exact decision index.html's inline bootstrap script implements,
 * extracted here purely so it has one real unit test asserting the two
 * stay behaviorally identical (see that file's test in
 * src/lib/theme.test.ts) -- this function is not itself called by the
 * bootstrap script (which can't import a module before first paint), but
 * every branch here has a same-shaped branch there.
 */
export function computeBootstrapIsDark(storedPreference: string | null, prefersDark: boolean): boolean {
  if (storedPreference === "dark") return true;
  if (storedPreference === "light") return false;
  return prefersDark;
}
