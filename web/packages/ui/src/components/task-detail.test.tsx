import { act } from "react";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { FakeWsClient } from "@/test/fake-ws-client";
import { agentChipLabel, TaskDetailPane } from "@/components/task-detail";
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
    expect(within(entry).getByTestId("run-status")).toHaveTextContent("done");
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
    expect(screen.getByTestId("run-status")).toHaveTextContent("running");

    client.emit("run.attach", 0, "chunk", { text: "and more" });
    await flush();
    expect(screen.getByTestId("timeline-assistant")).toHaveTextContent("partial and more");

    client.nth("run.attach", 0).resolve({ runId: "run-1", stopReason: "end_turn" });
    await flush();
    expect(screen.getByTestId("run-status")).toHaveTextContent("done");
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
    expect(screen.getByTestId("run-status")).toHaveTextContent("running");
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
    const rows = await screen.findAllByTestId(/^model-provider-/);
    expect(rows.map((r) => r.getAttribute("data-testid"))).toEqual([
      "model-provider-claude-native",
      "model-provider-glm",
      "model-provider-codex-native",
    ]);
    expect(rows.map((r) => r.textContent?.replace(/\d+ models?$/, ""))).toEqual(["Claude Code", "GLM", "Codex"]);
  });

  it("falls back to the hardcoded two-provider list when provider.list rejects, and the form still submits", async () => {
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);

    client.nth("run.list", 0).resolve([]);
    client.nth("provider.list", 0).reject(new Error("boom"));
    await flush();

    fireEvent.click(screen.getByTestId("composer-model-picker"));
    const rows = await screen.findAllByTestId(/^model-provider-/);
    expect(rows.map((r) => r.getAttribute("data-testid"))).toEqual(["model-provider-claude-native", "model-provider-glm"]);
    expect(rows.map((r) => r.textContent?.replace(/\d+ models?$/, ""))).toEqual(["claude-native", "glm"]);
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
    client.nth("provider.list", 1).resolve(providersResult);
    await flush();

    expect(screen.getByTestId("composer-model-picker")).toBeInTheDocument();
    expect(screen.queryByTestId("task-run-config-pill")).not.toBeInTheDocument();

    fireEvent.click(screen.getByTestId("composer-model-picker"));
    fireEvent.click(await screen.findByTestId("model-provider-glm"));
    fireEvent.click(await screen.findByTestId("model-row-glm:"));
    await flush();
    expect(screen.queryByTestId("task-run-config-pill")).not.toBeInTheDocument();
    // The meta row itself stays (Detailed toggle).
    expect(screen.getByTestId("detail-level-toggle")).toBeInTheDocument();
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
    // Both task-detail.tsx (the pill's own agent-name lookup) and
    // composer.tsx fetch profile.list independently -- resolve both.
    client.nth("profile.list", 0).resolve([profile]);
    client.nth("profile.list", 1).resolve([profile]);
    await flush();

    fireEvent.click(screen.getByTestId("composer-model-picker"));
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
  it("task-detail: the header total updates live from a usage event", async () => {
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);

    client.nth("run.list", 0).resolve([runningRun()]);
    await flush();
    expect(screen.queryByTestId("chat-usage-total")).not.toBeInTheDocument();

    client.emit("run.attach", 0, "usage", {
      usage: { inputTokens: 12345, outputTokens: 1234, costUsd: 0.31, source: "claude_result" },
    });
    await flush();

    expect(screen.getByTestId("chat-usage-total")).toHaveTextContent("12.3k in · 1.2k out · $0.31");
  });

  it("task-detail: no double counting when a usage event follows the run.list usage", async () => {
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);

    client.nth("run.list", 0).resolve([
      runningRun({ usage: { inputTokens: 10000, outputTokens: 1000, source: "claude_result" } }),
    ]);
    await flush();
    expect(screen.getByTestId("chat-usage-total")).toHaveTextContent("10k in · 1k out");

    // The live event REPLACES the seeded value, never adds to it.
    client.emit("run.attach", 0, "usage", {
      usage: { inputTokens: 12345, outputTokens: 1234, source: "claude_result" },
    });
    await flush();

    expect(screen.getByTestId("chat-usage-total")).toHaveTextContent("12.3k in · 1.2k out");
  });

  it("task-detail: switching chats drops the previous chat's total", async () => {
    const client = new FakeWsClient();
    const { rerender } = render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);

    client.nth("run.list", 0).resolve([
      runningRun({ usage: { inputTokens: 10000, outputTokens: 1000, source: "claude_result" } }),
    ]);
    await flush();
    expect(screen.getByTestId("chat-usage-total")).toHaveTextContent("10k in");

    rerender(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_B} isDefaultChat={false} />);
    await flush();

    // Gone immediately -- no stale total while chat B's list is in flight.
    expect(screen.queryByTestId("chat-usage-total")).not.toBeInTheDocument();

    client.nth("run.list", 1).resolve([]);
    await flush();
    expect(screen.queryByTestId("chat-usage-total")).not.toBeInTheDocument();
  });

  it("task-detail: a late usage event from the previous chat's stream is ignored", async () => {
    const client = new FakeWsClient();
    const { rerender } = render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);

    client.nth("run.list", 0).resolve([runningRun()]);
    await flush();

    rerender(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_B} isDefaultChat={false} />);
    client.nth("run.list", 1).resolve([]);
    await flush();
    expect(screen.queryByTestId("chat-usage-total")).not.toBeInTheDocument();

    // The old chat's still-open run.attach delivers a usage event after the
    // switch -- the cancelled session must drop it.
    client.emit("run.attach", 0, "usage", {
      usage: { inputTokens: 99999, outputTokens: 99999, source: "claude_result" },
    });
    await flush();

    expect(screen.queryByTestId("chat-usage-total")).not.toBeInTheDocument();
  });
});

describe("chat meta row (app-header-and-tabs)", () => {
  it("AH-AC12 no title heading; Detailed toggle remains (the run-config pill only names an agent)", async () => {
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={TASK_A} chat={CHAT_A} isDefaultChat />);
    client.nth("run.list", 0).resolve([]);
    await flush();

    // The "<task> · <chat>" h2 moved to the app header.
    expect(screen.queryByRole("heading", { level: 2 })).not.toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: new RegExp(TASK_A.Title) })).not.toBeInTheDocument();
    expect(screen.queryByText(/·\s*Chat/)).not.toBeInTheDocument();
    expect(screen.getByTestId("detail-level-toggle")).toBeInTheDocument();
  });
});
