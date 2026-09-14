import { describe, expect, it } from "vitest";

import { appendTimelineEvent, buildTimeline, type TimelineItem, type TimelineToolCallItem } from "@/hooks/use-run-timeline";
import { formatElapsed, timelineToText } from "@/components/timeline/timeline-text";
import type { RunLogEvent } from "@/lib/types";

function toolItem(items: TimelineItem[], toolCallId: string): TimelineToolCallItem {
  const found = items.find((i) => i.kind === "tool_call" && i.toolCallId === toolCallId);
  if (!found || found.kind !== "tool_call") throw new Error(`no tool call ${toolCallId}`);
  return found;
}

describe("appendTimelineEvent", () => {
  it("coalesces consecutive same-role text into one item and splits on a role change", () => {
    const items = buildTimeline([
      { type: "chunk", text: "hello " },
      { type: "chunk", text: "world" },
      { type: "thinking", text: "hmm" },
      { type: "chunk", text: "back" },
    ]);

    expect(items.map((i) => i.kind)).toEqual(["assistant", "thinking", "assistant"]);
    expect(items[0]).toMatchObject({ kind: "assistant", text: "hello world" });
    expect(items[2]).toMatchObject({ kind: "assistant", text: "back" });
  });

  it("keeps every untouched item's object identity when appending", () => {
    const before = buildTimeline([
      { type: "chunk", text: "one" },
      { type: "thinking", text: "two" },
      { type: "chunk", text: "three" },
    ]);
    const after = appendTimelineEvent(before, { type: "chunk", text: " more" });

    // This identity guarantee is what makes TimelineRow's memo actually
    // skip work -- without it every row re-renders on every chunk.
    expect(after[0]).toBe(before[0]);
    expect(after[1]).toBe(before[1]);
    expect(after[2]).not.toBe(before[2]);
    expect(after[2]).toMatchObject({ text: "three more" });
    // The id is assigned once and survives the append.
    expect(after[2]!.id).toBe(before[2]!.id);
  });

  it("merges a tool call by toolCallId rather than replacing it", () => {
    let items = buildTimeline([
      { type: "tool_call", toolCallId: "t1", toolName: "Bash", title: "echo hi", status: "running", input: { command: "echo hi" } },
    ]);
    expect(items).toHaveLength(1);

    // The completion event carries status/result only -- ACP's
    // tool_call_update is a partial update (ADR 0008).
    items = appendTimelineEvent(items, { type: "tool_call", toolCallId: "t1", status: "success", result: { output: "hi" } });

    expect(items).toHaveLength(1);
    const call = toolItem(items, "t1");
    expect(call.status).toBe("success");
    expect(call.result).toEqual({ output: "hi" });
    // Identity fields survive the partial update.
    expect(call.toolName).toBe("Bash");
    expect(call.title).toBe("echo hi");
    expect(call.input).toEqual({ command: "echo hi" });
  });

  it("treats an ACP tool_call with no status as running", () => {
    const items = buildTimeline([{ type: "tool_call", toolCallId: "t1", toolName: "execute" }]);
    expect(toolItem(items, "t1").status).toBe("running");
  });

  it("does not throw on malformed or unknown events", () => {
    const malformed: RunLogEvent[] = [
      { type: "chunk" }, // no text
      { type: "chunk", text: "" }, // empty text
      { type: "tool_call" }, // no toolCallId
      { type: "done", stopReason: "end_turn" },
      { type: "permission_request", requestId: "r1", summary: "s", options: [] },
      { type: "permission_resolved", requestId: "r1", optionId: "o1" },
      { type: "todo_list", text: "future event kind" },
    ];

    const items = buildTimeline(malformed);
    // Only the unrecognised type becomes a row; the rest are ignored or
    // handled elsewhere.
    expect(items).toEqual([{ kind: "unknown", id: "unknown-0", eventType: "todo_list" }]);
  });

  it("folds 2000 chunk events into one item in linear time", () => {
    const events: RunLogEvent[] = Array.from({ length: 2000 }, (_, i) => ({ type: "chunk", text: `line ${i}\n` }));

    const started = performance.now();
    const items = buildTimeline(events);
    const elapsed = performance.now() - started;

    expect(items).toHaveLength(1);
    // Generous headroom: the point is to catch a quadratic regression
    // (2000 events copying a 2000-item array each time), not to benchmark.
    expect(elapsed).toBeLessThan(1000);
  });
});

describe("timelineToText", () => {
  it("renders the turn as readable plain text with tool calls as one-line markers", () => {
    const items = buildTimeline([
      { type: "user", text: "ignored" }, // not a wire type -> unknown row
      { type: "thinking", text: "let me look" },
      { type: "chunk", text: "Here is the answer." },
      { type: "tool_call", toolCallId: "t1", toolName: "Bash", title: "go test", status: "success" },
    ]);

    expect(timelineToText("run the tests", items)).toBe(
      ["> run the tests", "", "[unrecognised event: user]", "[thinking] let me look", "Here is the answer.", "[tool] Bash: go test (success)", ""].join("\n"),
    );
  });
});

describe("formatElapsed", () => {
  it("renders sub-minute durations in seconds and longer ones in minutes", () => {
    expect(formatElapsed("2024-01-01T00:00:00Z", "2024-01-01T00:00:01.400Z")).toBe("1.4s");
    expect(formatElapsed("2024-01-01T00:00:00Z", "2024-01-01T00:02:07Z")).toBe("2m 07s");
  });

  it("returns null rather than NaN for an unparseable timestamp", () => {
    expect(formatElapsed("not a date", "2024-01-01T00:00:00Z")).toBeNull();
  });
});
