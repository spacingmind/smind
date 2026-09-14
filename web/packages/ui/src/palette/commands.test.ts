import { describe, expect, it } from "vitest";

import {
  filterCommands,
  flattenSources,
  fuzzyScore,
  toRows,
  type Command,
  type CommandSource,
} from "@/palette/commands";

function cmd(id: string, group: string, title: string, extra: Partial<Command> = {}): Command {
  return { id, group, title, run: () => {}, ...extra };
}

function source(id: string, groupRank: number, commands: Command[]): CommandSource {
  return { id, groupRank, commands };
}

describe("flattenSources", () => {
  it("namespaces ids by source and orders by group rank", () => {
    const flat = flattenSources([
      source("b", 1, [cmd("x", "Actions", "Cycle theme")]),
      source("a", 0, [cmd("x", "Tasks", "Fix the bug")]),
    ]);
    expect(flat.map((c) => c.key)).toEqual(["a:x", "b:x"]);
    expect(flat.map((c) => c.group)).toEqual(["Tasks", "Actions"]);
  });

  it("drops a duplicate key rather than throwing", () => {
    const flat = flattenSources([
      source("a", 0, [cmd("x", "Tasks", "First"), cmd("x", "Tasks", "Second")]),
    ]);
    expect(flat.map((c) => c.title)).toEqual(["First"]);
  });
});

describe("fuzzyScore", () => {
  it("matches a subsequence, not just a substring", () => {
    expect(fuzzyScore("Open task", "tsk")).not.toBeNull();
    expect(fuzzyScore("app-sidebar.tsx", "apsb")).not.toBeNull();
    expect(fuzzyScore("Open task", "zzz")).toBeNull();
  });

  it("is case-insensitive and scores an empty query as neutral", () => {
    expect(fuzzyScore("Cycle Theme", "theme")).not.toBeNull();
    expect(fuzzyScore("anything", "")).toBe(0);
  });

  it("scores a contiguous match above a scattered one", () => {
    expect(fuzzyScore("diff viewer", "diff")!).toBeGreaterThan(fuzzyScore("do it for free", "diff")!);
  });

  it("scores a word-boundary match above a mid-word one", () => {
    expect(fuzzyScore("open terminal", "t")!).toBeGreaterThan(fuzzyScore("later", "t")!);
  });

  it("prefers the shorter of two otherwise equal matches", () => {
    expect(fuzzyScore("Diff", "diff")!).toBeGreaterThan(fuzzyScore("Diff viewer settings", "diff")!);
  });
});

describe("filterCommands", () => {
  const commands = flattenSources([
    source("t", 0, [
      cmd("1", "Tasks", "Fix the bug", { subtitle: "fix-bug" }),
      cmd("2", "Tasks", "Add telemetry", { subtitle: "telemetry" }),
    ]),
    source("a", 1, [
      cmd("theme", "Actions", "Cycle theme", { keywords: ["dark mode", "appearance"] }),
      cmd("ws", "Actions", "New workspace"),
    ]),
  ]);

  it("returns everything for an empty query", () => {
    expect(filterCommands(commands, "")).toHaveLength(4);
    expect(filterCommands(commands, "   ")).toHaveLength(4);
  });

  it("filters across every source", () => {
    const titles = filterCommands(commands, "the").map((c) => c.title);
    expect(titles).toContain("Fix the bug");
    expect(titles).toContain("Cycle theme");
    expect(titles).not.toContain("New workspace");
  });

  it("matches on a subtitle and on a keyword, not only the title", () => {
    expect(filterCommands(commands, "fix-bug").map((c) => c.id)).toEqual(["1"]);
    expect(filterCommands(commands, "dark mode").map((c) => c.id)).toEqual(["theme"]);
  });

  it("does not match a query that straddles two fields", () => {
    // "bugtel" is title-of-one + subtitle-of-another; per-field matching
    // is what stops that coincidence from being a hit.
    expect(filterCommands(commands, "bugtel")).toEqual([]);
  });

  it("keeps results grouped while ordering groups by relevance", () => {
    const result = filterCommands(commands, "e");
    const groups = result.map((c) => c.group);
    // Every member of a group is contiguous -- no interleaving, so the
    // rendered headings never alternate row by row.
    expect(new Set(groups).size).toBe(groups.filter((g, i) => g !== groups[i - 1]).length);
  });

  it("ranks a title match above a keyword match", () => {
    const result = filterCommands(commands, "theme");
    expect(result[0]!.title).toBe("Cycle theme");
  });
});

describe("toRows", () => {
  it("marks only the first row of each group run with its heading", () => {
    const rows = toRows(
      flattenSources([
        source("a", 0, [cmd("1", "Tasks", "One"), cmd("2", "Tasks", "Two")]),
        source("b", 1, [cmd("3", "Actions", "Three")]),
      ]),
    );
    expect(rows.map((r) => r.groupStart)).toEqual(["Tasks", null, "Actions"]);
  });

  it("produces one row per command, so a heading is never a navigable position", () => {
    const commands = flattenSources([
      source("a", 0, [cmd("1", "Tasks", "One")]),
      source("b", 1, [cmd("2", "Actions", "Two")]),
    ]);
    expect(toRows(commands)).toHaveLength(commands.length);
  });
});
