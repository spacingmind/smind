/**
 * What plugins contribute to the web UI that is plain data: prompt slash
 * commands and themes (ADR-0042). The wire shapes are the daemon's
 * `plugin.commands` and `plugin.themes` results; see internal/plugins.
 */

export interface PluginCommand {
  plugin: string;
  name: string;
  /** `<plugin>:<name>`, always unique. */
  qualified: string;
  description?: string;
  /** The text the command inserts into the composer. It is never sent by the command itself. */
  prompt: string;
  /** The bare name the daemon gave this command (first plugin by id); clients re-derive it. */
  alias?: string;
}

export type ThemeScheme = "light" | "dark";

export interface PluginTheme {
  plugin: string;
  id: string;
  /** `<plugin>:<id>`: what the preference stores. */
  qualified: string;
  label: string;
  /** A complete token set per declared scheme; a scheme not listed is left alone. */
  schemes: Partial<Record<ThemeScheme, Record<string, string>>>;
}

/**
 * The CSS custom properties a plugin theme sets (per declared scheme): the colour tokens index.css declares in both
 * `:root` and `.dark`. Mirrors internal/plugins/themetokens.json, which the daemon validates against; the
 * `plugin-theme-tokens` test fails when either copy drifts from index.css or from each other.
 */
export const PLUGIN_THEME_TOKENS: readonly string[] = [
  "--accent",
  "--accent-foreground",
  "--background",
  "--border",
  "--border-hover",
  "--card",
  "--card-border",
  "--card-foreground",
  "--card-selected",
  "--chart-1",
  "--chart-2",
  "--chart-3",
  "--chart-4",
  "--chart-5",
  "--destructive",
  "--destructive-foreground",
  "--diff-added",
  "--diff-added-foreground",
  "--diff-addition",
  "--diff-deletion",
  "--diff-removed",
  "--diff-removed-foreground",
  "--find-highlight",
  "--find-highlight-active",
  "--foreground",
  "--foreground-inverse",
  "--foreground-muted",
  "--foreground-subtle",
  "--foreground-subtlest",
  "--git-added",
  "--git-deleted",
  "--git-descendant",
  "--git-ignored",
  "--git-modified",
  "--git-none",
  "--git-renamed",
  "--git-untracked",
  "--header",
  "--hover",
  "--input",
  "--input-bg",
  "--input-bg-focused",
  "--input-border",
  "--input-border-focused",
  "--input-border-hover",
  "--interaction-confirmation-foreground",
  "--interaction-confirmation-surface",
  "--menu",
  "--menu-hover",
  "--muted",
  "--muted-foreground",
  "--panel",
  "--popover",
  "--popover-border",
  "--popover-foreground",
  "--popover-header",
  "--primary",
  "--primary-foreground",
  "--ring",
  "--secondary",
  "--secondary-foreground",
  "--selected",
  "--sidebar",
  "--sidebar-accent",
  "--sidebar-accent-foreground",
  "--sidebar-border",
  "--sidebar-foreground",
  "--sidebar-primary",
  "--sidebar-primary-foreground",
  "--sidebar-ring",
  "--success",
  "--success-foreground",
  "--surface",
  "--surface-hover",
  "--syntax-attr",
  "--syntax-builtin",
  "--syntax-comment",
  "--syntax-function",
  "--syntax-keyword",
  "--syntax-meta",
  "--syntax-number",
  "--syntax-regexp",
  "--syntax-string",
  "--syntax-symbol",
  "--syntax-type",
  "--tag",
  "--terminal-bg",
  "--terminal-black",
  "--terminal-blue",
  "--terminal-bright-black",
  "--terminal-bright-blue",
  "--terminal-bright-cyan",
  "--terminal-bright-green",
  "--terminal-bright-magenta",
  "--terminal-bright-red",
  "--terminal-bright-white",
  "--terminal-bright-yellow",
  "--terminal-cursor",
  "--terminal-cursor-accent",
  "--terminal-cyan",
  "--terminal-fg",
  "--terminal-green",
  "--terminal-magenta",
  "--terminal-red",
  "--terminal-selection",
  "--terminal-selection-inactive",
  "--terminal-white",
  "--terminal-yellow",
  "--toast",
  "--tooltip",
  "--tooltip-foreground",
  "--tooltip-tag",
  "--tooltip-tag-foreground",
  "--warning",
  "--warning-foreground"
];

const TOKEN_SET: ReadonlySet<string> = new Set(PLUGIN_THEME_TOKENS);

/**
 * The whole grammar a theme value may use (same as internal/plugins/theme.go): a hex colour, an
 * rgb(), rgba(), hsl(), hsla() or the OK-LCH function, a plain length, or `transparent`. Nothing else: no url(), var(), calc(),
 * quotes, backslashes, semicolons or braces. Only ASCII digits and space/tab separators appear, so lookalikes and
 * newlines cannot match.
 */
const NUM = String.raw`[+-]?[0-9]*\.?[0-9]+(?:%|deg)?`;
const SEP = String.raw`(?:[ \t]*,[ \t]*|[ \t]+)`;
const THEME_VALUE_RE = new RegExp(
  "^(?:" +
    "#(?:[0-9a-fA-F]{3,4}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})" +
    `|(?:rgb|rgba|hsl|hsla|oklch)\\([ \\t]*${NUM}${SEP}${NUM}${SEP}${NUM}(?:[ \\t]*[,/][ \\t]*[+-]?[0-9]*\\.?[0-9]+%?)?[ \\t]*\\)` +
    String.raw`|-?[0-9]*\.?[0-9]+(?:px|rem|em|%)?` +
    "|transparent" +
    ")$",
);

export function isSafeThemeValue(value: unknown): value is string {
  return typeof value === "string" && value.length <= 64 && THEME_VALUE_RE.test(value);
}

/**
 * The token set to apply for one scheme, or null when it is not a complete set of safe values over exactly the
 * canonical tokens. The daemon already refuses such a theme at install; this is the client's own check, so a
 * daemon that was compromised or is newer than this build can never put an unlisted property or an unsafe value
 * on the page.
 */
export function usableThemeTokens(set: Record<string, string> | undefined): Record<string, string> | null {
  if (!set || typeof set !== "object") return null;
  const keys = Object.keys(set);
  if (keys.length !== TOKEN_SET.size) return null;
  for (const key of keys) {
    if (!TOKEN_SET.has(key) || !isSafeThemeValue(set[key])) return null;
  }
  return set;
}
