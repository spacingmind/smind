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

  it("astryx_layer_order_is_declared_in_index_html_before_any_stylesheet", () => {
    // Astryx injects its own `@layer reset, astryx-base, astryx-theme;` at runtime; the first declaration of a
    // layer name wins, so the full order must be the first style in <head> (ADR-0064, found in the real app).
    const html = readFileSync(path.join(uiRoot, "index.html"), "utf8").replace(/<!--[\s\S]*?-->/g, "");
    const head = html.slice(html.indexOf("<head>"), html.indexOf("</head>"));
    const firstStyle = head.indexOf("<style>");
    expect(firstStyle).toBeGreaterThan(-1);
    expect(head.slice(firstStyle)).toContain(LAYER_LINE);
    expect(head.indexOf(LAYER_LINE)).toBeLessThan(head.indexOf("<script"));
    expect(head.indexOf(LAYER_LINE)).toBeLessThan(head.indexOf("<link"));
  });
});
