/**
 * The attributes <html> must carry before first paint for the built sMind
 * theme stylesheet (src/theme/smind-theme.css) to apply (ADR-0064, AC5).
 *
 * The built CSS selects on two things (found in the generated file and in
 * Astryx's theme-application contract):
 *   - `[data-astryx-theme="smind"]`: scopes every theme rule (the <html>
 *     element is the scope root, so portals are covered too);
 *   - `html[data-theme="light"|"dark"]`: sets `color-scheme`, which the
 *     `light-dark()` token values follow.
 *
 * index.html's inline pre-paint script cannot import this module, so it
 * repeats this logic by hand (same names, same values); the test
 * `prepaint_script_sets_the_astryx_theme_attributes` keeps the two in sync.
 * The existing `dark` class (Tailwind's variant, src/lib/theme.ts) is not part
 * of this map and stays as it is.
 *
 * `system` is written as the scheme the page resolves to right now, the same
 * value as the `dark` class, instead of removing `data-theme`. Astryx's own
 * root provider removes it for system mode; the effect is identical, because
 * `color-scheme` then follows the same OS preference.
 */
import type { ThemePreference } from "../lib/theme";

export const ASTRYX_THEME_NAME = "smind";

export interface ThemeAttributes {
  "data-astryx-theme": typeof ASTRYX_THEME_NAME;
  "data-theme": "light" | "dark";
}

export function themeAttributesFor(mode: ThemePreference, prefersDark: boolean): ThemeAttributes {
  const dark = mode === "dark" || (mode !== "light" && prefersDark);
  return {
    "data-astryx-theme": ASTRYX_THEME_NAME,
    "data-theme": dark ? "dark" : "light",
  };
}

/** Writes the map onto an element (for src/lib/theme.ts to call next to its `dark` class toggle). */
export function applyThemeAttributes(root: Element, mode: ThemePreference, prefersDark: boolean): void {
  for (const [name, value] of Object.entries(themeAttributesFor(mode, prefersDark))) {
    root.setAttribute(name, value);
  }
}
