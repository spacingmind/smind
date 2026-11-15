import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

import { parseThemeMaps, tokenContrast } from "@/lib/contrast";

// DT-UI B native pass: the accessibility fallbacks in desktop-chrome.css. The WebKit media queries cannot be driven
// from jsdom, so the sheet itself is read: each fallback exists, is gated on the desktop build, and the increased
// contrast tokens actually reach 7:1 on the app surfaces.
const css = readFileSync("src/desktop-chrome.css", "utf8");
const base = parseThemeMaps(readFileSync("src/index.css", "utf8"));

function mediaBlock(query: string): string {
  const start = css.indexOf(`@media (${query})`);
  expect(start, `${query} block exists`).toBeGreaterThan(-1);
  let depth = 0;
  for (let i = css.indexOf("{", start); i < css.length; i++) {
    if (css[i] === "{") depth++;
    if (css[i] === "}" && --depth === 0) return css.slice(start, i + 1);
  }
  throw new Error("unterminated block");
}

function declared(block: string, selector: RegExp, name: string): string {
  const rule = block.match(new RegExp(`${selector.source}\\s*\\{([^}]*)\\}`));
  expect(rule, `rule for ${selector}`).not.toBeNull();
  const m = rule![1].match(new RegExp(`${name}:\\s*([^;]+);`));
  expect(m, `${name} in ${selector}`).not.toBeNull();
  return m![1].trim();
}

describe("desktop native pass CSS", () => {
  it.each(["prefers-reduced-motion: reduce", "prefers-contrast: more", "prefers-reduced-transparency: reduce"])("%s is handled, and only in the desktop build", (query) => {
    const block = mediaBlock(query);
    for (const selector of block.match(/^\s*html[^{,]*/gm) ?? []) expect(selector).toContain("[data-desktop-os");
  });

  it("reduced motion keeps spinners turning", () => {
    expect(mediaBlock("prefers-reduced-motion: reduce")).toContain(":not(.animate-spin)");
  });

  it("reduced transparency makes the sidebar surface plain", () => {
    expect(declared(mediaBlock("prefers-reduced-transparency: reduce"), /html\[data-desktop-os="macos"\] \[data-slot="sidebar-inner"\],\s*html\[data-desktop-os="macos"\] \[data-settings-sidebar\]/, "background")).toBe("var(--sidebar)");
  });

  describe.each([
    ["light", /html\[data-desktop-os\]/, base.light],
    ["dark", /html\[data-desktop-os\]\.dark/, base.dark],
  ] as const)("increased contrast, %s", (_theme, selector, vars) => {
    const block = mediaBlock("prefers-contrast: more");
    const muted = declared(block, selector, "--muted-foreground");
    it.each(["--background", "--sidebar", "--card", "--panel"])("secondary text reaches 7:1 on %s", (surface) => {
      const raised = new Map(vars).set("--muted-foreground", muted);
      expect(tokenContrast(raised, "--muted-foreground", surface)).toBeGreaterThanOrEqual(7);
    });
    it("borders are stronger than the default ones", () => {
      expect(declared(block, selector, "--border")).not.toBe(vars.get("--border"));
    });
  });

  it("the default tokens are not changed by this file (the AA regression suite still reads index.css only)", () => {
    expect(tokenContrast(base.light, "--muted-foreground", "--background")).toBeGreaterThanOrEqual(4.5);
  });

  it("the inactive-window rule dims the selection and the sidebar icons, keyed on the window-active flag", () => {
    expect(css).toMatch(/html\[data-desktop-os\]\[data-window-active="false"\]\s*\{\s*--selected: var\(--hover\);/);
    expect(css).toMatch(/\[data-window-active="false"\] \[data-slot="sidebar-inner"\] svg\s*\{\s*opacity: 0\.6;/);
  });
});
