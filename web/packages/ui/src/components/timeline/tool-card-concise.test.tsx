import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { RunTimeline } from "@/components/timeline/run-timeline";
import { ToolCall } from "@/components/timeline/tool-call";
import { buildTimeline, type RunEntry, type TimelineToolCallItem } from "@/hooks/use-run-timeline";
import type { RunLogEvent } from "@/lib/types";
import { useInterfaceModeStore } from "@/stores/interface-mode-store";

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
    permissionMode: "",
    autoAccept: false,
    thinkingLevel: "",
    ...overrides,
  };
}

const SECRET_COMMAND = "rm -rf build && make SECRET_FLAG=1";
const BASH = call({ toolName: "Bash", input: { command: SECRET_COMMAND }, result: "stdout line one\nstdout line two" });
const EDIT = call({
  toolCallId: "t2",
  toolName: "Edit",
  input: { file_path: "/work/report.md", old_string: "alpha", new_string: "bravo" },
});

function setMode(mode: "coding" | "office") {
  useInterfaceModeStore.getState().set(mode);
}

beforeEach(() => {
  window.localStorage.clear();
  useInterfaceModeStore.getState().reload();
});
afterEach(cleanup);

describe("concise tool cards (office mode)", () => {
  it("terminal_and_edit_hide_command_output_diff", () => {
    setMode("office");
    const { container } = render(
      <>
        <ToolCall item={BASH} />
        <ToolCall item={EDIT} />
      </>,
    );
    const text = container.textContent ?? "";
    expect(text).toContain("Ran a command");
    expect(text).toContain("Edited report.md");
    for (const hidden of [SECRET_COMMAND, "SECRET_FLAG", "stdout line", "alpha", "bravo"]) {
      expect(text).not.toContain(hidden);
    }
    expect(screen.queryByTestId("tool-call-toggle")).toBeNull();
    expect(container.querySelector("[aria-expanded]")).toBeNull();
    expect(screen.queryByTestId("tool-call-detail")).toBeNull();
    expect(screen.queryByTestId("tool-detail-diff")).toBeNull();
  });

  it("shows running, done and failed status", () => {
    setMode("office");
    for (const [status, label] of [
      ["running", "running"],
      ["success", "done"],
      ["failure", "failed"],
    ] as const) {
      const { unmount } = render(<ToolCall item={{ ...BASH, status }} />);
      expect(screen.getByTestId("tool-call-status")).toHaveTextContent(label);
      expect(screen.getByTestId("timeline-tool-call").getAttribute("data-status")).toBe(status);
      unmount();
    }
  });

  it("failed_shows_error_summary", () => {
    setMode("office");
    const failed = call({
      toolName: "Bash",
      status: "failure",
      input: { command: SECRET_COMMAND },
      result: "make: *** No rule to make target all. Stop.\nmore noise\nand more",
    });
    render(<ToolCall item={failed} />);
    expect(screen.getByTestId("tool-call-error-summary")).toHaveTextContent("make: *** No rule to make target all.");
    expect(screen.getByTestId("tool-call-error-summary")).not.toHaveTextContent("more noise");
    expect(screen.getByTestId("timeline-tool-call")).not.toHaveTextContent(SECRET_COMMAND);
  });

  it("a successful card has no error line, and the file link still opens the file", () => {
    setMode("office");
    const onOpenFile = vi.fn();
    render(<ToolCall item={EDIT} worktreePath="/work" onOpenFile={onOpenFile} />);
    expect(screen.queryByTestId("tool-call-error-summary")).toBeNull();
    fireEvent.click(screen.getByTestId("tool-call-open-path"));
    expect(onOpenFile).toHaveBeenCalledWith("report.md");
  });

  it("coding_mode_dom_unchanged", () => {
    // The default mode renders the full card: summary line is the command, expandable, detail has the diff.
    const { container } = render(
      <>
        <ToolCall item={BASH} />
        <ToolCall item={EDIT} />
      </>,
    );
    expect(container.querySelectorAll("[data-concise]").length).toBe(0);
    expect(container.querySelectorAll('[data-testid="tool-call-toggle"]').length).toBe(2);
    expect(container.textContent).toContain(SECRET_COMMAND);
    fireEvent.click(container.querySelectorAll('[data-testid="tool-call-toggle"]')[1]);
    expect(screen.getByTestId("tool-detail-diff")).toBeInTheDocument();
    // An explicit "coding" write is the same as the default.
    const before = container.innerHTML;
    cleanup();
    setMode("coding");
    const again = render(
      <>
        <ToolCall item={BASH} />
        <ToolCall item={EDIT} />
      </>,
    );
    fireEvent.click(again.container.querySelectorAll('[data-testid="tool-call-toggle"]')[1]);
    expect(again.container.innerHTML).toBe(before);
  });

  it("read_search_fetch_unchanged", () => {
    const items = [
      call({ toolName: "Read", input: { file_path: "/work/a.md" }, result: "file body" }),
      call({ toolCallId: "t3", toolName: "Grep", input: { pattern: "todo", path: "src" }, result: "a:1\nb:2" }),
      call({ toolCallId: "t4", toolName: "WebFetch", input: { url: "https://example.com" } }),
      call({ toolCallId: "t5", toolName: "mcp__docs__lookup", input: { whatsit: 1 } }),
    ];
    const html = (mode: "coding" | "office") => {
      setMode(mode);
      const out = render(
        <>
          {items.map((i) => (
            <ToolCall key={i.toolCallId} item={i} />
          ))}
        </>,
      );
      const h = out.container.innerHTML;
      out.unmount();
      return h;
    };
    expect(html("office")).toBe(html("coding"));
  });

  it("permission_and_error_cards_never_simplified", () => {
    const events: RunLogEvent[] = [
      { type: "tool_call", toolCallId: "t1", toolName: "Bash", title: "ls", status: "success" },
      { type: "permission_resolved", requestId: "r1", optionId: "allow-1", reason: "human" },
    ];
    const html = (mode: "coding" | "office") => {
      setMode(mode);
      const out = render(
        <ul>
          <RunTimeline run={run(events, { status: "error" })} />
        </ul>,
      );
      const row = out.container.querySelector('[data-testid="timeline-permission"]');
      const h = row?.outerHTML ?? "";
      out.unmount();
      return h;
    };
    const office = html("office");
    expect(office).toContain("You approved");
    expect(office).toBe(html("coding"));
  });

  it("DetailLevel grouping still renders tool calls in office mode", () => {
    setMode("office");
    render(
      <ul>
        <RunTimeline
          run={run([
            { type: "tool_call", toolCallId: "t1", toolName: "Bash", status: "success", input: { command: "a" } },
            { type: "tool_call", toolCallId: "t2", toolName: "Bash", status: "success", input: { command: "b" } },
          ])}
        />
      </ul>,
    );
    expect(screen.getAllByTestId("timeline-tool-call").length).toBeGreaterThan(0);
  });
});
