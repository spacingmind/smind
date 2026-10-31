import { act } from "react";
import { fireEvent, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { FakeWsClient } from "@/test/fake-ws-client";
import { renderWithClientProp as render } from "@/test/render-with-daemon";
import { agentChipLabel, TaskDetailPane } from "@/components/task-detail";
import {
  FOLLOW_THRESHOLD_PX,
  LOG_END_CLEARANCE_PX,
  PILL_BOTTOM_OFFSET_PX,
  PILL_HEIGHT_PX,
} from "@/components/timeline/use-auto-follow";
import type { RunConfigState } from "@/components/composer/run-config-toolbar";
import type { AgentProfile, Chat, ProviderListResult, RunLogsResult, RunSummary, Task } from "@/lib/types";

// The run-config IA plan persists RunConfigToolbar's state per task id
// (docs/design.md §9) -- most tests below share TASK_A.ID, so a leftover
// value from one test would otherwise leak into the next.
afterEach(() => {
  window.localStorage.clear();
});

const TASK_A: Task = {
  ID: 1,
  WorkspaceID: 1,
  SpaceID: null,
  Title: "Task A",
  Status: "active",
  WorktreePath: "/tmp/a",
  Branch: "task-a",
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-01T00:00:00Z",
  ArchivedAt: null,
};

const TASK_B: Task = { ...TASK_A, ID: 2, Title: "Task B", Branch: "task-b" };

const CHAT_A: Chat = {
  ID: 10,
  TaskID: TASK_A.ID,
  Title: "Chat",
  Provider: null,
  AgentSession: null,
  CreatedAt: "2024-01-01T00:00:00Z",
  ArchivedAt: null,
};
const CHAT_B: Chat = { ...CHAT_A, ID: 20, TaskID: TASK_B.ID };

function runningRun(overrides: Partial<RunSummary> = {}): RunSummary {
  return {
    ID: "run-1",
    TaskID: TASK_A.ID,
    ChatID: CHAT_A.ID,
    Provider: "claude-native",
    Prompt: "do the thing",
    Status: "running",
    StartedAt: "2024-01-01T00:00:00Z",
    FinishedAt: null,
    StopReason: "",
    Err: "",
    PermissionMode: "acceptEdits",
    AutoAccept: false,
    ThinkingLevel: "",
    ...overrides,
  };
}

function doneRun(overrides: Partial<RunSummary> = {}): RunSummary {
  return {
    ID: "run-1",
    TaskID: TASK_A.ID,
    ChatID: CHAT_A.ID,
    Provider: "claude-native",
    Prompt: "do the thing",
    Status: "done",
    StartedAt: "2024-01-01T00:00:00Z",
    FinishedAt: "2024-01-01T00:00:05Z",
    StopReason: "end_turn",
    Err: "",
    PermissionMode: "acceptEdits",
    AutoAccept: false,
    ThinkingLevel: "",
    ...overrides,
  };
}

/** Flushes pending microtasks, wrapped in `act` so React commits any resulting state updates before the caller asserts. */
async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

describe("TaskDetailPane", () => {
  it("selecting a task fetches and renders its run history", async () => {
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);

    expect(client.nth("run.list", 0).params).toEqual({ chatId: CHAT_A.ID });
    client.nth("run.list", 0).resolve([doneRun()]);
    await flush();

    const logsCall = client.nth("run.logs", 0);
    expect(logsCall.params).toEqual({ runId: "run-1" });
    const logs: RunLogsResult = {
      runId: "run-1",
      status: "done",
      stopReason: "end_turn",
      events: [
        { type: "chunk", text: "hello " },
        { type: "chunk", text: "world" },
        { type: "done", stopReason: "end_turn" },
      ],
    };
    logsCall.resolve(logs);
    await flush();

    const entry = screen.getByTestId("run-entry");
    expect(within(entry).getByTestId("timeline-assistant")).toHaveTextContent("hello world");
    expect(within(entry).getByTestId("run-meta")).toHaveAttribute("data-status", "done");
  });

  it("streams a running run's live chunks into the timeline as they arrive, not buffered until the terminal event", async () => {
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);

    client.nth("run.list", 0).resolve([runningRun()]);
    await flush();

    expect(client.nth("run.attach", 0).params).toEqual({ runId: "run-1" });

    client.emit("run.attach", 0, "chunk", { text: "partial " });
    await flush();
    expect(screen.getByTestId("timeline-assistant")).toHaveTextContent("partial");
    expect(screen.getByTestId("run-meta")).toHaveAttribute("data-status", "running");
    expect(within(screen.getByTestId("run-meta")).getByRole("status")).toHaveTextContent(/^Working…/);

    client.emit("run.attach", 0, "chunk", { text: "and more" });
    await flush();
    expect(screen.getByTestId("timeline-assistant")).toHaveTextContent("partial and more");

    client.nth("run.attach", 0).resolve({ runId: "run-1", stopReason: "end_turn" });
    await flush();
    expect(screen.getByTestId("run-meta")).toHaveAttribute("data-status", "done");
  });

  it("shows the chat timeline rail once two runs overflow a wide pane", async () => {
    const sizes: Record<string, number> = { scrollHeight: 3000, clientHeight: 600, clientWidth: 1200 };
    for (const [key, value] of Object.entries(sizes)) {
      Object.defineProperty(HTMLElement.prototype, key, { configurable: true, get: () => value });
    }
    try {
      const client = new FakeWsClient();
      render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);
      client.nth("run.list", 0).resolve([
        doneRun({ ID: "run-1", Prompt: "first ask" }),
        doneRun({ ID: "run-2", Prompt: "second ask" }),
      ]);
      await flush();
      for (const n of [0, 1]) {
        client.nth("run.logs", n).resolve({ runId: `run-${n + 1}`, status: "done", stopReason: "end_turn", events: [] });
      }
      await flush();
      expect(screen.getByTestId("chat-outline-rail")).toBeInTheDocument();
      expect(screen.getByTestId("chat-outline-tick-run-2")).toHaveAttribute("aria-label", "2 of 2: second ask");
    } finally {
      for (const key of Object.keys(sizes)) {
        delete (HTMLElement.prototype as unknown as Record<string, unknown>)[key];
      }
    }
  });

  it("the log end reserves clearance so the Jump to latest pill never covers the last line", async () => {
    // jsdom has no layout: assert the structural contract (a constant bottom
    // padding on the log column) instead of measuring overlap.
    const sizes: Record<string, number> = { scrollHeight: 3000, clientHeight: 600, clientWidth: 1200 };
    for (const [key, value] of Object.entries(sizes)) {
      Object.defineProperty(HTMLElement.prototype, key, { configurable: true, get: () => value });
    }
    try {
      const client = new FakeWsClient();
      render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);
      client.nth("run.list", 0).resolve([doneRun()]);
      await flush();
      client.nth("run.logs", 0).resolve({ runId: "run-1", status: "done", stopReason: "end_turn", events: [] });
      await flush();

      const log = screen.getByTestId("run-log-scroll");
      const column = screen.getByTestId("run-log-column");
      const padding = () => parseFloat(column.style.paddingBottom);
      expect(LOG_END_CLEARANCE_PX).toBeGreaterThanOrEqual(PILL_BOTTOM_OFFSET_PX + PILL_HEIGHT_PX + FOLLOW_THRESHOLD_PX);

      // Following (at the tail): no pill, and the padding is already there.
      Object.defineProperty(log, "scrollTop", { configurable: true, writable: true, value: 2400 });
      fireEvent.scroll(log);
      await flush();
      expect(screen.queryByTestId("jump-to-latest")).not.toBeInTheDocument();
      expect(padding()).toBeGreaterThanOrEqual(LOG_END_CLEARANCE_PX);

      // Pill visible: the same padding, so nothing shifts when it appears.
      log.scrollTop = 0;
      fireEvent.scroll(log);
      await flush();
      expect(screen.getByTestId("jump-to-latest")).toBeInTheDocument();
      expect(padding()).toBeGreaterThanOrEqual(LOG_END_CLEARANCE_PX);
    } finally {
      for (const key of Object.keys(sizes)) {
        delete (HTMLElement.prototype as unknown as Record<string, unknown>)[key];
      }
    }
  });

  it("the Jump to latest pill has an opaque surface, a border and a shadow", async () => {
    const sizes: Record<string, number> = { scrollHeight: 3000, clientHeight: 600, clientWidth: 1200 };
    for (const [key, value] of Object.entries(sizes)) {
      Object.defineProperty(HTMLElement.prototype, key, { configurable: true, get: () => value });
    }
    try {
      const client = new FakeWsClient();
      render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);
      client.nth("run.list", 0).resolve([doneRun()]);
      await flush();
      client.nth("run.logs", 0).resolve({ runId: "run-1", status: "done", stopReason: "end_turn", events: [] });
      await flush();

      // Scrolling away from the tail (3000 - 0 - 600 px from the bottom) flips
      // auto-follow off, which is what renders the pill.
      const log = screen.getByTestId("run-log-scroll");
      Object.defineProperty(log, "scrollTop", { configurable: true, writable: true, value: 0 });
      fireEvent.scroll(log);
      await flush();

      const pill = screen.getByTestId("jump-to-latest");
      expect(pill.className).toContain("bg-background");
      expect(pill.className).toContain("dark:bg-background");
      expect(pill.className).toContain("border-border");
      expect(pill.className).toContain("shadow-md");
      expect(pill.className).not.toContain("shadow-sm");

      fireEvent.click(pill);
      await flush();
      expect(screen.queryByTestId("jump-to-latest")).not.toBeInTheDocument();
    } finally {
      for (const key of Object.keys(sizes)) {
        delete (HTMLElement.prototype as unknown as Record<string, unknown>)[key];
      }
    }
  });

  it("switching to a different task and back doesn't duplicate entries or leak the live subscription", async () => {
    const client = new FakeWsClient();
    const { rerender } = render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);

    client.nth("run.list", 0).resolve([runningRun()]);
    await flush();
    client.emit("run.attach", 0, "chunk", { text: "hello" });
    await flush();
    expect(screen.getByTestId("timeline-assistant")).toHaveTextContent("hello");

    // Switch away: the first task's run.attach must be aborted (detached),
    // not left dangling or double-subscribed.
    rerender(<TaskDetailPane client={client} task={TASK_B} chat={CHAT_B} isDefaultChat />);
    await flush();
    expect(client.nth("run.attach", 0).options?.signal?.aborted).toBe(true);

    expect(client.nth("run.list", 1).params).toEqual({ chatId: CHAT_B.ID });
    client.nth("run.list", 1).resolve([]);
    await flush();
    expect(screen.getByText(/no runs yet/i)).toBeInTheDocument();

    // Switch back: the run kept going while detached (as it must), so by
    // the time we reselect Task A it shows as finished with its full text
    // -- fetched fresh via run.list + run.logs, not carried over from the
    // earlier live subscription.
    rerender(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);
    await flush();
    client.nth("run.list", 2).resolve([doneRun()]);
    await flush();
    client.nth("run.logs", 0).resolve({
      runId: "run-1",
      status: "done",
      stopReason: "end_turn",
      events: [
        { type: "chunk", text: "hello world" },
        { type: "done", stopReason: "end_turn" },
      ],
    } satisfies RunLogsResult);
    await flush();

    const entries = screen.getAllByTestId("run-entry");
    expect(entries).toHaveLength(1);
    expect(within(entries[0]!).getByTestId("timeline-assistant")).toHaveTextContent("hello world");
  });

  it("aborts (does not stop) an actively streaming run.attach on unmount", async () => {
    const client = new FakeWsClient();
    const { unmount } = render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);

    client.nth("run.list", 0).resolve([runningRun()]);
    await flush();
    const attachCall = client.nth("run.attach", 0);
    expect(attachCall.options?.signal?.aborted).toBeFalsy();

    unmount();

    expect(attachCall.options?.signal?.aborted).toBe(true);
    expect(client.calls.some((c) => c.method === "run.stop")).toBe(false);
  });

  it("shows the composer's diff chip from task.git, follows task.git events, and focuses the open task", async () => {
    const client = new FakeWsClient();
    const onOpenDiffTab = () => {};
    render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat onOpenDiffTab={onOpenDiffTab} />);
    client.nth("run.list", 0).resolve([]);
    await flush();
    expect(client.nth("task.gitFocus", 0).params).toEqual({ taskId: TASK_A.ID });
    client.nth("task.git", 0).resolve({
      taskId: TASK_A.ID,
      untrusted: false,
      computedAt: "2026-10-22T00:00:00Z",
      diff: { files: 1, insertions: 3, deletions: 1 },
    });
    await flush();
    expect(screen.getByTestId("composer-diff-stat")).toHaveTextContent("+3");

    act(() =>
      client.pushNotification("task.git", {
        taskId: TASK_A.ID,
        git: { taskId: TASK_A.ID, untrusted: false, computedAt: "2026-10-22T00:00:01Z", diff: { files: 2, insertions: 9, deletions: 1 } },
      }),
    );
    await flush();
    expect(screen.getByTestId("composer-diff-stat")).toHaveTextContent("+9");
    expect(client.calls.some((c) => c.method === "task.diff")).toBe(false);
  });

  it("submits the prompt form via run.start then run.attach, never task.prompt", async () => {
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);

    client.nth("run.list", 0).resolve([]);
    await flush();

    const promptInput = screen.getByLabelText("Prompt");
    fireEvent.change(promptInput, { target: { value: "do the thing" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    await flush();

    expect(client.calls.some((c) => c.method === "task.prompt")).toBe(false);

    const startCall = client.nth("run.start", 0);
    expect(startCall.params).toEqual({ taskId: TASK_A.ID, chatId: CHAT_A.ID, provider: "claude-native", prompt: "do the thing", whenBusy: "queue" });

    // run.attach must not have been issued before run.start resolves.
    expect(client.calls.some((c) => c.method === "run.attach")).toBe(false);

    startCall.resolve({ runId: "run-new" });
    await flush();

    const attachCall = client.nth("run.attach", 0);
    expect(attachCall.params).toEqual({ runId: "run-new" });

    // The call order overall is run.start then run.attach.
    const startIdx = client.calls.findIndex((c) => c.method === "run.start");
    const attachIdx = client.calls.findIndex((c) => c.method === "run.attach");
    expect(startIdx).toBeLessThan(attachIdx);

    expect(screen.getByTestId("run-entry")).toBeInTheDocument();
    expect(screen.getByTestId("run-meta")).toHaveAttribute("data-status", "running");
  });

  it("a rapid double-switch (A -> B -> A) discards task B's now-stale fetch instead of letting it overwrite the re-selected task A view", async () => {
    const client = new FakeWsClient();
    const { rerender } = render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);

    // Task A's first run.list is issued but never resolved before we move on.
    const staleListCall = client.nth("run.list", 0);

    rerender(<TaskDetailPane client={client} task={TASK_B} chat={CHAT_B} isDefaultChat />);
    client.nth("run.list", 1).resolve([]);
    await flush();

    rerender(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);
    await flush();

    // The stale first fetch for A resolves late, after A was reselected --
    // it must not populate the (re-selected) view.
    staleListCall.resolve([doneRun({ Prompt: "STALE" })]);
    await flush();
    expect(screen.queryByText("STALE")).not.toBeInTheDocument();

    // The fresh run.list issued by the second A selection is what should
    // drive the view.
    const freshListCall = client.nth("run.list", 2);
    freshListCall.resolve([doneRun({ Prompt: "FRESH" })]);
    await flush();
    client.nth("run.logs", 0).resolve({
      runId: "run-1",
      status: "done",
      stopReason: "end_turn",
      events: [{ type: "done", stopReason: "end_turn" }],
    } satisfies RunLogsResult);
    await flush();

    expect(screen.getByText("FRESH")).toBeInTheDocument();
    expect(screen.getAllByTestId("run-entry")).toHaveLength(1);
  });

  // Item 10 moved Stop from the run card into the composer -- one control
  // per task, always in the same place. These two keep asserting the
  // behaviour that mattered (run.stop, never an abort of the live
  // run.attach), just against the composer's button.
  it("shows a Stop button in the composer only while a run is live, and clicking it calls run.stop (not an abort)", async () => {
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);

    client.nth("run.list", 0).resolve([runningRun()]);
    await flush();

    const composer = screen.getByTestId("composer");
    // The run card no longer carries its own Stop.
    expect(within(screen.getByTestId("run-entry")).queryByRole("button", { name: "Stop" })).not.toBeInTheDocument();

    fireEvent.click(within(composer).getByRole("button", { name: "Stop" }));
    await flush();

    const stopCall = client.nth("run.stop", 0);
    expect(stopCall.params).toEqual({ runId: "run-1" });
    // Stopping must go through run.stop, never by aborting the active
    // run.attach subscription (that would only detach, not stop).
    expect(client.nth("run.attach", 0).options?.signal?.aborted).toBeFalsy();

    stopCall.resolve(undefined);
    await flush();

    // The run's own run.attach subscription observes the stop as its
    // terminal response, same as any other subscriber -- simulate the
    // server ending the stream that way, and confirm the Stop button
    // disappears once the run is no longer "running".
    client.nth("run.attach", 0).resolve({ runId: "run-1", stopReason: "" });
    await flush();
    expect(within(screen.getByTestId("composer")).queryByRole("button", { name: "Stop" })).not.toBeInTheDocument();
  });

  it("surfaces a run.stop failure without crashing, keeping the Stop button usable", async () => {
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);

    client.nth("run.list", 0).resolve([runningRun()]);
    await flush();

    fireEvent.click(within(screen.getByTestId("composer")).getByRole("button", { name: "Stop" }));
    await flush();

    client.nth("run.stop", 0).reject(new Error("boom"));
    await flush();

    expect(screen.getByText(/boom/i)).toBeInTheDocument();
    expect(within(screen.getByTestId("composer")).getByRole("button", { name: "Stop" })).not.toBeDisabled();
  });

  it("renders a pending permission request with a button per option when a permission_request event arrives", async () => {
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);

    client.nth("run.list", 0).resolve([runningRun()]);
    await flush();

    expect(screen.queryByTestId("pending-permission")).not.toBeInTheDocument();

    client.emit("run.attach", 0, "permission_request", {
      requestId: "req-1",
      summary: "Run a risky command",
      options: [
        { id: "allow-1", label: "Allow", kind: "allow_once" },
        { id: "deny-1", label: "Deny", kind: "reject_once" },
      ],
    });
    await flush();

    const pending = screen.getByTestId("pending-permission");
    expect(within(pending).getByText("Run a risky command")).toBeInTheDocument();
    expect(within(pending).getByRole("button", { name: "Allow" })).toBeInTheDocument();
    expect(within(pending).getByRole("button", { name: "Deny" })).toBeInTheDocument();
  });

  it("clicking a permission option calls run.respondPermission with the run/request/option ids", async () => {
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);

    client.nth("run.list", 0).resolve([runningRun()]);
    await flush();

    client.emit("run.attach", 0, "permission_request", {
      requestId: "req-1",
      summary: "Run a risky command",
      options: [
        { id: "allow-1", label: "Allow", kind: "allow_once" },
        { id: "deny-1", label: "Deny", kind: "reject_once" },
      ],
    });
    await flush();

    fireEvent.click(within(screen.getByTestId("pending-permission")).getByRole("button", { name: "Allow" }));
    await flush();

    const respondCall = client.nth("run.respondPermission", 0);
    expect(respondCall.params).toEqual({ runId: "run-1", requestId: "req-1", optionId: "allow-1" });

    // The prompt stays up (buttons disabled) until a permission_resolved
    // event actually clears it -- see the next two tests.
    expect(screen.getByTestId("pending-permission")).toBeInTheDocument();
    expect(within(screen.getByTestId("pending-permission")).getByRole("button", { name: "Allow" })).toBeDisabled();
  });

  it("clears the pending permission prompt once a matching permission_resolved event arrives from this tab's own click", async () => {
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);

    client.nth("run.list", 0).resolve([runningRun()]);
    await flush();

    client.emit("run.attach", 0, "permission_request", {
      requestId: "req-1",
      summary: "Run a risky command",
      options: [{ id: "allow-1", label: "Allow", kind: "allow_once" }],
    });
    await flush();

    fireEvent.click(within(screen.getByTestId("pending-permission")).getByRole("button", { name: "Allow" }));
    await flush();

    client.nth("run.respondPermission", 0).resolve(undefined);
    await flush();
    // Resolving the RPC call alone must not clear the prompt -- only the
    // server's own "permission_resolved" event does, since another
    // connection's answer arrives the same way and must behave identically.
    expect(screen.getByTestId("pending-permission")).toBeInTheDocument();

    client.emit("run.attach", 0, "permission_resolved", { requestId: "req-1", optionId: "allow-1" });
    await flush();

    expect(screen.queryByTestId("pending-permission")).not.toBeInTheDocument();
  });

  it("clears the pending permission prompt on a permission_resolved event that did not originate from this tab's own click", async () => {
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);

    client.nth("run.list", 0).resolve([runningRun()]);
    await flush();

    client.emit("run.attach", 0, "permission_request", {
      requestId: "req-1",
      summary: "Run a risky command",
      options: [{ id: "allow-1", label: "Allow", kind: "allow_once" }],
    });
    await flush();
    expect(screen.getByTestId("pending-permission")).toBeInTheDocument();

    // No respondPermission call was ever made from this tab -- some other
    // connection answered it, and the resolution still arrives as a normal
    // event on this run's own subscription.
    expect(client.calls.some((c) => c.method === "run.respondPermission")).toBe(false);

    client.emit("run.attach", 0, "permission_resolved", { requestId: "req-1", optionId: "allow-1" });
    await flush();

    expect(screen.queryByTestId("pending-permission")).not.toBeInTheDocument();
  });

  it("keeps the pending-permission card outside the scrolling log, pinned above the prompt form, while log chunks keep streaming in", async () => {
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);

    client.nth("run.list", 0).resolve([runningRun()]);
    await flush();

    client.emit("run.attach", 0, "permission_request", {
      requestId: "req-1",
      summary: "Run a risky command",
      options: [{ id: "allow-1", label: "Allow", kind: "allow_once" }],
    });
    await flush();

    // The card lives in a dock that is a *sibling* of the scrolling log
    // container, not a descendant of it -- scrolling the log (or it
    // growing arbitrarily long) can never carry the card off-screen with
    // it, unlike the old inline-in-the-run-entry placement.
    const scrollRegion = screen.getByTestId("run-log-scroll");
    const dock = screen.getByTestId("pending-permission-dock");
    expect(scrollRegion.contains(dock)).toBe(false);
    expect(within(dock).getByTestId("pending-permission")).toBeInTheDocument();
    // L-AC6: borderless like the timeline: the card alone, no border or background block behind it.
    expect(dock.className).not.toMatch(/(^|\s)(border|bg-)/);

    // A long/streaming log keeps growing inside the scroll region...
    for (let i = 0; i < 50; i++) {
      client.emit("run.attach", 0, "chunk", { text: `line ${i}\n` });
    }
    await flush();
    expect(screen.getByTestId("timeline-assistant")).toHaveTextContent("line 49");

    // ...and the pending permission card is still right there, unaffected,
    // no scrolling needed to find it.
    expect(screen.getByTestId("pending-permission-dock")).toBeInTheDocument();
    expect(screen.getByTestId("pending-permission")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Allow" })).toBeInTheDocument();
  });

  it("surfaces a run.respondPermission failure without crashing, keeping the option buttons usable", async () => {
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);

    client.nth("run.list", 0).resolve([runningRun()]);
    await flush();

    client.emit("run.attach", 0, "permission_request", {
      requestId: "req-1",
      summary: "Run a risky command",
      options: [{ id: "allow-1", label: "Allow", kind: "allow_once" }],
    });
    await flush();

    fireEvent.click(within(screen.getByTestId("pending-permission")).getByRole("button", { name: "Allow" }));
    await flush();

    client.nth("run.respondPermission", 0).reject(new Error("boom"));
    await flush();

    const pending = screen.getByTestId("pending-permission");
    expect(within(pending).getByText(/boom/i)).toBeInTheDocument();
    expect(within(pending).getByRole("button", { name: "Allow" })).not.toBeDisabled();
  });

  it("shows a connection-lost banner while connectionStatus is 'reconnecting', without discarding the currently-shown runs", async () => {
    const client = new FakeWsClient();
    const { rerender } = render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat connectionStatus="connected" />);

    client.nth("run.list", 0).resolve([runningRun()]);
    await flush();
    client.emit("run.attach", 0, "chunk", { text: "hello" });
    await flush();

    expect(screen.queryByTestId("connection-banner")).not.toBeInTheDocument();

    rerender(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat connectionStatus="reconnecting" />);
    await flush();

    expect(screen.getByTestId("connection-banner")).toBeInTheDocument();
    // The banner is additive -- the run entry already on screen (with its
    // already-streamed text) is not thrown away while waiting to resync.
    expect(screen.getByTestId("timeline-assistant")).toHaveTextContent("hello");

    rerender(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat connectionStatus="connected" />);
    await flush();
    expect(screen.queryByTestId("connection-banner")).not.toBeInTheDocument();
  });

  it("given a new post-reconnect client, re-issues run.list and re-attaches to the same still-running run instead of a fresh run.start", async () => {
    const client1 = new FakeWsClient();
    const { rerender } = render(<TaskDetailPane client={client1} task={TASK_A} chat={CHAT_A} isDefaultChat />);

    client1.nth("run.list", 0).resolve([runningRun()]);
    await flush();
    expect(client1.nth("run.attach", 0).params).toEqual({ runId: "run-1" });
    client1.emit("run.attach", 0, "chunk", { text: "partial output" });
    await flush();
    expect(screen.getByTestId("timeline-assistant")).toHaveTextContent("partial output");

    // App.tsx swaps in a genuinely new WsClient instance after a
    // successful reconnect -- the task selection itself is untouched.
    const client2 = new FakeWsClient();
    rerender(<TaskDetailPane client={client2} task={TASK_A} chat={CHAT_A} isDefaultChat />);
    await flush();

    expect(client2.nth("run.list", 0).params).toEqual({ chatId: CHAT_A.ID });
    client2.nth("run.list", 0).resolve([runningRun()]);
    await flush();

    // Re-attaches to the same still-running run id -- never a fresh
    // run.start, which would spawn a duplicate run server-side.
    expect(client2.calls.some((c) => c.method === "run.start")).toBe(false);
    const reattach = client2.nth("run.attach", 0);
    expect(reattach.params).toEqual({ runId: "run-1" });

    client2.emit("run.attach", 0, "chunk", { text: "more output" });
    await flush();
    expect(screen.getByTestId("timeline-assistant")).toHaveTextContent("more output");
  });

  it("the combined picker lists the providers from provider.list (3 providers, labels over ids)", async () => {
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);

    const listCall = client.nth("provider.list", 0);
    expect(listCall.params).toBeUndefined();
    listCall.resolve({
      providers: [
        { id: "claude-native", label: "Claude Code" },
        { id: "glm", label: "GLM" },
        { id: "codex-native", label: "Codex" },
      ],
    } satisfies ProviderListResult);
    await flush();

    fireEvent.click(screen.getByTestId("composer-model-picker"));
    // Opens on the current provider's view; Back lists the providers.
    fireEvent.click(await screen.findByTestId("model-picker-back"));
    const rows = await screen.findAllByTestId(/^model-provider-/);
    expect(rows.map((r) => r.getAttribute("data-testid"))).toEqual([
      "model-provider-claude-native",
      "model-provider-glm",
      "model-provider-codex-native",
    ]);
    expect(rows.map((r) => r.textContent?.replace(/(\d+ models?|Default model)$/, ""))).toEqual(["Claude Code", "GLM", "Codex"]);
  });

  it("falls back to the built-in four-provider list when provider.list rejects (catalog labels), and the form still submits", async () => {
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);

    client.nth("run.list", 0).resolve([]);
    client.nth("provider.list", 0).reject(new Error("boom"));
    await flush();

    fireEvent.click(screen.getByTestId("composer-model-picker"));
    // Opens on the current provider's view; Back lists the providers.
    fireEvent.click(await screen.findByTestId("model-picker-back"));
    const rows = await screen.findAllByTestId(/^model-provider-/);
    expect(rows.map((r) => r.getAttribute("data-testid"))).toEqual(["model-provider-claude-native", "model-provider-codex-native", "model-provider-glm", "model-provider-kimi"]);
    expect(rows.map((r) => r.textContent?.replace(/(\d+ models?|Default model)$/, ""))).toEqual(["Claude Code", "Codex", "GLM", "Kimi"]);
    fireEvent.click(rows[0]);
    fireEvent.click(await screen.findByTestId("model-row-claude-native:"));

    fireEvent.change(screen.getByLabelText("Prompt"), { target: { value: "do the thing" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    await flush();

    const startCall = client.nth("run.start", 0);
    expect(startCall.params).toEqual({ taskId: TASK_A.ID, chatId: CHAT_A.ID, provider: "claude-native", prompt: "do the thing", whenBusy: "queue" });
  });

  it("defaults the permission-mode selector to the provider default and omits permissionMode from run.start", async () => {
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);

    client.nth("run.list", 0).resolve([]);
    await flush();

    expect(screen.getByLabelText("Permission mode")).toHaveTextContent("Accept File Edits");

    fireEvent.change(screen.getByLabelText("Prompt"), { target: { value: "do the thing" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    await flush();

    const startCall = client.nth("run.start", 0);
    expect(startCall.params).toEqual({ taskId: TASK_A.ID, chatId: CHAT_A.ID, provider: "claude-native", prompt: "do the thing", whenBusy: "queue" });
  });

  it("sends the picked permissionMode in run.start", async () => {
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);

    client.nth("run.list", 0).resolve([]);
    await flush();

    fireEvent.click(screen.getByLabelText("Permission mode"));
    fireEvent.click(await screen.findByRole("option", { name: "Plan Mode" }));
    fireEvent.change(screen.getByLabelText("Prompt"), { target: { value: "do the thing" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    await flush();

    const startCall = client.nth("run.start", 0);
    expect(startCall.params).toEqual({
      taskId: TASK_A.ID,
      chatId: CHAT_A.ID,
      provider: "claude-native",
      prompt: "do the thing",
      permissionMode: "plan",
      whenBusy: "queue",
    });
  });

  it("fetches and renders GLM's live config options, but never for a Claude run", async () => {
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);

    client.nth("run.list", 0).resolve([runningRun({ Provider: "glm" })]);
    await flush();

    const listCall = client.nth("run.listConfigOptions", 0);
    expect(listCall.params).toEqual({ runId: "run-1" });
    listCall.resolve({
      options: [
        {
          configId: "thinking-level",
          name: "Thinking Level",
          type: "select",
          currentValue: "medium",
          options: [
            { value: "low", name: "Low" },
            { value: "medium", name: "Medium" },
            { value: "high", name: "High" },
          ],
        },
      ],
    });
    await flush();

    expect(screen.getByLabelText("Thinking Level")).toHaveTextContent("Medium");
    // A Claude/Codex run never even asks -- there's no live-session config
    // surface for it at all (see ThinkingLevel's own doc comment).
    expect(client.calls.some((c) => c.method === "run.listConfigOptions")).toBe(true);
    expect(client.calls.filter((c) => c.method === "run.listConfigOptions")).toHaveLength(1);
    // run-config IA: GLM/Kimi's live options render inside the composer's
    // RunConfigToolbar row, not a separate control elsewhere in the pane.
    expect(within(screen.getByTestId("composer-toolbar-row")).getByLabelText("Thinking Level")).toBeInTheDocument();
  });

  it("omits the config-options control entirely for a Codex run (its model applies from the next run, ADR-0025)", async () => {
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);

    client.nth("run.list", 0).resolve([runningRun({ Provider: "codex-native" })]);
    await flush();
    client.emit("run.attach", 0, "chunk", { text: "hi" });
    await flush();

    expect(client.calls.some((c) => c.method === "run.listConfigOptions")).toBe(false);
    expect(screen.queryByTestId("run-config-options")).not.toBeInTheDocument();
  });

  it("re-fetches config options as the live run streams more events", async () => {
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);

    client.nth("run.list", 0).resolve([runningRun({ Provider: "kimi" })]);
    await flush();
    client.nth("run.listConfigOptions", 0).resolve({ options: [] });
    await flush();

    // Nothing to show yet -- the session's option list wasn't ready on the
    // first fetch (a plausible race between run.start and NewSession).
    expect(screen.queryByTestId("run-config-options")).not.toBeInTheDocument();

    client.emit("run.attach", 0, "chunk", { text: "hello" });
    await flush();

    const secondList = client.nth("run.listConfigOptions", 1);
    expect(secondList.params).toEqual({ runId: "run-1" });
    secondList.resolve({
      options: [{ configId: "web-search", name: "Web Search", type: "boolean", currentValue: true }],
    });
    await flush();

    expect(screen.getByTestId("run-config-option-toggle")).toHaveTextContent("On");
  });

  it("changing a config option calls run.setConfigOption and reflects the new value", async () => {
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);

    client.nth("run.list", 0).resolve([runningRun({ Provider: "glm" })]);
    await flush();
    client.nth("run.listConfigOptions", 0).resolve({
      options: [
        {
          configId: "thinking-level",
          name: "Thinking Level",
          type: "select",
          currentValue: "medium",
          options: [
            { value: "low", name: "Low" },
            { value: "high", name: "High" },
          ],
        },
      ],
    });
    await flush();

    fireEvent.click(screen.getByLabelText("Thinking Level"));
    fireEvent.click(await screen.findByRole("option", { name: "High" }));
    await flush();

    const setCall = client.nth("run.setConfigOption", 0);
    expect(setCall.params).toEqual({ runId: "run-1", configId: "thinking-level", value: "high" });

    await act(async () => {
      setCall.resolve({
        options: [
          {
            configId: "thinking-level",
            name: "Thinking Level",
            type: "select",
            currentValue: "high",
            options: [
              { value: "low", name: "Low" },
              { value: "high", name: "High" },
            ],
          },
        ],
      });
    });

    expect(screen.getByLabelText("Thinking Level")).toHaveTextContent("High");
  });

  it("model option change calls run.setConfigOption", async () => {
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);

    client.nth("run.list", 0).resolve([runningRun({ Provider: "claude-native" })]);
    await flush();
    client.nth("run.listConfigOptions", 0).resolve({
      options: [
        {
          configId: "model",
          name: "Model",
          category: "model",
          type: "select",
          currentValue: "default",
          options: [
            { value: "default", name: "Default" },
            { value: "opus", name: "Opus (latest)" },
          ],
        },
      ],
    });
    await flush();

    // Named apart from the composer's own (next-run) Model picker.
    fireEvent.click(screen.getByLabelText("Model (this session)"));
    fireEvent.click(await screen.findByRole("option", { name: "Opus (latest)" }));
    await flush();

    expect(client.nth("run.setConfigOption", 0).params).toEqual({ runId: "run-1", configId: "model", value: "opus" });
  });

  it("surfaces a failed run.setConfigOption as an error instead of silently no-op'ing", async () => {
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);

    client.nth("run.list", 0).resolve([runningRun({ Provider: "glm" })]);
    await flush();
    client.nth("run.listConfigOptions", 0).resolve({
      options: [{ configId: "thinking-level", name: "Thinking Level", type: "select", currentValue: "medium" }],
    });
    await flush();

    fireEvent.click(screen.getByRole("button", { name: "Set" }));
    await flush();

    const setCall = client.nth("run.setConfigOption", 0);
    await act(async () => {
      setCall.reject(new Error("no such config option"));
    });

    expect(screen.getByTestId("run-config-options-error")).toHaveTextContent("no such config option");
  });

  it("the Mode: row is gone and the toolbar mode pill drives the live run", async () => {
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);

    client.nth("run.list", 0).resolve([runningRun({ PermissionMode: "acceptEdits" })]);
    await flush();

    expect(screen.queryByTestId("permission-mode-control")).not.toBeInTheDocument();
    expect(screen.queryByText("Mode:")).not.toBeInTheDocument();
    expect(screen.getAllByLabelText("Permission mode")).toHaveLength(1);
    const pill = screen.getByLabelText("Permission mode");
    expect(pill).toHaveTextContent("Accept File Edits");
    // Claude takes no auto-accept toggle.
    expect(screen.queryByTestId("composer-auto-accept")).not.toBeInTheDocument();

    fireEvent.click(pill);
    fireEvent.click(await screen.findByRole("option", { name: "Plan Mode" }));
    await flush();

    const setCall = client.nth("run.setPermissionMode", 0);
    expect(setCall.params).toEqual({ runId: "run-1", modeId: "plan" });
    await act(async () => {
      setCall.resolve({ permissionMode: "plan", autoAccept: false });
    });
    expect(screen.getByLabelText("Permission mode")).toHaveTextContent("Plan Mode");
  });

  it("the Auto-accept pill flips a running ACP run via run.setPermissionMode", async () => {
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);

    client.nth("run.list", 0).resolve([runningRun({ Provider: "glm", PermissionMode: "default" })]);
    client.nth("provider.list", 0).resolve({
      providers: [{ id: "glm", label: "GLM", defaultMode: "default", supportsAutoAccept: true, liveModeSwitch: true, modes: [{ id: "default", label: "Default" }] }],
    });
    await flush();

    const toggle = screen.getByTestId("composer-auto-accept");
    expect(toggle).toHaveAttribute("aria-pressed", "false");
    fireEvent.click(toggle);
    await flush();

    const setCall = client.nth("run.setPermissionMode", 0);
    expect(setCall.params).toEqual({ runId: "run-1", autoAccept: true });
    await act(async () => {
      setCall.resolve({ permissionMode: "default", autoAccept: true });
    });
    expect(screen.getByTestId("composer-auto-accept")).toHaveAttribute("aria-pressed", "true");
  });

  it("surfaces a failed run.setPermissionMode as an error instead of silently no-op'ing", async () => {
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);

    client.nth("run.list", 0).resolve([runningRun()]);
    await flush();

    fireEvent.click(screen.getByLabelText("Permission mode"));
    fireEvent.click(await screen.findByRole("option", { name: "Plan Mode" }));
    await flush();

    await act(async () => {
      client.nth("run.setPermissionMode", 0).reject(new Error("run already finished"));
    });

    expect(screen.getByTestId("permission-mode-error")).toHaveTextContent("run already finished");
  });

  it("a running Codex run keeps next-run semantics on the mode pill (no mid-run switch)", async () => {
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);

    client.nth("run.list", 0).resolve([runningRun({ Provider: "codex-native", PermissionMode: "auto" })]);
    await flush();

    fireEvent.click(screen.getByLabelText("Permission mode"));
    const options = await screen.findAllByRole("option");
    fireEvent.click(options[0]);
    await flush();
    expect(client.calls.filter((c) => c.method === "run.setPermissionMode")).toHaveLength(0);
  });

  it("once the run has finished the mode pill edits the next run again (no RPC)", async () => {
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);

    client.nth("run.list", 0).resolve([doneRun()]);
    await flush();
    client.nth("run.logs", 0).resolve({ runId: "run-1", status: "done", events: [] });
    await flush();

    fireEvent.click(screen.getByLabelText("Permission mode"));
    fireEvent.click(await screen.findByRole("option", { name: "Plan Mode" }));
    await flush();
    expect(client.calls.filter((c) => c.method === "run.setPermissionMode")).toHaveLength(0);
    expect(screen.getByLabelText("Permission mode")).toHaveTextContent("Plan Mode");
  });

  it("shows Retry with higher effort for a failed Claude run below extended and resubmits at the next tier", async () => {
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);

    client.nth("run.list", 0).resolve([
      doneRun({
        Status: "error",
        Err: "boom",
        Provider: "claude-native",
        PermissionMode: "plan",
        ThinkingLevel: "off",
      }),
    ]);
    await flush();
    client.nth("run.logs", 0).resolve({ runId: "run-1", status: "error", err: "boom", events: [] });
    await flush();

    const retryButton = screen.getByTestId("run-retry-higher-effort");
    fireEvent.click(retryButton);
    await flush();

    const startCall = client.nth("run.start", 0);
    expect(startCall.params).toEqual({
      taskId: TASK_A.ID,
      chatId: CHAT_A.ID,
      provider: "claude-native",
      prompt: "do the thing",
      permissionMode: "plan",
      thinkingLevel: "standard",
      whenBusy: "queue",
    });
  });

  it("shows no Retry with higher effort for a failed run already at extended", async () => {
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);

    client.nth("run.list", 0).resolve([
      doneRun({ Status: "error", Err: "boom", Provider: "claude-native", ThinkingLevel: "extended" }),
    ]);
    await flush();
    client.nth("run.logs", 0).resolve({ runId: "run-1", status: "error", err: "boom", events: [] });
    await flush();

    expect(screen.queryByTestId("run-retry-higher-effort")).not.toBeInTheDocument();
  });

  it("shows no Retry with higher effort for a failed non-Claude provider's run", async () => {
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);

    client.nth("run.list", 0).resolve([doneRun({ Status: "error", Err: "boom", Provider: "glm", ThinkingLevel: "off" })]);
    await flush();
    client.nth("run.logs", 0).resolve({ runId: "run-1", status: "error", err: "boom", events: [] });
    await flush();

    expect(screen.queryByTestId("run-retry-higher-effort")).not.toBeInTheDocument();
  });

  it("shows no Retry with higher effort for a successful Claude run", async () => {
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);

    client.nth("run.list", 0).resolve([doneRun({ Provider: "claude-native", ThinkingLevel: "off" })]);
    await flush();
    client.nth("run.logs", 0).resolve({ runId: "run-1", status: "done", stopReason: "end_turn", events: [] });
    await flush();

    expect(screen.queryByTestId("run-retry-higher-effort")).not.toBeInTheDocument();
  });
});

describe("run-config pill (run-config IA)", () => {
  it("does not mirror the composer toolbar: no pill without an agent, however the toolbar is configured", async () => {
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);

    client.nth("run.list", 0).resolve([]);
    const providersResult: ProviderListResult = {
      providers: [
        { id: "claude-native", label: "Claude Code" },
        { id: "glm", label: "GLM" },
      ],
    };
    client.nth("provider.list", 0).resolve(providersResult);
    await flush();

    expect(screen.getByTestId("composer-model-picker")).toBeInTheDocument();
    expect(screen.queryByTestId("task-run-config-pill")).not.toBeInTheDocument();

    fireEvent.click(screen.getByTestId("composer-model-picker"));
    fireEvent.click(await screen.findByTestId("model-picker-back"));
    fireEvent.click(await screen.findByTestId("model-provider-glm"));
    fireEvent.click(await screen.findByTestId("model-row-glm:"));
    await flush();
    expect(screen.queryByTestId("task-run-config-pill")).not.toBeInTheDocument();
    // The meta row itself stays; the Detailed toggle has nothing to reveal with no runs.
    expect(screen.getByTestId("task-chat-meta")).toBeInTheDocument();
  });

  it("shows the agent's name once one is picked from the toolbar", async () => {
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);

    client.nth("run.list", 0).resolve([]);
    const profile: AgentProfile = {
      ID: 1,
      Name: "Quick Fixes",
      Provider: "claude-native",
      PermissionMode: "",
      AutoAccept: false,
      ThinkingLevel: "",
      Notes: "",
      CreatedAt: "2026-09-25T00:00:00Z",
      UpdatedAt: "2026-09-25T00:00:00Z",
    };
    // task-detail.tsx (the pill's agent-name lookup) and composer.tsx share ONE profile.list fetch
    // (ADR-0032): resolving it answers both.
    client.nth("profile.list", 0).resolve([profile]);
    await flush();

    fireEvent.click(screen.getByTestId("composer-model-picker"));
    fireEvent.click(await screen.findByTestId("model-picker-back"));
    fireEvent.click(await screen.findByTestId("model-profile-row-1"));
    await flush();

    // Only the agent: provider/model/mode/thinking are the composer toolbar's.
    expect(screen.getByTestId("task-run-config-pill")).toHaveTextContent(/^Quick Fixes$/);

    fireEvent.click(screen.getByTestId("task-run-config-pill"));
    expect(screen.getByTestId("composer-toolbar-row")).toHaveFocus();
  });
});

describe("agentChipLabel", () => {
  const BASE: RunConfigState = { baseAgentId: null, custom: false, provider: "claude-native", permissionMode: "", autoAccept: false, thinkingLevel: "", model: "" };
  const profiles: AgentProfile[] = [
    { ID: 1, Name: "Quick Fixes", Provider: "claude-native", PermissionMode: "", AutoAccept: false, ThinkingLevel: "", Notes: "", CreatedAt: "", UpdatedAt: "" },
  ];

  it("names the agent when one is applied, and marks hand-edits", () => {
    expect(agentChipLabel({ ...BASE, baseAgentId: "1" }, profiles)).toBe("Quick Fixes");
    expect(agentChipLabel({ ...BASE, baseAgentId: "1", custom: true }, profiles)).toBe("Quick Fixes · edited");
  });

  it("is null with no agent, or one that is not (yet) in the profile list", () => {
    expect(agentChipLabel(BASE, profiles)).toBeNull();
    expect(agentChipLabel({ ...BASE, custom: true }, profiles)).toBeNull();
    expect(agentChipLabel({ ...BASE, baseAgentId: "9" }, profiles)).toBeNull();
  });
});

describe("MCP servers pill", () => {
  it("mcp pill reflects the latest run only", async () => {
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);

    // Older run has servers, newest has none -> no pill.
    client.nth("run.list", 0).resolve([
      doneRun({ ID: "run-old", StartedAt: "2024-01-01T00:00:00Z" }),
      doneRun({ ID: "run-new", StartedAt: "2024-01-01T00:00:10Z" }),
    ]);
    await flush();
    client.nth("run.logs", 0).resolve({
      runId: "run-old",
      status: "done",
      events: [{ type: "mcp_servers", mcpServers: [{ name: "github", transport: "stdio", status: "active" }] }],
    } satisfies RunLogsResult);
    client.nth("run.logs", 1).resolve({ runId: "run-new", status: "done", events: [] } satisfies RunLogsResult);
    await flush();

    expect(screen.queryByTestId("run-mcp-indicator")).not.toBeInTheDocument();
  });

  it("mcp pill shows the newest run's servers when it reported them", async () => {
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);

    client.nth("run.list", 0).resolve([doneRun({ ID: "run-old", StartedAt: "2024-01-01T00:00:00Z" })]);
    await flush();
    client.nth("run.logs", 0).resolve({
      runId: "run-old",
      status: "done",
      events: [{ type: "mcp_servers", mcpServers: [{ name: "github", transport: "stdio", status: "active" }] }],
    } satisfies RunLogsResult);
    await flush();

    expect(screen.getByTestId("run-mcp-indicator")).toHaveTextContent("MCP 1");
  });

  it("mcp pill is scoped to the chat's own runs", async () => {
    const client = new FakeWsClient();
    const { rerender } = render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);

    client.nth("run.list", 0).resolve([doneRun()]);
    await flush();
    client.nth("run.logs", 0).resolve({
      runId: "run-1",
      status: "done",
      events: [{ type: "mcp_servers", mcpServers: [{ name: "github", transport: "stdio", status: "active" }] }],
    } satisfies RunLogsResult);
    await flush();
    expect(screen.getByTestId("run-mcp-indicator")).toHaveTextContent("MCP 1");

    // Switch to the task's other chat: the scoping mechanism is run.list's
    // own chatId filter, and the pane drops A's runs immediately -- the
    // pill never lingers on chat B, and B's list is fetched for B's id.
    rerender(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_B} isDefaultChat={false} />);
    await flush();

    expect(client.nth("run.list", 1).params).toEqual({ chatId: CHAT_B.ID });
    expect(screen.queryByTestId("run-mcp-indicator")).not.toBeInTheDocument();

    // Chat B's own run carries no mcp_servers event -> no pill for B either.
    client.nth("run.list", 1).resolve([doneRun({ ID: "run-b", ChatID: CHAT_B.ID })]);
    await flush();
    client.nth("run.logs", 1).resolve({ runId: "run-b", status: "done", events: [] } satisfies RunLogsResult);
    await flush();
    expect(screen.queryByTestId("run-mcp-indicator")).not.toBeInTheDocument();
  });
});

describe("chat usage total", () => {
  it("task-detail renders no chat usage total", async () => {
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);

    client.nth("run.list", 0).resolve([
      runningRun({ usage: { inputTokens: 10000, outputTokens: 1000, costUsd: 0.31, source: "claude_result" } }),
    ]);
    await flush();
    expect(screen.queryByTestId("chat-usage-total")).not.toBeInTheDocument();

    // A live usage event doesn't bring it back either.
    client.emit("run.attach", 0, "usage", {
      usage: { inputTokens: 12345, outputTokens: 1234, costUsd: 0.31, source: "claude_result" },
    });
    await flush();
    expect(screen.queryByTestId("chat-usage-total")).not.toBeInTheDocument();
  });
});

describe("chat meta row (app-header-and-tabs)", () => {
  it("AH-AC12 no title heading (the run-config pill only names an agent); the Detailed toggle hides when a run has nothing to collapse", async () => {
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);
    client.nth("run.list", 0).resolve([]);
    await flush();

    // The "<task> · <chat>" h2 moved to the app header.
    expect(screen.queryByRole("heading", { level: 2 })).not.toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: new RegExp(TASK_A.Title) })).not.toBeInTheDocument();
    expect(screen.queryByText(/·\s*Chat/)).not.toBeInTheDocument();
    expect(screen.queryByTestId("detail-level-toggle")).not.toBeInTheDocument();
  });

  it("hides the Detailed toggle on an error-only run", async () => {
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);
    client.nth("run.list", 0).resolve([doneRun({ Status: "error", Err: "boom" })]);
    await flush();
    client.nth("run.logs", 0).resolve({ runId: "run-1", status: "error", err: "boom", events: [] });
    await flush();
    expect(screen.getByTestId("run-error-box")).toBeInTheDocument();
    expect(screen.queryByTestId("detail-level-toggle")).not.toBeInTheDocument();
  });

  it("keeps the Detailed toggle on a run with two or more tool calls", async () => {
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);
    client.nth("run.list", 0).resolve([doneRun()]);
    await flush();
    client.nth("run.logs", 0).resolve({
      runId: "run-1",
      status: "done",
      events: [
        { type: "tool_call", toolCallId: "t1", toolName: "Bash", title: "ls", status: "success" },
        { type: "tool_call", toolCallId: "t2", toolName: "Read", title: "a.go", status: "success" },
      ],
    } as RunLogsResult);
    await flush();
    expect(screen.getByTestId("detail-level-toggle")).toBeInTheDocument();
  });
});

describe("TaskDetailPane message hover actions", () => {
  async function renderWithDoneRun(overrides: Partial<RunSummary> = {}, stopReason = "end_turn") {
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);
    client.nth("run.list", 0).resolve([doneRun({ ...overrides, StopReason: stopReason })]);
    await flush();
    client.nth("run.logs", 0).resolve({
      runId: "run-1",
      status: "done",
      stopReason,
      events: [{ type: "chunk", text: "answer" }, { type: "done", stopReason }],
    } satisfies RunLogsResult);
    await flush();
    return client;
  }

  it("Continue sends \"Continue\" with the run's provider and model through the queue path", async () => {
    const client = await renderWithDoneRun({
      Provider: "glm",
      Model: "glm-5.3",
      PermissionMode: "accept_edits",
      AutoAccept: true,
      ThinkingLevel: "extended",
    }, "max_tokens");
    fireEvent.click(await screen.findByTestId("run-continue"));
    await flush();
    const startCall = client.nth("run.start", 0);
    expect(startCall.params).toEqual({
      taskId: TASK_A.ID,
      chatId: CHAT_A.ID,
      provider: "glm",
      prompt: "Continue",
      whenBusy: "queue",
      permissionMode: "accept_edits",
      autoAccept: true,
      thinkingLevel: "extended",
      model: "glm-5.3",
    });
    expect(client.calls.some((c) => c.method === "task.prompt")).toBe(false);
  });

  describe("J1: Continue on an errored run", () => {
    async function renderWithErrorRun(overrides: Partial<RunSummary>) {
      const client = new FakeWsClient();
      render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);
      client.nth("run.list", 0).resolve([doneRun({ Status: "error", StopReason: "", Err: "claude CLI exited before sending a result message", ...overrides })]);
      await flush();
      client.nth("run.logs", 0).resolve({
        runId: "run-1",
        status: "error",
        stopReason: "",
        // run.logs carries the same derived fields as run.list: a terminal answer replaces what the list seeded.
        disposition: overrides.Disposition || undefined,
        failureKind: overrides.FailureKind || undefined,
        window: overrides.FailureWindow || undefined,
        resetAt: overrides.FailureResetAt || undefined,
        events: [],
      } satisfies RunLogsResult);
      await flush();
      return client;
    }

    it("cli_exited with a disposition: one click sends the error prompt once, on the run's own provider/model, through the queue", async () => {
      const client = await renderWithErrorRun({ Provider: "glm", Model: "glm-5.3", FailureKind: "cli_exited", Disposition: "needs-continue" });
      const button = await screen.findByTestId("run-continue");
      fireEvent.click(button);
      fireEvent.click(button);
      await flush();
      expect(client.calls.filter((c) => c.method === "run.start")).toHaveLength(1);
      expect(client.nth("run.start", 0).params).toEqual({
        taskId: TASK_A.ID,
        chatId: CHAT_A.ID,
        provider: "glm",
        prompt: "Continue from where you left off (the previous run ended unexpectedly: the agent CLI exited without a result).",
        whenBusy: "queue",
        permissionMode: "acceptEdits",
        model: "glm-5.3",
      });
      expect(screen.queryByTestId("run-retry")).toBeNull();
    });

    it("no model is pinned when the run had none", async () => {
      const client = await renderWithErrorRun({ FailureKind: "cli_exited", Disposition: "needs-continue", Model: "" });
      fireEvent.click(await screen.findByTestId("run-continue"));
      await flush();
      expect(client.nth("run.start", 0).params).not.toHaveProperty("model");
    });

    it.each([["permission/timeout", ""], ["auth", "auth"], ["rate_limited before reset", "rate_limited"], ["cli_exited without a session", "cli_exited"]])(
      "%s has no disposition: Retry, no Continue",
      async (_name, FailureKind) => {
        await renderWithErrorRun({ FailureKind, FailureResetAt: FailureKind === "rate_limited" ? "2999-01-01T00:00:00Z" : undefined });
        expect(screen.queryByTestId("run-continue")).toBeNull();
        expect(await screen.findByTestId("run-retry")).toBeTruthy();
      },
    );

    it("rate_limited after its reset (disposition) offers Continue", async () => {
      await renderWithErrorRun({ FailureKind: "rate_limited", FailureWindow: "5h", FailureResetAt: "2020-01-01T00:00:00Z", Disposition: "needs-continue" });
      expect(await screen.findByTestId("run-continue")).toBeTruthy();
      expect(screen.getByTestId("run-stop-reason").textContent).toBe("Rate limited (5h window) · limit has reset");
    });
  });

  it("Retry on fallback creates a new chat and sends profileId", async () => {
    const client = new FakeWsClient();
    const jumps: number[] = [];
    render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat onJumpToChat={(id) => jumps.push(id)} />);
    client.nth("profile.list", 0).resolve([
      {
        ID: 9,
        Name: "Backup GLM",
        Provider: "glm",
        PermissionMode: "accept_edits",
        AutoAccept: true,
        ThinkingLevel: "extended",
        Model: "glm-5.3",
        Notes: "",
        CreatedAt: "2024-01-01T00:00:00Z",
        UpdatedAt: "2024-01-01T00:00:00Z",
      } satisfies AgentProfile,
    ]);
    client.nth("run.list", 0).resolve([
      doneRun({ Status: "error", Err: "limit", StopReason: "", FailureKind: "rate_limited", FailureWindow: "5h", FailureResetAt: "2999-01-01T00:00:00Z", ProfileID: 7 }),
    ]);
    await flush();
    client.nth("run.logs", 0).resolve({ runId: "run-1", status: "error", failureKind: "rate_limited", window: "5h", resetAt: "2999-01-01T00:00:00Z", profileId: 7, fallbackProfileId: 9, events: [] } satisfies RunLogsResult);
    await flush();

    fireEvent.click(await screen.findByTestId("run-retry-fallback"));
    await flush();
    expect(client.nth("chat.create", 0).params).toEqual({ taskId: TASK_A.ID });
    client.nth("chat.create", 0).resolve({ ...CHAT_A, ID: 11 });
    await flush();
    expect(client.nth("run.start", 0).params).toEqual({
      taskId: TASK_A.ID,
      chatId: 11,
      provider: "glm",
      prompt: "do the thing",
      whenBusy: "queue",
      permissionMode: "accept_edits",
      autoAccept: true,
      thinkingLevel: "extended",
      model: "glm-5.3",
      profileId: 9,
    });
    client.nth("run.start", 0).resolve({ runId: "run-2" });
    await flush();
    expect(jumps).toEqual([11]);
  });

  it("Wait until queues the same prompt on this chat with notBefore and the run's profileId", async () => {
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);
    client.nth("run.list", 0).resolve([
      doneRun({ Status: "error", Err: "limit", StopReason: "", FailureKind: "rate_limited", FailureWindow: "5h", FailureResetAt: "2999-01-01T00:00:00Z", ProfileID: 7 }),
    ]);
    await flush();
    client.nth("run.logs", 0).resolve({ runId: "run-1", status: "error", failureKind: "rate_limited", resetAt: "2999-01-01T00:00:00Z", events: [] } satisfies RunLogsResult);
    await flush();

    fireEvent.click(await screen.findByTestId("run-wait"));
    await flush();
    expect(client.nth("run.start", 0).params).toEqual({
      taskId: TASK_A.ID,
      chatId: CHAT_A.ID,
      provider: "claude-native",
      prompt: "do the thing",
      whenBusy: "queue",
      permissionMode: "acceptEdits",
      profileId: 7,
      notBefore: "2999-01-01T00:00:30.000Z",
    });
    client.nth("run.start", 0).resolve({ queued: true, queueItemId: 5 });
    await flush();
    expect(screen.getByTestId("run-wait-queued")).toBeInTheDocument();
  });

  it("Re-run submits the same prompt, provider, model and permission with whenBusy queue", async () => {
    const client = await renderWithDoneRun({
      Provider: "glm",
      Model: "glm-5.3",
      PermissionMode: "accept_edits",
      AutoAccept: true,
      ThinkingLevel: "extended",
    });

    fireEvent.click(screen.getByTestId("run-rerun"));
    await flush();

    expect(client.nth("run.start", 0).params).toEqual({
      taskId: TASK_A.ID,
      chatId: CHAT_A.ID,
      provider: "glm",
      prompt: "do the thing",
      whenBusy: "queue",
      permissionMode: "accept_edits",
      autoAccept: true,
      thinkingLevel: "extended",
      model: "glm-5.3",
    });
    expect(screen.queryByTestId("rerun-error")).not.toBeInTheDocument();
  });

  it("Re-run uses the composer's current permission mode on the same provider, not the old run's", async () => {
    const client = await renderWithDoneRun({ Provider: "claude-native", PermissionMode: "bypassPermissions", AutoAccept: true });

    fireEvent.click(screen.getByTestId("run-rerun"));
    await flush();

    const params = client.nth("run.start", 0).params as Record<string, unknown>;
    expect(params.provider).toBe("claude-native");
    expect(params.permissionMode).not.toBe("bypassPermissions");
    expect(params.autoAccept).toBeUndefined();
  });

  it("a double click on Re-run submits once", async () => {
    const client = await renderWithDoneRun();

    fireEvent.click(screen.getByTestId("run-rerun"));
    fireEvent.click(screen.getByTestId("run-rerun"));
    await flush();

    expect(client.calls.filter((c) => c.method === "run.start")).toHaveLength(1);
  });

  it("Re-run failure shows an inline alert", async () => {
    const client = await renderWithDoneRun();

    fireEvent.click(screen.getByTestId("run-rerun"));
    await flush();
    client.nth("run.start", 0).reject(new Error("daemon says no"));
    await flush();

    expect(screen.getByTestId("rerun-error")).toHaveTextContent("daemon says no");
    expect(screen.getByTestId("run-entry")).toBeInTheDocument();
  });

  it("Edit & resend prefills the composer draft: empty is replaced, non-empty is appended after a blank line, and the textarea is focused", async () => {
    await renderWithDoneRun();
    const input = screen.getByLabelText("Prompt") as HTMLTextAreaElement;

    fireEvent.click(screen.getByTestId("run-prompt-edit"));
    await flush();
    expect(input.value).toBe("do the thing");
    expect(input).toHaveFocus();

    fireEvent.change(input, { target: { value: "my notes" } });
    input.blur();
    fireEvent.click(screen.getByTestId("run-prompt-edit"));
    await flush();
    expect(input.value).toBe("my notes\n\ndo the thing");
    expect(input).toHaveFocus();
  });
});
