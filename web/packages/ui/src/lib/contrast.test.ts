import { describe, expect, it } from "vitest";

import { composite, contrastRatio, parseColor, parseThemeMaps, toHex } from "@/lib/contrast";

const rgb = (v: string, vars = new Map<string, string>()) => parseColor(v, vars);

describe("contrast maths", () => {
  it("black on white is 21", () => {
    expect(contrastRatio(rgb("#000"), rgb("#ffffff"))).toBeCloseTo(21, 5);
  });

  it("#767676 on white is 4.54", () => {
    expect(contrastRatio(rgb("#767676"), rgb("#fff"))).toBeCloseTo(4.54, 2);
  });

  it("oklch(0.556 0 0) is #737373", () => {
    expect(toHex(rgb("oklch(0.556 0 0)"))).toBe("#737373");
  });

  it("parses rgba, oklch alpha and color-mix with transparent", () => {
    expect(rgb("rgba(13, 13, 13, 0.03)").a).toBeCloseTo(0.03);
    expect(rgb("oklch(0.5 0 0 / 40%)").a).toBeCloseTo(0.4);
    const c = rgb("color-mix(in srgb, var(--w) 12%, transparent)", new Map([["--w", "#e07b00"]]));
    expect(toHex(c)).toBe("#e07b00");
    expect(c.a).toBeCloseTo(0.12);
  });

  it("composites over an opaque surface", () => {
    expect(toHex(composite(rgb("rgba(0,0,0,0.5)"), rgb("#ffffff")))).toBe("#808080");
  });

  it("resolves var() chains and dark falls back to :root", () => {
    const { light, dark } = parseThemeMaps(
      ":root {\n  --a: #111111;\n  --b: var(--a);\n  --c: #222222;\n}\n.dark {\n  --a: #eeeeee;\n}\n",
    );
    expect(toHex(parseColor("var(--b)", light))).toBe("#111111");
    expect(toHex(parseColor("var(--b)", dark))).toBe("#eeeeee");
    expect(toHex(parseColor("var(--c)", dark))).toBe("#222222");
  });
});
