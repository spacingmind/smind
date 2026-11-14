import { describe, expect, it } from "vitest";

import { buildTerminalBlock, byteLength, clipText, fenceBlock, SEND_CAP_BYTES } from "@/lib/send-set";

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
});
