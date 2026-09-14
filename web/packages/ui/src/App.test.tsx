import { act } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { App } from "@/App";
import { SHORTCUT_BINDINGS } from "@/keyboard/shortcuts";
import { WsClient } from "@/lib/ws-client";
import { FakeSocket } from "@/test/fake-socket";
import type { RunSummary, Task, Workspace } from "@/lib/types";

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
  // Item 3's hash routing writes `window.location.hash`, and jsdom's
  // location survives across tests within a file -- without this, a
  // route left over from an earlier test would hijack the next test's
  // mount via its own initial-pendingRoute read.
  window.location.hash = "";
  // Item 3's tab persistence writes to localStorage keyed by task id, and
  // this file's tests reuse the same TASK_A/TASK_B ids (1/2) across many
  // cases -- without this, an earlier test's opened tab or active-tab
  // choice leaks into a later test's "freshly selected task" assumptions.
  window.localStorage.clear();
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

  it("an empty daemon (every list RPC resolving null) renders the sidebar empty state without throwing", async () => {
    // Regression for the fresh-install crash: workspace.list answered
    // literal null and useWorkspaceTree called .map on it. Backend now
    // guarantees [], but the UI must tolerate any daemon version.
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await flush();
    expect(screen.getByText("Connected to daemon")).toBeInTheDocument();

    respond(socket, "workspace.list", null);
    await flush();
    respond(socket, "run.list", null);
    await flush();

    expect(screen.getByText("Welcome to smind")).toBeInTheDocument();
  });
});

describe("App keyboard shortcuts", () => {
  /** Ctrl-based combos: jsdom's navigator is not a mac, so `Mod` resolves to Ctrl. */
  function pressCtrl(key: string, code: string, extra: Record<string, unknown> = {}): void {
    fireEvent.keyDown(document, { key, code, ctrlKey: true, ...extra });
  }

  it("the tab-close x activates on Enter and on Space, closing only that tab", async () => {
    for (const key of ["Enter", " "]) {
      const socket = new FakeSocket();
      const connect = vi.fn().mockResolvedValue(new WsClient(socket));
      const view = render(<App connect={connect} />);
      await resolveSidebar(socket, [TASK_A]);
      await openFileInTask(socket, TASK_A, "# A");

      const close = screen.getByRole("button", { name: "Close README.md" });
      expect(close).toHaveAttribute("tabindex", "0");
      close.focus();
      fireEvent.keyDown(close, { key });
      await flush();

      expect(screen.queryByRole("tab", { name: /README\.md/ })).not.toBeInTheDocument();
      for (const name of ["Chat", "Files", "Diff", "Terminal"]) {
        expect(screen.getByRole("tab", { name })).toBeInTheDocument();
      }
      view.unmount();
    }
  });

  it("Ctrl+W closes the active file tab but leaves a non-closable base tab alone", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket, [TASK_A]);
    await openFileInTask(socket, TASK_A, "# A");

    await act(async () => {
      pressCtrl("w", "KeyW");
    });
    await flush();
    expect(screen.queryByRole("tab", { name: /README\.md/ })).not.toBeInTheDocument();

    // Chat is active now and isn't closable -- the shortcut matches what
    // the strip offers, rather than being a stronger way to remove a tab.
    await act(async () => {
      pressCtrl("w", "KeyW");
    });
    await flush();
    expect(screen.getByRole("tab", { name: "Chat" })).toBeInTheDocument();
  });

  it("Ctrl+Alt+<digit> activates the tab at that position", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket, [TASK_A]);

    clickTaskRow(TASK_A);
    await flush();
    respondAll(socket, "run.list", []);
    await flush();
    expect(screen.getByRole("tab", { name: "Chat" })).toHaveAttribute("aria-selected", "true");

    // Default strip order is Chat, Files, Diff, Terminal.
    await act(async () => {
      pressCtrl("3", "Digit3", { altKey: true });
    });
    await flush();
    expect(screen.getByRole("tab", { name: "Diff" })).toHaveAttribute("aria-selected", "true");

    // A digit past the end of the strip is a no-op, not a crash.
    await act(async () => {
      pressCtrl("9", "Digit9", { altKey: true });
    });
    await flush();
    expect(screen.getByRole("tab", { name: "Diff" })).toHaveAttribute("aria-selected", "true");
  });

  it("Ctrl+] and Ctrl+[ step through tasks and wrap at both ends", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket, [TASK_A, TASK_B]);

    clickTaskRow(TASK_A);
    await flush();
    respondAll(socket, "run.list", []);
    await flush();

    async function step(key: string, code: string): Promise<void> {
      await act(async () => {
        pressCtrl(key, code);
      });
      await flush();
      respondAll(socket, "run.list", []);
      await flush();
    }

    await step("]", "BracketRight");
    expect(screen.getByRole("heading", { name: TASK_B.Title })).toBeInTheDocument();

    // Past the end wraps back to the first task.
    await step("]", "BracketRight");
    expect(screen.getByRole("heading", { name: TASK_A.Title })).toBeInTheDocument();

    // ...and backwards wraps the other way.
    await step("[", "BracketLeft");
    expect(screen.getByRole("heading", { name: TASK_B.Title })).toBeInTheDocument();
  });

  it("Ctrl+B toggles the sidebar through the registry (it moved out of the shadcn primitive's own listener)", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket);

    const sidebar = document.querySelector("[data-slot='sidebar']")!;
    expect(sidebar).toHaveAttribute("data-state", "expanded");

    await act(async () => {
      pressCtrl("b", "KeyB");
    });
    await flush();
    // One press, one toggle -- a double-toggle here would mean both the
    // registry and the removed primitive listener fired.
    expect(document.querySelector("[data-slot='sidebar']")).toHaveAttribute(
      "data-state",
      "collapsed",
    );

    await act(async () => {
      pressCtrl("b", "KeyB");
    });
    await flush();
    expect(document.querySelector("[data-slot='sidebar']")).toHaveAttribute(
      "data-state",
      "expanded",
    );
  });

  it("Shift+? opens the shortcuts dialog listing every binding", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket);

    expect(screen.queryByTestId("shortcuts-dialog")).not.toBeInTheDocument();
    await act(async () => {
      fireEvent.keyDown(document, { key: "?", code: "Slash", shiftKey: true });
    });
    await flush();

    expect(screen.getByTestId("shortcuts-dialog")).toBeInTheDocument();
    expect(screen.getAllByTestId("shortcut-row")).toHaveLength(SHORTCUT_BINDINGS.length);
  });
});

describe("App command palette", () => {
  function openPalette(): void {
    fireEvent.keyDown(document, { key: "k", code: "KeyK", ctrlKey: true });
  }

  function rowTitles(): string[] {
    return screen
      .queryAllByTestId("command-palette-row")
      .map((r) => r.querySelector("span span")?.textContent ?? "");
  }

  it("Ctrl+K opens a palette carrying every shell source: tasks, workspaces, tabs and actions", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket, [TASK_A, TASK_B]);

    await act(async () => {
      openPalette();
    });
    await flush();

    const titles = rowTitles();
    expect(titles).toContain(TASK_A.Title);
    expect(titles).toContain(TASK_B.Title);
    // A workspace lands on its first task; with no task selected there are
    // no tab entries yet, but the sidebar's own registered actions are there.
    expect(titles).toContain(WORKSPACE.Title);
    expect(titles).toContain("New workspace");
    expect(titles).toContain("Open accounts");
    expect(titles).toContain("Cycle theme");
  });

  it("running a task entry selects that task", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket, [TASK_A, TASK_B]);

    await act(async () => {
      openPalette();
    });
    await flush();

    const input = screen.getByTestId("command-palette-input");
    fireEvent.change(input, { target: { value: TASK_B.Title } });
    await act(async () => {
      fireEvent.keyDown(input, { key: "Enter" });
    });
    await flush();
    respondAll(socket, "run.list", []);
    await flush();

    expect(screen.queryByTestId("command-palette")).not.toBeInTheDocument();
    expect(screen.getByRole("heading", { name: TASK_B.Title })).toBeInTheDocument();
  });

  it("with a task selected, an Open <tab> entry activates that tab", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket, [TASK_A]);

    clickTaskRow(TASK_A);
    await flush();
    respondAll(socket, "run.list", []);
    await flush();
    expect(screen.getByRole("tab", { name: "Chat" })).toHaveAttribute("aria-selected", "true");

    await act(async () => {
      openPalette();
    });
    await flush();
    const input = screen.getByTestId("command-palette-input");
    fireEvent.change(input, { target: { value: "Open Diff" } });
    await act(async () => {
      fireEvent.keyDown(input, { key: "Enter" });
    });
    await flush();

    expect(screen.getByRole("tab", { name: "Diff" })).toHaveAttribute("aria-selected", "true");
  });

  it("lists the selected task's changed files, and opening one opens its editor tab", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket, [TASK_A]);

    clickTaskRow(TASK_A);
    await flush();
    respondAll(socket, "run.list", []);
    // ShellCommands' task.files fetch for the newly selected task.
    respondAll(socket, "task.files", {
      files: [{ path: "src/main.go", status: "modified", staged: false }],
    });
    await flush();

    await act(async () => {
      openPalette();
    });
    await flush();
    const input = screen.getByTestId("command-palette-input");
    fireEvent.change(input, { target: { value: "main.go" } });
    expect(rowTitles()).toContain("main.go");

    await act(async () => {
      fireEvent.keyDown(input, { key: "Enter" });
    });
    await flush();
    respondAll(socket, "file.read", { content: "package main" });
    await flush();

    expect(screen.getByRole("tab", { name: /main\.go/ })).toBeInTheDocument();
  });

  it("a task.files failure still leaves the palette usable", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket, [TASK_A]);

    clickTaskRow(TASK_A);
    await flush();
    respondAll(socket, "run.list", []);
    for (const env of socket.sent.filter((e) => e.method === "task.files")) {
      if (env.id) socket.emit({ id: env.id, error: { message: "no worktree" } });
    }
    await flush();

    await act(async () => {
      openPalette();
    });
    await flush();
    expect(screen.getByTestId("command-palette")).toBeInTheDocument();
    expect(rowTitles()).toContain(TASK_A.Title);
  });
});

describe("App routing", () => {
  /** Simulates the browser firing hashchange for a URL edit or back/forward -- jsdom's own auto-fire timing under fake timers isn't something a test should depend on. */
  function navigateHash(hash: string): void {
    window.location.hash = hash;
    window.dispatchEvent(new Event("hashchange"));
  }

  it("mounting at a task URL selects that task and its active tab", async () => {
    window.location.hash = `#/workspace/${WORKSPACE.ID}/task/${TASK_B.ID}/diff`;
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket, [TASK_A, TASK_B]);
    // The restore effect's own selectTask -> TaskDetailPane mount fetch.
    respondAll(socket, "run.list", []);
    await flush();

    // The route's tab is "diff", so Radix's own unmount-inactive-content
    // behavior means the Chat pane (and its task-title heading) isn't
    // rendered at all -- the sidebar's own active-row marker is what
    // stands in for "this task got selected" here.
    expect(
      document.querySelector(`[data-testid="sidebar-task-row"][data-task-id="${TASK_B.ID}"]`),
    ).toHaveAttribute("data-active", "true");
    expect(screen.getByRole("tab", { name: "Diff" })).toHaveAttribute("aria-selected", "true");
  });

  it("selecting a task updates the URL, and switching tabs updates it again", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket, [TASK_A]);

    clickTaskRow(TASK_A);
    await flush();
    respondAll(socket, "run.list", []);
    await flush();
    expect(window.location.hash).toBe(`#/workspace/${WORKSPACE.ID}/task/${TASK_A.ID}/task`);

    const diffTab = screen.getByRole("tab", { name: "Diff" });
    diffTab.focus();
    fireEvent.click(diffTab);
    await flush();
    expect(window.location.hash).toBe(`#/workspace/${WORKSPACE.ID}/task/${TASK_A.ID}/diff`);
  });

  it("a file tab's URL round-trips its path", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket, [TASK_A]);
    await openFileInTask(socket, TASK_A, "# A");

    expect(window.location.hash).toBe(
      `#/workspace/${WORKSPACE.ID}/task/${TASK_A.ID}/file/README.md`,
    );
  });

  it("back (a hashchange to an earlier URL) re-selects that task", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket, [TASK_A, TASK_B]);

    clickTaskRow(TASK_A);
    await flush();
    respondAll(socket, "run.list", []);
    await flush();
    const taskAHash = window.location.hash;

    clickTaskRow(TASK_B);
    await flush();
    respondAll(socket, "run.list", []);
    await flush();
    expect(screen.getByRole("heading", { name: TASK_B.Title })).toBeInTheDocument();

    await act(async () => {
      navigateHash(taskAHash);
    });
    await flush();
    respondAll(socket, "run.list", []);
    await flush();

    expect(screen.getByRole("heading", { name: TASK_A.Title })).toBeInTheDocument();
  });

  it("a URL naming a task task.list doesn't return lands on the empty state without throwing", async () => {
    window.location.hash = `#/workspace/${WORKSPACE.ID}/task/999/task`;
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    expect(() => render(<App connect={connect} />)).not.toThrow();
    await resolveSidebar(socket, [TASK_A]);
    await flush();

    expect(screen.getByTestId("app-empty-state")).toBeInTheDocument();
  });

  it("opening two file tabs, remounting the app, restores both and the active one", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    const first = render(<App connect={connect} />);
    await resolveSidebar(socket, [TASK_A]);

    clickTaskRow(TASK_A);
    await flush();
    respondAll(socket, "run.list", []);
    await flush();

    // Opening a file switches the active tab away from Files (Radix
    // unmounts the inactive pane), so Files has to be reselected before
    // each pick -- refetching file.list each time, same as a real remount.
    for (const name of ["a.md", "b.md"]) {
      const filesTab = screen.getByRole("tab", { name: "Files" });
      // Radix activates a tab on mousedown (or on focus, in its default
      // "automatic" mode) -- not on click. `.focus()` alone is a no-op
      // the second time around here, since the Files trigger is already
      // `document.activeElement` from the first iteration (nothing else
      // in this flow steals it), so mousedown is the one that reliably
      // reactivates it regardless of where focus currently sits.
      fireEvent.mouseDown(filesTab, { button: 0 });
      await flush();
      respondAll(socket, "file.list", [
        { name: "a.md", isDir: false, size: 1 },
        { name: "b.md", isDir: false, size: 1 },
      ]);
      await flush();
      fireEvent.click(document.querySelector(`[data-testid="file-row"][data-path="${name}"]`)!);
      await flush();
      respondAll(socket, "file.read", { content: `# ${name}` });
      await flush();
    }
    expect(screen.getByRole("tab", { name: /b\.md/ })).toHaveAttribute("aria-selected", "true");
    first.unmount();

    // A fresh mount, socket and connect -- as a real reload produces --
    // against the same localStorage and location.hash.
    const socket2 = new FakeSocket();
    const connect2 = vi.fn().mockResolvedValue(new WsClient(socket2));
    render(<App connect={connect2} />);
    await resolveSidebar(socket2, [TASK_A]);
    respondAll(socket2, "run.list", []);
    await flush();
    respondAll(socket2, "file.read", { content: "# b.md" });
    await flush();

    expect(screen.getByRole("tab", { name: /a\.md/ })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: /b\.md/ })).toHaveAttribute("aria-selected", "true");
  });
});

describe("App sidebar resize", () => {
  afterEach(() => {
    window.localStorage.clear();
  });

  it("renders a drag handle between the sidebar and the content", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket);

    expect(screen.getByTestId("sidebar-resize-handle")).toBeInTheDocument();
  });

  it("a fresh mount reads the sidebar width back from persistence (simulating a reload after a previous resize)", async () => {
    // Simulates "the user dragged the sidebar to 300px, then reloaded" --
    // useSidebarWidth (see its own dedicated test for the clamp/persist
    // logic in isolation) reads this back instead of the default on mount.
    window.localStorage.setItem("smind:sidebar-width", "300");

    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    const { container } = render(<App connect={connect} />);
    await resolveSidebar(socket);

    const wrapper = container.querySelector('[data-slot="sidebar-wrapper"]') as HTMLElement | null;
    expect(wrapper).not.toBeNull();
    expect(wrapper!.style.getPropertyValue("--sidebar-width")).toBe("300px");
  });

  it("a persisted width past the max bound still clamps on read-back, never rendering wider than SIDEBAR_MAX_WIDTH", async () => {
    window.localStorage.setItem("smind:sidebar-width", "999999");

    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    const { container } = render(<App connect={connect} />);
    await resolveSidebar(socket);

    const wrapper = container.querySelector('[data-slot="sidebar-wrapper"]') as HTMLElement | null;
    expect(wrapper!.style.getPropertyValue("--sidebar-width")).toBe("512px");
  });
});
