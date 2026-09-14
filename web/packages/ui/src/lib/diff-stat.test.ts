import { describe, expect, it } from "vitest";

import { EMPTY_DIFF_STAT, formatDiffStat, parseDiffStat } from "@/lib/diff-stat";

const TWO_FILE_DIFF = `diff --git a/file.txt b/file.txt
index a29bdeb..0226208 100644
--- a/file.txt
+++ b/file.txt
@@ -1,2 +1,2 @@
 line1
-old line
+line2 added
diff --git a/new.txt b/new.txt
new file mode 100644
index 0000000..f2ba8f8
--- /dev/null
+++ b/new.txt
@@ -0,0 +1,2 @@
+brand new
+second line
`;

describe("parseDiffStat", () => {
  it("counts files, additions and deletions, excluding the +++/--- file headers", () => {
    expect(parseDiffStat(TWO_FILE_DIFF)).toEqual({ files: 2, additions: 3, deletions: 1 });
  });

  it("returns all zeroes for an empty diff", () => {
    expect(parseDiffStat("")).toEqual(EMPTY_DIFF_STAT);
  });

  it("falls back to counting +++ headers when the diff carries no `diff --git` lines", () => {
    const headerless = `--- a/x.txt\n+++ b/x.txt\n@@ -1 +1 @@\n-a\n+b\n`;
    expect(parseDiffStat(headerless)).toEqual({ files: 1, additions: 1, deletions: 1 });
  });
});

describe("formatDiffStat", () => {
  it("singularizes a one-file diff", () => {
    expect(formatDiffStat({ files: 1, additions: 2, deletions: 0 })).toBe("1 file +2 −0");
    expect(formatDiffStat({ files: 3, additions: 42, deletions: 7 })).toBe("3 files +42 −7");
  });
});
