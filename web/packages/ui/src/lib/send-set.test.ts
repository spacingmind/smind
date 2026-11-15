import { describe, expect, it } from "vitest";

import { appendChipBlocks, buildTerminalBlock, byteLength, clipText, fenceBlock, fileRefChipLabel, SEND_CAP_BYTES, terminalChipLabel, type ContextChip } from "@/lib/send-set";

describe("send-set", () => {
  it("byteLength counts UTF-8 bytes, not code units", () => {
    expect(byteLength("héllo")).toBe(6);
    expect(byteLength("😀")).toBe(4);
  });

  it("clipText leaves a text that fits alone", () => {
    expect(clipText("hello", 10)).toEqual({ text: "hello", omittedBytes: 0 });
    expect(clipText("hello")).toEqual({ text: "hello", omittedBytes: 0 });
  });

  it("clipText cuts on a code-point boundary and reports the omitted bytes", () => {
    // "héllo" is 6 bytes; the 4-byte emoji would end past a cap of 8, so it is left whole.
    const clipped = clipText("héllo😀!", 8);
    expect(clipped.text).toBe("héllo");
    expect(byteLength(clipped.text)).toBe(6);
    expect(clipped.omittedBytes).toBe(5);
  });

  it("fenceBlock uses a fence longer than the body's backtick runs", () => {
    expect(fenceBlock("H", "plain")).toBe("H\n```\nplain\n```");
    expect(fenceBlock("H", "say ``` hi")).toBe("H\n````\nsay ``` hi\n````");
    expect(fenceBlock("H", "say ```` hi")).toBe("H\n`````\nsay ```` hi\n`````");
    // One trailing newline is folded into the closing fence, not left as a blank line.
    expect(fenceBlock("H", "x\n")).toBe("H\n```\nx\n```");
  });

  it("buildTerminalBlock states the shown size and the omitted tail", () => {
    const { block, shownBytes, omittedBytes } = buildTerminalBlock("a".repeat(SEND_CAP_BYTES + 4096), "last 100 lines");
    expect(shownBytes).toBe(SEND_CAP_BYTES);
    expect(omittedBytes).toBe(4096);
    expect(block).toContain("(4.0 KiB omitted)");
    // The header states what actually goes in, not the full size.
    expect(block.split("\n")[0]).toBe("Terminal output, last 100 lines, 16 KiB:");
    // The cut text, the omitted line and the closing fence are all inside the block.
    expect(block).toContain(`\n${"`".repeat(3)}\n`);
  });

  it("buildTerminalBlock's header for a short selection is exact", () => {
    const { block, shownBytes, omittedBytes } = buildTerminalBlock("hello", "selection");
    expect(block).toBe("Terminal output, selection, 5 B:\n```\nhello\n```");
    expect(shownBytes).toBe(5);
    expect(omittedBytes).toBe(0);
  });

  it("terminalChipLabel names the kind, counts the lines and states the shown size", () => {
    expect(terminalChipLabel("hello", "selection")).toBe("Terminal selection · 1 line · 5 B");
    expect(terminalChipLabel("one\ntwo", "last 100 lines")).toBe("Terminal output · 2 lines · 7 B");
  });

  it("terminalChipLabel says (cut) only when the cap dropped something", () => {
    expect(terminalChipLabel("hello", "selection")).not.toContain("(cut)");
    expect(terminalChipLabel(`${"a".repeat(SEND_CAP_BYTES)}extra`, "last 100 lines")).toMatch(/ \(cut\)$/);
  });

  it("fileRefChipLabel shows the basename, the line range (EN DASH) and the size", () => {
    expect(fileRefChipLabel({ path: "src/handler.go" }, 78)).toBe("handler.go · 78 B");
    expect(fileRefChipLabel({ path: "src/handler.go", line: 1, endLine: 4 }, 78)).toBe("handler.go L1–4 · 78 B");
    expect(fileRefChipLabel({ path: "src/handler.go", line: 3 }, 12)).toBe("handler.go L3 · 12 B");
    // An endLine equal to line is no range, and the dash is U+2013, not a hyphen.
    expect(fileRefChipLabel({ path: "a.go", line: 2, endLine: 2 }, 5)).toBe("a.go L2 · 5 B");
    expect(fileRefChipLabel({ path: "a.go", line: 1, endLine: 4 }, 5)).toContain("L1\u20134");
  });

  const terminalChip = (body: string): ContextChip => ({ localId: "chip-1", label: "x", kind: "terminal", body, blockLabel: "last 100 lines" });
  const fileChip: ContextChip = { localId: "chip-2", label: "x", kind: "file_ref", ref: { path: "a.go" } };

  it("appendChipBlocks returns the text unchanged with no terminal chips", () => {
    expect(appendChipBlocks("plain", [])).toBe("plain");
    expect(appendChipBlocks("plain", [fileChip])).toBe("plain");
  });

  it("appendChipBlocks appends one block per terminal chip, in order, after a blank line", () => {
    const out = appendChipBlocks("look at this", [terminalChip("one"), fileChip, terminalChip("two")]);
    const first = buildTerminalBlock("one", "last 100 lines").block;
    const second = buildTerminalBlock("two", "last 100 lines").block;
    expect(out).toBe(`look at this\n\n${first}\n\n${second}`);
  });

  it("appendChipBlocks trims the text's trailing newline once before the blank line", () => {
    const out = appendChipBlocks("prompt\n", [terminalChip("one")]);
    expect(out).toBe(`prompt\n\n${buildTerminalBlock("one", "last 100 lines").block}`);
  });

  it("appendChipBlocks fences a body that contains backticks longer than the run", () => {
    const out = appendChipBlocks("prompt", [terminalChip("say ``` hi")]);
    expect(out).toContain("\n````\nsay ``` hi\n````");
  });
});
