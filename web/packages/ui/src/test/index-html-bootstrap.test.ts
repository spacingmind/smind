import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

import { THEME_STORAGE_KEY } from "@/lib/theme";

// Deliberately not `new URL("../../index.html", import.meta.url)` -- Vite's
// transform special-cases exactly that syntactic shape (a relative-URL
// asset reference) and rewrites it to a dev-server URL at build time, which
// under vitest's jsdom environment resolves to a bogus
// http://localhost/index.html instead of this file's real path. Resolving
// `import.meta.url` alone, then joining with node:path, isn't pattern-
// matched the same way.
const HERE = dirname(fileURLToPath(import.meta.url));
const INDEX_HTML_PATH = join(HERE, "../../index.html");

/**
 * index.html's pre-paint bootstrap script can't import lib/theme.ts (it
 * has to run before any module graph loads, which is the whole point --
 * see that file's comment), so it's a hand-synced copy of the same
 * decision. This is a cheap tripwire, not full behavioral coverage
 * (lib/theme.test.ts's computeBootstrapIsDark covers the actual
 * decision table): it fails loudly if the inline script's storage key
 * ever drifts from lib/theme.ts's, or if the script disappears entirely.
 */
describe("index.html's pre-paint theme bootstrap", () => {
  const html = readFileSync(INDEX_HTML_PATH, "utf-8");

  it("embeds an inline script that toggles the dark class before main.tsx loads", () => {
    const headEnd = html.indexOf("</head>");
    const mainScript = html.indexOf('src="/src/main.tsx"');
    expect(headEnd).toBeGreaterThan(-1);
    expect(mainScript).toBeGreaterThan(headEnd);
    expect(html).toMatch(/classList\.toggle\(\s*["']dark["']/);
  });

  it("reads the same storage key lib/theme.ts writes to", () => {
    expect(html).toContain(`"${THEME_STORAGE_KEY}"`);
  });

  it("falls back to prefers-color-scheme rather than defaulting to light or dark unconditionally", () => {
    expect(html).toContain("prefers-color-scheme: dark");
  });
});
