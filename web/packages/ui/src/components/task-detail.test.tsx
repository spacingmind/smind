import { act } from "react";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { FakeWsClient } from "@/test/fake-ws-client";
import { TaskDetailPane } from "@/components/task-detail";
import type { RunLogsResult, RunSummary, Task } from "@/lib/types";

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

function runningRun(overrides: Partial<RunSummary> = {}): RunSummary {
  return {
    ID: "run-1",
    TaskID: TASK_A.ID,
    Provider: "claude-native",
    Prompt: "do the thing",
    Status: "running",
    StartedAt: "2024-01-01T00:00:00Z",
    FinishedAt: null,
    StopReason: "",
    Err: "",
    ...overrides,
  };
}

function doneRun(overrides: Partial<RunSummary> = {}): RunSummary {
  return {
    ID: "run-1",
    TaskID: TASK_A.ID,
    Provider: "claude-native",
    Prompt: "do the thing",
    Status: "done",
    StartedAt: "2024-01-01T00:00:00Z",
    FinishedAt: "2024-01-01T00:00:05Z",
    StopReason: "end_turn",
    Err: "",
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
    render(<TaskDetailPane client={client} task={TASK_A} />);

    expect(client.nth("run.list", 0).params).toBeUndefined();
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
    expect(within(entry).getByTestId("run-text")).toHaveTextContent("hello world");
    expect(within(entry).getByTestId("run-status")).toHaveTextContent("done");
  });

  it("streams a running run's live chunks into the timeline as they arrive, not buffered until the terminal event", async () => {
    const client = new FakeWsClient();
    render(<TaskDetailPane client={client} task={TASK_A} />);

    client.nth("run.list", 0).resolve([runningRun()]);
    await flush();

    expect(client.nth("run.attach", 0).params).toEqual({ runId: "run-1" });

    client.emit("run.attach", 0, "chunk", { text: "partial " });
    await flush();
    expect(screen.getByTestId("run-text")).toHaveTextContent("partial");
    expect(screen.getByTestId("run-status")).toHaveTextContent("running");

    client.emit("run.attach", 0, "chunk", { text: "and more" });
    await flush();
    expect(screen.getByTestId("run-text")).toHaveTextContent("partial and more");

    client.nth("run.attach", 0).resolve({ runId: "run-1", stopReason: "end_turn" });
    await flush();
    expect(screen.getByTestId("run-status")).toHaveTextContent("done");
  });

  it("switching to a different task and back doesn't duplicate entries or leak the live subscription", async () => {
    const client = new FakeWsClient();
    const { rerender } = render(<TaskDetailPane client={client} task={TASK_A} />);

    client.nth("run.list", 0).resolve([runningRun()]);
    await flush();
    client.emit("run.attach", 0, "chunk", { text: "hello" });
    await flush();
    expect(screen.getByTestId("run-text")).toHaveTextContent("hello");

    // Switch away: the first task's run.attach must be aborted (detached),
    // not left dangling or double-subscribed.
    rerender(<TaskDetailPane client={client} task={TASK_B} />);
    await flush();
    expect(client.nth("run.attach", 0).options?.signal?.aborted).toBe(true);

    expect(client.nth("run.list", 1).params).toBeUndefined();
    client.nth("run.list", 1).resolve([]);
    await flush();
    expect(screen.getByText(/no runs yet/i)).toBeInTheDocument();

    // Switch back: the run kept going while detached (as it must), so by
    // the time we reselect Task A it shows as finished with its full text
    // -- fetched fresh via run.list + run.logs, not carried over from the
    // earlier live subscription.
    rerender(<TaskDetailPane client={client} task={TASK_A} />);
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
    expect(within(entries[0]!).getByTestId("run-text")).toHaveTextContent("hello world");
  });

  it("aborts (does not stop) an actively streaming run.attach on unmount", async () => {
    const client = new FakeWsClient();
    const { unmount } = render(<TaskDetailPane client={client} task={TASK_A} />);

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
    render(<TaskDetailPane client={client} task={TASK_A} />);

    client.nth("run.list", 0).resolve([]);
    await flush();

    const promptInput = screen.getByLabelText("Prompt");
    fireEvent.change(promptInput, { target: { value: "do the thing" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    await flush();

    expect(client.calls.some((c) => c.method === "task.prompt")).toBe(false);

    const startCall = client.nth("run.start", 0);
    expect(startCall.params).toEqual({ taskId: TASK_A.ID, provider: "claude-native", prompt: "do the thing" });

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
    const { rerender } = render(<TaskDetailPane client={client} task={TASK_A} />);

    // Task A's first run.list is issued but never resolved before we move on.
    const staleListCall = client.nth("run.list", 0);

    rerender(<TaskDetailPane client={client} task={TASK_B} />);
    client.nth("run.list", 1).resolve([]);
    await flush();

    rerender(<TaskDetailPane client={client} task={TASK_A} />);
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
});
