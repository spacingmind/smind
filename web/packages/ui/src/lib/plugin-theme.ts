import { PLUGIN_THEME_TOKENS, usableThemeTokens, type PluginTheme, type ThemeScheme } from "@/lib/plugin-contrib";

/** The selected plugin theme with its tokens, so the next load can paint it before the daemon has answered. */
export const PLUGIN_THEME_STORAGE_KEY = "smind:plugin-theme";

export interface StoredPluginTheme {
  /** `<plugin>:<id>`. */
  id: string;
  theme: PluginTheme;
}

function isTheme(value: unknown): value is PluginTheme {
  const t = value as PluginTheme | null;
  return (
    typeof t === "object" &&
    t !== null &&
    typeof t.plugin === "string" &&
    typeof t.id === "string" &&
    typeof t.qualified === "string" &&
    typeof t.label === "string" &&
    typeof t.schemes === "object" &&
    t.schemes !== null
  );
}

/** The persisted selection, or null for anything absent, corrupt or thrown (storage disabled). */
export function readStoredPluginTheme(): StoredPluginTheme | null {
  if (typeof window === "undefined") return null;
  try {
    const raw = window.localStorage.getItem(PLUGIN_THEME_STORAGE_KEY);
    if (!raw) return null;
    const parsed = JSON.parse(raw) as { id?: unknown; theme?: unknown } | null;
    if (!parsed || typeof parsed.id !== "string" || !isTheme(parsed.theme) || parsed.theme.qualified !== parsed.id) return null;
    return { id: parsed.id, theme: parsed.theme };
  } catch {
    return null;
  }
}

export function writeStoredPluginTheme(value: StoredPluginTheme | null): void {
  if (typeof window === "undefined") return;
  try {
    if (value === null) window.localStorage.removeItem(PLUGIN_THEME_STORAGE_KEY);
    else window.localStorage.setItem(PLUGIN_THEME_STORAGE_KEY, JSON.stringify(value));
  } catch {
    // Best-effort only.
  }
}

/** Removes every plugin-set token from the root, which hands each back to index.css's own value for the scheme. */
export function clearPluginThemeTokens(): void {
  const style = document.documentElement.style;
  for (const token of PLUGIN_THEME_TOKENS) style.removeProperty(token);
}

/**
 * Applies `theme` for the scheme now in effect as CSS custom properties on the root element (never as stylesheet
 * text), after clearing whatever was applied before. A theme that does not declare the scheme, or whose set for it
 * is not complete and safe, leaves the scheme on its own colours: a theme never leaks into the scheme it did not
 * declare. Returns whether anything was applied.
 */
export function applyPluginTheme(scheme: ThemeScheme, theme: PluginTheme | null): boolean {
  clearPluginThemeTokens();
  const tokens = theme ? usableThemeTokens(theme.schemes[scheme]) : null;
  if (!tokens) return false;
  const style = document.documentElement.style;
  for (const token of PLUGIN_THEME_TOKENS) style.setProperty(token, tokens[token]!);
  return true;
}

/** The scheme the page is in right now (index.html's bootstrap and ThemeProvider own the `dark` class). */
export function currentScheme(): ThemeScheme {
  return document.documentElement.classList.contains("dark") ? "dark" : "light";
}

/** Pre-paint: puts the remembered theme on the root before React renders, so a reload does not flash the default. */
export function applyStoredPluginTheme(): void {
  const stored = readStoredPluginTheme();
  if (stored) applyPluginTheme(currentScheme(), stored.theme);
}
