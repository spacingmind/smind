import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";

import {
  applyResolvedTheme,
  readStoredThemePreference,
  resolveTheme,
  writeStoredThemePreference,
  type ResolvedTheme,
  type ThemePreference,
} from "@/lib/theme";

const DARK_MEDIA_QUERY = "(prefers-color-scheme: dark)";

function prefersDarkNow(): boolean {
  if (typeof window === "undefined" || !window.matchMedia) return false;
  return window.matchMedia(DARK_MEDIA_QUERY).matches;
}

interface ThemeContextValue {
  /** The user's stored choice -- what the theme control shows as selected. */
  preference: ThemePreference;
  /** What's actually applied right now -- `preference` with `system` resolved. */
  resolved: ResolvedTheme;
  setPreference: (pref: ThemePreference) => void;
}

/**
 * A fully-functional standalone default (not `null`): every consumer below
 * (ThemeToggle, TerminalPane) can call `useTheme()` outside a
 * `<ThemeProvider>` -- as every one of their *own* component test files
 * does today, rendering them directly rather than under the full app tree
 * -- and get correct one-shot behavior (reads storage/matchMedia,
 * writes/applies on `setPreference`) rather than a thrown error. What it
 * doesn't get without the real provider is *reactivity*: an OS theme
 * change or a `setPreference` call elsewhere won't re-render this
 * consumer. That's the right tradeoff here -- none of those component
 * tests exercise cross-component theme reactivity, and main.tsx's real
 * `<ThemeProvider>` (which every non-test render lives under) is what
 * every reactive `useTheme()` behavior in the acceptance criteria and
 * this file's own test suite actually exercises.
 */
function standaloneThemeContextValue(): ThemeContextValue {
  const preference = readStoredThemePreference();
  return {
    preference,
    resolved: resolveTheme(preference, prefersDarkNow()),
    setPreference: (pref) => {
      writeStoredThemePreference(pref);
      applyResolvedTheme(resolveTheme(pref, prefersDarkNow()));
    },
  };
}

const ThemeContext = createContext<ThemeContextValue>(standaloneThemeContextValue());

/**
 * Owns the theme preference (light/dark/system), persists it, resolves
 * `system` via `prefers-color-scheme`, and is the only place besides
 * index.html's pre-paint script that touches the `dark` class on
 * `<html>` -- see lib/theme.ts's applyResolvedTheme. Mounted once at the
 * app root (main.tsx), above everything that might want to read or
 * change the theme.
 */
export function ThemeProvider({ children }: { children: ReactNode }) {
  const [preference, setPreferenceState] = useState<ThemePreference>(() => readStoredThemePreference());
  const [systemPrefersDark, setSystemPrefersDark] = useState<boolean>(() => prefersDarkNow());

  const resolved = resolveTheme(preference, systemPrefersDark);

  // Applies on every resolve change (preference change, or the OS query
  // flipping while pinned to "system") -- index.html's pre-paint script
  // already set the initial class before this ever runs, so this effect
  // is a no-op re-application on first mount in the common case, and the
  // one that actually matters on every subsequent change.
  useEffect(() => {
    applyResolvedTheme(resolved);
  }, [resolved]);

  // Only listens while "system" is selected, so a light/dark-pinned user
  // never has the OS query silently steer them back -- matches the plan's
  // "reacts to OS changes while on system" acceptance criterion precisely
  // (not "always listens").
  useEffect(() => {
    if (preference !== "system" || typeof window === "undefined" || !window.matchMedia) return;
    const mql = window.matchMedia(DARK_MEDIA_QUERY);
    const onChange = (e: MediaQueryListEvent) => setSystemPrefersDark(e.matches);
    mql.addEventListener("change", onChange);
    return () => mql.removeEventListener("change", onChange);
  }, [preference]);

  const setPreference = useCallback((pref: ThemePreference) => {
    setPreferenceState(pref);
    writeStoredThemePreference(pref);
  }, []);

  const value = useMemo<ThemeContextValue>(
    () => ({ preference, resolved, setPreference }),
    [preference, resolved, setPreference],
  );

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}

export function useTheme(): ThemeContextValue {
  return useContext(ThemeContext);
}
