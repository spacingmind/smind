import { act, fireEvent, screen } from "@testing-library/react";
import { renderWithClientProp as render } from "@/test/render-with-daemon";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { TaskLineageChips, TaskLineageHeader } from "@/components/task-lineage";
import { usageChipText } from "@/lib/task-lineage";
import { TaskDetailPane } from "@/components/task-detail";
import type { DaemonEvents } from "@/hooks/use-daemon-events";
import { FakeWsClient } from "@/test/fake-ws-client";
import type { TaskLineage } from "@/hooks/use-task-lineage";
import type { TaskTreeResult, TaskTreeNode } from "@/lib/task-lineage";
import type { Chat, RunLogsResult, Task } from "@/lib/types";

// The run-config IA plan persists RunConfigToolbar's state per task id --
// clear it so a leftover value from one test can't leak into the next.
afterEach(() => {
  window.localStorage.clear();
});

const TASK: Task = {
  ID: 2,
  WorkspaceID: 1,
  SpaceID: null,
  Title: "Task B",
  Status: "active",
  WorktreePath: "/tmp/b",
  Branch: "task-b",
  CreatedAt: "2026-10-11T00:00:00Z",
  UpdatedAt: "2026-10-11T00:00:00Z",
  ArchivedAt: null,
};

const CHAT: Chat = {
  ID: 10,
  TaskID: TASK.ID,
  Title: "Chat",
  Provider: null,
  AgentSession: null,
  CreatedAt: "2026-10-11T00:00:00Z",
  ArchivedAt: null,
};

function node(partial: Partial<TaskTreeNode> & { id: number }): TaskTreeNode {
  return {
    parentId: null,
    workspaceId: 1,
    title: `node ${partial.id}`,
    depth: 0,
    taskStatus: "active",
    tier: null,
    childCount: 0,
    run: { state: "idle", runningRunIds: [], pendingPermissions: 0 },
    counts: { running: 0, waitingPermission: 0, failed: 0, done: 0, idle: 0 },
    usage: {
      own: { runs: 0, inputTokens: null, outputTokens: null, cacheReadTokens: null, cacheWriteTokens: null, reasoningTokens: null, costUsd: null },
      subtree: { runs: 0, inputTokens: null, outputTokens: null, cacheReadTokens: null, cacheWriteTokens: null, reasoningTokens: null, costUsd: null },
      archivedDescendants: 0,
    },
    ...partial,
  };
}

const EMPTY_LINEAGE: TaskLineage = { parent: null, node: null, children: [] };

function usage(runs: number, over: Partial<TaskTreeResult["nodes"][number]["usage"]["subtree"]> = {}) {
  return { runs, inputTokens: null, outputTokens: null, cacheReadTokens: null, cacheWriteTokens: null, reasoningTokens: null, costUsd: null, ...over };
}

/** A minimal DaemonEvents fake: topic -> listeners, plus an emit helper. */
function fakeEvents(): DaemonEvents & { emit(topic: string, payload: unknown): void } {
  const listeners = new Map<string, Set<(payload: unknown) => void>>();
  return {
    subscribe(topic, listener) {
      let set = listeners.get(topic);
      if (!set) {
        set = new Set();
        listeners.set(topic, set);
      }
      set.add(listener);
      return () => set!.delete(listener);
    },
    emit(topic, payload) {
      for (const fn of listeners.get(topic) ?? []) fn(payload);
    },
  };
}

/** Flushes pending microtasks, wrapped in `act` so React commits before assertions. */
async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

/** Renders the pane and settles the background RPCs the header strip needs (task.tree, task.get parent, provider/profile lists, run.logs). */
async function renderPane(client: FakeWsClient, events?: ReturnType<typeof fakeEvents>, task: Task = TASK) {
  render(<TaskDetailPane client={client} task={task} chat={CHAT} isDefaultChat={true} events={events} />);
  await act(async () => {
    for (const call of client.calls) {
      if (call.method === "run.logs") call.resolve({ runId: "run-1", status: "done", events: [] } satisfies RunLogsResult);
      if (call.method === "provider.list") call.resolve({ providers: [] });
      if (call.method === "profile.list") call.resolve([]);
    }
  });
}

describe("TaskLineageHeader (unit)", () => {
  it("renders nothing for a task with neither parent nor children", () => {
    const { container } = render(<TaskLineageHeader lineage={EMPTY_LINEAGE} />);
    expect(container.firstChild).toBeNull();
  });

  it("usage rollup is omitted when nothing is reported and never shows 0", () => {
    // Descendants exist, but nothing reported any usage -- no chip at all.
    const lineage: TaskLineage = {
      parent: null,
      node: node({ id: 2, counts: { running: 1, waitingPermission: 0, failed: 0, done: 0, idle: 0 }, usage: { own: usage(0), subtree: usage(3), archivedDescendants: 0 } }),
      children: [node({ id: 3, parentId: 2, run: { state: "running", runningRunIds: ["r1"], pendingPermissions: 0 } })],
    };
    render(<TaskLineageHeader lineage={lineage} />);
    expect(screen.queryByTestId("task-tree-usage")).toBeNull();
  });

  it("usage rollup shows the subtree total and does not double count the task's own runs", () => {
    const own = { ...usage(1), inputTokens: 1000, outputTokens: 500, costUsd: 0.3 };
    const subtree = { ...usage(3), inputTokens: 5000, outputTokens: 2500, costUsd: 1.5 }; // already includes own
    const lineage: TaskLineage = {
      parent: null,
      node: node({
        id: 2,
        counts: { running: 1, waitingPermission: 0, failed: 0, done: 0, idle: 0 },
        usage: { own, subtree, archivedDescendants: 0 },
      }),
      children: [node({ id: 3, parentId: 2, run: { state: "running", runningRunIds: ["r1"], pendingPermissions: 0 } })],
    };
    render(<TaskLineageHeader lineage={lineage} />);
    const chip = screen.getByTestId("task-tree-usage");
    const doubled = { ...usage(4), inputTokens: 6000, outputTokens: 3000, costUsd: 1.8 }; // own + subtree: the bug
    expect(chip.textContent).toContain(usageChipText(subtree)!);
    expect(chip.textContent).not.toContain(usageChipText(doubled)!);
    expect(chip.getAttribute("title")).toContain(`own: ${usageChipText(own)}`);
    expect(chip.getAttribute("title")).toContain(`tree: ${usageChipText(subtree)}`);
  });

  it("the usage chip keeps its full text, with the tokens in a max-sm:hidden span (cost-only on narrow screens)", () => {
    const own = { ...usage(1), inputTokens: 1000, outputTokens: 500, costUsd: 0.3 };
    const subtree = { ...usage(3), inputTokens: 5000, outputTokens: 2500, costUsd: 1.5 };
    const lineage: TaskLineage = {
      parent: null,
      node: node({
        id: 2,
        counts: { running: 1, waitingPermission: 0, failed: 0, done: 0, idle: 0 },
        usage: { own, subtree, archivedDescendants: 0 },
      }),
      children: [],
    };
    render(<TaskLineageHeader lineage={lineage} />);
    const chip = screen.getByTestId("task-tree-usage");
    expect(chip.textContent).toBe(`Task ${usageChipText(subtree)}`);
    const tokens = screen.getByTestId("task-tree-usage-tokens");
    expect(tokens.className).toContain("max-sm:hidden");
    expect(tokens.textContent).toContain("7.5k tok");
    expect(tokens.textContent).not.toContain("$");
  });

  it("with no cost the usage chip keeps its tokens visible on narrow screens", () => {
    const own = { ...usage(1), inputTokens: 1000, outputTokens: 500, costUsd: null };
    const lineage: TaskLineage = {
      parent: null,
      node: node({
        id: 2,
        counts: { running: 1, waitingPermission: 0, failed: 0, done: 0, idle: 0 },
        usage: { own, subtree: own, archivedDescendants: 0 },
      }),
      children: [],
    };
    render(<TaskLineageHeader lineage={lineage} />);
    expect(screen.queryByTestId("task-tree-usage-tokens")).toBeNull();
    expect(screen.getByTestId("task-tree-usage").textContent).toContain("1.5k tok");
  });
});

describe("task lineage header (in the pane)", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("shows spawned-by breadcrumb and jumps to the parent", async () => {
    const client = new FakeWsClient();
    const onJumpToTask = vi.fn();
    await renderPane(client);
    act(() => {
      client.nth("task.tree").resolve({ nodes: [node({ id: 2 })], truncated: false } satisfies TaskTreeResult);
    });
    await flush();
    // ParentTaskID arrives with the task itself; re-render with it.
    await renderPane(client, undefined, { ...TASK, ParentTaskID: 1 });
    await act(async () => {
      for (const call of client.calls) {
        if (call.method === "task.get") call.resolve({ ...TASK, ID: 1, Title: "Task A" });
      }
    });
    await flush();

    const crumb = screen.getByTestId("task-spawned-by");
    expect(crumb.textContent).toContain("Spawned by Task A");
    fireEvent.click(crumb);
    expect(onJumpToTask).not.toHaveBeenCalled(); // prop not passed in renderPane
  });

  it("no breadcrumb for a root task", async () => {
    const client = new FakeWsClient();
    await renderPane(client);
    act(() => {
      client.nth("task.tree").resolve({ nodes: [node({ id: 2 })], truncated: false } satisfies TaskTreeResult);
    });
    await flush();
    expect(screen.queryByTestId("task-spawned-by")).toBeNull();
    expect(screen.queryByTestId("task-lineage-header")).toBeNull();
  });

  it("archived parent breadcrumb is inert", async () => {
    const client = new FakeWsClient();
    await renderPane(client, undefined, { ...TASK, ParentTaskID: 1 });
    act(() => {
      client.nth("task.tree").resolve({ nodes: [node({ id: 2 })], truncated: false } satisfies TaskTreeResult);
    });
    await act(async () => {
      for (const call of client.calls) {
        if (call.method === "task.get") call.resolve({ ...TASK, ID: 1, Title: "Task A", ArchivedAt: "2026-10-11T00:00:00Z" });
      }
    });
    await flush();

    const crumb = screen.getByTestId("task-spawned-by");
    expect(crumb.tagName).toBe("SPAN");
    expect(crumb.textContent).toBe("Task A (archived)");
  });

  it("children strip lists live children with status pills", async () => {
    const client = new FakeWsClient();
    await renderPane(client);
    act(() => {
      client.nth("task.tree").resolve({
        nodes: [
          node({ id: 2 }),
          node({ id: 3, parentId: 2, title: "Coder one", run: { state: "running", runningRunIds: ["r1"], pendingPermissions: 0 } }),
          node({ id: 4, parentId: 2, title: "Coder two", run: { state: "waiting_permission", runningRunIds: [], pendingPermissions: 1 } }),
        ],
        truncated: false,
      } satisfies TaskTreeResult);
    });
    await flush();

    const pills = screen.getAllByTestId("task-child-pill");
    expect(pills.map((p) => p.dataset.childId)).toEqual(["3", "4"]);
    expect(pills[0].dataset.childState).toBe("running");
    expect(pills[0].textContent).toContain("Coder one");
    expect(withinPill(pills[0]).querySelector('[data-status="running"]')).not.toBeNull();
    expect(pills[1].dataset.childState).toBe("waiting_permission");
  });

  it("clicking a child pill selects that child", async () => {
    const client = new FakeWsClient();
    const onJumpToTask = vi.fn();
    render(
      <TaskDetailPane
        client={client}
        task={TASK}
        chat={CHAT}
        isDefaultChat={true}
        onJumpToTask={onJumpToTask}
      />,
    );
    act(() => {
      client.nth("task.tree").resolve({ nodes: [node({ id: 2 }), node({ id: 3, parentId: 2, title: "Coder one" })], truncated: false } satisfies TaskTreeResult);
    });
    await flush();

    fireEvent.click(screen.getByTestId("task-child-pill"));
    expect(onJumpToTask).toHaveBeenCalledWith(3);
  });

  it("children strip updates when a child is created", async () => {
    const client = new FakeWsClient();
    const events = fakeEvents();
    await renderPane(client, events);
    act(() => {
      client.nth("task.tree").resolve({ nodes: [node({ id: 2 }), node({ id: 3, parentId: 2, title: "Coder one" })], truncated: false } satisfies TaskTreeResult);
    });
    await flush();
    expect(screen.getAllByTestId("task-child-pill")).toHaveLength(1);

    act(() => {
      events.emit("task.created", { task: { ...TASK, ID: 4, ParentTaskID: 2 } });
    });
    vi.advanceTimersByTime(200);
    await act(async () => {
      for (const call of client.calls) {
        if (call.method === "run.logs") call.resolve({ runId: "run-1", status: "done", events: [] } satisfies RunLogsResult);
      }
    });
    act(() => {
      client.nth("task.tree", 1).resolve({
        nodes: [node({ id: 2 }), node({ id: 3, parentId: 2, title: "Coder one" }), node({ id: 4, parentId: 2, title: "Coder two" })],
        truncated: false,
      } satisfies TaskTreeResult);
    });
    await flush();
    expect(screen.getAllByTestId("task-child-pill")).toHaveLength(2);
  });

  it("status summary counts running, needs you and done", async () => {
    const client = new FakeWsClient();
    await renderPane(client);
    act(() => {
      client.nth("task.tree").resolve({
        nodes: [
          node({
            id: 2,
            counts: { running: 2, waitingPermission: 1, failed: 0, done: 3, idle: 0 },
          }),
          node({ id: 3, parentId: 2 }),
        ],
        truncated: false,
      } satisfies TaskTreeResult);
    });
    await flush();
    expect(screen.getByTestId("task-tree-summary").textContent).toBe("2 running · 1 needs you · 3 done");
  });

  it("one meta row: the task total and lineage chips share the chat meta row, with no separate bordered strip", async () => {
    const client = new FakeWsClient();
    await renderPane(client);
    act(() => {
      client.nth("task.tree").resolve({
        nodes: [
          node({
            id: 2,
            counts: { running: 1, waitingPermission: 0, failed: 0, done: 0, idle: 0 },
            usage: { own: usage(1, { costUsd: 0.5, inputTokens: 100, outputTokens: 20 }), subtree: usage(2, { costUsd: 1.12, inputTokens: 500_000, outputTokens: 57_000 }), archivedDescendants: 0 },
          }),
          node({ id: 3, parentId: 2, title: "Coder one" }),
        ],
        truncated: false,
      } satisfies TaskTreeResult);
    });
    await flush();

    const row = screen.getByTestId("task-chat-meta");
    expect(row).toContainElement(screen.getByTestId("task-tree-usage"));
    expect(row).toContainElement(screen.getByTestId("task-tree-summary"));
    expect(row).toContainElement(screen.getByTestId("task-child-pill"));
    expect(screen.getByTestId("task-tree-usage").textContent).toMatch(/^Task \$1\.12 · /);
    expect(screen.queryByTestId("task-lineage-header")).toBeNull();
    expect(row.className).not.toMatch(/(^|\s)border(-b)?(\s|$)/);
  });

  it("usage rollup is omitted when nothing is reported and never shows 0", async () => {
    const client = new FakeWsClient();
    await renderPane(client);
    act(() => {
      client.nth("task.tree").resolve({
        nodes: [node({ id: 2, usage: { own: usage(1), subtree: usage(2), archivedDescendants: 0 } }), node({ id: 3, parentId: 2 })],
        truncated: false,
      } satisfies TaskTreeResult);
    });
    await flush();
    expect(screen.queryByTestId("task-tree-usage")).toBeNull();
  });
});

function withinPill(pill: HTMLElement): HTMLElement {
  return pill;
}

describe("TaskLineageHeader rate limit (ADR-0041 R3)", () => {
  const limitedRun = { state: "error" as const, runningRunIds: [], pendingPermissions: 0, failureKind: "rate_limited", window: "5h" };

  it("hints a rate-limited child on its pill but does not repeat the task's own reset (its run footer owns it)", () => {
    const reset = new Date(Date.now() + 2 * 3_600_000).toISOString();
    const lineage: TaskLineage = {
      parent: null,
      node: node({ id: 2, run: { ...limitedRun, resetAt: reset } }),
      children: [
        node({ id: 3, parentId: 2, title: "Limited", run: { ...limitedRun, resetAt: reset } }),
        node({ id: 4, parentId: 2, title: "Broken", run: { state: "error", runningRunIds: [], pendingPermissions: 0 } }),
      ],
    };
    render(<TaskLineageHeader lineage={lineage} />);
    expect(screen.queryByTestId("task-rate-limit")).toBeNull();
    const [limited, broken] = screen.getAllByTestId("task-child-pill");
    expect(limited.dataset.childUrgency).toBe("warning");
    expect(limited.title).toMatch(/^Rate limited · resets /);
    expect(broken.dataset.childUrgency).toBe("danger");
    expect(broken.title).toBe("");
  });

  it("a plain node renders no hint", () => {
    render(<TaskLineageHeader lineage={{ parent: null, node: node({ id: 2, usage: { ...node({ id: 2 }).usage, subtree: usage(1, { costUsd: 0.1 }) } }), children: [] }} />);
    expect(screen.queryByTestId("task-rate-limit")).toBeNull();
  });
});

describe("TaskLineageHeader stalled marker (stalled-tree alert AC7)", () => {
  it("renders the stalled marker with tooltip", () => {
    const since = new Date(Date.now() - 20 * 60_000 - 5_000).toISOString();
    const lineage: TaskLineage = {
      parent: null,
      node: node({ id: 2, stalled: true, stalledSince: since }),
      children: [node({ id: 3, parentId: 2, title: "Kid", stalled: true, stalledSince: since })],
    };
    render(<TaskLineageHeader lineage={lineage} />);
    const marker = screen.getByTestId("task-stalled");
    expect(marker.textContent).toBe("stalled");
    expect(marker.title).toBe("No run for 20 min");
    expect(marker.className).toContain("text-warning");
    const pill = screen.getByTestId("task-child-pill");
    expect(pill.dataset.childStalled).toBe("true");
    expect(pill.title).toBe("No run for 20 min");
  });

  it("omits the marker when not stalled", () => {
    render(<TaskLineageHeader lineage={{ parent: null, node: node({ id: 2, usage: { ...node({ id: 2 }).usage, subtree: usage(1, { costUsd: 0.1 }) } }), children: [node({ id: 3, parentId: 2 })] }} />);
    expect(screen.queryByTestId("task-stalled")).toBeNull();
    expect(screen.getByTestId("task-child-pill").dataset.childStalled).toBeUndefined();
  });
});

describe("TaskLineage remote children (fleet F4)", () => {
  const remote = () => node({ id: -1, parentId: 2, title: "Remote work", remote: true, host: "box", hostId: "h1", globalId: "box:7" });

  it("TestTaskLineageRemoteNodeShowsHostChip", () => {
    const onJumpToTask = vi.fn();
    render(
      <TaskLineageHeader
        lineage={{ parent: null, node: node({ id: 2 }), children: [node({ id: 3, parentId: 2, title: "Local work" }), remote()] }}
        onJumpToTask={onJumpToTask}
      />,
    );
    const pills = screen.getAllByTestId("task-child-pill");
    expect(pills).toHaveLength(2);
    const remotePill = pills[1];
    expect(remotePill.dataset.remote).toBe("true");
    expect(screen.getByTestId("task-child-host-chip").textContent).toBe("box");
    expect(remotePill.title).toBe("box:7 (read only: owned by box)");
    fireEvent.click(remotePill);
    expect(onJumpToTask).not.toHaveBeenCalled();
    fireEvent.click(pills[0]);
    expect(onJumpToTask).toHaveBeenCalledWith(3);
  });

  it("TestRemoteTaskHeaderReadOnlyWithHostChip", () => {
    const onJumpToTask = vi.fn();
    const parent = node({
      id: 2,
      counts: { running: 1, waitingPermission: 0, failed: 0, done: 0, idle: 0 },
    });
    render(
      <div>
        <TaskLineageChips lineage={{ parent: null, node: parent, children: [remote()] }} onJumpToTask={onJumpToTask} />
      </div>,
    );
    expect(screen.getByTestId("task-tree-summary").textContent).toBe("1 running");
    const pill = screen.getByTestId("task-child-pill");
    expect(pill.tagName).toBe("SPAN");
    expect(pill.getAttribute("role")).toBeNull();
    expect(pill.dataset.remote).toBe("true");
    expect(screen.queryByRole("button")).toBeNull();
    fireEvent.click(pill);
    expect(onJumpToTask).not.toHaveBeenCalled();
  });

  it("TestRemoteChildPillsNotHiddenBelowSm", () => {
    render(
      <TaskLineageHeader
        lineage={{ parent: null, node: node({ id: 2 }), children: [node({ id: 3, parentId: 2, title: "Local work" }), remote()] }}
      />,
    );
    const [local, remotePill] = screen.getAllByTestId("task-child-pill");
    expect(local.parentElement?.className).toContain("max-sm:hidden");
    expect(remotePill.parentElement?.className).not.toContain("max-sm:hidden");
  });
});
