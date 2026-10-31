import { describe, expect, it } from "vitest";

import {
  DELIVERABLE_EXTENSIONS,
  MAX_CANDIDATES,
  MAX_CARDS,
  checkable,
  extractCandidates,
  extensionOf,
  formatSize,
  hasInAppView,
  isDeliverable,
  limitCards,
  normalizeCandidate,
  pathsInMessage,
  typeLabel,
} from "@/components/timeline/deliverables";
import type { TimelineItem } from "@/hooks/use-run-timeline";
import type { FileStatResult } from "@/lib/types";

const WT = "/home/u/wt";

function tool(toolName: string, input: unknown, status: "success" | "failure" | "running" = "success", n = 0): TimelineItem {
  return { kind: "tool_call", id: `t${n}`, toolCallId: `c${n}`, toolName, status, input };
}
function say(text: string, n = 0): TimelineItem {
  return { kind: "assistant", id: `a${n}`, text };
}

describe("path_normalisation_table", () => {
  const table: Array<[string, string | undefined, string | null]> = [
    // relative forms
    ["out/report.pdf", WT, "out/report.pdf"],
    ["./out//report.pdf", WT, "out/report.pdf"],
    ["my report final.pdf", WT, "my report final.pdf"], // spaces are fine
    ["<my file.pdf>", WT, "my file.pdf"], // markdown angle-bracket target
    ["  out/a.pdf  ", WT, "out/a.pdf"],
    // parent segments anywhere
    ["../x.pdf", WT, null],
    ["out/../../x.pdf", WT, null],
    ["out/../x.pdf", WT, null],
    ["..", WT, null],
    // home and schemes
    ["~/x.pdf", WT, null],
    ["~x.pdf", WT, null],
    ["https://example.com/x.pdf", WT, null],
    ["http://example.com/x.pdf", WT, null],
    ["file:///x.pdf", WT, null],
    ["data:text/plain,hi", WT, null],
    ["mailto:a@b.c", WT, null],
    ["javascript:alert(1)", WT, null],
    // absolute: inside, outside, a sibling sharing the prefix
    ["/home/u/wt/out/a.pdf", WT, "out/a.pdf"],
    ["/home/u/wt/a.pdf", WT, "a.pdf"],
    ["/home/u/other/a.pdf", WT, null],
    ["/home/u/wt-evil/a.pdf", WT, null],
    ["/home/u/wt", WT, null], // the root itself is not a file
    ["/etc/passwd", WT, null],
    ["/home/u/wt/out/a.pdf", undefined, null], // no root to convert against
    ["/home/u/wt/out/a.pdf", "", null],
    // directories, empties, control characters
    ["out/", WT, null],
    ["", WT, null],
    ["   ", WT, null],
    [".", WT, null],
    ["./", WT, null],
    ["a\u0000b.pdf", WT, null],
    ["a\nb.pdf", WT, null],
    ["x".repeat(600) + ".pdf", WT, null],
    // Windows
    [String.raw`out\a.pdf`, String.raw`C:\Users\u\wt`, "out/a.pdf"],
    [String.raw`C:\Users\u\wt\out\a.pdf`, String.raw`C:\Users\u\wt`, "out/a.pdf"],
    ["c:/users/u/WT/out/a.pdf", String.raw`C:\Users\u\wt`, "out/a.pdf"], // case-insensitive drive path
    [String.raw`C:\Users\u\wt\out\a.pdf`, String.raw`\\?\C:\Users\u\wt`, "out/a.pdf"], // verbatim root
    [String.raw`C:\Users\u\wt\out\a.pdf`, String.raw`C:\Users\u\wt\\`, "out/a.pdf"],
    [String.raw`D:\x\a.pdf`, String.raw`C:\Users\u\wt`, null],
    [String.raw`C:\Users\u\wt-evil\a.pdf`, String.raw`C:\Users\u\wt`, null],
    [String.raw`\\server\share\a.pdf`, String.raw`C:\Users\u\wt`, null],
    ["//server/share/a.pdf", String.raw`C:\Users\u\wt`, null],
    [String.raw`C:a.pdf`, String.raw`C:\Users\u\wt`, null], // drive-relative
    [String.raw`..\a.pdf`, String.raw`C:\Users\u\wt`, null],
    [String.raw`out\..\..\a.pdf`, String.raw`C:\Users\u\wt`, null],
  ];
  for (const [raw, root, want] of table) {
    it(`${JSON.stringify(raw)} under ${JSON.stringify(root)} -> ${JSON.stringify(want)}`, () => {
      expect(normalizeCandidate(raw, root)).toBe(want);
    });
  }
});

describe("candidates", () => {
  it("candidates_from_write_tools_and_final_message", () => {
    const items: TimelineItem[] = [
      say("I'll start with the report.", 0),
      tool("Write", { file_path: `${WT}/out/a.pdf` }, "success", 1),
      tool("Edit", { file_path: "out/failed.pdf" }, "failure", 2), // not successful
      tool("Edit", { file_path: "out/still-running.pdf" }, "running", 3), // not finished
      tool("Read", { file_path: "out/read.pdf" }, "success", 4), // read intent
      tool("delete", { path: "out/deleted.pdf" }, "success", 5), // never leaves a file
      tool("move", { path: "out/moved.pdf" }, "success", 6),
      tool("MultiEdit", { file_path: "notes/plan.md" }, "success", 7),
      tool("NotebookEdit", { notebook_path: "x.ipynb", path: "nb/analysis.csv" }, "success", 8),
      tool("Write", { file_path: "out/a.pdf" }, "success", 9), // duplicate
      tool("edit", { path: String.raw`C:\Users\u\wt\sheets\q3.xlsx` }, "success", 10), // ACP kind, Windows absolute (outside this root)
      say("Done. See [the report](out/summary.md), the data in `out/data.csv`, and `src/main.go`.\n\n```sh\ncat example.pdf\n```\n", 11),
    ];
    const over = extractCandidates(items, WT, true);
    expect(over).toEqual(["out/a.pdf", "notes/plan.md", "nb/analysis.csv", "out/summary.md", "out/data.csv", "src/main.go"]);
    // before the run is over the last message is not final: tool paths only
    expect(extractCandidates(items, WT, false)).toEqual(["out/a.pdf", "notes/plan.md", "nb/analysis.csv"]);
    // only the LAST assistant message counts
    const two: TimelineItem[] = [say("first: `old.pdf`", 0), say("final: `new.pdf`", 1)];
    expect(extractCandidates(two, WT, true)).toEqual(["new.pdf"]);
    // Windows root: the same Write path converts, the outside one does not
    const win = extractCandidates([tool("Write", { file_path: String.raw`C:\Users\u\wt\out\a.pdf` }), tool("Write", { file_path: String.raw`D:\o\b.pdf` }, "success", 1)], String.raw`C:\Users\u\wt`, true);
    expect(win).toEqual(["out/a.pdf"]);
  });

  it("markdown_links_and_inline_code_in_reading_order", () => {
    const text = [
      "Inline `second.csv` comes after [first](first/file.pdf#page=2) in reading order,",
      "then [spaced](<my report.docx>) and [encoded](out/a%20b.xlsx) and [abs](/home/u/wt/out/z.pdf \"title\").",
      "Not files: [web](https://e.com/a.pdf), [anchor](#top), [up](../x.pdf), [home](~/h.pdf), `rm -rf .`, `a/../b.pdf`, `/etc/p.pdf`, `~/q.pdf`, `no_extension`, `version 1.2.3.45678`.",
    ].join(" ");
    const got = pathsInMessage(text, WT);
    expect(got).toContain("first/file.pdf");
    expect(got).toContain("my report.docx");
    expect(got).toContain("out/a b.xlsx");
    expect(got).toContain("out/z.pdf");
    expect(got).toContain("second.csv");
    // "version 1.2.3.45678" has the shape of an inline-code path; its extension is not on the list, so it is never stat'd
    expect(got).toContain("version 1.2.3.45678");
    expect(got).toHaveLength(6);
    expect(checkable(got)).toHaveLength(5);
    // reading order: the inline span (position 7) precedes the first link
    expect(got.indexOf("second.csv")).toBeLessThan(got.indexOf("first/file.pdf"));
    // a fenced block hides its paths
    expect(pathsInMessage("```\n[a](a.pdf) `b.pdf`\n```", WT)).toEqual([]);
  });

  it("shell_text_never_parsed", () => {
    const items: TimelineItem[] = [
      tool("Bash", { command: "python make.py > out/report.pdf && cp out/report.pdf final.pdf" }),
      tool("execute", { command: "echo hi > notes.md" }, "success", 1),
      tool("BashOutput", { command: "cat out/report.pdf" }, "success", 2),
      tool("SomeUnknownTool", { command: "touch x.docx" }, "success", 3), // classified by shape: a command
      tool("Grep", { pattern: "report.pdf", path: "out/report.pdf" }, "success", 4),
      tool("WebFetch", { url: "https://e.com/x.pdf" }, "success", 5),
    ];
    expect(extractCandidates(items, WT, true)).toEqual([]);
  });
});

describe("caps_6_cards_20_candidates", () => {
  it("limits what is checked and what is shown", () => {
    expect(MAX_CARDS).toBe(6);
    expect(MAX_CANDIDATES).toBe(20);
    const many = Array.from({ length: 30 }, (_, i) => `out/f${i}.pdf`);
    expect(checkable(many)).toHaveLength(20);
    expect(checkable(many)[0]).toBe("out/f0.pdf");
    // extensions outside the allow-list are never stat'd, and do not use up the 20
    const mixed = ["a.exe", "b.sh", "c.svg", "d.zip", "e.docm", "noext", "out/ok.pdf", ".pdf"];
    expect(checkable(mixed)).toEqual(["out/ok.pdf"]);
    const { cards, more } = limitCards(Array.from({ length: 9 }, (_, i) => i));
    expect(cards).toEqual([0, 1, 2, 3, 4, 5]);
    expect(more).toBe(3);
    expect(limitCards([1, 2])).toEqual({ cards: [1, 2], more: 0 });
    expect(limitCards([])).toEqual({ cards: [], more: 0 });
    expect(new Set(DELIVERABLE_EXTENSIONS)).toEqual(new Set(["pdf", "docx", "xlsx", "pptx", "md", "txt", "csv", "html", "png", "jpg", "jpeg", "gif", "webp"]));
  });
});

describe("verification and labels", () => {
  const stat = (over: Partial<FileStatResult> = {}): FileStatResult => ({
    size: 100, mtime: "2026-10-31T10:05:00Z", kind: "pdf", mime: "application/pdf", ext: ".pdf", mismatch: false, previewable: true, ...over,
  });
  const STARTED = "2026-10-31T10:00:00Z";

  it("isDeliverable: fresh, allow-listed, not a mismatch", () => {
    expect(isDeliverable(stat(), STARTED)).toBe(true);
    expect(isDeliverable(stat({ mtime: STARTED }), STARTED)).toBe(true); // at the start is fine
    expect(isDeliverable(stat({ mtime: "2026-10-31T09:59:59Z" }), STARTED)).toBe(false); // older than the run
    expect(isDeliverable(stat({ mismatch: true }), STARTED)).toBe(false);
    expect(isDeliverable(stat({ ext: ".exe" }), STARTED)).toBe(false);
    expect(isDeliverable(stat({ ext: ".PDF" }), STARTED)).toBe(true);
    expect(isDeliverable(stat({ mtime: "garbage" }), STARTED)).toBe(false);
    expect(isDeliverable(stat(), "garbage")).toBe(false);
  });

  it("labels and sizes", () => {
    expect(typeLabel("pdf")).toBe("PDF");
    expect(typeLabel("docx")).toBe("Word");
    expect(typeLabel("png")).toBe("PNG");
    expect(formatSize(812)).toBe("812 bytes");
    expect(formatSize(240 * 1024)).toBe("240 KB");
    expect(formatSize(1.4 * 1024 * 1024)).toBe("1.4 MB");
    expect(formatSize(60 * 1024 * 1024)).toBe("60 MB");
    expect(extensionOf("a/b/C.PDF")).toBe("pdf");
    expect(extensionOf("a/.pdf")).toBe("");
    expect(hasInAppView("pdf")).toBe(true);
    expect(hasInAppView("docx")).toBe(false);
    expect(hasInAppView("xlsx")).toBe(false);
    expect(hasInAppView("pptx")).toBe(false);
  });
});
