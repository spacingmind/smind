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
 * xterm field -> the `--color-terminal-*` custom property it reads
 * (zcode-visual-parity plan, P4 -- ZCode's own
 * `terminal/terminalTheme.ts` token map, ported onto this file's
 * existing computed-style probe rather than xterm's own `css.toColor`,
 * which doesn't understand `color-mix()`/`var()`). The 16-color ANSI
 * set was ported into index.css in P1 but left unused until now
 * (audit-smind-current.md §6's "no terminal ANSI palette tied to
 * theme" finding).
 */
const TERMINAL_THEME_TOKENS = {
  background: "--color-terminal-bg",
  foreground: "--color-terminal-fg",
  cursor: "--color-terminal-cursor",
  cursorAccent: "--color-terminal-cursor-accent",
  selectionBackground: "--color-terminal-selection",
  selectionInactiveBackground: "--color-terminal-selection-inactive",
  black: "--color-terminal-black",
  red: "--color-terminal-red",
  green: "--color-terminal-green",
  yellow: "--color-terminal-yellow",
  blue: "--color-terminal-blue",
  magenta: "--color-terminal-magenta",
  cyan: "--color-terminal-cyan",
  white: "--color-terminal-white",
  brightBlack: "--color-terminal-bright-black",
  brightRed: "--color-terminal-bright-red",
  brightGreen: "--color-terminal-bright-green",
  brightYellow: "--color-terminal-bright-yellow",
  brightBlue: "--color-terminal-bright-blue",
  brightMagenta: "--color-terminal-bright-magenta",
  brightCyan: "--color-terminal-bright-cyan",
  brightWhite: "--color-terminal-bright-white",
} satisfies Partial<Record<keyof ITheme, string>>;

/**
 * The terminal's full chrome + 16-color ANSI palette, pulled from the
 * app's own `--color-terminal-*` tokens so xterm no longer defaults to
 * a fixed palette regardless of the app's theme.
 */
export function resolveTerminalTheme(): ITheme {
  const theme: ITheme = {};
  for (const [field, varName] of Object.entries(TERMINAL_THEME_TOKENS) as [
    keyof typeof TERMINAL_THEME_TOKENS,
    string,
  ][]) {
    theme[field] = resolveCssColor(varName);
  }
  return theme;
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
