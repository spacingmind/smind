import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

const HERE = dirname(fileURLToPath(import.meta.url));
const SRC = join(HERE, "..");

/** Every non-test .ts/.tsx file under dir, recursively; [] when it is absent. */
function collect(dir: string): string[] {
  if (!existsSync(dir)) return [];
  const out: string[] = [];
  for (const entry of readdirSync(dir)) {
    const p = join(dir, entry);
    if (statSync(p).isDirectory()) out.push(...collect(p));
    else if (/\.tsx?$/.test(entry) && !/\.test\./.test(entry)) out.push(p);
  }
  return out;
}

const FILES = [
  ...collect(join(SRC, "components", "plugin-view")),
  join(SRC, "lib", "plugin-view-types.ts"),
  ...collect(join(SRC, "components", "plugin-panel")),
  join(SRC, "hooks", "use-plugin-panels.ts"),
].filter((p) => existsSync(p));

const FORBIDDEN = [
  "dangerouslySetInnerHTML",
  "innerHTML",
  "outerHTML",
  "insertAdjacentHTML",
  "eval(",
  "new Function",
  "rehype-raw",
  "<img",
];

describe("plugin view safety", () => {
  it("no raw html sinks in the plugin renderer directories", () => {
    expect(FILES.length).toBeGreaterThanOrEqual(3);
    for (const file of FILES) {
      const source = readFileSync(file, "utf-8");
      for (const sink of FORBIDDEN) {
        expect(source.includes(sink), `${file} must not contain ${sink}`).toBe(false);
      }
    }
  });
});
