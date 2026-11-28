import { execFileSync } from "node:child_process";
import { describe, expect, it } from "vitest";

const UI = "web/packages/ui/src";
const WRAPPERS = ["button", "input", "switch", "dialog"] as const;

// Export names recorded from origin/develop before the Astryx swap.
const EXPECTED_EXPORTS: Record<(typeof WRAPPERS)[number], string[]> = {
  button: ["Button", "buttonVariants"],
  input: ["Input"],
  switch: ["Switch"],
  dialog: [
    "Dialog",
    "DialogBody",
    "DialogClose",
    "DialogContent",
    "DialogDescription",
    "DialogFooter",
    "DialogHeader",
    "DialogOverlay",
    "DialogPortal",
    "DialogTitle",
    "DialogTrigger",
  ],
};

// Outside components/, the Wave 0 branch may touch exactly these.
const ALLOWED_OTHER = new Set([
  // Tests that pinned Tailwind class names or a mock the Astryx wrappers no longer satisfy (assertions moved to behaviour).
  `${UI}/test/no-hardcoded-colors.test.ts`,
  `${UI}/test/setup.ts`,
  `${UI}/index.css`,
  `${UI}/lib/astryx-build.test.ts`,
  `${UI}/lib/astryx-wave0-scope.test.ts`,
  `${UI}/lib/theme.ts`,
  `${UI}/theme/smind-theme.css`,
  `${UI}/theme/smind-theme.test.ts`,
  `${UI}/theme/smind-theme.ts`,
  `${UI}/theme/theme-attributes.ts`,
]);

function git(...args: string[]): string {
  return execFileSync("git", args, { encoding: "utf8" });
}

function exportNames(source: string): string[] {
  const names = new Set<string>();
  for (const m of source.matchAll(/export\s*\{([^}]*)\}/g)) {
    for (const n of m[1].split(",")) {
      const name = n.trim().split(/\s+as\s+/).pop()?.trim();
      if (name) names.add(name);
    }
  }
  for (const m of source.matchAll(/export\s+(?:async\s+)?(?:function|const|class)\s+(\w+)/g)) names.add(m[1]);
  return [...names].sort();
}

function hasBase(): boolean {
  try {
    git("rev-parse", "--verify", "origin/develop");
    return true;
  } catch {
    return false;
  }
}

describe("astryx wave 0 scope", () => {
  it("no_call_site_changed_for_the_four_wrappers", () => {
    const root = git("rev-parse", "--show-toplevel").trim();
    const allowedComponents = new Set(
      WRAPPERS.flatMap((w) => [`${UI}/components/ui/${w}.tsx`, `${UI}/components/ui/${w}.test.tsx`]),
    );

    // The recorded list matches what the wrappers export now.
    for (const w of WRAPPERS) {
      const now = exportNames(git("-C", root, "show", `HEAD:${UI}/components/ui/${w}.tsx`));
      expect(now, `${w} exports`).toEqual([...EXPECTED_EXPORTS[w]].sort());
    }

    if (!hasBase()) return; // no origin/develop in this checkout: the diff cannot be taken
    for (const w of WRAPPERS) {
      const before = exportNames(git("-C", root, "show", `origin/develop:${UI}/components/ui/${w}.tsx`));
      expect(before, `${w} exports on develop`).toEqual([...EXPECTED_EXPORTS[w]].sort());
    }

    const changed = git("-C", root, "diff", "--name-only", "origin/develop...HEAD", "--", UI)
      .split("\n")
      .filter(Boolean);
    const strays = changed.filter((p) => {
      // A call site's PRODUCTION file must not change; its test may move an assertion from a class name to behaviour.
      if (p.startsWith(`${UI}/components/`)) return !allowedComponents.has(p) && !/\.test\.tsx?$/.test(p);
      return !ALLOWED_OTHER.has(p);
    });
    expect(strays).toEqual([]);
  });
});
