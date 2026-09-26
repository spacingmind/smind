import { act } from "react";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { App } from "@/App";
import { SHORTCUT_BINDINGS } from "@/keyboard/shortcuts";
import { WsClient } from "@/lib/ws-client";
import { FakeSocket } from "@/test/fake-socket";
import { resetTerminalSessions } from "@/lib/terminal-sessions";
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

const TASK_C: Task = {
  ...TASK,
  ID: 3,
  Title: "Task C",
  Branch: "task-c",
};

const TASK_D: Task = {
  ...TASK,
  ID: 4,
  Title: "Task D",
  Branch: "task-d",
};

const TASK_E: Task = {
  ...TASK,
  ID: 5,
  Title: "Task E",
  Branch: "task-e",
};

const TASK_F: Task = {
  ...TASK,
  ID: 6,
  Title: "Task F",
  Branch: "task-f",
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

/**
 * Opens `label`'s tab via the pane's "+" menu -- the real flow now that
 * only Chat is seeded on first visit (dogfood default-tabs fix; the other
 * three base kinds are one click away instead of pre-opened clutter).
 * Radix's DropdownMenuTrigger opens on pointerdown, same as
 * theme-toggle.test.tsx. The newly opened tab is already active (openTab
 * activates whatever it just opened), so callers don't need a separate
 * focus+click to select it.
 */
async function openBaseTab(label: "Files" | "Diff" | "Terminal"): Promise<void> {
  // Enter, not pointerdown -- Radix's DropdownMenuTrigger opens on either,
  // but under this file's fake timers a bare fireEvent.pointerDown never
  // flips data-state to "open" (unclear why; keyboard activation is
  // reliable and just as real a user path).
  const trigger = screen.getByTestId("tabs-new-tab");
  trigger.focus();
  fireEvent.keyDown(trigger, { key: "Enter" });
  await flush();
  fireEvent.click(screen.getByRole("menuitem", { name: `Open ${label}` }));
  await flush();
}

/**
 * Splits `title`'s tab to the right via the tab strip's own "Split" menu --
 * replaces the old single "Open to the side" button (Item 3's rewrite).
 * Same focus+Enter dance as `openBaseTab` for opening the trigger reliably
 * under this file's fake timers.
 */
async function splitTabRight(title: string): Promise<void> {
  const trigger = screen.getByRole("button", { name: `Split ${title}` });
  trigger.focus();
  fireEvent.keyDown(trigger, { key: "Enter" });
  await flush();
  fireEvent.click(screen.getByTestId("workspace-tab-split-right"));
  await flush();
}

/** Selects task's row, opens its Files tab, expands nothing, and clicks the README.md row -- the file-open flow the tab registry tests build on. */
async function openFileInTask(socket: FakeSocket, task: Task, content: string): Promise<void> {
  clickTaskRow(task);
  await flush();
  respondAll(socket, "run.list", []);
  await flush();

  await openBaseTab("Files");
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
  // The session<->tab binding (lib/terminal-sessions.ts, Item 20) lives
  // outside React and outside this file's per-test render tree -- without
  // clearing it, a later test opening a terminal tab for the same task id
  // would see it as already bound to a prior test's session and try to
  // reconnect instead of creating one (same reasoning as
  // terminal-pane.test.tsx's own afterEach).
  resetTerminalSessions();
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

  it("closing a file tab removes only that tab, leaving the other open base tabs intact", async () => {
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
      ApprovalPolicy: "manual",
      ThinkingLevel: "",
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
        ApprovalPolicy: "manual",
        ThinkingLevel: "",
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
      for (const name of ["Chat", "Files"]) {
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

    // Strip order after opening Diff via "+" (only Chat is seeded now):
    // Chat, Diff. Opening it also activates it, so switch back to Chat
    // first -- otherwise the assertion below would pass even if the
    // shortcut itself did nothing.
    await openBaseTab("Diff");
    const chatTab = screen.getByRole("tab", { name: "Chat" });
    chatTab.focus();
    fireEvent.click(chatTab);
    await flush();
    expect(screen.getByRole("tab", { name: "Chat" })).toHaveAttribute("aria-selected", "true");

    await act(async () => {
      pressCtrl("2", "Digit2", { altKey: true });
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

  it("Shift+? opens Settings on the Shortcuts section, listing every binding (AC5: the old dialog is now a Settings section)", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket);

    expect(screen.queryByTestId("settings-screen")).not.toBeInTheDocument();
    await act(async () => {
      fireEvent.keyDown(document, { key: "?", code: "Slash", shiftKey: true });
    });
    await flush();

    expect(screen.getByTestId("settings-section-shortcuts")).toBeInTheDocument();
    expect(screen.getAllByTestId("shortcut-row")).toHaveLength(SHORTCUT_BINDINGS.length);
  });

  it("Mod+, opens Settings on its default section, not the Shortcuts one a prior Shift+? left behind", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket);

    await act(async () => {
      fireEvent.keyDown(document, { key: "?", code: "Slash", shiftKey: true });
    });
    await flush();
    expect(screen.getByTestId("settings-section-shortcuts")).toBeInTheDocument();
    fireEvent.click(screen.getByTestId("settings-back-button"));
    await flush();

    await act(async () => {
      pressCtrl(",", "Comma");
    });
    await flush();

    expect(screen.getByTestId("settings-section-appearance")).toBeInTheDocument();
    expect(screen.queryByTestId("settings-section-shortcuts")).not.toBeInTheDocument();
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
    expect(titles).toContain("Settings: Providers");
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

    // Diff isn't seeded any more (dogfood default-tabs fix); opening it
    // via "+" already updates the URL once...
    await openBaseTab("Diff");
    expect(window.location.hash).toBe(`#/workspace/${WORKSPACE.ID}/task/${TASK_A.ID}/diff`);

    // ...switching back to Chat and forward to Diff again proves it's the
    // click, not just the open, that drives the URL.
    const chatTab = screen.getByRole("tab", { name: "Chat" });
    chatTab.focus();
    fireEvent.click(chatTab);
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

    // Files isn't seeded any more (dogfood default-tabs fix) -- open it
    // once via "+"; picking a.md switches the active tab away from Files
    // (Radix unmounts the inactive pane), so Files has to be reselected
    // before the second pick -- refetching file.list each time, same as a
    // real remount.
    await openBaseTab("Files");
    respondAll(socket, "file.list", [
      { name: "a.md", isDir: false, size: 1 },
      { name: "b.md", isDir: false, size: 1 },
    ]);
    await flush();
    fireEvent.click(document.querySelector(`[data-testid="file-row"][data-path="a.md"]`)!);
    await flush();
    respondAll(socket, "file.read", { content: "# a.md" });
    await flush();

    // Radix activates a tab on mousedown (or on focus, in its default
    // "automatic" mode) -- not on click.
    const filesTab = screen.getByRole("tab", { name: "Files" });
    fireEvent.mouseDown(filesTab, { button: 0 });
    await flush();
    respondAll(socket, "file.list", [
      { name: "a.md", isDir: false, size: 1 },
      { name: "b.md", isDir: false, size: 1 },
    ]);
    await flush();
    fireEvent.click(document.querySelector(`[data-testid="file-row"][data-path="b.md"]`)!);
    await flush();
    respondAll(socket, "file.read", { content: "# b.md" });
    await flush();
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

  it("icon-collapsing the sidebar shrinks its ResizablePanel instead of leaving its expanded width as dead space", async () => {
    // jsdom never lays anything out, so react-resizable-panels can't report
    // real pixel widths here (a real-browser check confirmed the panel
    // actually goes 256px -> 48px -> 256px) -- what jsdom *can* prove is
    // that toggling collapse drives the panel's own flex-grow via its
    // imperative handle at all, which is the bug this guards: before this
    // fix, nothing ever called collapse()/expand(), so the panel's style
    // never changed no matter what the Sidebar's own data-state said.
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    const { container } = render(<App connect={connect} />);
    await resolveSidebar(socket);

    const panel = container.querySelector('[data-slot="resizable-panel"]') as HTMLElement;
    const sidebar = document.querySelector('[data-slot="sidebar"]');
    expect(sidebar).toHaveAttribute("data-state", "expanded");
    const expandedStyle = panel.getAttribute("style");

    await act(async () => {
      fireEvent.keyDown(document, { key: "b", code: "KeyB", ctrlKey: true });
    });
    await flush();

    expect(sidebar).toHaveAttribute("data-state", "collapsed");
    expect(panel.getAttribute("style")).not.toBe(expandedStyle);

    await act(async () => {
      fireEvent.keyDown(document, { key: "b", code: "KeyB", ctrlKey: true });
    });
    await flush();

    expect(sidebar).toHaveAttribute("data-state", "expanded");
    expect(panel.getAttribute("style")).toBe(expandedStyle);
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

describe("App quick-open (Item 18)", () => {
  it("Ctrl+P opens quick-open for the selected task, and Enter opens the chosen file as a tab", async () => {
    vi.stubGlobal("navigator", { ...navigator, platform: "Linux x86_64" });
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket);

    clickTaskRow(TASK);
    await flush();
    respondAll(socket, "run.list", []);
    await flush();

    fireEvent.keyDown(document, { key: "p", ctrlKey: true });
    await flush();

    expect(screen.getByTestId("quick-open")).toBeInTheDocument();
    respond(socket, "task.searchIndex", { paths: ["README.md", "src/main.go"] });
    await flush();

    fireEvent.change(screen.getByTestId("quick-open-input"), { target: { value: "main" } });
    await flush();
    fireEvent.keyDown(screen.getByTestId("quick-open-input"), { key: "Enter" });
    await flush();

    expect(screen.queryByTestId("quick-open")).not.toBeInTheDocument();
    expect(screen.getByRole("tab", { name: /main\.go/ })).toBeInTheDocument();

    vi.unstubAllGlobals();
  });

  it("does not open before any task is selected", async () => {
    vi.stubGlobal("navigator", { ...navigator, platform: "Linux x86_64" });
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket);

    fireEvent.keyDown(document, { key: "p", ctrlKey: true });
    await flush();

    expect(screen.queryByTestId("quick-open")).not.toBeInTheDocument();

    vi.unstubAllGlobals();
  });
});

describe("App splits (Item 6)", () => {
  it("the \"Split\" menu lists all 4 directions (Item 7)", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket, [TASK_A]);

    clickTaskRow(TASK_A);
    await flush();
    respondAll(socket, "run.list", []);
    await flush();

    await openBaseTab("Diff");

    const trigger = screen.getByRole("button", { name: "Split Diff" });
    trigger.focus();
    fireEvent.keyDown(trigger, { key: "Enter" });
    await flush();

    expect(screen.getByTestId("workspace-tab-split-left")).toBeInTheDocument();
    expect(screen.getByTestId("workspace-tab-split-right")).toBeInTheDocument();
    expect(screen.getByTestId("workspace-tab-split-up")).toBeInTheDocument();
    expect(screen.getByTestId("workspace-tab-split-down")).toBeInTheDocument();
  });

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

    await openBaseTab("Files");
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

    await openBaseTab("Files");
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
      ApprovalPolicy: "manual",
      ThinkingLevel: "",
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

    // Terminal isn't seeded any more (dogfood default-tabs fix); opening
    // it force-mounts (Item 20) independent of which tab ends up active.
    await openBaseTab("Terminal");

    respond(socket, "terminal.list", []);
    await flush();
    respond(socket, "terminal.create", { terminalId: "term-1" });
    await flush();
    respond(socket, "terminal.attach", { terminalId: "term-1" });
    await flush();

    expect(socket.sent.filter((e) => e.method === "terminal.create")).toHaveLength(1);

    await splitTabRight("Terminal");

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

  it("splitting twice creates a third pane, each retaining its own independent tab state (Item 4)", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    // TASK_B, not TASK_A -- this opens a fresh terminal tab and expects a
    // genuine terminal.create, which a taskId=1 terminal tab (TASK_A's)
    // wouldn't get if an earlier test in this file already bound
    // "1:terminal" to a session id (lib/terminal-sessions.ts's bindings
    // map isn't reset between tests in this file).
    await resolveSidebar(socket, [TASK_B]);

    clickTaskRow(TASK_B);
    await flush();
    respondAll(socket, "run.list", []);
    await flush();

    // First split: Diff moves out of the default pane into a second pane.
    await openBaseTab("Diff");
    await splitTabRight("Diff");

    const primaryPane = screen.getByTestId("primary-pane");

    // Second split, from the default pane again: open Terminal there,
    // then split it into its own (third) pane.
    const newTabTrigger = within(primaryPane).getByTestId("tabs-new-tab");
    newTabTrigger.focus();
    fireEvent.keyDown(newTabTrigger, { key: "Enter" });
    await flush();
    fireEvent.click(screen.getByRole("menuitem", { name: "Open Terminal" }));
    await flush();
    respond(socket, "terminal.list", []);
    await flush();
    respond(socket, "terminal.create", { terminalId: "term-1" });
    await flush();
    respond(socket, "terminal.attach", { terminalId: "term-1" });
    await flush();

    const splitTerminalTrigger = within(primaryPane).getByRole("button", { name: "Split Terminal" });
    splitTerminalTrigger.focus();
    fireEvent.keyDown(splitTerminalTrigger, { key: "Enter" });
    await flush();
    fireEvent.click(screen.getByTestId("workspace-tab-split-down"));
    await flush();

    // The second split wraps the default pane's own tree position in a new
    // group (a pane converting to a group at that spot), so its Tabs root
    // remounts fresh -- re-query it rather than reuse the pre-split node.
    const finalPrimaryPane = screen.getByTestId("primary-pane");

    // Three independent panes now exist -- the fixed primary-pane/side-pane
    // testids only tell the common 0-or-1-split case apart, so the third
    // pane is targeted via data-pane-id instead.
    const allPanes = Array.from(document.querySelectorAll("[data-pane-id]"));
    expect(allPanes).toHaveLength(3);

    const diffPane = allPanes.find((el) => within(el as HTMLElement).queryByRole("tab", { name: "Diff" }));
    const terminalPane = allPanes.find((el) => within(el as HTMLElement).queryByRole("tab", { name: "Terminal" }));
    expect(diffPane).toBeDefined();
    expect(terminalPane).toBeDefined();
    expect(diffPane).not.toBe(terminalPane);
    expect(diffPane).not.toBe(finalPrimaryPane);
    expect(terminalPane).not.toBe(finalPrimaryPane);

    // Each pane really is independent -- neither Diff nor Terminal leaked
    // back into the default pane, which kept only Chat.
    expect(within(finalPrimaryPane).getByRole("tab", { name: "Chat" })).toBeInTheDocument();
    expect(within(finalPrimaryPane).queryByRole("tab", { name: "Diff" })).not.toBeInTheDocument();
    expect(within(finalPrimaryPane).queryByRole("tab", { name: "Terminal" })).not.toBeInTheDocument();
  });

  it("with 3 panes open, each pane's own \"+\" -> \"Open Files\" lands the new tab in that exact pane, not whichever non-default pane the old \"side\" sentinel would have picked first (Item 9)", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket, [TASK_C]);

    clickTaskRow(TASK_C);
    await flush();
    respondAll(socket, "run.list", []);
    await flush();

    // Same 2-splits-deep fixture shape as Item 4's 3-pane test: Diff moves
    // into a second pane (splitRight), then Terminal moves into a third
    // (splitDown from the default pane again).
    await openBaseTab("Diff");
    await splitTabRight("Diff");

    const primaryPane = screen.getByTestId("primary-pane");
    const newTabTrigger = within(primaryPane).getByTestId("tabs-new-tab");
    newTabTrigger.focus();
    fireEvent.keyDown(newTabTrigger, { key: "Enter" });
    await flush();
    fireEvent.click(screen.getByRole("menuitem", { name: "Open Terminal" }));
    await flush();
    respond(socket, "terminal.list", []);
    await flush();
    respond(socket, "terminal.create", { terminalId: "term-1" });
    await flush();
    respond(socket, "terminal.attach", { terminalId: "term-1" });
    await flush();

    const splitTerminalTrigger = within(primaryPane).getByRole("button", { name: "Split Terminal" });
    splitTerminalTrigger.focus();
    fireEvent.keyDown(splitTerminalTrigger, { key: "Enter" });
    await flush();
    fireEvent.click(screen.getByTestId("workspace-tab-split-down"));
    await flush();

    const finalPrimaryPane = screen.getByTestId("primary-pane");
    const allPanes = Array.from(document.querySelectorAll("[data-pane-id]"));
    expect(allPanes).toHaveLength(3);
    const diffPane = allPanes.find((el) => within(el as HTMLElement).queryByRole("tab", { name: "Diff" }))!;
    const terminalPane = allPanes.find((el) => within(el as HTMLElement).queryByRole("tab", { name: "Terminal" }))!;
    expect(diffPane).toBeDefined();
    expect(terminalPane).toBeDefined();

    // Files hasn't been opened anywhere yet -- open it from the Diff
    // pane's own "+", which under the old "side" sentinel would have
    // landed in whichever non-default pane collectAllPanes lists first
    // (the terminal pane, per this tree's DFS order), not necessarily the
    // one whose "+" was actually clicked.
    const diffPaneNewTabTrigger = within(diffPane as HTMLElement).getByTestId("tabs-new-tab");
    diffPaneNewTabTrigger.focus();
    fireEvent.keyDown(diffPaneNewTabTrigger, { key: "Enter" });
    await flush();
    fireEvent.click(screen.getByRole("menuitem", { name: "Open Files" }));
    await flush();
    respondAll(socket, "file.list", []);
    await flush();

    expect(within(diffPane as HTMLElement).getByRole("tab", { name: "Files" })).toBeInTheDocument();
    expect(within(terminalPane as HTMLElement).queryByRole("tab", { name: "Files" })).not.toBeInTheDocument();
    expect(within(finalPrimaryPane).queryByRole("tab", { name: "Files" })).not.toBeInTheDocument();
  });
});

describe("App pane focus and pane/tab keyboard actions (Item 6)", () => {
  /** Every pane's outer wrapper, keyed by its data-pane-id. */
  function panesById(): Record<string, HTMLElement> {
    const result: Record<string, HTMLElement> = {};
    for (const el of Array.from(document.querySelectorAll<HTMLElement>("[data-pane-id]"))) {
      result[el.dataset.paneId!] = el;
    }
    return result;
  }

  /** The pane id currently carrying the visible focused-pane ring, or null if there's only one pane (no ring shown). */
  function focusedPaneId(): string | null {
    const focused = screen.queryByTestId("pane-focused");
    return focused?.querySelector("[data-pane-id]")?.getAttribute("data-pane-id") ?? null;
  }

  async function press(key: string, code: string, extra: Record<string, unknown> = {}): Promise<void> {
    await act(async () => {
      fireEvent.keyDown(document, { key, code, ctrlKey: true, ...extra });
    });
    await flush();
  }

  /** Splits Diff to the right of the primary pane, returning [primaryPaneId, sidePaneId]. Mirrors the file's existing splitTabRight flow. */
  async function splitDiffToSide(): Promise<[string, string]> {
    await openBaseTab("Diff");
    await splitTabRight("Diff");
    const ids = Object.keys(panesById());
    const side = ids.find((id) => id !== "primary")!;
    return ["primary", side];
  }

  it("Mod+Shift+ArrowLeft/Right moves the focused-pane ring between panes", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket, [TASK_A]);
    clickTaskRow(TASK_A);
    await flush();
    respondAll(socket, "run.list", []);
    await flush();

    const [primaryId, sideId] = await splitDiffToSide();
    // Splitting a tab off focuses the pane it landed in (use-task-tabs.ts).
    expect(focusedPaneId()).toBe(sideId);

    await press("ArrowLeft", "ArrowLeft", { shiftKey: true });
    expect(focusedPaneId()).toBe(primaryId);

    await press("ArrowRight", "ArrowRight", { shiftKey: true });
    expect(focusedPaneId()).toBe(sideId);
  });

  it("tab.jump (Mod+Alt+<digit>) targets whichever pane is currently focused, not always the default one", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket, [TASK_A]);
    clickTaskRow(TASK_A);
    await flush();
    respondAll(socket, "run.list", []);
    await flush();

    // Primary ends up [Chat, Files]; splitting Diff off leaves it there and
    // carries only Diff into a fresh side pane.
    await openBaseTab("Files");
    const [primaryId, sideId] = await splitDiffToSide();

    // Give the side pane a second tab too, so "position 1" and "position
    // 2" mean something different in each pane.
    const sidePaneNewTab = within(panesById()[sideId]!).getByTestId("tabs-new-tab");
    sidePaneNewTab.focus();
    fireEvent.keyDown(sidePaneNewTab, { key: "Enter" });
    await flush();
    fireEvent.click(screen.getByRole("menuitem", { name: "Open Terminal" }));
    await flush();
    respondAll(socket, "terminal.list", []);
    await flush();

    // Side is focused (the split above landed there); Mod+Alt+2 there
    // means "the side pane's 2nd tab" -- Terminal, not Files.
    expect(focusedPaneId()).toBe(sideId);
    await press("2", "Digit2", { altKey: true });
    expect(within(panesById()[sideId]!).getByRole("tab", { name: "Terminal" })).toHaveAttribute(
      "aria-selected",
      "true",
    );

    // Move focus to primary; the same Mod+Alt+2 now means primary's 2nd
    // tab -- Files.
    await press("ArrowLeft", "ArrowLeft", { shiftKey: true });
    expect(focusedPaneId()).toBe(primaryId);
    await press("2", "Digit2", { altKey: true });
    expect(within(panesById()[primaryId]!).getByRole("tab", { name: "Files" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
  });

  it("the default pane-focus combo does not fire from inside the composer textarea -- it's the browser's own text-selection key there", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket, [TASK_A]);
    clickTaskRow(TASK_A);
    await flush();
    respondAll(socket, "run.list", []);
    await flush();

    const [, sideId] = await splitDiffToSide();
    expect(focusedPaneId()).toBe(sideId);

    const composer = screen.getByLabelText("Prompt");
    composer.focus();
    await act(async () => {
      fireEvent.keyDown(composer, { key: "ArrowLeft", code: "ArrowLeft", ctrlKey: true, shiftKey: true });
    });
    await flush();

    // Focus never moved off the side pane -- Ctrl+Shift+ArrowLeft stayed
    // the composer's own word-select key.
    expect(focusedPaneId()).toBe(sideId);
  });

  it("a rebound pane-focus combo does fire from inside the composer textarea", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket, [TASK_A]);
    clickTaskRow(TASK_A);
    await flush();
    respondAll(socket, "run.list", []);
    await flush();

    const [primaryId, sideId] = await splitDiffToSide();
    expect(focusedPaneId()).toBe(sideId);

    // Rebind pane.focus.left away from its default -- the whole point of
    // `editableWhenRebound` is that a combo the user picked on purpose
    // isn't stuck with the default's text-field exemption.
    fireEvent.keyDown(document, { key: "?", code: "Slash", shiftKey: true });
    await flush();
    fireEvent.click(screen.getByLabelText("Change shortcut for Focus pane left"));
    fireEvent.keyDown(window, { key: "h", code: "KeyH", altKey: true, shiftKey: true });
    fireEvent.click(screen.getByTestId("settings-back-button"));
    await flush();

    const composer = screen.getByLabelText("Prompt");
    composer.focus();
    await act(async () => {
      fireEvent.keyDown(composer, { key: "h", code: "KeyH", altKey: true, shiftKey: true });
    });
    await flush();

    expect(focusedPaneId()).toBe(primaryId);
  });

  it("Mod+\\ splits the focused pane right with a fresh, empty pane", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket, [TASK_A]);
    clickTaskRow(TASK_A);
    await flush();
    respondAll(socket, "run.list", []);
    await flush();

    expect(Object.keys(panesById())).toHaveLength(1);
    await press("\\", "Backslash");
    expect(Object.keys(panesById())).toHaveLength(2);
    // The new pane carries no tabs and is now the one focused.
    const newPaneId = Object.keys(panesById()).find((id) => id !== "primary")!;
    expect(focusedPaneId()).toBe(newPaneId);
    expect(within(panesById()[newPaneId]!).getByTestId("tabs-empty-state")).toBeInTheDocument();
  });

  it("Mod+Shift+W closes the focused pane but never the tree's last one", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket, [TASK_A]);
    clickTaskRow(TASK_A);
    await flush();
    respondAll(socket, "run.list", []);
    await flush();

    const [primaryId, sideId] = await splitDiffToSide();
    expect(focusedPaneId()).toBe(sideId);

    await press("w", "KeyW", { shiftKey: true });
    expect(Object.keys(panesById())).toEqual([primaryId]);

    // One pane left -- closing again is a no-op, not a crash.
    await press("w", "KeyW", { shiftKey: true });
    expect(Object.keys(panesById())).toEqual([primaryId]);
  });

  it("Alt+Shift+T opens the focused pane's own new-tab menu", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket, [TASK_A]);
    clickTaskRow(TASK_A);
    await flush();
    respondAll(socket, "run.list", []);
    await flush();

    await act(async () => {
      fireEvent.keyDown(document, { key: "T", code: "KeyT", altKey: true, shiftKey: true });
    });
    await flush();

    expect(screen.getByRole("menuitem", { name: "Open Files" })).toBeInTheDocument();
  });

  it("Alt+Shift+] and Alt+Shift+[ cycle the focused pane's tabs and wrap", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket, [TASK_A]);
    clickTaskRow(TASK_A);
    await flush();
    respondAll(socket, "run.list", []);
    await flush();
    await openBaseTab("Diff");
    // Strip order: Chat, Diff -- Diff is active (openBaseTab activates it).
    expect(screen.getByRole("tab", { name: "Diff" })).toHaveAttribute("aria-selected", "true");

    await act(async () => {
      fireEvent.keyDown(document, { key: "]", code: "BracketRight", altKey: true, shiftKey: true });
    });
    await flush();
    // Past the end wraps back to the first tab.
    expect(screen.getByRole("tab", { name: "Chat" })).toHaveAttribute("aria-selected", "true");

    await act(async () => {
      fireEvent.keyDown(document, { key: "[", code: "BracketLeft", altKey: true, shiftKey: true });
    });
    await flush();
    expect(screen.getByRole("tab", { name: "Diff" })).toHaveAttribute("aria-selected", "true");
  });

  it("Mod+Shift+M moves the focused pane's active tab to the next pane", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket, [TASK_A]);
    clickTaskRow(TASK_A);
    await flush();
    respondAll(socket, "run.list", []);
    await flush();

    const [primaryId, sideId] = await splitDiffToSide();
    const sidePane = panesById()[sideId]!;
    expect(within(sidePane).getByRole("tab", { name: "Diff" })).toBeInTheDocument();
    expect(focusedPaneId()).toBe(sideId);

    await press("m", "KeyM", { shiftKey: true });

    // The side pane had only that one tab, so moving it away collapses the
    // now-empty pane back into the tree -- same as closing its last tab
    // would (detachTabFromTree's preserveEmptyPaneId only protects a
    // tree's sole remaining pane, not an ordinary sibling).
    const panesAfter = panesById();
    expect(Object.keys(panesAfter)).toEqual([primaryId]);
    expect(within(panesAfter[primaryId]!).getByRole("tab", { name: "Diff" })).toBeInTheDocument();
  });

  it("Mod+, opens Settings", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket);

    expect(screen.queryByTestId("settings-screen")).not.toBeInTheDocument();
    await press(",", "Comma");
    expect(screen.getByTestId("settings-screen")).toBeInTheDocument();
  });

  it("Alt+<digit> jumps to the Nth task in the sidebar, distinct from Ctrl+Alt+<digit>'s tab-position jump", async () => {
    // Not Mod+<digit>: Ctrl/Cmd+1-9 is the browser's own tab-switching
    // shortcut and the page never reliably sees it (see the binding's own
    // comment in keyboard/shortcuts.ts).
    async function pressAlt(key: string, code: string): Promise<void> {
      await act(async () => {
        fireEvent.keyDown(document, { key, code, altKey: true });
      });
      await flush();
    }

    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket, [TASK_A, TASK_B, TASK_C]);
    clickTaskRow(TASK_A);
    await flush();
    respondAll(socket, "run.list", []);
    await flush();

    await pressAlt("2", "Digit2");
    await flush();
    respondAll(socket, "run.list", []);
    await flush();
    expect(screen.getByRole("heading", { name: TASK_B.Title })).toBeInTheDocument();

    await pressAlt("3", "Digit3");
    await flush();
    respondAll(socket, "run.list", []);
    await flush();
    expect(screen.getByRole("heading", { name: TASK_C.Title })).toBeInTheDocument();
  });
});

describe("App tab context menu (Item 6)", () => {
  async function openFileTabNamed(socket: FakeSocket, name: string): Promise<void> {
    await openBaseTab("Files");
    respondAll(socket, "file.list", [{ name, isDir: false, size: 1 }]);
    await flush();
    fireEvent.click(screen.getByTestId("file-row"));
    await flush();
    respondAll(socket, "file.read", { content: "hello" });
    await flush();
  }

  async function openTerminal(socket: FakeSocket): Promise<void> {
    await openBaseTab("Terminal");
    respond(socket, "terminal.list", []);
    await flush();
    respond(socket, "terminal.create", { terminalId: "term-1" });
    await flush();
    respond(socket, "terminal.attach", { terminalId: "term-1" });
    await flush();
  }

  function openMenuFor(title: string) {
    fireEvent.contextMenu(screen.getByRole("tab", { name: new RegExp(title) }));
  }

  it("shows Copy path (not Rename) on a file tab, and Rename (not Copy path) on a terminal tab", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket, [TASK_A]);
    clickTaskRow(TASK_A);
    await flush();
    respondAll(socket, "run.list", []);
    await flush();

    await openFileTabNamed(socket, "README.md");
    openMenuFor("README.md");
    expect(screen.getByTestId("workspace-tab-menu-copy-path")).toBeInTheDocument();
    expect(screen.queryByTestId("workspace-tab-menu-rename")).not.toBeInTheDocument();
    fireEvent.keyDown(document, { key: "Escape" });

    await openTerminal(socket);
    openMenuFor("Terminal");
    expect(screen.getByTestId("workspace-tab-menu-rename")).toBeInTheDocument();
    expect(screen.queryByTestId("workspace-tab-menu-copy-path")).not.toBeInTheDocument();
  });

  it("copies a file tab's path to the clipboard", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    vi.stubGlobal("navigator", { ...navigator, clipboard: { writeText } });
    try {
      const socket = new FakeSocket();
      const connect = vi.fn().mockResolvedValue(new WsClient(socket));
      render(<App connect={connect} />);
      await resolveSidebar(socket, [TASK_A]);
      clickTaskRow(TASK_A);
      await flush();
      respondAll(socket, "run.list", []);
      await flush();
      await openFileTabNamed(socket, "README.md");

      openMenuFor("README.md");
      fireEvent.click(screen.getByTestId("workspace-tab-menu-copy-path"));

      expect(writeText).toHaveBeenCalledWith("README.md");
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it("renames a terminal tab on Enter, and leaves it alone on Escape", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket, [TASK_A]);
    clickTaskRow(TASK_A);
    await flush();
    respondAll(socket, "run.list", []);
    await flush();
    await openTerminal(socket);

    openMenuFor("Terminal");
    fireEvent.click(screen.getByTestId("workspace-tab-menu-rename"));

    const input = screen.getByTestId("workspace-tab-rename-input");
    fireEvent.change(input, { target: { value: "build" } });
    fireEvent.keyDown(input, { key: "Enter" });
    fireEvent.blur(input);
    await flush();

    expect(screen.getByRole("tab", { name: /build/ })).toBeInTheDocument();

    openMenuFor("build");
    fireEvent.click(screen.getByTestId("workspace-tab-menu-rename"));
    const secondInput = screen.getByTestId("workspace-tab-rename-input");
    fireEvent.change(secondInput, { target: { value: "should not stick" } });
    fireEvent.keyDown(secondInput, { key: "Escape" });
    await flush();

    expect(screen.getByRole("tab", { name: /build/ })).toBeInTheDocument();
    expect(screen.queryByRole("tab", { name: /should not stick/ })).not.toBeInTheDocument();
  });

  it("Close others / Close to the left / Close to the right target only the clicked tab's pane", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket, [TASK_A]);
    clickTaskRow(TASK_A);
    await flush();
    respondAll(socket, "run.list", []);
    await flush();

    // Strip order: Chat, Files, Diff, Terminal.
    await openBaseTab("Files");
    await openBaseTab("Diff");
    await openTerminal(socket);

    openMenuFor("Diff");
    fireEvent.click(screen.getByTestId("workspace-tab-menu-close-left"));
    await flush();
    expect(screen.queryByRole("tab", { name: "Chat" })).not.toBeInTheDocument();
    expect(screen.queryByRole("tab", { name: "Files" })).not.toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Diff" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Terminal" })).toBeInTheDocument();

    openMenuFor("Diff");
    fireEvent.click(screen.getByTestId("workspace-tab-menu-close-others"));
    await flush();
    expect(screen.getByRole("tab", { name: "Diff" })).toBeInTheDocument();
    expect(screen.queryByRole("tab", { name: "Terminal" })).not.toBeInTheDocument();
  });

  it("still offers the existing split entries alongside the new items", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket, [TASK_A]);
    clickTaskRow(TASK_A);
    await flush();
    respondAll(socket, "run.list", []);
    await flush();
    await openBaseTab("Diff");

    openMenuFor("Diff");

    expect(screen.getByTestId("workspace-tab-menu-split-left")).toBeInTheDocument();
    expect(screen.getByTestId("workspace-tab-menu-split-right")).toBeInTheDocument();
    expect(screen.getByTestId("workspace-tab-menu-split-up")).toBeInTheDocument();
    expect(screen.getByTestId("workspace-tab-menu-split-down")).toBeInTheDocument();
  });
});

describe("App pane focus vs. web-find's per-pane focus-within (rebase interop)", () => {
  it("Mod+F opens Find only in the pane that actually has DOM focus (Chat vs. a split-off Terminal), unaffected by the split-tree click-to-focus-pane handler", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket, [TASK_A]);
    clickTaskRow(TASK_A);
    await flush();
    respondAll(socket, "run.list", []);
    await flush();

    // Terminal, opened in primary alongside Chat, then split into its own
    // pane -- Chat stays behind in primary.
    await openBaseTab("Terminal");
    respond(socket, "terminal.list", []);
    await flush();
    respond(socket, "terminal.create", { terminalId: "term-1" });
    await flush();
    respond(socket, "terminal.attach", { terminalId: "term-1" });
    await flush();
    await splitTabRight("Terminal");

    const panes = Object.fromEntries(
      Array.from(document.querySelectorAll<HTMLElement>("[data-pane-id]")).map((el) => [
        el.dataset.paneId!,
        el,
      ]),
    );
    const paneIds = Object.keys(panes);
    expect(paneIds).toHaveLength(2);
    const sideId = paneIds.find((id) => id !== "primary")!;
    // Chat's find-focus wrapper (task-detail.tsx) is still in primary.
    expect(within(panes.primary!).getByTestId("run-log-scroll")).toBeInTheDocument();
    expect(within(panes[sideId]!).getByTestId("terminal-container")).toBeInTheDocument();

    // Nothing focused yet -- Mod+F opens Find nowhere.
    fireEvent.keyDown(document, { key: "f", code: "KeyF", ctrlKey: true });
    expect(screen.queryByTestId("find-bar")).not.toBeInTheDocument();

    // A click (App.tsx's split-tree focus handler is onPointerDownCapture,
    // never stopPropagation/preventDefault-ing) followed by the DOM focus
    // a real click into the terminal would cause -- proving the two focus
    // concepts (the split-tree's `focusedPaneId`, and web-find's own
    // usePaneFocusWithin) don't fight each other over the same click.
    fireEvent.pointerDown(panes[sideId]!);
    fireEvent.focus(within(panes[sideId]!).getByTestId("terminal-container"));
    fireEvent.keyDown(document, { key: "f", code: "KeyF", ctrlKey: true });

    expect(within(panes[sideId]!).getByTestId("find-bar")).toBeInTheDocument();
    expect(within(panes.primary!).queryByTestId("find-bar")).not.toBeInTheDocument();

    // Switching DOM focus to primary's Chat moves which pane's Find claims
    // Mod+F next, independent of the split-tree's own focused pane (which
    // the click above already moved to the side pane, and which
    // tab.close/tab.jump/etc. still target).
    fireEvent.pointerDown(panes.primary!);
    fireEvent.focus(within(panes.primary!).getByTestId("run-log-scroll"));
    fireEvent.keyDown(document, { key: "f", code: "KeyF", ctrlKey: true });

    expect(within(panes.primary!).getByTestId("find-bar")).toBeInTheDocument();
  });
});

describe("App drag-to-split (Item 8)", () => {
  interface StubRect {
    left: number;
    top: number;
    width: number;
    height: number;
  }

  // jsdom never lays anything out, so getBoundingClientRect is always a
  // zero rect by default -- every drop would resolve to the same (0,0)-
  // relative position without this. Keyed by element identity (a WeakMap,
  // same idea as resizable.test.tsx's testid-keyed stub) since the Item 8
  // droppable pane wrapper has no testid of its own -- callers grab the
  // actual DOM node via `within(...).getByTestId(...)`.parentElement and
  // register its rect directly.
  const rectByElement = new WeakMap<Element, StubRect>();
  let originalGetBoundingClientRect: typeof Element.prototype.getBoundingClientRect;

  beforeEach(() => {
    originalGetBoundingClientRect = Element.prototype.getBoundingClientRect;
    Element.prototype.getBoundingClientRect = function (this: Element) {
      const r = rectByElement.get(this) ?? { left: 0, top: 0, width: 0, height: 0 };
      return {
        x: r.left,
        y: r.top,
        width: r.width,
        height: r.height,
        top: r.top,
        left: r.left,
        right: r.left + r.width,
        bottom: r.top + r.height,
        toJSON: () => {},
      } as DOMRect;
    };
  });

  afterEach(() => {
    Element.prototype.getBoundingClientRect = originalGetBoundingClientRect;
  });

  function stubRect(el: Element, rect: StubRect): void {
    rectByElement.set(el, rect);
  }

  /**
   * Simulates a dnd-kit PointerSensor drag from scratch: pointerdown on
   * the draggable element (dnd-kit's activator handler requires
   * `isPrimary`/`button: 0`), then two pointermoves dispatched on
   * `document` -- PointerSensor attaches its move/end listeners there,
   * not on the node itself, once a drag is pending. The first move
   * crosses the 8px activation-distance threshold and only *starts* the
   * drag (dnd-kit's own AbstractPointerSensor doesn't forward that
   * event's coordinates as a position update, it just flips `activated`);
   * the second move is what actually delivers `target` as the drag's
   * live position, recomputing `active.rect.current.translated`. A final
   * pointerup at `target` ends the drag. Choosing `tabCenter` to be the
   * dragged element's own stubbed center means the translated rect's
   * center lands exactly on `target` with no extra offset arithmetic at
   * each call site (translated = initialRect shifted by target-tabCenter,
   * and initialRect's own center is tabCenter).
   */
  async function dragTabTo(
    tabElement: Element,
    tabCenter: { x: number; y: number },
    target: { x: number; y: number },
  ): Promise<void> {
    fireEvent.pointerDown(tabElement, {
      pointerId: 1,
      isPrimary: true,
      button: 0,
      clientX: tabCenter.x,
      clientY: tabCenter.y,
    });
    fireEvent.pointerMove(document, { pointerId: 1, clientX: tabCenter.x + 20, clientY: tabCenter.y });
    fireEvent.pointerMove(document, { pointerId: 1, clientX: target.x, clientY: target.y });
    fireEvent.pointerUp(document, { pointerId: 1, clientX: target.x, clientY: target.y });
    // dnd-kit's PointerSensor suppresses the stray "click" that would
    // otherwise fire right after a drag (it adds a capture-phase
    // document-level stopPropagation listener on drag start) and removes
    // it via `setTimeout(..., 50)`, not synchronously on drag end --
    // without advancing past that window here, the listener leaks into
    // whatever runs next (the test's own later assertions' events, or the
    // next test entirely) and silently swallows every click in the
    // document until it expires. This file runs under fake timers (the
    // top-level `beforeEach`'s `vi.useFakeTimers()`), so it's advanced
    // explicitly rather than waited on in real wall-clock time -- same
    // pattern as this file's own `advanceReconnectTimer`.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(60);
    });
  }

  /**
   * `task` with Diff split into its own side pane and Terminal left in
   * primary -- two draggable, movable tabs in two distinct panes, the
   * fixture every test below drags between. Each test passes its own
   * task (TASK_D/E/F) rather than sharing one: `lib/terminal-sessions.ts`
   * binds a session to `${taskId}:terminal` for the process lifetime of
   * this test file, so a second test reusing the same task id would skip
   * the `terminal.create` round-trip this setup waits on.
   */
  async function setUpTwoPanes(socket: FakeSocket, task: Task): Promise<void> {
    await resolveSidebar(socket, [task]);
    clickTaskRow(task);
    await flush();
    respondAll(socket, "run.list", []);
    await flush();

    await openBaseTab("Diff");
    await splitTabRight("Diff");

    const primaryPane = screen.getByTestId("primary-pane");
    const newTabTrigger = within(primaryPane).getByTestId("tabs-new-tab");
    newTabTrigger.focus();
    fireEvent.keyDown(newTabTrigger, { key: "Enter" });
    await flush();
    fireEvent.click(screen.getByRole("menuitem", { name: "Open Terminal" }));
    await flush();
    respond(socket, "terminal.list", []);
    await flush();
    respond(socket, "terminal.create", { terminalId: "term-1" });
    await flush();
    respond(socket, "terminal.attach", { terminalId: "term-1" });
    await flush();
  }

  /** Registers stubbed rects for both panes' droppable wrappers (side-by-side, 400x300 each) and for `terminalTab` (the tab every test drags), returning nothing -- callers then call `dragTabTo`. */
  function stubTwoPaneRects(primaryPane: HTMLElement, sidePane: HTMLElement, terminalTab: HTMLElement): void {
    stubRect(primaryPane.parentElement!, { left: 0, top: 0, width: 400, height: 300 });
    stubRect(sidePane.parentElement!, { left: 400, top: 0, width: 400, height: 300 });
    stubRect(terminalTab, { left: 50, top: 10, width: 60, height: 20 });
  }

  it("dropping a movable tab on another pane's left-edge zone splits it left", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await setUpTwoPanes(socket, TASK_D);

    const primaryPane = screen.getByTestId("primary-pane");
    const sidePane = screen.getByTestId("side-pane");
    const terminalTab = within(primaryPane).getByRole("tab", { name: "Terminal" });
    stubTwoPaneRects(primaryPane, sidePane, terminalTab);

    // x=430 is 30px (7.5%) into the side pane's left edge -- well inside
    // its 15% (60px) edge threshold.
    await dragTabTo(terminalTab, { x: 80, y: 20 }, { x: 430, y: 150 });
    await flush();

    const allPanes = Array.from(document.querySelectorAll("[data-pane-id]"));
    expect(allPanes).toHaveLength(3);
    const terminalPane = allPanes.find((el) => within(el as HTMLElement).queryByRole("tab", { name: "Terminal" }));
    expect(terminalPane).toBeDefined();
    expect(within(screen.getByTestId("primary-pane")).queryByRole("tab", { name: "Terminal" })).not.toBeInTheDocument();
  });

  it("dropping on a pane's center zone moves the tab there instead of splitting", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await setUpTwoPanes(socket, TASK_E);

    const primaryPane = screen.getByTestId("primary-pane");
    const sidePane = screen.getByTestId("side-pane");
    const terminalTab = within(primaryPane).getByRole("tab", { name: "Terminal" });
    stubTwoPaneRects(primaryPane, sidePane, terminalTab);

    // (600, 150) is the side pane's own center -- well inside its 40% center band.
    await dragTabTo(terminalTab, { x: 80, y: 20 }, { x: 600, y: 150 });
    await flush();

    expect(document.querySelectorAll("[data-pane-id]")).toHaveLength(2);
    expect(within(screen.getByTestId("side-pane")).getByRole("tab", { name: "Terminal" })).toBeInTheDocument();
    expect(within(screen.getByTestId("primary-pane")).queryByRole("tab", { name: "Terminal" })).not.toBeInTheDocument();
  });

  it("releasing outside every pane's droppable zone is a no-op", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await setUpTwoPanes(socket, TASK_F);

    const primaryPane = screen.getByTestId("primary-pane");
    const sidePane = screen.getByTestId("side-pane");
    const terminalTab = within(primaryPane).getByRole("tab", { name: "Terminal" });
    stubTwoPaneRects(primaryPane, sidePane, terminalTab);

    await dragTabTo(terminalTab, { x: 80, y: 20 }, { x: 5000, y: 5000 });
    await flush();

    expect(document.querySelectorAll("[data-pane-id]")).toHaveLength(2);
    expect(within(screen.getByTestId("primary-pane")).getByRole("tab", { name: "Terminal" })).toBeInTheDocument();
  });
});

describe("App settings view (dogfood Item 4)", () => {
  it("the sidebar's settings button swaps the main area to the settings screen, and Back returns to the previous view", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket);
    clickTaskRow(TASK);
    await flush();
    respondAll(socket, "run.list", []);
    await flush();

    expect(screen.getByTestId("primary-pane")).toBeInTheDocument();
    expect(screen.queryByTestId("settings-screen")).not.toBeInTheDocument();

    fireEvent.click(screen.getByTestId("sidebar-settings-button"));
    await flush();
    expect(screen.getByTestId("settings-screen")).toBeInTheDocument();
    expect(screen.queryByTestId("primary-pane")).not.toBeInTheDocument();

    fireEvent.click(screen.getByTestId("settings-back-button"));
    await flush();
    expect(screen.queryByTestId("settings-screen")).not.toBeInTheDocument();
    expect(screen.getByTestId("primary-pane")).toBeInTheDocument();
  });

  it("Escape from the settings screen returns to the task view", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket);

    fireEvent.click(screen.getByTestId("sidebar-settings-button"));
    await flush();
    expect(screen.getByTestId("settings-screen")).toBeInTheDocument();

    await act(async () => {
      fireEvent.keyDown(document, { key: "Escape" });
    });
    await flush();
    expect(screen.queryByTestId("settings-screen")).not.toBeInTheDocument();
  });

  it("the settings view shows with no task selected too (from the empty state)", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket, []);

    fireEvent.click(screen.getByTestId("sidebar-settings-button"));
    await flush();
    expect(screen.getByTestId("settings-screen")).toBeInTheDocument();
  });
});

describe("App chat column (dogfood Item 2)", () => {
  it("the chat tab's timeline scrolls inside a centered max-width column, while the Files pane stays full-width", async () => {
    const socket = new FakeSocket();
    const connect = vi.fn().mockResolvedValue(new WsClient(socket));
    render(<App connect={connect} />);
    await resolveSidebar(socket);
    clickTaskRow(TASK);
    await flush();
    respondAll(socket, "run.list", []);
    await flush();

    const column = screen.getByTestId("run-log-column");
    expect(column.className).toContain("mx-auto");
    expect(column.className).toContain("max-w-3xl");

    await openBaseTab("Files");
    respondAll(socket, "file.list", []);
    await flush();

    const explorer = screen.getByTestId("file-explorer-pane");
    expect(explorer.className).not.toContain("max-w");
  });
});
