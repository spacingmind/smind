import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { RunTimeline } from "@/components/timeline/run-timeline";
import { buildTimeline, type RunEntry } from "@/hooks/use-run-timeline";
import type { RunLogEvent } from "@/lib/types";

function run(events: RunLogEvent[]): RunEntry {
  return {
    id: "run-1",
    provider: "glm",
    prompt: "do the thing",
    status: "done",
    startedAt: "2024-01-01T00:00:00Z",
    finishedAt: "2024-01-01T00:00:02Z",
    items: buildTimeline(events),
    approvalPolicy: "manual",
    thinkingLevel: "",
  };
}

describe("TimelineRow's unrecognised-event fallback", () => {
  it("shows the ACP kind, not the generic 'raw' wire type, for a raw event (ADR 0010)", () => {
    render(<RunTimeline run={run([{ type: "raw", kind: "plan", payload: { sessionUpdate: "plan" } } as RunLogEvent])} />);

    expect(screen.getByText("Unrecognised event: plan")).toBeInTheDocument();
    expect(screen.queryByText("Unrecognised event: raw")).not.toBeInTheDocument();
  });

  it("reveals the raw payload on expand", () => {
    render(<RunTimeline run={run([{ type: "raw", kind: "plan", payload: { sessionUpdate: "plan" } } as RunLogEvent])} />);

    fireEvent.click(screen.getByText("Unrecognised event: plan"));
    expect(screen.getByText(/"sessionUpdate": "plan"/)).toBeInTheDocument();
  });

  it("falls back to the wire type when there is no ACP kind to show", () => {
    render(<RunTimeline run={run([{ type: "todo_list", text: "future event kind" } as RunLogEvent])} />);

    expect(screen.getByText("Unrecognised event: todo_list")).toBeInTheDocument();
  });
});
