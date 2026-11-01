import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

import { parseThemeMaps } from "@/lib/contrast";

// The diff's `--syntax-*` tokens (index.css) must be readable on every
// row the highlighted code can sit on -- WCAG AA (4.5:1) against the
// context row (`--card`), the +/- row tints and their word-level
// highlights, and the "change" tints (diff2html variables, same
// color-mix percentages and `--diff-*-tint` sources as index.css's `--d2h-*`). Retune the tokens
// with this test, not by eye (git-github-ux G6).

const CSS = readFileSync(join(dirname(fileURLToPath(import.meta.url)), "..", "index.css"), "utf8");
const AA = 4.5;

type Rgb = [number, number, number];

/** The declarations of the first top-level `selector {` block that defines `--syntax-keyword`. */
function themeBlock(selector: ":root" | ".dark"): Record<string, string> {
  const re = new RegExp(`(?:^|\\n)${selector.replace(".", "\\.")}\\s*\\{`, "g");
  for (const m of CSS.matchAll(re)) {
    const start = m.index! + m[0].length;
    let depth = 1;
    let i = start;
    while (depth > 0 && i < CSS.length) {
      if (CSS[i] === "{") depth++;
      else if (CSS[i] === "}") depth--;
      i++;
    }
    const body = CSS.slice(start, i - 1);
    if (!body.includes("--syntax-keyword")) continue;
    const out: Record<string, string> = {};
    for (const d of body.matchAll(/(--[\w-]+)\s*:\s*([^;]+);/g)) out[d[1]!] = d[2]!.trim();
    return out;
  }
  throw new Error(`no ${selector} block defines --syntax-keyword`);
}

function hex(value: string): Rgb {
  const m = /^#([0-9a-f]{6})$/i.exec(value);
  if (!m) throw new Error(`expected a 6-digit hex color, got "${value}"`);
  const n = parseInt(m[1]!, 16);
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
}

const mix = (fg: Rgb, bg: Rgb, alpha: number): Rgb => fg.map((c, i) => c * alpha + bg[i]! * (1 - alpha)) as Rgb;

function luminance([r, g, b]: Rgb): number {
  const lin = (c: number) => ((c /= 255) <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4);
  return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b);
}

function contrast(a: Rgb, b: Rgb): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x) as [number, number];
  return (hi + 0.05) / (lo + 0.05);
}

describe.each([":root", ".dark"] as const)("diff syntax tokens (%s)", (selector) => {
  const vars = themeBlock(selector);
  // the tint sources live in the app-internal :root/.dark block, not the one that defines --syntax-*
  const tints = parseThemeMaps(CSS)[selector === ":root" ? "light" : "dark"];
  const card = hex(vars["--card"]!);
  const added = hex(tints.get("--diff-added-tint")!);
  const removed = hex(tints.get("--diff-removed-tint")!);
  const warning = hex(vars["--warning"]!);
  const backgrounds: Record<string, Rgb> = {
    context: card,
    "added row": mix(added, card, 0.15),
    "added change row": mix(added, card, 0.2),
    "added word highlight": mix(added, card, 0.3),
    "removed row": mix(removed, card, 0.12),
    "removed change row": mix(warning, card, 0.2),
    "removed word highlight": mix(removed, card, 0.3),
  };
  const tokens = Object.entries(vars).filter(([name]) => name.startsWith("--syntax-"));

  it("defines the full token set", () => {
    expect(tokens.map(([name]) => name).sort()).toEqual(
      [
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
      ].sort(),
    );
  });

  it.each(tokens)("%s is AA on every diff row background", (name, value) => {
    const fg = hex(value);
    for (const [label, bg] of Object.entries(backgrounds)) {
      expect(contrast(fg, bg), `${name} on ${label}`).toBeGreaterThanOrEqual(AA);
    }
  });
});

describe("diff row fills", () => {
  it("mix the tint tokens this test measures, never the AA text tokens (--diff-added / --diff-removed)", () => {
    expect(CSS.match(/color-mix\([^)]*var\(--diff-(?:added|removed)\)/g) ?? []).toEqual([]);
    for (const name of ["ins-bg", "ins-border", "ins-highlight-bg", "del-bg", "del-border", "del-highlight-bg", "change-ins"]) {
      expect(CSS, `--d2h-${name}`).toMatch(new RegExp(`--d2h-${name}-color: color-mix\\(in oklch, var\\(--diff-(?:added|removed)-tint\\)`));
    }
  });
});
