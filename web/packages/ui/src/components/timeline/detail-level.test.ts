import { renderHook } from "@testing-library/react";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { afterEach, describe, expect, it } from "vitest";

import { detailLevelChangesView, groupTimeline, useDetailLevel } from "@/components/timeline/detail-level";
import { buildTimeline } from "@/hooks/use-run-timeline";
import type { RunLogEvent } from "@/lib/types";

describe("groupTimeline unknown rows", () => {
  const items = buildTimeline([
    { type: "chunk", text: "answer" } as RunLogEvent,
    { type: "raw", kind: "plan", payload: { sessionUpdate: "plan" } } as RunLogEvent,
    { type: "todo_list" } as unknown as RunLogEvent,
  ]);

  it("overview drops unknown rows, detailed keeps them", () => {
    expect(groupTimeline(items, "detailed").map((g) => (g.kind === "item" ? g.item.kind : g.kind))).toEqual([
      "assistant",
      "unknown",
      "unknown",
    ]);
    expect(groupTimeline(items, "overview").map((g) => (g.kind === "item" ? g.item.kind : g.kind))).toEqual(["assistant"]);
  });
});

describe("groupTimeline plan rows", () => {
  it("overview keeps plan items", () => {
    const items = buildTimeline([
      { type: "tool_call", toolCallId: "t1", toolName: "Bash", title: "a", status: "success" } as RunLogEvent,
      { type: "tool_call", toolCallId: "t2", toolName: "Bash", title: "b", status: "success" } as RunLogEvent,
      { type: "raw", kind: "plan", payload: { entries: [{ content: "Ship it", status: "in_progress", priority: "high" }] } } as RunLogEvent,
    ]);
    // A plan breaks a tool-call run like any non-tool item, and Overview keeps it.
    expect(groupTimeline(items, "overview").map((g) => (g.kind === "item" ? g.item.kind : g.kind))).toEqual(["tool-group", "plan"]);
  });
});

describe("detailLevelChangesView", () => {
  const tool = (id: string) => ({ type: "tool_call", toolCallId: id, toolName: "Bash", title: id, status: "success" }) as RunLogEvent;
  const run = (events: RunLogEvent[]) => ({ items: buildTimeline(events) });

  it("is false for no runs, plain chat and a single tool call", () => {
    expect(detailLevelChangesView([])).toBe(false);
    expect(detailLevelChangesView([run([{ type: "chunk", text: "hi" } as RunLogEvent])])).toBe(false);
    expect(detailLevelChangesView([run([tool("a")])])).toBe(false);
  });

  it("is true for a run of two tool calls or an unclassified event", () => {
    expect(detailLevelChangesView([run([tool("a"), tool("b")])])).toBe(true);
    expect(detailLevelChangesView([run([{ type: "todo_list" } as unknown as RunLogEvent])])).toBe(true);
  });
});

describe("auto detail level (chat surface decision 3)", () => {
  const tool = (id: string) => ({ type: "tool_call", toolCallId: id, toolName: "Bash", title: id, status: "success" }) as RunLogEvent;
  const shape = (events: RunLogEvent[], level: "auto" | "detailed" | "overview", finished: boolean) =>
    groupTimeline(buildTimeline(events), level, { finished }).map((g) => (g.kind === "item" ? g.item.kind : `${g.kind}:${g.items.length}`));

  afterEach(() => window.localStorage.clear());

  it("finished-run-collapses-three-or-more-tool-rows", () => {
    expect(shape([tool("a"), tool("b"), tool("c")], "auto", true)).toEqual(["tool-group:3"]);
    expect(shape([tool("a"), tool("b"), tool("c"), tool("d"), tool("e"), tool("f")], "auto", true)).toEqual(["tool-group:6"]);
    // two in a row stay readable inline
    expect(shape([tool("a"), tool("b")], "auto", true)).toEqual(["tool_call", "tool_call"]);
    // only CONSECUTIVE rows: a message between splits the run below the threshold
    expect(shape([tool("a"), tool("b"), { type: "chunk", text: "hi" } as RunLogEvent, tool("c")], "auto", true)).toEqual(["tool_call", "tool_call", "assistant", "tool_call"]);
  });

  it("most-detailed-level-never-collapses", () => {
    expect(shape([tool("a"), tool("b"), tool("c"), tool("d")], "detailed", true)).toEqual(["tool_call", "tool_call", "tool_call", "tool_call"]);
  });

  it("live-run-never-collapses", () => {
    expect(shape([tool("a"), tool("b"), tool("c"), tool("d")], "auto", false)).toEqual(["tool_call", "tool_call", "tool_call", "tool_call"]);
    // an unclassified event is shown in auto like in detailed (only the explicit overview hides it)
    expect(shape([{ type: "todo_list" } as unknown as RunLogEvent], "auto", true)).toEqual(["unknown"]);
  });

  it("the default level is auto, and a stored choice still wins", () => {
    expect(renderHook(() => useDetailLevel()).result.current[0]).toBe("auto");
    window.localStorage.setItem("smind:tool-call-detail-level", "detailed");
    expect(renderHook(() => useDetailLevel()).result.current[0]).toBe("detailed");
    window.localStorage.setItem("smind:tool-call-detail-level", "overview");
    expect(renderHook(() => useDetailLevel()).result.current[0]).toBe("overview");
  });

  it("find-open-never-collapses: task-detail turns auto into detailed while Find is open", () => {
    const src = readFileSync(resolve(__dirname, "../task-detail.tsx"), "utf8");
    expect(src).toContain('const renderedLevel = detailLevel === "auto" && chatFind.open ? "detailed" : detailLevel;');
    expect(src).toContain("detailLevel={renderedLevel}");
  });
});
