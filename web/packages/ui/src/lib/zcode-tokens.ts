/**
 * Typed name map between ZCode design tokens and smind's `index.css`
 * (zcode-parity plan, Stage 1). ZCode is a visual spec: this table records
 * which ZCode token each smind raw var stands for and the value both themes
 * must resolve to; `zcode-tokens.test.ts` fails when `index.css` drifts.
 *
 * Rows: [zcode name, smind raw var, light value (`:root`), dark value
 * (`.dark`), status, `@theme inline` export or null].
 *   - ported: already present and value-equal to ZCode's Zai palette.
 *   - alias:  same resolved value, expressed through smind's own raw vars
 *             (ZCode `var(--color-border)` is smind `var(--border)`).
 *   - added:  introduced in Stage 1.
 */
export type ZcodeTokenStatus = "ported" | "alias" | "added";

export interface ZcodeToken {
  zcode: string;
  smind: string;
  light: string | null;
  dark: string | null;
  status: ZcodeTokenStatus;
  theme: string | null;
}

type Row = [string, string, string | null, string | null, ZcodeTokenStatus, string | null];

const ROWS: Row[] = [
  ["--shadow-composer-soft", "--elevation-shadow-composer", "0 1px 3px rgb(0 0 0 / 4%), 0 6px 18px rgb(0 0 0 / 4%)", "0 0 #0000", "added", "--shadow-composer-soft"],
  ["--color-background", "--background", "#f8f8f8", "#161616", "ported", "--color-background"],
  ["--color-background-win-alt", "--background-win-alt", "#ececee", "#2b2b2b", "added", "--color-background-win-alt"],
  ["--color-background-alt", "--background-alt", "color-mix(in oklab, var(--background) 70%, transparent)", "color-mix(in oklab, var(--background-win-alt) 60%, transparent)", "added", "--color-background-alt"],
  ["--color-brand", "--brand", "#000000", "#ffffff", "added", "--color-brand"],
  ["--color-trajectory-user", "--trajectory-user", "#2563eb", "#60a5fa", "added", "--color-trajectory-user"],
  ["--color-trajectory-assistant", "--trajectory-assistant", "#0f766e", "#2dd4bf", "added", "--color-trajectory-assistant"],
  ["--color-trajectory-reasoning", "--trajectory-reasoning", "#7c3aed", "#a78bfa", "added", "--color-trajectory-reasoning"],
  ["--color-trajectory-tool-call", "--trajectory-tool-call", "#d97706", "#f59e0b", "added", "--color-trajectory-tool-call"],
  ["--color-trajectory-tool-result", "--trajectory-tool-result", "#0284c7", "#38bdf8", "added", "--color-trajectory-tool-result"],
  ["--color-accent", "--accent", "#ebf4ff", "#001d3d", "ported", "--color-accent"],
  ["--color-find-highlight", "--find-highlight", "#fff4eb", "#542500", "ported", "--color-find-highlight"],
  ["--color-find-highlight-active", "--find-highlight-active", "#ffb26b", "#ff8a30", "ported", "--color-find-highlight-active"],
  ["--color-border", "--border", "rgba(13, 13, 13, 0.1)", "rgba(255, 255, 255, 0.1)", "ported", "--color-border"],
  ["--color-border-hover", "--border-hover", "rgba(13, 13, 13, 0.15)", "rgba(255, 255, 255, 0.15)", "ported", "--color-border-hover"],
  ["--color-hover", "--hover", "rgba(13, 13, 13, 0.05)", "rgba(255, 255, 255, 0.05)", "ported", "--color-hover"],
  ["--color-selected", "--selected", "rgba(13, 13, 13, 0.05)", "rgba(255, 255, 255, 0.1)", "ported", "--color-selected"],
  ["--color-header", "--header", "#ffffff", "#202020", "ported", "--color-header"],
  ["--color-panel", "--panel", "#ffffff", "#202020", "ported", "--color-panel"],
  ["--color-sidebar", "--sidebar", "#f0f0f0", "#161616", "ported", "--color-sidebar"],
  ["--color-surface", "--surface", "rgba(13, 13, 13, 0.03)", "rgba(255, 255, 255, 0.05)", "ported", "--color-surface"],
  ["--color-user-message-bubble", "--user-message-bubble", "rgba(13, 13, 13, 0.05)", "rgba(255, 255, 255, 0.08)", "added", "--color-user-message-bubble"],
  ["--color-surface-hover", "--surface-hover", "rgba(13, 13, 13, 0.05)", "rgba(255, 255, 255, 0.1)", "ported", "--color-surface-hover"],
  ["--color-card", "--card", "#ffffff", "#2b2b2b", "ported", "--color-card"],
  ["--color-card-selected", "--card-selected", "var(--input-bg)", "var(--input-bg)", "alias", "--color-card-selected"],
  ["--color-card-border", "--card-border", "var(--border)", "var(--border)", "alias", "--color-card-border"],
  ["--color-popover", "--popover", "#ffffff", "#2b2b2b", "ported", "--color-popover"],
  ["--color-popover-foreground", "--popover-foreground", "var(--foreground)", "var(--foreground)", "alias", "--color-popover-foreground"],
  ["--color-popover-header", "--popover-header", "#f8f8f8", "#202020", "ported", "--color-popover-header"],
  ["--color-popover-border", "--popover-border", "var(--border)", "var(--border)", "alias", "--color-popover-border"],
  ["--color-input", "--input-bg", "#ffffff", "#2b2b2b", "ported", "--color-input"],
  ["--color-input-focused", "--input-bg-focused", "var(--input-bg)", "var(--input-bg)", "alias", "--color-input-focused"],
  ["--color-input-border", "--input-border", "var(--border)", "var(--border)", "alias", "--color-input-border"],
  ["--color-input-border-hover", "--input-border-hover", "rgba(13, 13, 13, 0.15)", "rgba(255, 255, 255, 0.15)", "alias", "--color-input-border-hover"],
  ["--color-input-border-focused", "--input-border-focused", "var(--input-border-hover)", "var(--input-border-hover)", "alias", "--color-input-border-focused"],
  ["--color-terminal-bg", "--terminal-bg", "var(--background)", "var(--background)", "alias", "--color-terminal-bg"],
  ["--color-terminal-fg", "--terminal-fg", "var(--foreground)", "var(--foreground)", "alias", "--color-terminal-fg"],
  ["--color-terminal-cursor", "--terminal-cursor", "#0d0d0d", "#f8f8f8", "ported", "--color-terminal-cursor"],
  ["--color-terminal-cursor-accent", "--terminal-cursor-accent", "var(--background)", "#161616", "alias", "--color-terminal-cursor-accent"],
  ["--color-terminal-selection", "--terminal-selection", "rgba(11, 127, 255, 0.22)", "rgba(64, 153, 255, 0.28)", "ported", "--color-terminal-selection"],
  ["--color-terminal-selection-inactive", "--terminal-selection-inactive", "rgba(13, 13, 13, 0.1)", "rgba(255, 255, 255, 0.1)", "ported", "--color-terminal-selection-inactive"],
  ["--color-terminal-black", "--terminal-black", "#5c5c5c", "#363636", "ported", "--color-terminal-black"],
  ["--color-terminal-red", "--terminal-red", "#e03131", "#ff5c5c", "ported", "--color-terminal-red"],
  ["--color-terminal-green", "--terminal-green", "#1e8a3e", "#46bf72", "ported", "--color-terminal-green"],
  ["--color-terminal-yellow", "--terminal-yellow", "#e07b00", "#ff8a30", "ported", "--color-terminal-yellow"],
  ["--color-terminal-blue", "--terminal-blue", "#0b7fff", "#4099ff", "ported", "--color-terminal-blue"],
  ["--color-terminal-magenta", "--terminal-magenta", "#9e77ed", "#7b5ce5", "ported", "--color-terminal-magenta"],
  ["--color-terminal-cyan", "--terminal-cyan", "#0aa7a7", "#42c8c8", "ported", "--color-terminal-cyan"],
  ["--color-terminal-white", "--terminal-white", "#adadad", "#adadad", "ported", "--color-terminal-white"],
  ["--color-terminal-bright-black", "--terminal-bright-black", "#888888", "#747474", "ported", "--color-terminal-bright-black"],
  ["--color-terminal-bright-red", "--terminal-bright-red", "#e03131", "#ff9999", "ported", "--color-terminal-bright-red"],
  ["--color-terminal-bright-green", "--terminal-bright-green", "#1e8a3e", "#87d9a4", "ported", "--color-terminal-bright-green"],
  ["--color-terminal-bright-yellow", "--terminal-bright-yellow", "#e07b00", "#ffb26b", "ported", "--color-terminal-bright-yellow"],
  ["--color-terminal-bright-blue", "--terminal-bright-blue", "#0066dd", "#80beff", "ported", "--color-terminal-bright-blue"],
  ["--color-terminal-bright-magenta", "--terminal-bright-magenta", "#9e77ed", "#a888f2", "ported", "--color-terminal-bright-magenta"],
  ["--color-terminal-bright-cyan", "--terminal-bright-cyan", "#0aa7a7", "#8ee5e5", "ported", "--color-terminal-bright-cyan"],
  ["--color-terminal-bright-white", "--terminal-bright-white", "#0d0d0d", "#f8f8f8", "ported", "--color-terminal-bright-white"],
  ["--color-usage-chart-1", "--usage-chart-1", "#0b7fff", "#4099ff", "added", "--color-usage-chart-1"],
  ["--color-usage-chart-2", "--usage-chart-2", "#1e8a3e", "#46bf72", "added", "--color-usage-chart-2"],
  ["--color-usage-chart-3", "--usage-chart-3", "#9e77ed", "#7b5ce5", "added", "--color-usage-chart-3"],
  ["--color-usage-chart-4", "--usage-chart-4", "#e03131", "#ff5c5c", "added", "--color-usage-chart-4"],
  ["--color-usage-chart-5", "--usage-chart-5", "#e07b00", "#ff8a30", "added", "--color-usage-chart-5"],
  ["--color-usage-chart-6", "--usage-chart-6", "#0aa7a7", "#42c8c8", "added", "--color-usage-chart-6"],
  ["--color-context-breakdown-1", "--context-breakdown-1", "#0b7fff", "#4099ff", "added", "--color-context-breakdown-1"],
  ["--color-context-breakdown-2", "--context-breakdown-2", "#338fff", "#66adff", "added", "--color-context-breakdown-2"],
  ["--color-context-breakdown-3", "--context-breakdown-3", "#5ca7ff", "#80beff", "added", "--color-context-breakdown-3"],
  ["--color-context-breakdown-4", "--context-breakdown-4", "#85bbff", "#9dceff", "added", "--color-context-breakdown-4"],
  ["--color-context-breakdown-5", "--context-breakdown-5", "#acd0ff", "#b9ddff", "added", "--color-context-breakdown-5"],
  ["--color-context-breakdown-6", "--context-breakdown-6", "#c8ddff", "#d0e8ff", "added", "--color-context-breakdown-6"],
  ["--color-context-breakdown-7", "--context-breakdown-7", "#e0ecff", "#e4f1ff", "added", "--color-context-breakdown-7"],
  ["--color-usage-heatmap-0", "--usage-heatmap-0", "color-mix(in oklab, #0b7fff 0%, var(--surface))", "color-mix(in oklab, #4099ff 0%, var(--surface))", "added", "--color-usage-heatmap-0"],
  ["--color-usage-heatmap-1", "--usage-heatmap-1", "color-mix(in oklab, #0b7fff 18%, var(--surface))", "color-mix(in oklab, #4099ff 24%, var(--surface))", "added", "--color-usage-heatmap-1"],
  ["--color-usage-heatmap-2", "--usage-heatmap-2", "color-mix(in oklab, #0b7fff 36%, var(--surface))", "color-mix(in oklab, #4099ff 42%, var(--surface))", "added", "--color-usage-heatmap-2"],
  ["--color-usage-heatmap-3", "--usage-heatmap-3", "color-mix(in oklab, #0b7fff 58%, var(--surface))", "color-mix(in oklab, #4099ff 62%, var(--surface))", "added", "--color-usage-heatmap-3"],
  ["--color-usage-heatmap-4", "--usage-heatmap-4", "color-mix(in oklab, #0066dd 82%, var(--surface))", "color-mix(in oklab, #80beff 78%, var(--surface))", "added", "--color-usage-heatmap-4"],
  ["--color-tab", "--tab", "#f0f0f0", "#202020", "added", "--color-tab"],
  ["--color-tab-active", "--tab-active", "#ffffff", "#161616", "added", "--color-tab-active"],
  ["--color-tab-border", "--tab-border", "var(--border)", "var(--border)", "added", "--color-tab-border"],
  ["--color-menu", "--menu", "#ffffff", "#2b2b2b", "ported", "--color-menu"],
  ["--color-menu-hover", "--menu-hover", "#f0f0f0", "#363636", "ported", "--color-menu-hover"],
  ["--color-primary", "--primary", "#000000", "#ffffff", "ported", "--color-primary"],
  ["--color-primary-foreground", "--primary-foreground", "#ffffff", "#000000", "ported", "--color-primary-foreground"],
  ["--color-secondary", "--secondary", "#e6e6e6", "#363636", "ported", "--color-secondary"],
  ["--color-interaction-ask-surface", "--interaction-ask-surface", "#ebf4ff", "#001d3d", "added", "--color-interaction-ask-surface"],
  ["--color-interaction-ask-foreground", "--interaction-ask-foreground", "#0066dd", "#80beff", "added", "--color-interaction-ask-foreground"],
  ["--color-interaction-ask-fill", "--interaction-ask-fill", "rgba(70, 191, 114, 0.2)", "rgba(70, 191, 114, 0.24)", "added", "--color-interaction-ask-fill"],
  ["--color-interaction-confirmation-surface", "--interaction-confirmation-surface", "#eaf7ee", "rgba(70, 191, 114, 0.16)", "ported", "--color-interaction-confirmation-surface"],
  ["--color-interaction-confirmation-foreground", "--interaction-confirmation-foreground", "#166b32", "#87d9a4", "ported", "--color-interaction-confirmation-foreground"],
  ["--color-success-surface", "--success-surface", "#eaf7ee", "rgba(70, 191, 114, 0.16)", "added", "--color-success-surface"],
  ["--color-success-surface-foreground", "--success-surface-foreground", "#166b32", "#87d9a4", "added", "--color-success-surface-foreground"],
  ["--color-progress-surface", "--progress-surface", "var(--color-blue-50)", "rgba(70, 130, 240, 0.16)", "added", "--color-progress-surface"],
  ["--color-progress-surface-foreground", "--progress-surface-foreground", "var(--color-blue-700)", "var(--color-blue-300)", "added", "--color-progress-surface-foreground"],
  ["--color-foreground", "--foreground", "var(--color-neutral-800)", "var(--color-neutral-300)", "ported", "--color-foreground"],
  ["--color-foreground-subtle", "--foreground-subtle", "color-mix(in oklab, var(--color-neutral-800) 60%, transparent)", "color-mix(in oklab, var(--color-neutral-300) 60%, transparent)", "ported", "--color-foreground-subtle"],
  ["--color-foreground-subtlest", "--foreground-subtlest", "color-mix(in oklab, var(--color-neutral-800) 40%, transparent)", "color-mix(in oklab, var(--color-neutral-300) 30%, transparent)", "ported", "--color-foreground-subtlest"],
  ["--lucide-subtlest-opacity", "--lucide-subtlest-opacity", "0.4", "0.3", "added", null],
  ["--color-foreground-inverse", "--foreground-inverse", "#ffffff", "#000000", "ported", "--color-foreground-inverse"],
  ["--color-success", "--success", "#1e8a3e", "#46bf72", "ported", "--color-success"],
  ["--color-success-foreground", "--success-foreground", "#ffffff", "#000000", "ported", "--color-success-foreground"],
  ["--color-idle-task", "--idle-task", "#9e77ed", "#7b5ce5", "added", "--color-idle-task"],
  ["--color-idle-task-surface", "--idle-task-surface", "#f5f3ff", "#160d38", "added", "--color-idle-task-surface"],
  ["--color-destructive", "--destructive", "#e03131", "#ff5c5c", "ported", "--color-destructive"],
  ["--color-destructive-foreground", "--destructive-foreground", "#ffffff", "#ffffff", "ported", "--color-destructive-foreground"],
  ["--color-warning", "--warning", "#e07b00", "#ff8a30", "ported", "--color-warning"],
  ["--color-warning-foreground", "--warning-foreground", "#ffffff", "#000000", "ported", "--color-warning-foreground"],
  ["--color-warning-surface", "--warning-surface", "color-mix(in srgb, var(--warning) 12%, transparent)", "color-mix(in srgb, var(--warning) 16%, transparent)", "added", "--color-warning-surface"],
  ["--color-warning-surface-foreground", "--warning-surface-foreground", "#914e00", "#ffa159", "added", "--color-warning-surface-foreground"],
  ["--color-destructive-surface", "--destructive-surface", "color-mix(in srgb, var(--destructive) 10%, transparent)", "color-mix(in srgb, var(--destructive) 16%, transparent)", "added", "--color-destructive-surface"],
  ["--color-destructive-surface-foreground", "--destructive-surface-foreground", "#b42020", "#ff8585", "added", "--color-destructive-surface-foreground"],
  ["--color-diff-added", "--diff-added", "#1e8a3e", "#46bf72", "ported", "--color-diff-added"],
  ["--color-diff-added-foreground", "--diff-added-foreground", "#ffffff", "#000000", "ported", "--color-diff-added-foreground"],
  ["--color-diff-removed", "--diff-removed", "#e03131", "#ff5c5c", "ported", "--color-diff-removed"],
  ["--color-diff-removed-foreground", "--diff-removed-foreground", "#ffffff", "#000000", "ported", "--color-diff-removed-foreground"],
  ["--color-git-none", "--git-none", "var(--foreground)", "var(--foreground)", "alias", "--color-git-none"],
  ["--color-git-modified", "--git-modified", "#e07b00", "#ff8a30", "ported", "--color-git-modified"],
  ["--color-git-added", "--git-added", "#1e8a3e", "#46bf72", "ported", "--color-git-added"],
  ["--color-git-deleted", "--git-deleted", "#e03131", "#ff5c5c", "ported", "--color-git-deleted"],
  ["--color-git-renamed", "--git-renamed", "#0b7fff", "#4099ff", "ported", "--color-git-renamed"],
  ["--color-git-untracked", "--git-untracked", "#1e8a3e", "#46bf72", "ported", "--color-git-untracked"],
  ["--color-git-ignored", "--git-ignored", "var(--foreground-subtlest)", "var(--foreground-subtlest)", "alias", "--color-git-ignored"],
  ["--color-git-descendant", "--git-descendant", "#e07b00", "#ff8a30", "ported", "--color-git-descendant"],
  ["--color-toast", "--toast", "#ffffff", "#2b2b2b", "ported", "--color-toast"],
  ["--color-tooltip", "--tooltip", "#f0f0f0", "#2b2b2b", "ported", "--color-tooltip"],
  ["--color-tooltip-foreground", "--tooltip-foreground", "#0d0d0d", "#f8f8f8", "ported", "--color-tooltip-foreground"],
  ["--color-tooltip-tag", "--tooltip-tag", "#e6e6e6", "#363636", "ported", "--color-tooltip-tag"],
  ["--color-tooltip-tag-foreground", "--tooltip-tag-foreground", "#5c5c5c", "#adadad", "ported", "--color-tooltip-tag-foreground"],
  ["--color-tag", "--tag", "#e6e6e6", "#363636", "ported", "--color-tag"],
];

export const ZCODE_TOKENS: readonly ZcodeToken[] = ROWS.map(
  ([zcode, smind, light, dark, status, theme]) => ({ zcode, smind, light, dark, status, theme }),
);

/** Interface type scale (`@theme inline`), formulas off `--ui-font-size`. */
export const ZCODE_UI_FONT_TOKENS: Readonly<Record<string, string>> = {
  "--text-ui-xl": "calc(var(--ui-font-size) + 4px)",
  "--text-ui-lg": "calc(var(--ui-font-size) + 2px)",
  "--text-ui-base": "var(--ui-font-size)",
  "--text-ui-caption": "calc(var(--ui-font-size) - 1px)",
  "--text-ui-sm": "calc(var(--ui-font-size) - 2px)",
  "--text-ui-xs": "calc(var(--ui-font-size) - 4px)",
  "--text-ui-settings-title": "calc(var(--ui-font-size) + 10px)",
  "--text-ui-settings-title-lg": "calc(var(--ui-font-size) + 16px)",
};
