import { describe, expect, it } from "vitest";

import { detailLevelChangesView, groupTimeline } from "@/components/timeline/detail-level";
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
