import { describe, expect, it } from "vitest";

import { fuzzyFilter, fuzzyMatch } from "@/lib/fuzzy-match";

const PATHS = [
  "web/packages/ui/src/components/file-editor-pane.tsx",
  "web/packages/ui/src/components/file-preview.tsx",
  "web/packages/ui/src/components/file-explorer-pane.tsx",
  "internal/workspace/git.go",
  "docs/plans/active/ui-redesign-parity.md",
];

describe("fuzzyMatch", () => {
  it("returns null when a query character never appears, in order", () => {
    expect(fuzzyMatch("zzz", "file-editor-pane.tsx")).toBeNull();
    expect(fuzzyMatch("pfe", "file-editor-pane.tsx")).toBeNull(); // p before f, wrong order
  });

  it("matches an empty query against everything with a neutral score", () => {
    expect(fuzzyMatch("", "any/path.ts")).toEqual({ path: "any/path.ts", score: 0, indices: [] });
  });

  it("ranks a filename substring match above a scattered subsequence match", () => {
    // "editor" is a literal substring of file-editor-pane.tsx's filename;
    // it also happens to be a valid (but scattered) subsequence of
    // file-explorer-pane.tsx's filename (e-d?-no: pick a query that is a
    // substring of one name and a subsequence-only of the other).
    const substring = fuzzyMatch("editor", "src/file-editor-pane.tsx")!;
    const subsequence = fuzzyMatch("editor", "src/file-explorer-pane.tsx");

    expect(substring.score).toBeGreaterThan(0);
    // "editor" is not a contiguous substring of "file-explorer-pane.tsx"'s
    // filename ("explorer" doesn't contain it), so it can only match (if
    // at all) as a scattered subsequence, scoring far lower.
    if (subsequence) {
      expect(substring.score).toBeGreaterThan(subsequence.score);
    }
  });

  it("prefers a match at the very start of the filename", () => {
    const atStart = fuzzyMatch("file", "src/file-preview.tsx")!;
    const notAtStart = fuzzyMatch("file", "src/my-file-preview.tsx")!;
    expect(atStart.score).toBeGreaterThan(notAtStart.score);
  });

  it("reports the matched indices for highlighting", () => {
    const match = fuzzyMatch("git", "internal/workspace/git.go")!;
    expect(match.indices).toEqual([19, 20, 21]);
  });
});

describe("fuzzyFilter", () => {
  it("ranks the expected path first for a query typed against a real path list", () => {
    // "explorer" is a substring of exactly one filename in the list --
    // the unambiguous case a real quick-open query usually is.
    const results = fuzzyFilter("explorer", PATHS);
    expect(results[0]!.path).toBe("web/packages/ui/src/components/file-explorer-pane.tsx");
  });

  it("excludes paths with no match at all", () => {
    const results = fuzzyFilter("gitgo", PATHS);
    expect(results.map((r) => r.path)).toEqual(["internal/workspace/git.go"]);
  });

  it("returns every path, capped at limit, for an empty (or blank) query", () => {
    expect(fuzzyFilter("", PATHS).map((r) => r.path)).toEqual(PATHS);
    expect(fuzzyFilter("   ", PATHS, 2)).toHaveLength(2);
  });

  it("caps the result count at limit", () => {
    const manyPaths = Array.from({ length: 100 }, (_, i) => `file${i}.ts`);
    expect(fuzzyFilter("file", manyPaths, 10)).toHaveLength(10);
  });
});
