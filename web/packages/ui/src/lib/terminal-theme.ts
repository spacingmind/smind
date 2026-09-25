import type { ITheme } from "@xterm/xterm";
import type { ISearchDecorationOptions } from "@xterm/addon-search";

/**
 * Resolves a CSS custom property (as currently applied to `<html>`, i.e.
 * after `.dark` is toggled) to a literal color string xterm.js can use --
 * xterm's `ITheme` fields are plain CSS color strings, not live `var()`
 * references, so they need to be captured once per theme change and
 * re-applied (see terminal-pane.tsx's effect), unlike CodeMirror's own
 * chrome, which references the app's CSS variables directly and updates
 * for free when `.dark` toggles.
 *
 * The trick: set the var reference as an element's `color`, then read
 * back `getComputedStyle(...).color` -- the CSSOM always serializes a
 * resolved color as `rgb()`/`rgba()`, regardless of the original color
 * function it was specified with (OKLCH, a named color, whatever), which
 * sidesteps needing an OKLCH-aware parser here or in xterm.
 */
function resolveCssColor(varName: string): string {
  const probe = document.createElement("span");
  probe.style.color = `var(${varName})`;
  probe.style.display = "none";
  document.body.appendChild(probe);
  const resolved = getComputedStyle(probe).color;
  document.body.removeChild(probe);
  return resolved;
}

/**
 * The terminal's chrome (background/foreground/cursor/selection) pulled
 * from the app's own tokens, so xterm no longer defaults to a fixed
 * light-mode palette regardless of the app's theme (audit-smind-current.md
 * §6's "no terminal ANSI palette tied to theme" finding -- chrome only;
 * ANSI colors 0-15 are left at xterm's own defaults for this item, a
 * fuller ANSI palette being a design decision of its own, deferred to the
 * plan's Item 20 terminal v2).
 */
export function resolveTerminalTheme(): ITheme {
  return {
    background: resolveCssColor("--background"),
    foreground: resolveCssColor("--foreground"),
    cursor: resolveCssColor("--foreground"),
    cursorAccent: resolveCssColor("--background"),
    selectionBackground: resolveCssColor("--accent"),
  };
}

/**
 * `@xterm/addon-search`'s match/active-match decoration colors (AC3),
 * resolved the same way as {@link resolveTerminalTheme} -- the addon wants
 * literal color strings, not live `var()` references, so this is called
 * fresh on every search rather than cached, which is what makes a theme
 * toggle mid-search show the new colors on the very next keystroke with no
 * extra wiring.
 */
export function resolveSearchDecorations(): ISearchDecorationOptions {
  const match = resolveCssColor("--find-highlight");
  const active = resolveCssColor("--find-highlight-active");
  return {
    matchBackground: match,
    matchBorder: match,
    matchOverviewRuler: match,
    activeMatchBackground: active,
    activeMatchBorder: active,
    activeMatchColorOverviewRuler: active,
  };
}
