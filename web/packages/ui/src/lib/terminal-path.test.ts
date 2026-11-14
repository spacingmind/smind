import { describe, expect, it } from "vitest";

import { resolveTerminalPath } from "@/lib/terminal-path";

describe("resolveTerminalPath", () => {
  it("accepts a plain relative path", () => {
    expect(resolveTerminalPath("src/a.ts", "/work/t1")).toBe("src/a.ts");
  });

  it("drops ./ segments", () => {
    expect(resolveTerminalPath("./a/b.ts", "/work/t1")).toBe("a/b.ts");
  });

  it("refuses empty input", () => {
    expect(resolveTerminalPath("", "/work/t1")).toBeNull();
  });

  it("refuses any NUL byte", () => {
    expect(resolveTerminalPath("a\0b", "/work/t1")).toBeNull();
  });

  it("refuses a leading ..", () => {
    expect(resolveTerminalPath("../x", "/work/t1")).toBeNull();
  });

  it("refuses an interior .. even though it would stay inside", () => {
    expect(resolveTerminalPath("a/../b", "/work/t1")).toBeNull();
  });

  it("refuses ~", () => {
    expect(resolveTerminalPath("~/x", "/work/t1")).toBeNull();
  });

  it("refuses UNC and device forms", () => {
    expect(resolveTerminalPath("//server/share/x", "/work/t1")).toBeNull();
    expect(resolveTerminalPath("\\\\server\\share\\x", "/work/t1")).toBeNull();
    expect(resolveTerminalPath("\\\\.\\C:\\x", "/work/t1")).toBeNull();
    expect(resolveTerminalPath("\\\\?\\C:\\x", "/work/t1")).toBeNull();
  });

  it("refuses an absolute path outside the worktree", () => {
    expect(resolveTerminalPath("/etc/passwd", "/work/t1")).toBeNull();
  });

  it("makes an absolute path inside the worktree relative", () => {
    expect(resolveTerminalPath("/work/t1/src/a.ts", "/work/t1")).toBe("src/a.ts");
  });

  it("requires the prefix to end at a segment boundary", () => {
    expect(resolveTerminalPath("/work/t1evil/a.ts", "/work/t1")).toBeNull();
  });

  it("accepts a Windows drive-letter path against the same drive-letter worktree", () => {
    expect(resolveTerminalPath("C:\\work\\t1\\src\\a.ts", "C:\\work\\t1")).toBe("src/a.ts");
  });

  it("compares case-insensitively for a drive-letter worktree", () => {
    expect(resolveTerminalPath("c:/work/t1/a.ts", "C:\\work\\t1")).toBe("a.ts");
  });

  it("compares case-sensitively for a POSIX worktree", () => {
    expect(resolveTerminalPath("/Work/T1/a.ts", "/work/t1")).toBeNull();
  });

  it("refuses any absolute path without a worktree", () => {
    expect(resolveTerminalPath("/work/t1/a.ts", null)).toBeNull();
    expect(resolveTerminalPath("C:/work/t1/a.ts", null)).toBeNull();
  });

  it("refuses what normalises to nothing", () => {
    expect(resolveTerminalPath(".", "/work/t1")).toBeNull();
    expect(resolveTerminalPath("/", "/work/t1")).toBeNull();
  });
});
