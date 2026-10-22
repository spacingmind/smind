import { describe, expect, it } from "vitest";

import { applyFileMentionReplacement, findActiveFileMention, mentionedPaths, rankPaths } from "@/lib/file-mention";

describe("findActiveFileMention", () => {
  const at = (text: string, cursorIndex = text.length) => findActiveFileMention({ text, cursorIndex });
  it.each([
    ["@", { start: 0, end: 1, query: "" }],
    ["@src/ma", { start: 0, end: 7, query: "src/ma" }],
    ["look at @src", { start: 8, end: 12, query: "src" }],
    ["line\n@x", { start: 5, end: 7, query: "x" }],
  ])("opens for %j", (text, range) => expect(at(text)).toEqual(range));
  it.each([["mail a@b"], ["a@"], ["say \"@x"], ["@a b"], ["no mention"], ["x@y.com"]])("stays closed for %j", (text) =>
    expect(at(text)).toBeNull(),
  );
  it("uses the caret, not the end of the text", () => {
    expect(at("@ab cd", 3)).toEqual({ start: 0, end: 3, query: "ab" });
  });
});

describe("applyFileMentionReplacement", () => {
  it("inserts @path and a space and puts the caret after it", () => {
    const text = "see @sr please";
    const mention = findActiveFileMention({ text, cursorIndex: 7 })!;
    expect(applyFileMentionReplacement({ text, mention, path: "src/a.go" })).toEqual({ text: "see @src/a.go please", cursor: 14 });
  });
  it("quotes a path with whitespace", () => {
    const mention = { start: 0, end: 1, query: "" };
    expect(applyFileMentionReplacement({ text: "@", mention, path: "my docs/a b.md" })).toEqual({ text: '@"my docs/a b.md" ', cursor: 18 });
  });
});

describe("mentionedPaths", () => {
  it("keeps only remembered paths still in the text", () => {
    expect(mentionedPaths("fix @src/a.go now", ["src/a.go", "src/b.go"])).toEqual(["src/a.go"]);
  });
  it("matches the quoted form and ignores lookalikes", () => {
    expect(mentionedPaths('@"a b/c.md" x', ["a b/c.md"])).toEqual(["a b/c.md"]);
    expect(mentionedPaths("@src/a.go.bak", ["src/a.go"])).toEqual([]);
    expect(mentionedPaths("x@src/a.go", ["src/a.go"])).toEqual([]);
    expect(mentionedPaths("@src/a.go, ok", ["src/a.go", "src/a.go"])).toEqual(["src/a.go"]);
  });
});

describe("rankPaths", () => {
  const paths = ["internal/server/main.go", "main.go", "README.md"];
  it("ranks by score then shorter path", () => {
    expect(rankPaths(paths, "main").map((r) => r.path)).toEqual(["main.go", "internal/server/main.go"]);
  });
  it("keeps the index order for an empty query and caps at 30", () => {
    const many = Array.from({ length: 50 }, (_, i) => `f${i}.ts`);
    const ranked = rankPaths(many, "");
    expect(ranked).toHaveLength(30);
    expect(ranked[0]!.path).toBe("f0.ts");
  });
});
