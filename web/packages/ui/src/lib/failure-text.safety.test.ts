// failure-text.safety.test.ts (AC7): no sentence, label or control text of the failure
// classes may ask to merge, approve or always-allow -- these classes exist so a human
// decides, never the run itself. Same source-scan shape as mobile's stewardSafety.test.ts:
// read the sources with node:fs, strip comments, then look at what is left. The scan covers
// all four files that own failure wording (failure-text.ts sentences, failure-class.ts
// cards and controls, run-error-box.tsx and run-footer-model.ts rendering); comment-only
// mentions (e.g. "never says what to approve") are stripped before matching, and no
// unrelated string literal in these files currently matches, so no narrower scope is needed.

import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

const UNSAFE = /merge|approve|always[ _-]?allow/i;

const strip = (s: string): string => s.replace(/\/\*[\s\S]*?\*\//g, "").replace(/(^|[^:])\/\/[^\n]*/g, "$1");

/** Every string literal of a comment-stripped TypeScript source. */
function stringLiterals(src: string): string[] {
  return [...src.matchAll(/"(?:[^"\\\n]|\\.)*"|'(?:[^'\\\n]|\\.)*'|`(?:[^`\\]|\\.)*`/g)].map((m) => m[0]);
}

/** The unsafe string literals of one source file (the comment in it is not text the UI shows). */
function unsafeStrings(src: string): string[] {
  return stringLiterals(strip(src)).filter((lit) => UNSAFE.test(lit));
}

const SOURCES = [
  join(__dirname, "failure-text.ts"),
  join(__dirname, "failure-class.ts"),
  join(__dirname, "..", "components", "timeline", "run-error-box.tsx"),
  join(__dirname, "..", "components", "timeline", "run-footer-model.ts"),
];

describe("failure class label safety (AC7)", () => {
  it("no sentence, label or control text mentions merge, approve or always-allow", () => {
    for (const f of SOURCES) {
      expect(unsafeStrings(readFileSync(f, "utf8")), f).toEqual([]);
    }
  });

  it("the scanner catches a planted approval control and ignores a comment mention", () => {
    expect(unsafeStrings('const label = "Approve the merge";')).toEqual(['"Approve the merge"']);
    expect(unsafeStrings('const x = "Always allow";')).toEqual(['"Always allow"']);
    expect(unsafeStrings('const x = "always-allow";')).toEqual(['"always-allow"']);
    // A mention inside a comment is stripped, not shown.
    expect(unsafeStrings('// never says what to merge or approve\nconst x = "Continue";')).toEqual([]);
    // The class's own words stay legal.
    expect(unsafeStrings('const hint = "Review the permission policy";')).toEqual([]);
  });
});
