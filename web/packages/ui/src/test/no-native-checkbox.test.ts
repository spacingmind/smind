import { readdirSync, readFileSync } from "node:fs";
import { dirname, join, relative } from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

// See no-hardcoded-colors.test.ts: join a plain path instead of `new URL("..", import.meta.url)`.
const SRC_DIR = join(dirname(fileURLToPath(import.meta.url)), "..");

const NATIVE_CHECKBOX = /type=["']checkbox["']/;

/** Files still on a native checkbox. This list may only shrink: migrate a file, then delete its entry. */
const ALLOWED_LEGACY: string[] = [
  "components/archive-preview.tsx",
  "components/diff-viewer-pane.tsx",
  "components/git/sync-dialog.tsx",
  "components/learning/skill-export-dialog.tsx",
  "components/pickers/auto-accept-toggle.tsx",
  "components/settings/automation-form.tsx",
  "components/settings/mcp-servers-section.tsx",
  "components/settings/plugins-section.tsx",
  "components/settings/profile-contract-fields.tsx",
];

function listFiles(dir: string): string[] {
  const out: string[] = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    if (entry.name === "node_modules" || entry.name.startsWith(".")) continue;
    const full = join(dir, entry.name);
    if (entry.isDirectory()) {
      out.push(...listFiles(full));
    } else if (/\.tsx?$/.test(entry.name) && !/\.test\.tsx?$/.test(entry.name)) {
      out.push(full);
    }
  }
  return out;
}

describe("native checkboxes", () => {
  it("no component uses a native checkbox outside ui/checkbox.tsx", () => {
    const offenders = listFiles(SRC_DIR)
      .map((f) => relative(SRC_DIR, f).split("\\").join("/"))
      .filter((rel) => rel !== "components/ui/checkbox.tsx")
      .filter((rel) => NATIVE_CHECKBOX.test(readFileSync(join(SRC_DIR, rel), "utf-8")))
      .sort();
    expect(offenders).toEqual([...ALLOWED_LEGACY].sort());
  });
});
