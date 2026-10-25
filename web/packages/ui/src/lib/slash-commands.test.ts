import { describe, expect, it } from "vitest";

import type { PluginCommand } from "@/lib/plugin-contrib";
import { applySlashReplacement, buildSlashCommands, findActiveSlash, rankSlashCommands } from "@/lib/slash-commands";

const cmd = (plugin: string, name: string, prompt = `${plugin} ${name}`): PluginCommand => ({
  plugin,
  name,
  qualified: `${plugin}:${name}`,
  description: `${name} desc`,
  prompt,
});

describe("slash command list", () => {
  it("TestSlashCommands_BuiltinWinsCollision: a built-in keeps the bare name; the plugin command is /<plugin>:<name>", () => {
    const rows = buildSlashCommands([{ name: "review", description: "built-in review", text: "BUILTIN" }], [cmd("acme", "review"), cmd("acme", "plan")]);
    expect(rows.map((r) => r.label)).toEqual(["review", "acme:review", "plan"]);
    expect(rows[0]).toMatchObject({ builtin: true, text: "BUILTIN", source: "built-in" });
    expect(rows[1]).toMatchObject({ builtin: false, source: "acme", text: "acme review" });
    // The shadowed command is still reachable by its qualified name, and not by the bare one.
    expect(rankSlashCommands(rows, "acme:rev").map((r) => r.key)).toEqual(["plugin:acme:review"]);
    // "review" ranks the built-in (bare-name prefix) ahead of the plugin row that only contains it.
    expect(rankSlashCommands(rows, "review").map((r) => r.key)).toEqual(["builtin:review", "plugin:acme:review"]);
  });

  it("TestSlashCommands_FirstPluginByIdWinsBetweenPlugins (input order is irrelevant)", () => {
    const rows = buildSlashCommands([], [cmd("zeta", "review"), cmd("alpha", "review"), cmd("zeta", "only")]);
    expect(rows.map((r) => r.label)).toEqual(["review", "zeta:review", "only"]);
    expect(rows[0]!.source).toBe("alpha");
    expect(rows[1]!.source).toBe("zeta");
  });

  it("lists built-ins before plugin commands and nothing when there are none", () => {
    expect(buildSlashCommands([], [])).toEqual([]);
    const rows = buildSlashCommands([{ name: "b", description: "", text: "B" }], [cmd("a", "a")]);
    expect(rows.map((r) => r.builtin)).toEqual([true, false]);
  });

  it("ranks prefix before substring and an empty query lists everything", () => {
    const rows = buildSlashCommands([], [cmd("p", "review"), cmd("p", "preview"), cmd("p", "plan")]);
    expect(rankSlashCommands(rows, "").length).toBe(3);
    expect(rankSlashCommands(rows, "rev").map((r) => r.label)).toEqual(["review", "preview"]);
    expect(rankSlashCommands(rows, "zzz")).toEqual([]);
  });
});

describe("findActiveSlash", () => {
  it("opens only for a `/` that starts the text with the caret inside the token", () => {
    expect(findActiveSlash({ text: "/", cursorIndex: 1 })).toEqual({ start: 0, end: 1, query: "" });
    expect(findActiveSlash({ text: "/rev", cursorIndex: 4 })).toEqual({ start: 0, end: 4, query: "rev" });
    expect(findActiveSlash({ text: "/rev", cursorIndex: 2 })).toEqual({ start: 0, end: 2, query: "r" });
    expect(findActiveSlash({ text: "/rev now", cursorIndex: 8 })).toBeNull();
    expect(findActiveSlash({ text: "see /usr/bin", cursorIndex: 12 })).toBeNull();
    expect(findActiveSlash({ text: "http://x", cursorIndex: 8 })).toBeNull();
    expect(findActiveSlash({ text: "/rev", cursorIndex: 0 })).toBeNull();
    expect(findActiveSlash({ text: "", cursorIndex: 0 })).toBeNull();
  });
});

describe("applySlashReplacement", () => {
  it("replaces the token with the prompt and puts the caret right after it, keeping the rest", () => {
    const [row] = buildSlashCommands([], [cmd("a", "go", "Do the thing.")]);
    const slash = findActiveSlash({ text: "/g tail", cursorIndex: 2 })!;
    expect(applySlashReplacement({ text: "/g tail", slash, command: row! })).toEqual({ text: "Do the thing. tail", cursor: 13 });
  });
});
