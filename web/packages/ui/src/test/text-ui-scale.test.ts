import { readdirSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

// Mirrors no-hardcoded-colors.test.ts's shape: a grep-based guard over the
// same source tree.

const SRC_DIR = join(dirname(fileURLToPath(import.meta.url)), "..");

/**
 * `text-sm`/`text-xs`/`text-base` -- Tailwind's built-in font-size
 * utilities -- with or without a variant prefix (`md:text-sm`,
 * `dark:text-xs`, ...). refs/zcode/DESIGN.md's "Highest-priority UI
 * constraint": application UI must use the `text-ui-*` scale instead.
 */
const TAILWIND_BUILTIN_TEXT_SIZE = /\btext-(?:sm|xs|base)\b/;

/** An arbitrary UI font size (`text-[13px]`, `text-[0.8rem]`, ...). */
const ARBITRARY_TEXT_SIZE = /\btext-\[[\d.]+(?:px|rem|em)\]/;

/** An inline `font-size` (raw CSS, a React inline-style prop, or a direct `.style.fontSize` assignment). */
const INLINE_FONT_SIZE = /font-size\s*:|fontSize\s*[:=]/;

/**
 * Files exempt from the guard: code, diff, and terminal *content*
 * rendering, which DESIGN.md carves out as keeping its own independent
 * numeric font-size setting (`--font-scale-code`, diff2html's own CSS,
 * xterm's `fontSize` option) -- their surrounding chrome (headers, buttons,
 * error rows) still must use `text-ui-*` and is not exempt.
 *
 * `code-mirror-editor.tsx`'s one `text-sm` sets the CodeMirror container's
 * ambient font-size, which `.cm-content` (the code itself) inherits -- the
 * code-content exception, not app-UI chrome.
 */
const EXEMPT_FILES = new Set(["components/code-mirror-editor.tsx"]);

/**
 * Migration is landing one directory per commit (zcode-visual-parity plan,
 * P1 Step 3) -- this list grows with each commit until it covers the whole
 * tree, at which point it's deleted along with this comment and the filter
 * below. A path (relative to `src/`) is "covered" if it equals an entry
 * here or is nested under one.
 */
const MIGRATED_ROOTS = [
  "components/ui",
  "components/composer",
  "components/find",
  "components/permission",
  "components/settings",
  "components/timeline",
];

function isMigrated(rel: string): boolean {
  return MIGRATED_ROOTS.some((root) => rel === root || rel.startsWith(`${root}/`));
}

function listFiles(dir: string): string[] {
  const out: string[] = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    if (entry.name === "node_modules" || entry.name.startsWith(".")) continue;
    const full = join(dir, entry.name);
    if (entry.isDirectory()) {
      out.push(...listFiles(full));
    } else if (/\.tsx?$/.test(entry.name)) {
      out.push(full);
    }
  }
  return out;
}

function relPath(f: string): string {
  return f.slice(SRC_DIR.length + 1);
}

describe("text-ui-* scale guard (zcode-visual-parity plan, P1 Step 3)", () => {
  const files = listFiles(SRC_DIR)
    .filter((f) => !f.endsWith(".test.ts") && !f.endsWith(".test.tsx"))
    .filter((f) => !EXEMPT_FILES.has(relPath(f)))
    .filter((f) => isMigrated(relPath(f)));

  it("no app-UI file uses a Tailwind built-in text-size utility", () => {
    const offenders = files.filter((f) => TAILWIND_BUILTIN_TEXT_SIZE.test(readFileSync(f, "utf-8")));
    expect(offenders.map(relPath)).toEqual([]);
  });

  it("no app-UI file uses an arbitrary text-[...] font size", () => {
    const offenders = files.filter((f) => ARBITRARY_TEXT_SIZE.test(readFileSync(f, "utf-8")));
    expect(offenders.map(relPath)).toEqual([]);
  });

  it("no app-UI file sets an inline font-size", () => {
    const offenders = files.filter((f) => INLINE_FONT_SIZE.test(readFileSync(f, "utf-8")));
    expect(offenders.map(relPath)).toEqual([]);
  });

  // Deliberately-violating fixtures, proving the three patterns above
  // actually detect what they claim to -- a guard that never fires red
  // against anything is not a guard.
  it.each([
    ['className="text-sm"', TAILWIND_BUILTIN_TEXT_SIZE],
    ['className="md:text-xs"', TAILWIND_BUILTIN_TEXT_SIZE],
    ['className="text-base font-medium"', TAILWIND_BUILTIN_TEXT_SIZE],
    ['className="text-[13px]"', ARBITRARY_TEXT_SIZE],
    ['className="text-[0.8rem]"', ARBITRARY_TEXT_SIZE],
    ["style={{ fontSize: 13 }}", INLINE_FONT_SIZE],
    ["el.style.fontSize = '13px';", INLINE_FONT_SIZE],
  ] as const)("detects a violation in %j", (source, pattern) => {
    expect(pattern.test(source)).toBe(true);
  });

  // And the legal spellings must NOT trip any pattern (no false positives).
  it.each([
    'className="text-ui-sm"',
    'className="md:text-ui-xs"',
    'className="text-ui-base font-medium"',
    'className="text-ui-xl text-ui-lg text-ui-caption"',
  ])("does not flag the legal spelling %j", (source) => {
    expect(TAILWIND_BUILTIN_TEXT_SIZE.test(source)).toBe(false);
    expect(ARBITRARY_TEXT_SIZE.test(source)).toBe(false);
    expect(INLINE_FONT_SIZE.test(source)).toBe(false);
  });
});
