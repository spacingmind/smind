import { act } from "react";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { App } from "@/App";
import { WsClient } from "@/lib/ws-client";
import { FakeSocket } from "@/test/fake-socket";
import type { RunLogsResult, RunSummary, Task, TerminalSessionStatus, Workspace } from "@/lib/types";

const WORKSPACE: Workspace = {
  ID: 1,
  Path: "/tmp/ws",
  Title: "My Workspace",
  RoutingPolicy: "",
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-01T00:00:00Z",
};

const TASK: Task = {
  ID: 42,
  WorkspaceID: 1,
  SpaceID: null,
  Title: "Fix the bug",
  Status: "active",
  WorktreePath: "/tmp/a",
  Branch: "fix-bug",
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-01T00:00:00Z",
  ArchivedAt: null,
};

const TASK_A: Task = {
  ...TASK,
  ID: 1,
  Title: "Task A",
  Branch: "task-a",
};

const TASK_B: Task = {
  ...TASK,
  ID: 2,
  Title: "Task B",
  Branch: "task-b",
};

/** Flushes pending microtasks, wrapped in `act` so React commits any resulting state updates before the caller asserts -- same helper other component test files use. */
async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

/**
 * Fires the reconnect loop's pending backoff timer, wrapped in `act` --
 * unlike reconnect.test.ts (which asserts on the reconnect module in
 * isolation, so the *only* pending timer is ever its own), a full App
 * render tree has other unrelated pending timers too (React's own
 * scheduler, etc.), so advancing to just "the next" timer can fire one of
 * those instead of the reconnect backoff. Advancing by the full 10s max
 * backoff window fires everything due within it -- safe here because,
 * unlike the backoff-growth test in reconnect.test.ts, every reconnect in
 * this file's tests succeeds on the first attempt, so there's no retry
 * cascade to worry about.
 */
async function advanceReconnectTimer(): Promise<void> {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(10_000);
  });
}

/** Resolves socket's nth pending request for method with result, by inspecting what WsClient actually sent over the wire (App.test.tsx drives real WsClient instances, not FakeWsClient, since it needs genuine new-instance-per-reconnect semantics). */
function respond(socket: FakeSocket, method: string, result: unknown, index = 0): void {
  const matches = socket.sent.filter((e) => e.method === method);
  const env = matches[index];
  if (!env?.id) throw new Error(`no ${method} request #${index} sent yet (have ${matches.length})`);
  socket.emit({ id: env.id, result });
}

/** Resolves every request sent so far for method (later duplicate responses to an already-answered id are ignored by WsClient, so this is safe even as requests accumulate across task switches). */
function respondAll(socket: FakeSocket, method: string, result: unknown): void {
  for (const env of socket.sent.filter((e) => e.method === method)) {
    if (env.id) socket.emit({ id: env.id, result });
  }
}

/** Drives AppSidebar's workspace.list -> {space.list, task.list} sequence for a single workspace, plus useTaskAttention's initial run.list (empty). */
async function resolveSidebar(socket: FakeSocket, tasks: Task[] = [TASK]): Promise<void> {
  await flush();
  respond(socket, "workspace.list", [WORKSPACE]);
  await flush();
  respond(socket, "space.list", []);
  respond(socket, "task.list", tasks);
  // useTaskAttention fires run.list on connect; an empty run list keeps
  // these tests free of badge noise. TaskDetailPane's own run.list (on
  // selection) gets answered separately via respondAll.
  respond(socket, "run.list", []);
  await flush();
}

/** Clicks task's sidebar row specifically -- its title also appears as TaskDetailPane's h2 heading once selected, so a plain getByText matches both. */
function clickTaskRow(task: Task): void {
  fireEvent.click(screen.getAllByText(task.Title)[0]!);
}

/** Selects task's row, opens its Files tab, expands nothing, and clicks the README.md row -- the file-open flow the tab registry tests build on. */
async function openFileInTask(socket: FakeSocket, task: Task, content: string): Promise<void> {
  clickTaskRow(task);
  await flush();
  respondAll(socket, "run.list", []);
  await flush();

  // Radix's tab trigger needs DOM focus before its click activates a tab
  // (it activates on pointer-down-with-focus semantics); jsdom's
  // fireEvent.click doesn't focus first like a real browser click does.
  const filesTab = screen.getByRole("tab", { name: "Files" });
  filesTab.focus();
  fireEvent.click(filesTab);
  await flush();
  respondAll(socket, "file.list", [{ name: "README.md", isDir: false, size: 1 }]);
  await flush();
  fireEvent.click(screen.getByTestId("file-row"));
  await flush();
  respondAll(socket, "file.read", { content });
  await flush();
}

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

describe("App", () => {
  it("an initial connect failure shows the disconnected/error state (regression: don't retry silently)", async () => {
    const connect = vi.fn().mockRejectedValue(new Error("no daemon listening"));
    render(<App connect={connect} />);
    await flush();

    expect(screen.getByText(/disconnected: no daemon listening/i)).toBeInTheDocument();
    expect(connect).toHaveBeenCalledTimes(1);
  });

  it("an unexpected disconnect updates the header away from 'Connected to daemon', and a successful reconnect brings it back", async () => {
    const socket1 = new FakeSocket();
    const socket2 = new FakeSocket();
    const connect = vi.fn().mockResolvedValueOnce(new WsClient(socket1)).mockResolvedValueOnce(new WsClient(socket2));

    render(<App connect={connect} />);
    await flush();
    expect(screen.getByText("Connected to daemon")).toBeInTheDocument();

    socket1.emitClose();
    await flush();
    expect(screen.queryByText("Connected to daemon")).not.toBeInTheDocument();
    expect(screen.getByText(/reconnecting to daemon/i)).toBeInTheDocument();

    await advanceReconnectTimer();
    await flush();
    expect(connect).toHaveBeenCalledTimes(2);
    expect(screen.getByText("Connected to daemon")).toBeInTheDocument();
  });

  it("after a successful reconnect, AppSidebar re-fetches against the new client (proving it received a different client reference, not the same instance mutated in place)", async () => {
    const socket1 = new FakeSocket();
    const socket2 = new FakeSocket();
    const connect = vi.fn().mockResolvedValueOnce(new WsClient(socket1)).mockResolvedValueOnce(new WsClient(socket2));

    render(<App connect={connect} />);
    await resolveSidebar(socket1);
    expect(screen.getByText("Fix the bug")).toBeInTheDocument();

    expect(socket2.sent.some((e) => e.method === "workspace.list")).toBe(false);

    socket1.emitClose();
    await flush();
    await advanceReconnectTimer();
    await flush();

    // useWorkspaceTree's effect is keyed on the `client` reference -- if
    // AppSidebar had been handed back the *same* WsClient instance (just
    // reconnected in place), no new workspace.list would fire at all,
    // since nothing about the effect's dependency would have changed.
    expect(socket2.sent.some((e) => e.method === "workspace.list")).toBe(true);
    expect(socket1.sent.filter((e) => e.method === "workspace.list")).toHaveLength(1);
  });

  it("a task selected before disconnect remains selected after reconnect (selection state isn't thrown away, only data is refetched)", async () => {
    const socket1 = new FakeSocket();
    const socket2 = new FakeSocket();
    const connect = vi.fn().mockResolvedValueOnce(new WsClient(socket1)).mockResolvedValueOnce(new WsClient(socket2));

    render(<App connect={connect} />);
    await resolveSidebar(socket1);

    const row = screen.getByText("Fix the bug");
    fireEvent.click(row);
    await flush();

    // TaskDetailPane mounts for the selected task and issues its own
    // run.list against the pre-disconnect client.
    respond(socket1, "run.list", [], 1);
    await flush();
    expect(screen.getByRole("heading", { name: "Fix the bug" })).toBeInTheDocument();

    socket1.emitClose();
    await flush();
    await advanceReconnectTimer();
    await flush();

    // The task is still selected/rendered after reconnect, and its pane
    // re-fetched fresh against the new client rather than being unmounted.
    expect(screen.getByRole("heading", { name: "Fix the bug" })).toBeInTheDocument();
    expect(socket2.sent.some((e) => e.method === "run.list")).toBe(true);
  });

  it("the same file path opened in task A and task B yields two distinct tabs, both preserved across task switches", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket, [TASK_A, TASK_B]);

    await openFileInTask(socket, TASK_A, "# A");
    expect(screen.getByTestId("file-editor-path")).toHaveTextContent("README.md");
    expect(screen.getByRole("tab", { name: /README\.md/ })).toBeInTheDocument();

    // Switch to task B: fresh default tab set, no leaked file tab...
    clickTaskRow(TASK_B);
    await flush();
    respondAll(socket, "run.list", []);
    await flush();
    expect(screen.queryByRole("tab", { name: /README\.md/ })).not.toBeInTheDocument();

    // ...and the same path opens as B's own independent tab.
    await openFileInTask(socket, TASK_B, "# B");
    expect(screen.getByTestId("file-editor-path")).toHaveTextContent("README.md");

    // Switch back to A: its README.md tab survived, with A's content once
    // its FileEditorPane remounts (per-task scoping, ADR 0004).
    clickTaskRow(TASK_A);
    await flush();
    respondAll(socket, "run.list", []);
    await flush();

    const readmeTab = screen.getByRole("tab", { name: /README\.md/ });
    readmeTab.focus();
    fireEvent.click(readmeTab);
    await flush();
    respondAll(socket, "file.read", { content: "# A" });
    await flush();
    expect(screen.getByRole("tab", { name: /README\.md/ })).toBeInTheDocument();
  });

  it("closing a file tab removes only that tab, leaving the base tabs intact", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket, [TASK_A]);

    await openFileInTask(socket, TASK_A, "# A");

    fireEvent.click(screen.getByRole("button", { name: "Close README.md" }));
    await flush();

    expect(screen.queryByRole("tab", { name: /README\.md/ })).not.toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Chat" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Files" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Diff" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Terminal" })).toBeInTheDocument();
  });

  it("a task with an errored run shows an attention dot, and selecting the task clears it", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await flush();
    respond(socket, "workspace.list", [WORKSPACE]);
    await flush();
    respond(socket, "space.list", []);
    respond(socket, "task.list", [TASK_A, TASK_B]);
    await flush();

    const erroredRun: RunSummary = {
      ID: "run-1",
      TaskID: TASK_B.ID,
      Provider: "glm",
      Prompt: "do it",
      Status: "error",
      StartedAt: "2024-01-01T00:00:00Z",
      FinishedAt: "2024-01-01T00:01:00Z",
      StopReason: "",
      Err: "boom",
    };
    // run.list #0 is useTaskAttention's (fired on connect, before any selection).
    respond(socket, "run.list", [erroredRun]);
    await flush();

    expect(screen.getByTestId("task-attention")).toBeInTheDocument();

    // Selecting the task snapshots its terminal runs as seen -> dot clears.
    clickTaskRow(TASK_B);
    await flush();
    // run.list #1 is TaskDetailPane's own fetch for the selected task.
    respond(socket, "run.list", [], 1);
    await flush();

    expect(screen.queryByTestId("task-attention")).not.toBeInTheDocument();
  });

  it("a running run with an unresolved permission request shows an attention dot", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await flush();
    respond(socket, "workspace.list", [WORKSPACE]);
    await flush();
    respond(socket, "space.list", []);
    respond(socket, "task.list", [TASK_A, TASK_B]);
    respond(socket, "run.list", [
      {
        ID: "run-2",
        TaskID: TASK_A.ID,
        Provider: "glm",
        Prompt: "do it",
        Status: "running",
        StartedAt: "2024-01-01T00:00:00Z",
        FinishedAt: null,
        StopReason: "",
        Err: "",
      } satisfies RunSummary,
    ]);
    await flush();
    respond(socket, "run.logs", {
      runId: "run-2",
      status: "running",
      events: [{ type: "permission_request", requestId: "req-1", summary: "run a command" }],
    });
    await flush();

    expect(screen.getByTestId("task-attention")).toBeInTheDocument();
  });
});

/** Feeds one ADR-0005 notification down the socket (App holds a real WsClient per connection). */
function pushNotification(socket: FakeSocket, topic: string, payload: unknown): void {
  act(() => {
    socket.emit({ event: { topic, seq: 1, payload } } as never);
  });
}

describe("App live events", () => {
  it("a terminal run.status notification badges the task without any refetch", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket, [TASK_A, TASK_B]);

    const sentBefore = socket.sent.length;

    // Live terminal run for unseen task B -> immediate badge, no RPC.
    pushNotification(socket, "run.status", { runId: "run-live", taskId: TASK_B.ID, status: "done" });
    await flush();

    expect(screen.getByTestId("task-attention")).toBeInTheDocument();
    expect(socket.sent.length).toBe(sentBefore);
  });

  it("a permission.pending notification badges the task live", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket, [TASK_A, TASK_B]);

    pushNotification(socket, "permission.pending", {
      runId: "run-p",
      taskId: TASK_A.ID,
      requestId: "req-9",
      summary: "run a command",
      options: [],
    });
    await flush();

    expect(screen.getByTestId("task-attention")).toBeInTheDocument();
  });

  it("a task.status notification updates the sidebar row's status text live", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket, [TASK_A]);

    // TASK_A.Status is "active" from the fetch; the event says "done".
    expect(screen.getByText("active")).toBeInTheDocument();

    pushNotification(socket, "task.status", { taskId: TASK_A.ID, status: "done" });
    await flush();

    expect(screen.queryByText("active")).not.toBeInTheDocument();
    expect(screen.getByText("done")).toBeInTheDocument();
  });
});

describe("App splits (Item 6)", () => {
  it("the file explorer's \"Open to side\" action opens directly into a new side pane -- a genuinely different placement than the row's own \"prefer\" click", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket, [TASK_A]);

    clickTaskRow(TASK_A);
    await flush();
    respondAll(socket, "run.list", []);
    await flush();

    // No side pane yet -- proves this isn't just "prefer" collapsing to
    // primary because none exists.
    expect(screen.queryByTestId("side-pane")).not.toBeInTheDocument();

    const filesTab = screen.getByRole("tab", { name: "Files" });
    filesTab.focus();
    fireEvent.click(filesTab);
    await flush();
    respondAll(socket, "file.list", [{ name: "README.md", isDir: false, size: 1 }]);
    await flush();

    fireEvent.contextMenu(screen.getByTestId("file-row"));
    fireEvent.click(screen.getByTestId("file-menu-open-to-side"));
    await flush();
    respondAll(socket, "file.read", { content: "# A" });
    await flush();

    const sidePane = screen.getByTestId("side-pane");
    expect(within(sidePane).getByRole("tab", { name: /README\.md/ })).toBeInTheDocument();
    expect(screen.getByTestId("file-editor-path")).toHaveTextContent("README.md");

    // Primary keeps its own tabs -- the file did not also land there.
    const primaryPane = screen.getByTestId("primary-pane");
    expect(within(primaryPane).queryByRole("tab", { name: /README\.md/ })).not.toBeInTheDocument();
  });

  it("a tool-call's file click-through prefers an already-open side pane over primary", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket, [TASK_A]);

    clickTaskRow(TASK_A);
    await flush();
    // TaskDetailPane mounts for "Chat" (the default active tab) and fires
    // its own run.list here; left unanswered on purpose -- switching to
    // Files below unmounts it before it resolves, which use-run-timeline's
    // own cancellation guard already covers, and a fresh one is fetched
    // once Chat is reactivated below.

    const filesTab = screen.getByRole("tab", { name: "Files" });
    filesTab.focus();
    fireEvent.click(filesTab);
    await flush();
    respondAll(socket, "file.list", [{ name: "README.md", isDir: false, size: 1 }]);
    await flush();
    fireEvent.contextMenu(screen.getByTestId("file-row"));
    fireEvent.click(screen.getByTestId("file-menu-open-to-side"));
    await flush();
    respondAll(socket, "file.read", { content: "# A" });
    await flush();

    // Back to Chat -- a fresh TaskDetailPane mount, with a run carrying a
    // file-naming tool call.
    const chatTab = screen.getByRole("tab", { name: "Chat" });
    chatTab.focus();
    fireEvent.click(chatTab);
    await flush();

    const doneRun: RunSummary = {
      ID: "run-1",
      TaskID: TASK_A.ID,
      Provider: "claude-native",
      Prompt: "read the file",
      Status: "done",
      StartedAt: "2024-01-01T00:00:00Z",
      FinishedAt: "2024-01-01T00:00:05Z",
      StopReason: "end_turn",
      Err: "",
    };
    // run.list #0 is useTaskAttention's (on connect); #1 is the first,
    // abandoned "Chat" mount; #2 is this remount's.
    respond(socket, "run.list", [doneRun], 2);
    await flush();

    const logs: RunLogsResult = {
      runId: "run-1",
      status: "done",
      stopReason: "end_turn",
      events: [
        {
          type: "tool_call",
          toolCallId: "t1",
          toolName: "Read",
          status: "success",
          input: { file_path: "/tmp/a/internal/a.go" },
        },
      ],
    };
    respond(socket, "run.logs", logs);
    await flush();

    fireEvent.click(screen.getByTestId("tool-call-open-path"));
    await flush();
    respondAll(socket, "file.read", { content: "package internal" });
    await flush();

    const sidePane = screen.getByTestId("side-pane");
    expect(within(sidePane).getByRole("tab", { name: /a\.go/ })).toBeInTheDocument();
    expect(within(sidePane).getByRole("tab", { name: /README\.md/ })).toBeInTheDocument();

    const primaryPane = screen.getByTestId("primary-pane");
    expect(within(primaryPane).queryByRole("tab", { name: /a\.go/ })).not.toBeInTheDocument();
  });

  it("moving a terminal tab to the side pane detaches without stopping it -- no terminal.close, no second terminal.create -- the App-level scenario Item 6's side-dock commit (59617f8) promised in a follow-up commit that was never written", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket, [TASK_A]);

    clickTaskRow(TASK_A);
    await flush();
    respondAll(socket, "run.list", []);
    await flush();

    // The terminal tab force-mounts (Item 20) as soon as the task is
    // selected, independent of which tab is active.
    respond(socket, "terminal.list", []);
    await flush();
    respond(socket, "terminal.create", { terminalId: "term-1" });
    await flush();
    respond(socket, "terminal.attach", { terminalId: "term-1" });
    await flush();

    expect(socket.sent.filter((e) => e.method === "terminal.create")).toHaveLength(1);

    fireEvent.click(screen.getByRole("button", { name: "Open Terminal to the side" }));
    await flush();

    // The pane remounts fresh in the side dock's own <Tabs> root, which
    // re-runs TerminalPane's list-then-attach effect exactly like a
    // reconnect: it must find its own still-running session and reattach
    // to it, never spawn a fresh one.
    const relistedSessions: TerminalSessionStatus[] = [
      { ID: "term-1", TaskID: TASK_A.ID, StartedAt: "2024-01-01T00:00:00Z", Status: "running", ClosedAt: null },
    ];
    respond(socket, "terminal.list", relistedSessions, 1);
    await flush();
    respond(socket, "terminal.attach", { terminalId: "term-1" }, 1);
    await flush();

    expect(socket.sent.some((e) => e.method === "terminal.close")).toBe(false);
    expect(socket.sent.filter((e) => e.method === "terminal.create")).toHaveLength(1);

    const sidePane = screen.getByTestId("side-pane");
    expect(within(sidePane).getByRole("tab", { name: "Terminal" })).toBeInTheDocument();
  });
});
