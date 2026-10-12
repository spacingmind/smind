import { describe, expect, it } from "vitest";

import { groupTimeline } from "@/components/timeline/detail-level";
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
