import { describe, expect, it } from "vitest";

import { expandHome, normalizeProjectPath, parentOf, pathProblem, samePath } from "@/lib/project-path";

describe("normalizeProjectPath", () => {
  it("trims, collapses repeated separators and drops a trailing one, except on the root", () => {
    const table: [string, string][] = [
      ["  /a/b  ", "/a/b"],
      ["/a/b/", "/a/b"],
      ["//a//b/", "/a/b"],
      ["/a//b//c", "/a/b/c"],
      ["/", "/"],
      ["//", "/"],
      ["", ""],
      ["   ", ""],
      ["~", "~"],
      ["~/x", "~/x"],
      ["~/x/", "~/x"],
      ["~user", "~user"],
      ["a/b", "a/b"],
    ];
    for (const [input, want] of table) expect(normalizeProjectPath(input), input).toBe(want);
  });

  it("keeps case and treats a Windows path's backslashes as separators, keeping the drive root", () => {
    const table: [string, string][] = [
      ["/a/B/c/", "/a/B/c"],
      ["C:\\code\\x", "C:\\code\\x"],
      ["C:\\code\\x\\", "C:\\code\\x"],
      ["C:\\\\code", "C:\\code"],
      ["C://code//x/", "C:/code/x"],
      ["C:\\", "C:\\"],
      ["C:/", "C:/"],
    ];
    for (const [input, want] of table) expect(normalizeProjectPath(input), input).toBe(want);
  });
});

describe("expandHome", () => {
  it("expands ~ and ~/x to the given home and leaves any other string, including ~user, unchanged", () => {
    const home = "/home/daemon";
    const table: [string, string][] = [
      ["~", home],
      ["~/x", `${home}/x`],
      ["~/", `${home}/`],
      ["~user", "~user"],
      ["~user/x", "~user/x"],
      ["a/b", "a/b"],
      ["/x", "/x"],
      [" ~/x", " ~/x"],
    ];
    for (const [input, want] of table) expect(expandHome(input, home), input).toBe(want);
  });

  it("does not double the separator when the home is the filesystem root", () => {
    expect(expandHome("~", "/")).toBe("/");
    expect(expandHome("~/x", "/")).toBe("/x");
  });
});

describe("pathProblem", () => {
  it("reports empty for a blank path, relative for a non-absolute one, and null otherwise", () => {
    const table: [string, "empty" | "relative" | null][] = [
      ["", "empty"],
      ["   ", "empty"],
      ["/a/b", null],
      ["/", null],
      ["  /a/b  ", null],
      ["~", null],
      ["~/x", null],
      ["~user", null],
      ["a/b", "relative"],
      ["./a", "relative"],
      ["a", "relative"],
      ["C:", "relative"],
      ["C:\\code\\x", null],
      ["C:/code/x", null],
      ["C:\\", null],
      ["C:/", null],
    ];
    for (const [input, want] of table) expect(pathProblem(input), input).toBe(want);
  });
});

describe("parentOf", () => {
  it("returns the containing directory, with roots as their own parent", () => {
    const table: [string, string][] = [
      ["/a/b", "/a"],
      ["/a", "/"],
      ["/", "/"],
      ["/a/b/", "/a"],
      ["C:\\code\\x", "C:\\code"],
      ["C:\\code", "C:\\"],
      ["C:\\", "C:\\"],
      ["C:/code/x", "C:/code"],
      ["C:/code", "C:/"],
      ["~/x", "~"],
      ["~", "~"],
      ["a/b", "a"],
    ];
    for (const [input, want] of table) expect(parentOf(input), input).toBe(want);
  });
});

describe("samePath", () => {
  it("compares normalised, case-sensitive paths", () => {
    const table: [string, string, boolean][] = [
      ["/a/b/", "/a/b", true],
      ["/a//b", "/a/b/", true],
      ["  /a/b  ", "/a/b", true],
      ["~/x", "~/x/", true],
      ["C:\\code", "C:\\code\\", true],
      ["/a/b", "/a/B", false],
      ["/a", "/a/b", false],
    ];
    for (const [a, b, want] of table) expect(samePath(a, b), `${a} vs ${b}`).toBe(want);
  });
});
