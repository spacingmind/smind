import { readdirSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

// See index-html-bootstrap.test.ts's comment: `new URL("..", import.meta.url)`
// is a syntactic shape Vite special-cases (asset-URL rewriting), which
// misresolves under vitest's jsdom environment -- join a plain path instead.
const SRC_DIR = join(dirname(fileURLToPath(import.meta.url)), "..");

// Tailwind's palette-color utilities (bg-red-500, text-emerald-600, ...) --
// the exact kind of ad hoc color the status/status-dot tokens exist to
// replace (see docs/design.md §1's rule and index.css's Item 1 addition).
const PALETTE_COLOR_UTILITY =
  /\b(?:bg|text|border|fill|stroke|ring|from|to|via|decoration|outline|caret|accent)-(red|green|blue|yellow|amber|emerald|zinc|slate|gray|neutral|orange|purple|violet|sky|teal|cyan|rose|pink|indigo|lime|fuchsia)-\d{2,3}\b/;

const HEX_OR_OKLCH_COLOR = /#[0-9a-fA-F]{3,8}\b|oklch\(/;

function listFiles(dir: string): string[] {
  const out: string[] = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    if (entry.name === "node_modules" || entry.name.startsWith(".")) continue;
    const full = join(dir, entry.name);
    if (entry.isDirectory()) {
      out.push(...listFiles(full));
    } else if (/\.(tsx?|css)$/.test(entry.name)) {
      out.push(full);
    }
  }
  return out;
}

/**
 * Item 1's acceptance criterion: no component hardcodes a color outside
 * index.css. A grep-based test, per the plan's own suggestion -- cheap,
 * and it's exactly the kind of regression a code reviewer's eye
 * eventually misses on a large diff (this test itself caught several
 * pre-existing instances -- amber/emerald banners and dots -- while
 * landing Item 1; see docs/design.md's Decisions).
 */
describe("no component hardcodes a color outside index.css", () => {
  const files = listFiles(SRC_DIR).filter((f) => !f.endsWith("index.css") && !f.endsWith(".test.ts") && !f.endsWith(".test.tsx"));

  it("no .ts/.tsx file uses a Tailwind palette-color utility class", () => {
    const offenders = files
      .filter((f) => /\.tsx?$/.test(f))
      .filter((f) => PALETTE_COLOR_UTILITY.test(readFileSync(f, "utf-8")));
    expect(offenders).toEqual([]);
  });

  it("no .ts/.tsx file hardcodes a hex or oklch color literal", () => {
    const offenders = files
      .filter((f) => /\.tsx?$/.test(f))
      .filter((f) => HEX_OR_OKLCH_COLOR.test(readFileSync(f, "utf-8")));
    expect(offenders).toEqual([]);
  });
});
