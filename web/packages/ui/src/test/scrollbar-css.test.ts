import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

// Source guards over index.css, the same pattern as
// desktop-webview-css.test.ts: jsdom has no CSS cascade, so the global
// scrollbar rules (sidebar-footer-resize-scrollbar plan, D8, SFR-S-AC1..3)
// can only be asserted against the stylesheet text.

const CSS = readFileSync(join(dirname(fileURLToPath(import.meta.url)), "..", "index.css"), "utf-8");

function stripRule(rule: string): string {
  return rule.replace(/^[\s\S]*?\*\/\s*/, "").replace(/\s+/g, " ").trim();
}

/** Ungated ::-webkit-scrollbar rules (not behind html[data-desktop-os]). */
const globalScrollbarRules = (CSS.match(/^[^{}]*::-webkit-scrollbar[^{}]*\{[^}]*\}/gm) ?? []).map(stripRule);

describe("global themed scrollbar (SFR-S-AC1)", () => {
  it("global ::-webkit-scrollbar rules exist and are not desktop-gated", () => {
    expect(globalScrollbarRules.length).toBeGreaterThan(0);
    for (const rule of globalScrollbarRules) {
      expect(rule).not.toContain("data-desktop-os");
    }
    const bar = globalScrollbarRules.find((r) => r.startsWith("::-webkit-scrollbar {")) ?? "";
    expect(bar).toBe("::-webkit-scrollbar { width: 8px; height: 8px; }");
    expect(globalScrollbarRules.find((r) => r.startsWith("::-webkit-scrollbar-track"))).toContain(
      "background: transparent",
    );
    const thumb = globalScrollbarRules.find((r) => r.startsWith("::-webkit-scrollbar-thumb {")) ?? "";
    // Thumb colour comes from a theme token via color-mix -- no hex, no bare
    // palette colour -- so it themes in light and dark.
    expect(thumb).toContain("color-mix(in srgb, var(--color-foreground) 18%, transparent)");
    expect(thumb).not.toMatch(/#[0-9a-fA-F]{3,8}\b/);
    expect(thumb).toContain("border: 2px solid transparent");
    expect(thumb).toContain("background-clip: content-box");
    expect(thumb).toContain("border-radius: 9999px");
    expect(thumb).toContain("min-height: 32px");
    // Hover tiers: brighter when the scroller is hovered, brightest on the thumb.
    expect(globalScrollbarRules.find((r) => r.startsWith(":hover::-webkit-scrollbar-thumb"))).toContain(
      "var(--color-foreground) 32%",
    );
    expect(globalScrollbarRules.find((r) => r.startsWith("::-webkit-scrollbar-thumb:hover"))).toContain(
      "var(--color-foreground) 50%",
    );
    // Arrow buttons hidden, corner transparent.
    expect(globalScrollbarRules.find((r) => r.startsWith("::-webkit-scrollbar-button"))).toBe(
      "::-webkit-scrollbar-button { display: none; width: 0; height: 0; }",
    );
    expect(globalScrollbarRules.find((r) => r.startsWith("::-webkit-scrollbar-corner"))).toContain(
      "background: transparent",
    );
  });
});

describe("standard scrollbar properties contained (SFR-S-AC2)", () => {
  it("no scrollbar-width/scrollbar-color outside the @supports fallback and the xterm rule", () => {
    const withoutFallback = CSS.replace(
      /@supports not selector\(::-webkit-scrollbar\) \{[\s\S]*?\n\}/g,
      "",
    );
    const standard = withoutFallback.match(/[^{}]*\{[^{}]*scrollbar-(?:width|color)[^{}]*\}/g) ?? [];
    expect(standard.map((r) => r.replace(/\s+/g, " ").trim().replace(/^\/\*[\s\S]*?\*\/ /, ""))).toEqual([
      ".terminal-xterm-shell .xterm-viewport { scrollbar-width: none; }",
    ]);
    // The Firefox fallback itself is guarded and uses the same token.
    expect(CSS).toMatch(
      /@supports not selector\(::-webkit-scrollbar\) \{\s*\*\s*\{\s*scrollbar-width: thin;/,
    );
    expect(CSS).toMatch(/@supports[^{]*\{[^@]*?color-mix\(in srgb, var\(--color-foreground\) 28%, transparent\) transparent/);
  });
});

describe("xterm and .no-scrollbar still win (SFR-S-AC3)", () => {
  it("the xterm viewport scrollbar stays hidden and is more specific than the global rules", () => {
    expect(CSS).toMatch(/\.terminal-xterm-shell \.xterm-viewport \{\s*scrollbar-width: none/);
    expect(CSS).toMatch(
      /\.terminal-xterm-shell \.xterm-viewport::-webkit-scrollbar \{\s*display: none;/,
    );
    // .no-scrollbar is a Tailwind @utility in shadcn/tailwind.css; a class
    // selector beats these bare pseudo-element rules, and the utility sets
    // scrollbar-width: none -- verify it still exists.
    // Resolved exactly like index.css's `@import "shadcn/tailwind.css"`, so it
    // does not depend on where the package manager hoists node_modules.
    const shadcnCss = readFileSync(createRequire(import.meta.url).resolve("shadcn/tailwind.css"), "utf-8");
    expect(shadcnCss).toMatch(/@utility no-scrollbar[\s\S]*?scrollbar-width: none/);
    expect(shadcnCss).toMatch(/@utility no-scrollbar[\s\S]*?&::-webkit-scrollbar \{\s*display: none;/);
  });
});
