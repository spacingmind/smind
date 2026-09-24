import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

// Mirrors token-presence.test.ts's shape: parse index.css and assert on its
// contents rather than rendering anything.

const INDEX_CSS = readFileSync(
  join(dirname(fileURLToPath(import.meta.url)), "..", "index.css"),
  "utf-8",
);

const FIXTURE: Record<string, { light: string; dark: string } | string> = JSON.parse(
  readFileSync(join(dirname(fileURLToPath(import.meta.url)), "zcode-tokens.fixture.json"), "utf-8"),
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

/** Every `--name: value;` declaration in a block, last declaration wins (standard CSS). */
function declarations(cssBlock: string): Record<string, string> {
  const map: Record<string, string> = {};
  const re = /(--[a-zA-Z0-9-]+)\s*:\s*([^;]+);/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(cssBlock))) {
    map[m[1]] = m[2].trim();
  }
  return map;
}

/**
 * Resolves a token's value by following `var(--x)` chains through `map`
 * until it hits a non-var value, or a `var(--x)` whose target isn't in
 * `map` at all (a Tailwind-global default like `--color-neutral-800`,
 * which neither smind's nor ZCode's own file declares) -- that var()
 * reference itself is the terminal value in that case, matching how
 * `zcode-tokens.fixture.json` records it.
 */
function resolve(map: Record<string, string>, name: string, seen = new Set<string>()): string | undefined {
  if (seen.has(name)) throw new Error(`circular token reference: ${[...seen, name].join(" -> ")}`);
  seen.add(name);
  const value = map[name];
  if (value === undefined) return undefined;
  const varMatch = /^var\((--[a-zA-Z0-9-]+)\)$/.exec(value);
  if (varMatch && map[varMatch[1]] !== undefined) {
    return resolve(map, varMatch[1], seen);
  }
  return value;
}

describe("ZCode token fixture parity (zcode-visual-parity plan, P1)", () => {
  const themeInline = declarations(block("@theme inline"));
  const root = declarations(block(":root"));
  const dark = declarations(block(".dark"));

  const lightMap = { ...themeInline, ...root };
  const darkMap = { ...themeInline, ...root, ...dark };

  const tokens = Object.keys(FIXTURE).filter((k) => !k.startsWith("$"));

  it.each(tokens)("%s resolves in :root (light)", (token) => {
    const expected = (FIXTURE[token] as { light: string; dark: string }).light;
    expect(resolve(lightMap, token)).toBe(expected);
  });

  it.each(tokens)("%s resolves in .dark", (token) => {
    const expected = (FIXTURE[token] as { light: string; dark: string }).dark;
    expect(resolve(darkMap, token)).toBe(expected);
  });
});
