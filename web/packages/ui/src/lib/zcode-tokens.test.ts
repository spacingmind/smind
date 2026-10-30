import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

import { ZCODE_TOKENS, ZCODE_UI_FONT_TOKENS } from "@/lib/zcode-tokens";

const css = readFileSync("src/index.css", "utf8");

const squash = (v: string) => v.replace(/\s+/g, " ").trim();

function declarationsIn(body: string): Map<string, string> {
  const out = new Map<string, string>();
  for (const d of body.matchAll(/^\s*(--[\w-]+):\s*([^;]+);/gm)) {
    out.set(d[1], squash(d[2]));
  }
  return out;
}

/** Merge every top-level `selector { ... }` block's `--var: value;` pairs. */
function declarations(selector: string): Map<string, string> {
  const out = new Map<string, string>();
  const blocks = css.matchAll(new RegExp(`^${selector} \\{\\n([\\s\\S]*?)\\n\\}`, "gm"));
  for (const block of blocks) {
    for (const [k, v] of declarationsIn(block[1])) out.set(k, v);
  }
  return out;
}

const themeInline = declarationsIn(/@theme inline \{\n([\s\S]*?)\n\}/.exec(css)?.[1] ?? "");
const light = declarations(":root");
const dark = declarations("\\.dark");

describe("zcode token drift", () => {
  it("every light value in the table matches index.css :root", () => {
    for (const t of ZCODE_TOKENS) {
      if (t.light === null) continue;
      expect(light.get(t.smind), `${t.smind} (light)`).toBe(t.light);
    }
  });

  it("every dark value in the table matches index.css .dark", () => {
    for (const t of ZCODE_TOKENS) {
      if (t.dark === null) continue;
      expect(dark.get(t.smind), `${t.smind} (dark)`).toBe(t.dark);
    }
  });

  it("every colour row is exported in @theme inline", () => {
    for (const t of ZCODE_TOKENS) {
      if (t.theme === null) continue;
      expect(themeInline.get(t.theme), `${t.theme} export`).toBe(`var(${t.smind})`);
    }
  });

  it("the interface type scale matches ZCode's formulas", () => {
    for (const [name, formula] of Object.entries(ZCODE_UI_FONT_TOKENS)) {
      expect(themeInline.get(name), name).toBe(formula);
    }
  });

  it("table has no duplicate zcode or smind names", () => {
    const zcode = ZCODE_TOKENS.map((t) => t.zcode);
    const smind = ZCODE_TOKENS.map((t) => t.smind);
    expect(new Set(zcode).size).toBe(zcode.length);
    expect(new Set(smind).size).toBe(smind.length);
  });
});
