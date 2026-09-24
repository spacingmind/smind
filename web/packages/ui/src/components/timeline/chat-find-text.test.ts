import { describe, expect, it } from "vitest";

import {
  buildFindPattern,
  countMatches,
  findMatches,
  nextMatchIndex,
  previousMatchIndex,
} from "@/components/timeline/chat-find-text";

describe("buildFindPattern", () => {
  it("returns null for an empty or whitespace-only query", () => {
    expect(buildFindPattern("")).toBeNull();
    expect(buildFindPattern("   ")).toBeNull();
  });

  it("is case-insensitive", () => {
    const pattern = buildFindPattern("Hello")!;
    expect(countMatches("say hello there", pattern)).toBe(1);
    expect(countMatches("say HELLO there", pattern)).toBe(1);
  });

  it("tolerates the query's whitespace matching a line-wrapped gap in the text", () => {
    const pattern = buildFindPattern("hello world")!;
    expect(countMatches("hello   world", pattern)).toBe(1);
    expect(countMatches("hello\nworld", pattern)).toBe(1);
  });

  it("escapes regex-special characters in the query", () => {
    const pattern = buildFindPattern("a.b*c")!;
    expect(countMatches("a.b*c is not axbyc", pattern)).toBe(1);
  });
});

describe("findMatches / countMatches", () => {
  it("counts every occurrence across a string", () => {
    const pattern = buildFindPattern("cat")!;
    expect(countMatches("cat cat cat", pattern)).toBe(3);
    expect(findMatches("cat cat cat", pattern)).toEqual([
      { start: 0, end: 3 },
      { start: 4, end: 7 },
      { start: 8, end: 11 },
    ]);
  });

  it("an empty query gives zero matches", () => {
    // buildFindPattern(" ") is null; callers treat "no pattern" as zero matches.
    expect(buildFindPattern("")).toBeNull();
  });

  it("gives zero matches when the query isn't present", () => {
    const pattern = buildFindPattern("xyz")!;
    expect(countMatches("no match here", pattern)).toBe(0);
  });
});

describe("nextMatchIndex / previousMatchIndex", () => {
  it("wraps forward from the last match to the first", () => {
    expect(nextMatchIndex(2, 3)).toBe(0);
    expect(nextMatchIndex(0, 3)).toBe(1);
  });

  it("wraps backward from the first match to the last", () => {
    expect(previousMatchIndex(0, 3)).toBe(2);
    expect(previousMatchIndex(2, 3)).toBe(1);
  });

  it("stays at 0 when there are no matches", () => {
    expect(nextMatchIndex(0, 0)).toBe(0);
    expect(previousMatchIndex(0, 0)).toBe(0);
  });
});
