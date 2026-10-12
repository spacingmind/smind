import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

// jsdom has no CSS cascade, so the D2 chrome rules (index.css's
// html[data-desktop-os] block) can't be asserted via getComputedStyle --
// these are source guards over index.css instead, the same pattern
// no-hardcoded-colors.test.ts / text-ui-scale.test.ts already use.

const CSS = readFileSync(join(dirname(fileURLToPath(import.meta.url)), "..", "index.css"), "utf-8");

/** Strips a leading block comment and collapses whitespace, so assertions don't fight indentation. */
function stripRule(rule: string): string {
  return rule.replace(/^[\s\S]*?\*\/\s*/, "").replace(/\s+/g, " ").trim();
}

/** Every rule gated on html[data-desktop-os] (any OS value), comment-stripped and whitespace-normalized. */
const desktopRules = (CSS.match(/^[^{}]*html\[data-desktop-os[^\]]*\][^{}]*\{[^}]*\}/gm) ?? []).map(stripRule);

/** The D2 @layer base block (the one containing data-desktop-os), not index.css's earlier Tailwind one. */
const desktopBaseLayer = (CSS.match(/@layer base \{[\s\S]*?\n\}/g) ?? []).find((b) => b.includes("data-desktop-os")) ?? "";

/** CSS with every desktop-gated rule removed -- what the browser build sees. */
const BROWSER_CSS = CSS.replace(/^[^{}]*html\[data-desktop-os[^\]]*\][^{}]*\{[^}]*\}/gm, "");

describe("chrome-not-selectable (D2.3, desktop build only)", () => {
  it("the root rule sets user-select: none under html[data-desktop-os], inside @layer base", () => {
    // The root rule is a desktop-gated rule ending in exactly this body,
    // and it lives inside the D2 @layer base block (so Tailwind's explicit
    // select-text/select-none utilities still win over it).
    expect(desktopRules.some((r) => r.endsWith("html[data-desktop-os] { user-select: none; }"))).toBe(true);
    expect(desktopBaseLayer).toContain("html[data-desktop-os] {");
  });

  it("conversation text, code blocks, the file editor and diffs stay selectable; the terminal does not (xterm selects itself)", () => {
    const reenable = desktopRules.find((r) => /user-select: text/.test(r)) ?? "";
    for (const surface of [
      "input",
      "textarea",
      "[contenteditable]",
      "pre",
      "code",
      '[data-testid="run-log-column"]',
      ".d2h-wrapper",
    ]) {
      expect(reenable).toContain(surface);
    }
    expect(reenable).not.toContain(".xterm");
  });

  it("controls inside the timeline column stay unselectable", () => {
    const rule = desktopRules.find((r) =>
      r.includes('[data-testid="run-log-column"] :is(button, [role="button"], [role="tab"], [role="menuitem"]) { user-select: none; }'),
    );
    expect(rule).toBeTruthy();
  });

  it("no user-select rule exists outside the desktop gate (browser build unchanged)", () => {
    expect(BROWSER_CSS).not.toMatch(/user-select/);
  });
});

describe("desktop overscroll / cursor; global scrollbars (D2.4, D2.6, D8)", () => {
  it("overscroll-behavior: none applies to the document root only", () => {
    const rule = desktopRules.find((r) => /overscroll-behavior: none/.test(r)) ?? "";
    expect(rule).toContain("html[data-desktop-os],");
    expect(rule).toContain("html[data-desktop-os] body");
    expect(rule).not.toContain("*");
    expect(BROWSER_CSS).not.toMatch(/overscroll-behavior/);
  });

  it("buttons, tabs and menu items get cursor: default in @layer base; the cursor-pointer utility override sits outside it", () => {
    const rule = desktopRules.find((r) => /cursor: default/.test(r) && r.includes("button")) ?? "";
    for (const surface of ['button', '[role="tab"]', '[role="menuitem"]', '[role="button"]']) {
      expect(rule).toContain(surface);
    }
    // Utilities must beat these -- so the base ones live in @layer base...
    expect(desktopBaseLayer.replace(/\s+/g, " ")).toContain(rule);
    // ...except the deliberate .cursor-pointer override, which must NOT.
    const override = desktopRules.find((r) => r.startsWith("html[data-desktop-os] .cursor-pointer")) ?? "";
    expect(override).toBe("html[data-desktop-os] .cursor-pointer { cursor: default; }");
    expect(desktopBaseLayer).not.toContain(".cursor-pointer");
    expect(BROWSER_CSS).not.toMatch(/cursor: default/);
  });

  it("global themed scrollbars are not desktop-gated (D8/SFR-S-AC1)", () => {
    // Ungated ::-webkit-scrollbar rules -- the browser build sees them too.
    const global = (CSS.match(/^[^{}]*::-webkit-scrollbar[^{}]*\{[^}]*\}/gm) ?? []).map(stripRule);
    const find = (pseudo: string) => global.find((r) => r.startsWith(`${pseudo} {`)) ?? "";
    expect(find("::-webkit-scrollbar")).toBe("::-webkit-scrollbar { width: 8px; height: 8px; }");
    expect(find("::-webkit-scrollbar-track")).toContain("background: transparent");
    const thumb = find("::-webkit-scrollbar-thumb");
    expect(thumb).toContain("var(--color-foreground)");
    expect(thumb).toContain("border-radius");
    expect(thumb).toContain("border: 2px solid transparent");
    expect(find("::-webkit-scrollbar-corner")).toContain("background: transparent");
    // Hover-brighter thumb tiers.
    expect(global.some((r) => r.includes(":hover::-webkit-scrollbar-thumb {"))).toBe(true);
    expect(global.some((r) => r.includes("::-webkit-scrollbar-thumb:hover {"))).toBe(true);
  });

  it("no scrollbar-width/scrollbar-color outside the @supports Firefox fallback and the xterm rule (SFR-S-AC2)", () => {
    // Chromium ignores ::-webkit-scrollbar* wherever these are non-auto.
    const withoutSupports = CSS.replace(/@supports not selector\(::-webkit-scrollbar\) \{[^}]*\{[^}]*\}[^}]*\}/g, "");
    const standard = withoutSupports.match(/[^{}]*\{[^{}]*scrollbar-(?:width|color)[^{}]*\}/g) ?? [];
    expect(standard.map((r) => r.replace(/\s+/g, " ").trim().replace(/^\/\*[\s\S]*?\*\/ /, ""))).toEqual([
      ".terminal-xterm-shell .xterm-viewport { scrollbar-width: none; }",
    ]);
    // The fallback itself is Firefox-only and uses the same foreground token.
    const fallback = CSS.match(/@supports not selector\(::-webkit-scrollbar\) \{[\s\S]*?\n\}/)?.[0] ?? "";
    expect(fallback).toContain("scrollbar-width: thin");
    expect(fallback).toContain("var(--color-foreground)");
    expect(fallback).toContain("transparent;");
  });

  it("index.css hides scrollbar buttons globally (SFR-S-AC1)", () => {
    const rule = stripRule(CSS.match(/^[^{}]*::-webkit-scrollbar-button[^{}]*\{[^}]*\}/gm)?.[0] ?? "");
    expect(rule).toBe("::-webkit-scrollbar-button { display: none; width: 0; height: 0; }");
  });

  it("the xterm scrollbar rules are untouched (still scrollbar-width: none)", () => {
    expect(CSS).toMatch(/\.terminal-xterm-shell \.xterm-viewport \{\s*scrollbar-width: none/);
  });
});
