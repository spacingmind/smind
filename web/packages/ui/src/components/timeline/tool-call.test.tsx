import { fireEvent, render, screen, within } from "@testing-library/react";
import { Hammer } from "lucide-react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { groupTimeline } from "@/components/timeline/detail-level";
import { RunTimeline } from "@/components/timeline/run-timeline";
import { ToolCall, worktreeRelativePath } from "@/components/timeline/tool-call";
import { countHits, inlineDiff, toolResultText } from "@/components/timeline/tool-call-detail";
import { registerToolRenderer, resolveToolRenderer, unregisterToolRenderer } from "@/components/timeline/tool-renderers";
import { groupStatus } from "@/components/timeline/tool-group-row";
import { buildTimeline, type RunEntry, type TimelineToolCallItem } from "@/hooks/use-run-timeline";
import type { RunLogEvent } from "@/lib/types";

function call(overrides: Partial<TimelineToolCallItem> = {}): TimelineToolCallItem {
  return { kind: "tool_call", id: "tool-0", toolCallId: "t1", status: "success", ...overrides };
}

function run(events: RunLogEvent[], overrides: Partial<RunEntry> = {}): RunEntry {
  return {
    id: "run-1",
    provider: "claude-native",
    prompt: "do the thing",
    status: "done",
    startedAt: "2024-01-01T00:00:00Z",
    finishedAt: "2024-01-01T00:00:02Z",
    items: buildTimeline(events),
    ...overrides,
  };
}

function expand(): void {
  fireEvent.click(screen.getByTestId("tool-call-toggle"));
}

describe("tool renderer registry", () => {
  afterEach(() => {
    unregisterToolRenderer("Frobnicate");
  });

  it("renders a registered tool through its own renderer, with no central switch to edit", () => {
    registerToolRenderer("Frobnicate", {
      intent: "terminal",
      icon: Hammer,
      label: "Frobnicator",
      summary: (input) => `frobbing ${String(input.target)}`,
    });

    render(<ToolCall item={call({ toolName: "Frobnicate", input: { target: "widget" } })} />);

    expect(screen.getByTestId("timeline-tool-call")).toHaveTextContent("Frobnicator");
    expect(screen.getByTestId("tool-call-summary")).toHaveTextContent("frobbing widget");
  });

  it("falls back to the generic card for an unregistered tool with an unrecognisable input shape", () => {
    render(<ToolCall item={call({ toolName: "Mystery", input: { whatsit: 3 } })} />);

    expect(resolveToolRenderer(call({ toolName: "Mystery", input: { whatsit: 3 } })).intent).toBe("generic");
    expand();
    expect(screen.getByTestId("tool-call-input")).toHaveTextContent('"whatsit": 3');
  });

  it("classifies an unregistered tool by the shape of its input", () => {
    expect(resolveToolRenderer(call({ toolName: "RunIt", input: { command: "ls" } })).intent).toBe("terminal");
    expect(resolveToolRenderer(call({ toolName: "Patch", input: { file_path: "/a", new_string: "x" } })).intent).toBe("edit");
    expect(resolveToolRenderer(call({ toolName: "Peek", input: { file_path: "/a" } })).intent).toBe("read");
    expect(resolveToolRenderer(call({ toolName: "Find", input: { pattern: "foo" } })).intent).toBe("search");
    expect(resolveToolRenderer(call({ toolName: "Get", input: { url: "https://x" } })).intent).toBe("fetch");
  });

  it("resolves both wire vocabularies onto the same intent", () => {
    // Claude's explicit names…
    expect(resolveToolRenderer(call({ toolName: "Bash" })).intent).toBe("terminal");
    expect(resolveToolRenderer(call({ toolName: "Read" })).intent).toBe("read");
    // …and ACP's ToolKind strings.
    expect(resolveToolRenderer(call({ toolName: "execute" })).intent).toBe("terminal");
    expect(resolveToolRenderer(call({ toolName: "edit" })).intent).toBe("edit");
    expect(resolveToolRenderer(call({ toolName: "search" })).intent).toBe("search");
  });
});

describe("intent card bodies", () => {
  it("terminal shows the command line and its output", () => {
    render(<ToolCall item={call({ toolName: "Bash", input: { command: "go test ./..." }, result: "ok  smind" })} />);
    expect(screen.getByTestId("tool-call-summary")).toHaveTextContent("go test ./...");
    expand();
    expect(screen.getByTestId("tool-detail-command")).toHaveTextContent("go test ./...");
    expect(screen.getByTestId("tool-detail-output")).toHaveTextContent("ok smind");
  });

  it("read shows the path and line range", () => {
    render(<ToolCall item={call({ toolName: "Read", input: { file_path: "/wt/a.go", offset: 10, limit: 5 } })} />);
    expect(screen.getByTestId("tool-call-summary")).toHaveTextContent("/wt/a.go:10-14");
  });

  it("edit shows an inline diff", () => {
    render(
      <ToolCall
        item={call({ toolName: "Edit", input: { file_path: "/wt/a.go", old_string: "before", new_string: "after" } })}
      />,
    );
    expand();
    const diff = screen.getByTestId("tool-detail-diff");
    expect(within(diff).getByText("-before")).toBeInTheDocument();
    expect(within(diff).getByText("+after")).toBeInTheDocument();
  });

  it("search shows the query and the hit count", () => {
    render(
      <ToolCall item={call({ toolName: "Grep", input: { pattern: "TODO", path: "internal" }, result: "a.go\nb.go\nc.go" })} />,
    );
    expect(screen.getByTestId("tool-call-summary")).toHaveTextContent("TODO in internal");
    expand();
    expect(screen.getByTestId("tool-detail-hits")).toHaveTextContent("3 hits");
  });
});

describe("tool call lifecycle", () => {
  it("updates the same card in place across running -> success and running -> failure", () => {
    const { rerender } = render(
      <ul>
        <RunTimeline
          run={run([{ type: "tool_call", toolCallId: "t1", toolName: "Bash", title: "go test", status: "running" }])}
        />
      </ul>,
    );
    expect(screen.getAllByTestId("timeline-tool-call")).toHaveLength(1);
    expect(screen.getByTestId("tool-call-status")).toHaveTextContent("running");

    for (const status of ["success", "failure"] as const) {
      rerender(
        <ul>
          <RunTimeline
            run={run([
              { type: "tool_call", toolCallId: "t1", toolName: "Bash", title: "go test", status: "running" },
              { type: "tool_call", toolCallId: "t1", status },
            ])}
          />
        </ul>,
      );
      // One card, not a second one -- merged by toolCallId.
      const cards = screen.getAllByTestId("timeline-tool-call");
      expect(cards).toHaveLength(1);
      expect(cards[0]).toHaveAttribute("data-tool-call-id", "t1");
      expect(screen.getByTestId("tool-call-status")).toHaveTextContent(status);
    }
  });
});

describe("file click-through", () => {
  it("opens the worktree-relative path of a card that names a file", () => {
    const onOpenFile = vi.fn();
    render(
      <ToolCall
        item={call({ toolName: "Read", input: { file_path: "/wt/task-1/internal/a.go" } })}
        worktreePath="/wt/task-1"
        onOpenFile={onOpenFile}
      />,
    );

    fireEvent.click(screen.getByTestId("tool-call-open-path"));
    expect(onOpenFile).toHaveBeenCalledWith("internal/a.go");
  });

  it("offers no click-through for a path outside the worktree, or a tool with no path", () => {
    const onOpenFile = vi.fn();
    const { rerender } = render(
      <ToolCall
        item={call({ toolName: "Read", input: { file_path: "/etc/passwd" } })}
        worktreePath="/wt/task-1"
        onOpenFile={onOpenFile}
      />,
    );
    expect(screen.queryByTestId("tool-call-open-path")).not.toBeInTheDocument();

    rerender(
      <ToolCall item={call({ toolName: "Bash", input: { command: "ls" } })} worktreePath="/wt/task-1" onOpenFile={onOpenFile} />,
    );
    expect(screen.queryByTestId("tool-call-open-path")).not.toBeInTheDocument();
  });

  it("worktreeRelativePath rejects traversal and sibling-prefix paths", () => {
    expect(worktreeRelativePath("/wt/task-1/a.go", "/wt/task-1")).toBe("a.go");
    expect(worktreeRelativePath("/wt/task-1/sub/b.go", "/wt/task-1/")).toBe("sub/b.go");
    expect(worktreeRelativePath("/wt/task-10/c.go", "/wt/task-1")).toBeNull();
    expect(worktreeRelativePath("/wt/task-1/../etc/passwd", "/wt/task-1")).toBeNull();
    expect(worktreeRelativePath("/wt/task-1", "/wt/task-1")).toBeNull();
  });
});

describe("detail level", () => {
  const threeCalls: RunLogEvent[] = [
    { type: "tool_call", toolCallId: "t1", toolName: "Bash", status: "success" },
    { type: "tool_call", toolCallId: "t2", toolName: "Read", status: "success" },
    { type: "tool_call", toolCallId: "t3", toolName: "Edit", status: "success" },
  ];

  it("collapses three consecutive tool calls into one row in overview, and restores them in detailed", () => {
    const { rerender } = render(
      <ul>
        <RunTimeline run={run(threeCalls)} detailLevel="overview" />
      </ul>,
    );

    expect(screen.queryAllByTestId("timeline-tool-call")).toHaveLength(0);
    const group = screen.getByTestId("timeline-tool-group");
    expect(group).toHaveAttribute("data-count", "3");
    expect(group).toHaveTextContent("3 tool calls");

    // Expanding the group reveals the individual cards.
    fireEvent.click(screen.getByTestId("tool-group-toggle"));
    expect(screen.getAllByTestId("timeline-tool-call")).toHaveLength(3);

    rerender(
      <ul>
        <RunTimeline run={run(threeCalls)} detailLevel="detailed" />
      </ul>,
    );
    expect(screen.queryByTestId("timeline-tool-group")).not.toBeInTheDocument();
    expect(screen.getAllByTestId("timeline-tool-call")).toHaveLength(3);
  });

  it("only groups consecutive calls, and leaves a lone call as its own card", () => {
    const items = buildTimeline([
      { type: "tool_call", toolCallId: "t1", toolName: "Bash", status: "success" },
      { type: "chunk", text: "thinking out loud" },
      { type: "tool_call", toolCallId: "t2", toolName: "Read", status: "success" },
      { type: "tool_call", toolCallId: "t3", toolName: "Edit", status: "success" },
    ]);

    const groups = groupTimeline(items, "overview");
    expect(groups.map((g) => g.kind)).toEqual(["item", "item", "tool-group"]);
  });

  it("a failed call in a collapsed group is not hidden behind a success signal", () => {
    expect(groupStatus([call({ status: "success" }), call({ status: "failure" })])).toBe("danger");
    expect(groupStatus([call({ status: "success" }), call({ status: "running" })])).toBe("running");
    expect(groupStatus([call({ status: "success" })])).toBe("success");
  });
});

describe("result and diff helpers", () => {
  it("pulls text out of the block shapes both providers wrap results in", () => {
    expect(toolResultText(call({ result: "plain" }))).toBe("plain");
    expect(toolResultText(call({ result: [{ type: "text", text: "claude shape" }] }))).toBe("claude shape");
    expect(toolResultText(call({ result: [{ type: "content", content: { type: "text", text: "acp shape" } }] }))).toBe(
      "acp shape",
    );
    // Anything else is still shown, as JSON, rather than swallowed.
    expect(toolResultText(call({ result: { code: 1 } }))).toContain('"code": 1');
  });

  it("counts hits from the shapes a search result actually uses", () => {
    expect(countHits("Found 12 files")).toBe(12);
    expect(countHits("No matches found")).toBe(0);
    expect(countHits("a.go\nb.go")).toBe(2);
    expect(countHits("   ")).toBeNull();
  });

  it("renders a write with no prior content as additions only", () => {
    expect(inlineDiff({ content: "new file\nline two" })).toEqual([
      { sign: "+", text: "new file" },
      { sign: "+", text: "line two" },
    ]);
    expect(inlineDiff({ whatsit: 1 })).toEqual([]);
  });
});
