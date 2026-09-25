import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

// Mirrors no-hardcoded-colors.test.ts's shape: parse index.css and assert
// on its contents rather than rendering anything.

const INDEX_CSS = readFileSync(
  join(dirname(fileURLToPath(import.meta.url)), "..", "index.css"),
  "utf-8",
);

/** The `{selector} { ... }` block (first occurrence), brace-balanced. */
function block(selector: string): string {
  const start = INDEX_CSS.indexOf(`${selector} {`);
  expect(start, `${selector} block not found in index.css`).toBeGreaterThan(-1);
  let depth = 0;
  let end = -1;
  for (let i = start; i < INDEX_CSS.length; i++) {
    if (INDEX_CSS[i] === "{") depth++;
    if (INDEX_CSS[i] === "}") {
      depth--;
      if (depth === 0) {
        end = i;
        break;
      }
    }
  }
  return INDEX_CSS.slice(start, end);
}

/**
 * Every token the visual-identity-console plan's Items 3 and 5 add, plus
 * `--content-size` (Item 2's one surviving type-role token -- the rest were
 * retired by zcode-visual-parity's P1 Step 4 in favor of `text-ui-*`, see
 * text-ui-scale.test.ts). Values may be identical across themes, but the
 * declaration must exist in BOTH `:root` and `.dark` -- a token defined
 * only in one theme makes the other theme silently fall back to whatever
 * inherits, which is exactly the drift this test exists to catch.
 *
 * For the shadow/duration/content tokens the convention is two spellings:
 * a `--{name}` custom property (value, per theme) declared in `:root`/
 * `.dark`, plus a `--text-{name}`/`--shadow-{tier}` alias in `@theme
 * inline` that turns it into a Tailwind utility. The tests below assert
 * both halves.
 */
const TOKENS = [
  "--content-size",
  "--elevation-shadow-sm",
  "--elevation-shadow-md",
  "--elevation-shadow-lg",
  "--duration-hover",
  "--duration-menu",
  "--duration-panel",
  "--diff-addition",
  "--diff-deletion",
] as const;

const THEME_ALIASES = [
  "--text-content",
  "--shadow-sm",
  "--shadow-md",
  "--shadow-lg",
  "--color-diff-addition",
  "--color-diff-deletion",
] as const;

describe("token presence (visual-identity-console Items 2, 3, 5)", () => {
  const root = block(":root");
  const dark = block(".dark");
  const theme = block("@theme inline");

  it.each(TOKENS)("declares %s in :root", (token) => {
    expect(root).toContain(`${token}:`);
  });

  it.each(TOKENS)("declares %s in .dark", (token) => {
    expect(dark).toContain(`${token}:`);
  });

  it.each(THEME_ALIASES)("re-exports %s via @theme inline", (alias) => {
    expect(theme).toContain(`${alias}:`);
  });

  it("re-exports --text-content with a paired line-height", () => {
    expect(theme).toContain("--text-content--line-height:");
  });
});
