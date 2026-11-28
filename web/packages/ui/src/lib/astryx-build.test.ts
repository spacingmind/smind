import { readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";

const uiRoot = path.resolve(__dirname, "../..");
const LAYER_LINE =
  "@layer reset, theme, base, astryx-base, astryx-theme, components, utilities;";

describe("astryx build wiring", () => {
  it("astryx_deps_are_exactly_pinned", () => {
    const pkg = JSON.parse(readFileSync(path.join(uiRoot, "package.json"), "utf8"));
    const all: Record<string, string> = { ...pkg.dependencies, ...pkg.devDependencies };
    const names = Object.keys(all).filter(
      (n) => n.startsWith("@astryxdesign/") || n === "@stylexjs/stylex",
    );
    expect(names.length).toBeGreaterThan(0);
    for (const n of names) expect(all[n], n).toMatch(/^\d+\.\d+\.\d+$/);
  });

  it("astryx_layer_order_is_declared_first", () => {
    const css = readFileSync(path.join(uiRoot, "src/index.css"), "utf8");
    const stripped = css.replace(/\/\*[\s\S]*?\*\//g, "");
    const first = stripped.split("\n").map((l) => l.trim()).find((l) => l !== "");
    expect(first).toBe(LAYER_LINE);
    expect(stripped.indexOf(LAYER_LINE)).toBeLessThan(stripped.indexOf("@import"));
  });
});
