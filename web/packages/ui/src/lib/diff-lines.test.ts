import { html } from "diff2html";
import { describe, expect, it } from "vitest";

import { filePathForElement, resolveDiffLine } from "@/lib/diff-lines";

const DIFF = `diff --git a/file.txt b/file.txt
index a29bdeb..0226208 100644
--- a/file.txt
+++ b/file.txt
@@ -1,2 +1,2 @@
 line1
-old line
+line2 added
`;

/** Renders DIFF with diff2html for real and returns the container, so these assertions are against the library's actual markup rather than a hand-written fixture of it. */
function renderDiff(outputFormat: "line-by-line" | "side-by-side"): HTMLElement {
  const el = document.createElement("div");
  el.innerHTML = html(DIFF, { outputFormat, drawFileList: false });
  document.body.appendChild(el);
  return el;
}

function lineElements(root: HTMLElement): Element[] {
  return [...root.querySelectorAll(".d2h-code-line-ctn")];
}

describe("resolveDiffLine", () => {
  it("reads the new-side line number from unified output", () => {
    const root = renderDiff("line-by-line");
    const added = lineElements(root).find((el) => el.textContent?.includes("line2 added"))!;

    expect(resolveDiffLine(added)).toEqual({ side: "new", line: 2, text: "line2 added" });
  });

  it("reads the old-side line number for a deletion", () => {
    const root = renderDiff("line-by-line");
    const removed = lineElements(root).find((el) => el.textContent?.includes("old line"))!;

    expect(resolveDiffLine(removed)).toEqual({ side: "old", line: 2, text: "old line" });
  });

  it("reads side-by-side output's bare-text line numbers too", () => {
    const root = renderDiff("side-by-side");
    const added = lineElements(root).find((el) => el.textContent?.includes("line2 added"))!;

    expect(resolveDiffLine(added)).toEqual({ side: "new", line: 2, text: "line2 added" });
  });

  it("returns null for a hunk header, and for anything outside a row", () => {
    const root = renderDiff("line-by-line");
    const hunk = [...root.querySelectorAll(".d2h-code-line")].find((el) => el.textContent?.includes("@@"))!;

    expect(resolveDiffLine(hunk)).toBeNull();
    expect(resolveDiffLine(root)).toBeNull();
    expect(resolveDiffLine(null)).toBeNull();
  });
});

describe("filePathForElement", () => {
  it("recovers the file a row belongs to from diff2html's own header", () => {
    const root = renderDiff("line-by-line");
    const added = lineElements(root).find((el) => el.textContent?.includes("line2 added"))!;

    expect(filePathForElement(added)).toBe("file.txt");
  });

  it("returns null outside any file wrapper", () => {
    expect(filePathForElement(document.body)).toBeNull();
  });
});
