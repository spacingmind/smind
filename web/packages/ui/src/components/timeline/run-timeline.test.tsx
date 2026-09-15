import { act } from "react";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { RunTimeline } from "@/components/timeline/run-timeline";
import { TimelineRow } from "@/components/timeline/timeline-row";
import { FOLLOW_THRESHOLD_PX, useAutoFollow } from "@/components/timeline/use-auto-follow";
import { buildTimeline, type RunEntry, type TimelineItem } from "@/hooks/use-run-timeline";
import type { RunLogEvent } from "@/lib/types";

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

describe("RunTimeline", () => {
  it("renders each event kind as its own row", () => {
    render(
      <ul>
        <RunTimeline
          run={run([
            { type: "user_message", text: "my turn" },
            { type: "thinking", text: "reasoning" },
            { type: "chunk", text: "the answer" },
            { type: "tool_call", toolCallId: "t1", toolName: "Bash", title: "go test", status: "success" },
            { type: "todo_list" },
          ])}
        />
      </ul>,
    );

    expect(screen.getByTestId("timeline-user")).toHaveTextContent("my turn");
    expect(screen.getByTestId("timeline-thinking")).toHaveTextContent("reasoning");
    expect(screen.getByTestId("timeline-assistant")).toHaveTextContent("the answer");
    expect(screen.getByTestId("timeline-tool-call")).toHaveTextContent("Bash");
    expect(screen.getByTestId("timeline-unknown")).toHaveTextContent("todo_list");
  });

  it.each([
    ["human", "You approved"],
    ["auto_safe", "Auto-approved"],
    ["timeout", "Timed out"],
  ] as const)("renders a resolved permission's reason %s as %s", (reason, label) => {
    render(
      <ul>
        <RunTimeline run={run([{ type: "permission_resolved", requestId: "r1", optionId: "allow-1", reason }])} />
      </ul>,
    );

    expect(screen.getByTestId("timeline-permission")).toHaveTextContent(label);
  });

  it("renders a resolved permission with no reason (an older server payload) without crashing, and no reason label", () => {
    expect(() =>
      render(
        <ul>
          <RunTimeline run={run([{ type: "permission_resolved", requestId: "r1", optionId: "allow-1" }])} />
        </ul>,
      ),
    ).not.toThrow();

    const row = screen.getByTestId("timeline-permission");
    expect(row).toHaveTextContent("Permission resolved");
    expect(row).not.toHaveTextContent("You approved");
    expect(row).not.toHaveTextContent("Auto-approved");
    expect(row).not.toHaveTextContent("Timed out");
  });

  it("renders assistant markdown as HTML, with a fenced block becoming pre > code", () => {
    render(
      <ul>
        <RunTimeline run={run([{ type: "chunk", text: "# Heading\n\n```go\nfmt.Println(1)\n```\n" }])} />
      </ul>,
    );

    const md = screen.getByTestId("timeline-markdown");
    expect(within(md).getByRole("heading", { level: 1 })).toHaveTextContent("Heading");
    const code = md.querySelector("pre > code");
    expect(code).not.toBeNull();
    expect(code).toHaveTextContent("fmt.Println(1)");
  });

  it("collapses reasoning by default", () => {
    render(
      <ul>
        <RunTimeline run={run([{ type: "thinking", text: "reasoning" }])} />
      </ul>,
    );
    const details = screen.getByTestId("timeline-thinking").querySelector("details");
    expect(details).not.toBeNull();
    expect(details).not.toHaveAttribute("open");
  });

  it("does not throw on an unrecognised event type and still renders the rows around it", () => {
    expect(() =>
      render(
        <ul>
          <RunTimeline
            run={run([{ type: "chunk", text: "before" }, { type: "from_the_future" }, { type: "chunk", text: "after" }])}
          />
        </ul>,
      ),
    ).not.toThrow();

    expect(screen.getAllByTestId("timeline-assistant")).toHaveLength(2);
    expect(screen.getByTestId("timeline-unknown")).toBeInTheDocument();
  });

  it("shows the turn's elapsed time and copies the turn as plain text", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });

    render(
      <ul>
        <RunTimeline run={run([{ type: "chunk", text: "the answer" }])} />
      </ul>,
    );

    expect(screen.getByTestId("run-elapsed")).toHaveTextContent("2.0s");

    await act(async () => {
      fireEvent.click(screen.getByTestId("run-copy-button"));
    });

    expect(writeText).toHaveBeenCalledWith("> do the thing\n\nthe answer\n");
    expect(screen.getByTestId("run-copy-button")).toHaveTextContent("Copied");
  });
});

describe("TimelineRow memoization", () => {
  /**
   * A render probe that doesn't require touching the component under
   * test: TimelineRow reads `item.text` while rendering an assistant row,
   * and memo's shallow prop compare only looks at the `item` *reference*,
   * never its properties. So counting getter hits counts renders exactly.
   */
  function probeItem(id: string, text: string): { item: TimelineItem; renders: () => number } {
    let reads = 0;
    const item = {
      kind: "assistant" as const,
      id,
      get text() {
        reads += 1;
        return text;
      },
    };
    return { item, renders: () => reads };
  }

  it("does not re-render an earlier row when a chunk appends a new one", () => {
    const first = probeItem("assistant-0", "one");
    const second = probeItem("assistant-1", "two");

    function List({ items }: { items: TimelineItem[] }) {
      return (
        <ul>
          {items.map((item) => (
            <TimelineRow key={item.id} item={item} />
          ))}
        </ul>
      );
    }

    const { rerender } = render(<List items={[first.item]} />);
    expect(first.renders()).toBe(1);

    // Append a second row, passing the *same* object for the first --
    // exactly what appendTimelineEvent produces (see timeline-model.test).
    rerender(<List items={[first.item, second.item]} />);

    expect(second.renders()).toBe(1);
    expect(first.renders()).toBe(1); // memo bailed out
  });

  it("does re-render a row whose item object actually changed", () => {
    const first = probeItem("assistant-0", "one");
    const { rerender } = render(<TimelineRow item={first.item} />);
    expect(first.renders()).toBe(1);

    const changed = probeItem("assistant-0", "one and more");
    rerender(<TimelineRow item={changed.item} />);
    expect(changed.renders()).toBe(1);
  });
});

describe("RunTimeline memoization", () => {
  /** Same render-probe technique as TimelineRow's memo test above, one level up: RunTimeline reads `run.prompt` unconditionally while rendering, so counting getter hits counts renders exactly. */
  function probeRun(id: string, items: TimelineItem[]): { run: RunEntry; renders: () => number } {
    let reads = 0;
    const runEntry = {
      id,
      provider: "claude-native" as const,
      status: "done" as const,
      startedAt: "2024-01-01T00:00:00Z",
      get prompt() {
        reads += 1;
        return "do the thing";
      },
      items,
    };
    return { run: runEntry as RunEntry, renders: () => reads };
  }

  it("does not recompute an untouched run's timeline when a sibling run's items change", () => {
    const other = probeRun("run-other", buildTimeline([{ type: "chunk", text: "unrelated" }]));
    const active = probeRun("run-active", buildTimeline([{ type: "chunk", text: "a" }]));

    function List({ runs }: { runs: RunEntry[] }) {
      return (
        <ul>
          {runs.map((r) => (
            <RunTimeline key={r.id} run={r} />
          ))}
        </ul>
      );
    }

    const { rerender } = render(<List runs={[other.run, active.run]} />);
    expect(other.renders()).toBe(1);

    // Simulate use-run-timeline.ts's appendEvent: only the touched run gets
    // a new object, exactly as `{ ...r, items }` does there.
    const updatedActive = { ...active.run, items: buildTimeline([{ type: "chunk", text: "ab" }]) };
    rerender(<List runs={[other.run, updatedActive]} />);

    expect(other.renders()).toBe(1); // still 1 -- RunTimeline's own memo bailed
  });
});

describe("useAutoFollow", () => {
  function Harness({ revision }: { revision: number }) {
    const follow = useAutoFollow<HTMLDivElement>(revision);
    return (
      <>
        <div ref={follow.ref} onScroll={follow.onScroll} data-testid="scroller" data-following={follow.following} />
        {!follow.following && (
          <button type="button" data-testid="jump" onClick={follow.jumpToLatest}>
            Jump to latest
          </button>
        )}
      </>
    );
  }

  /** jsdom has no layout, so the scroll geometry is stubbed directly. */
  function stub(el: HTMLElement, { scrollHeight, clientHeight }: { scrollHeight: number; clientHeight: number }): void {
    Object.defineProperty(el, "scrollHeight", { value: scrollHeight, configurable: true });
    Object.defineProperty(el, "clientHeight", { value: clientHeight, configurable: true });
  }

  it("sticks to the bottom while streaming, releases on a scroll up, and re-follows on jump", () => {
    const { rerender } = render(<Harness revision={0} />);
    const el = screen.getByTestId("scroller");
    stub(el, { scrollHeight: 1000, clientHeight: 400 });

    // New content while following pins the view to the tail.
    rerender(<Harness revision={1} />);
    expect(el.scrollTop).toBe(1000);
    expect(el).toHaveAttribute("data-following", "true");
    expect(screen.queryByTestId("jump")).not.toBeInTheDocument();

    // The user scrolls up; the jump affordance appears.
    el.scrollTop = 100;
    fireEvent.scroll(el);
    expect(el).toHaveAttribute("data-following", "false");
    expect(screen.getByTestId("jump")).toBeInTheDocument();

    // Further content must not move them.
    stub(el, { scrollHeight: 1400, clientHeight: 400 });
    rerender(<Harness revision={2} />);
    expect(el.scrollTop).toBe(100);

    fireEvent.click(screen.getByTestId("jump"));
    expect(el.scrollTop).toBe(1400);
    expect(el).toHaveAttribute("data-following", "true");
  });

  it("treats a near-bottom position within the threshold as still following", () => {
    render(<Harness revision={0} />);
    const el = screen.getByTestId("scroller");
    stub(el, { scrollHeight: 1000, clientHeight: 400 });

    el.scrollTop = 600 - FOLLOW_THRESHOLD_PX;
    fireEvent.scroll(el);
    expect(el).toHaveAttribute("data-following", "true");

    el.scrollTop = 600 - FOLLOW_THRESHOLD_PX - 1;
    fireEvent.scroll(el);
    expect(el).toHaveAttribute("data-following", "false");
  });
});
