import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ArchiveFinishedDialog, StopTreeDialog } from "@/components/tree-action-dialogs";
import { clearToasts } from "@/components/ui/toast";
import { FakeWsClient } from "@/test/fake-ws-client";
import type { Task } from "@/lib/types";
import type { WsClient } from "@/lib/ws-client";

function wsFake(): FakeWsClient & WsClient {
  return new FakeWsClient() as unknown as FakeWsClient & WsClient;
}

const TASK: Task = {
  ID: 7,
  WorkspaceID: 1,
  SpaceID: null,
  Title: "Parent task",
  Status: "active",
  WorktreePath: null,
  Branch: null,
  CreatedAt: "2026-10-11T00:00:00Z",
  UpdatedAt: "2026-10-11T00:00:00Z",
  ArchivedAt: null,
};

function item(id: number, title: string) {
  return { id, title };
}

function flush(): Promise<void> {
  return act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

async function resolveDryRun(client: FakeWsClient, method: string, payload: unknown): Promise<void> {
  await act(async () => {
    client.nth(method, 0).resolve(payload);
    await Promise.resolve();
    await Promise.resolve();
  });
}

describe("ArchiveFinishedDialog (PR-H11)", () => {
  it("archive finished dialog runs a dry run on open and lists the tasks", async () => {
    const client = wsFake();
    render(<ArchiveFinishedDialog client={client} task={TASK} open onOpenChange={vi.fn()} onArchived={vi.fn()} />);

    const dry = await waitFor(() => client.nth("task.archiveFinished", 0));
    expect(dry.params).toEqual({ taskId: 7, dryRun: true });
    await resolveDryRun(client, "task.archiveFinished", {
      dryRun: true,
      eligible: [item(8, "Coder one"), item(9, "Coder two"), item(10, "Coder three")],
      archived: [],
      skipped: [],
      failed: [],
      remaining: 0,
    });

    const list = screen.getByTestId("archive-finished-list");
    expect(list.textContent).toContain("Coder one");
    expect(list.textContent).toContain("Coder three");
    expect(screen.getByTestId("dialog-archive-finished-confirm").textContent).toBe("Archive 3");
  });

  it("archive finished dialog confirm calls task.archiveFinished once for the task", async () => {
    const client = wsFake();
    const onArchived = vi.fn();
    const onOpenChange = vi.fn();
    render(<ArchiveFinishedDialog client={client} task={TASK} open onOpenChange={onOpenChange} onArchived={onArchived} />);
    await resolveDryRun(client, "task.archiveFinished", {
      dryRun: true,
      eligible: [item(8, "Coder one")],
      archived: [],
      skipped: [],
      failed: [],
      remaining: 0,
    });

    fireEvent.click(screen.getByTestId("dialog-archive-finished-confirm"));
    const confirm = await waitFor(() => client.nth("task.archiveFinished", 1));
    expect(confirm.params).toEqual({ taskId: 7 });

    await act(async () => {
      confirm.resolve({
        dryRun: false,
        eligible: [item(8, "Coder one")],
        archived: [item(8, "Coder one")],
        skipped: [],
        failed: [],
        remaining: 0,
      });
    });
    await flush();

    const calls = client.calls.filter((c) => c.method === "task.archiveFinished");
    expect(calls).toHaveLength(2);
    expect(onOpenChange).toHaveBeenCalledWith(false);
    expect(onArchived).toHaveBeenCalled();
  });

  it("archive finished dialog with nothing eligible disables confirm and says why", async () => {
    const client = wsFake();
    render(<ArchiveFinishedDialog client={client} task={TASK} open onOpenChange={vi.fn()} onArchived={vi.fn()} />);
    await resolveDryRun(client, "task.archiveFinished", {
      dryRun: true,
      eligible: [],
      archived: [],
      skipped: [],
      failed: [],
      remaining: 0,
    });

    expect(screen.getByTestId("archive-finished-none").textContent).toContain("No finished sub-tasks");
    expect(screen.getByTestId("dialog-archive-finished-confirm")).toBeDisabled();
  });

  it("archive finished dialog says how many tasks are kept and why", async () => {
    const client = wsFake();
    render(<ArchiveFinishedDialog client={client} task={TASK} open onOpenChange={vi.fn()} onArchived={vi.fn()} />);
    await resolveDryRun(client, "task.archiveFinished", {
      dryRun: true,
      eligible: [item(8, "Coder one")],
      archived: [],
      skipped: [item(9, "Coder two", ), ...[]].map(() => ({ id: 9, title: "Coder two", reason: "has unfinished descendants" })),
      failed: [],
      remaining: 0,
    });

    const kept = screen.getByTestId("archive-finished-kept");
    expect(kept.textContent).toContain("1 finished task is kept");
    expect(kept.textContent).toContain("they still have unfinished sub-tasks");
  });

  it("archive finished partial failure keeps the dialog open and lists failures", async () => {
    const client = wsFake();
    const onOpenChange = vi.fn();
    render(<ArchiveFinishedDialog client={client} task={TASK} open onOpenChange={onOpenChange} onArchived={vi.fn()} />);
    await resolveDryRun(client, "task.archiveFinished", {
      dryRun: true,
      eligible: [item(8, "Coder one"), item(9, "Coder two")],
      archived: [],
      skipped: [],
      failed: [],
      remaining: 0,
    });

    fireEvent.click(screen.getByTestId("dialog-archive-finished-confirm"));
    const confirm = await waitFor(() => client.nth("task.archiveFinished", 1));
    await act(async () => {
      confirm.resolve({
        dryRun: false,
        eligible: [item(8, "Coder one"), item(9, "Coder two")],
        archived: [item(8, "Coder one")],
        skipped: [],
        failed: [{ ...item(9, "Coder two"), error: "archive failed: checkout conflict" }],
        remaining: 0,
      });
    });
    await flush();

    expect(onOpenChange).not.toHaveBeenCalled();
    expect(screen.getByTestId("archive-finished-failures").textContent).toContain("Coder two");
    expect(screen.getByText(/checkout conflict/)).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Archived 1 of 2 finished sub-tasks" })).toBeInTheDocument();
    expect(screen.getByTestId("dialog-archive-finished-confirm")).toHaveTextContent("Try again");
  });
});

describe("StopTreeDialog (PR-H11)", () => {
  it("stop tree dialog title counts running tasks and is singular for one", async () => {
    const client = wsFake();
    render(<StopTreeDialog client={client} task={TASK} open onOpenChange={vi.fn()} onStopped={vi.fn()} />);
    await resolveDryRun(client, "task.stopTree", {
      dryRun: true,
      runs: [{ ...item(7, "Parent task"), runId: "r1" }],
      queuedPrompts: 0,
      stopped: [],
      failed: [],
      queuedCancelled: 0,
    });
    expect(screen.getByRole("heading", { name: /Stop 1 running task in this tree\?/ })).toBeInTheDocument();

    const client2 = wsFake();
    render(<StopTreeDialog client={client2} task={TASK} open onOpenChange={vi.fn()} onStopped={vi.fn()} />);
    await resolveDryRun(client2, "task.stopTree", {
      dryRun: true,
      runs: [
        { ...item(7, "Parent task"), runId: "r1" },
        { ...item(8, "Coder one"), runId: "r2" },
        { ...item(9, "Coder two"), runId: "r3" },
        { ...item(10, "Coder three"), runId: "r4" },
      ],
      queuedPrompts: 0,
      stopped: [],
      failed: [],
      queuedCancelled: 0,
    });
    expect(screen.getByRole("heading", { name: /Stop 4 running tasks in this tree\?/ })).toBeInTheDocument();
  });

  it("stop tree dialog notes queued prompts that will be cancelled", async () => {
    const client = wsFake();
    render(<StopTreeDialog client={client} task={TASK} open onOpenChange={vi.fn()} onStopped={vi.fn()} />);
    await resolveDryRun(client, "task.stopTree", {
      dryRun: true,
      runs: [{ ...item(8, "Coder one"), runId: "r2" }],
      queuedPrompts: 2,
      stopped: [],
      failed: [],
      queuedCancelled: 0,
    });
    expect(screen.getByTestId("stop-tree-queued-note").textContent).toContain("2 queued prompts will be cancelled");
    // The running task is not the queried task itself.
    expect(screen.queryByTestId("stop-tree-includes-self")).toBeNull();
  });

  it("stop tree dialog confirm calls task.stopTree once for the task", async () => {
    const client = wsFake();
    const onOpenChange = vi.fn();
    const onStopped = vi.fn();
    render(<StopTreeDialog client={client} task={TASK} open onOpenChange={onOpenChange} onStopped={onStopped} />);
    await resolveDryRun(client, "task.stopTree", {
      dryRun: true,
      runs: [{ ...item(8, "Coder one"), runId: "r2" }],
      queuedPrompts: 0,
      stopped: [],
      failed: [],
      queuedCancelled: 0,
    });

    fireEvent.click(screen.getByTestId("dialog-stop-tree-confirm"));
    const confirm = await waitFor(() => client.nth("task.stopTree", 1));
    expect(confirm.params).toEqual({ taskId: 7 });

    await act(async () => {
      confirm.resolve({
        dryRun: false,
        runs: [{ ...item(8, "Coder one"), runId: "r2" }],
        queuedPrompts: 0,
        stopped: [{ ...item(8, "Coder one"), runId: "r2" }],
        failed: [],
        queuedCancelled: 0,
      });
    });
    await flush();

    const calls = client.calls.filter((c) => c.method === "task.stopTree");
    expect(calls).toHaveLength(2);
    expect(onOpenChange).toHaveBeenCalledWith(false);
    expect(onStopped).toHaveBeenCalled();
  });

  it("stop tree dialog lists runs that failed to stop", async () => {
    const client = wsFake();
    const onOpenChange = vi.fn();
    render(<StopTreeDialog client={client} task={TASK} open onOpenChange={onOpenChange} onStopped={vi.fn()} />);
    await resolveDryRun(client, "task.stopTree", {
      dryRun: true,
      runs: [{ ...item(8, "Coder one"), runId: "r2" }],
      queuedPrompts: 0,
      stopped: [],
      failed: [],
      queuedCancelled: 0,
    });

    fireEvent.click(screen.getByTestId("dialog-stop-tree-confirm"));
    const confirm = await waitFor(() => client.nth("task.stopTree", 1));
    await act(async () => {
      confirm.resolve({
        // A non-dry task.stopTree leaves runs/queuedPrompts empty (wire contract).
        dryRun: false,
        runs: [],
        queuedPrompts: 0,
        stopped: [],
        failed: [{ ...item(8, "Coder one"), runId: "r2", error: "run did not stop within 10s" }],
        queuedCancelled: 0,
      });
    });
    await flush();

    expect(onOpenChange).not.toHaveBeenCalled();
    expect(screen.getByText(/run did not stop within 10s/)).toBeInTheDocument();
    expect(screen.getByTestId("stop-tree-failures").textContent).toContain("Coder one");
    expect(screen.getByRole("heading", { name: "Stopped 0 of 1 running tasks" })).toBeInTheDocument();
    expect(screen.getByTestId("dialog-stop-tree-confirm")).toHaveTextContent("Try again");
  });
});

describe("tree action dialogs, shared (PR-H11)", () => {
  afterEach(() => {
    clearToasts();
  });

  it("dry run failure blocks confirm and shows the error", async () => {
    const client = wsFake();
    render(<StopTreeDialog client={client} task={TASK} open onOpenChange={vi.fn()} onStopped={vi.fn()} />);
    await act(async () => {
      client.nth("task.stopTree", 0).reject(new Error("task.stopTree: unknown task"));
    });
    await flush();

    expect(screen.getByText(/unknown task/)).toBeInTheDocument();
    expect(screen.getByTestId("dialog-stop-tree-confirm")).toBeDisabled();

    const client2 = wsFake();
    render(<ArchiveFinishedDialog client={client2} task={TASK} open onOpenChange={vi.fn()} onArchived={vi.fn()} />);
    await act(async () => {
      client2.nth("task.archiveFinished", 0).reject(new Error("task.archiveFinished: task is archived"));
    });
    await flush();

    expect(screen.getByText(/task is archived/)).toBeInTheDocument();
    expect(screen.getByTestId("dialog-archive-finished-confirm")).toBeDisabled();
  });

  it("a dry run landing after close or for another task is ignored", async () => {
    const client = wsFake();
    const { rerender } = render(
      <ArchiveFinishedDialog client={client} task={TASK} open onOpenChange={vi.fn()} onArchived={vi.fn()} />,
    );
    // The task changes before the dry run resolves: its result must be dropped.
    const OTHER: Task = { ...TASK, ID: 99, Title: "Other task" };
    rerender(<ArchiveFinishedDialog client={client} task={OTHER} open onOpenChange={vi.fn()} onArchived={vi.fn()} />);
    await act(async () => {
      client.nth("task.archiveFinished", 0).resolve({
        dryRun: true,
        eligible: [item(8, "Stale result")],
        archived: [],
        skipped: [],
        failed: [],
        remaining: 0,
      });
    });
    await flush();

    expect(screen.queryByText("Stale result")).toBeNull();
    expect(screen.getByTestId("dialog-archive-finished-confirm")).toBeDisabled();

    // Landing after close: resolved while open=false must not surface either.
    const closed = await waitFor(() => client.nth("task.archiveFinished", 1));
    rerender(<ArchiveFinishedDialog client={client} task={OTHER} open={false} onOpenChange={vi.fn()} onArchived={vi.fn()} />);
    await act(async () => {
      closed.resolve({
        dryRun: true,
        eligible: [item(9, "Closed result")],
        archived: [],
        skipped: [],
        failed: [],
        remaining: 0,
      });
    });
    await flush();
    expect(screen.queryByText("Closed result")).toBeNull();
  });
});
