import { describe, expect, it } from "vitest";

import { applyPatch, unifiedDiff } from "@/lib/text-diff";

describe("unifiedDiff", () => {
  it("is empty for equal texts", () => {
    expect(unifiedDiff("a\nb\n", "a\nb\n", "skill.md")).toBe("");
  });

  it("marks removed and added lines inside one hunk that covers the whole text", () => {
    const diff = unifiedDiff("one\ntwo\nthree\n", "one\n2\nthree\n", "skill.md");
    expect(diff).toContain("--- a/skill.md\n+++ b/skill.md\n@@ -1,3 +1,3 @@\n one\n-two\n+2\n three\n");
  });

  it("handles an empty side", () => {
    expect(unifiedDiff("", "x\n", "s")).toContain("@@ -0,0 +1,1 @@\n+x");
    expect(unifiedDiff("x\n", "", "s")).toContain("@@ -1,1 +0,0 @@\n-x");
  });
});

describe("applyPatch", () => {
  it("replaces the first occurrence", () => {
    expect(applyPatch("a b c", "b", "B")).toBe("a B c");
  });
  it("is null when the old text is gone or empty", () => {
    expect(applyPatch("a b c", "z", "Z")).toBeNull();
    expect(applyPatch("a", "", "Z")).toBeNull();
  });
});
