import rows from "@/lib/zcode-tokens.json";

/**
 * Typed name map between ZCode design tokens and smind's `index.css`
 * (zcode-parity plan, Stage 1). ZCode is a visual spec: this table records
 * which ZCode token each smind raw var stands for and the value both themes
 * must resolve to; `zcode-tokens.test.ts` fails when `index.css` drifts.
 *
 * Rows live in `zcode-tokens.json` (hex literals stay out of .ts files, see
 * test/no-hardcoded-colors.test.ts): zcode name, smind raw var, light value
 * (`:root`), dark value (`.dark`), status, `@theme inline` export or null.
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

export const ZCODE_TOKENS: readonly ZcodeToken[] = rows as ZcodeToken[];

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
