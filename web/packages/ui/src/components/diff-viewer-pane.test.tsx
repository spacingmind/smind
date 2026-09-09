import { act, fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { FakeWsClient } from "@/test/fake-ws-client";
import { DiffViewerPane } from "@/components/diff-viewer-pane";
import type { Task, TaskFileDiffResult, TaskFilesResult } from "@/lib/types";

const TASK: Task = {
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

const SAMPLE_DIFF = `diff --git a/file.txt b/file.txt
index a29bdeb..0226208 100644
--- a/file.txt
+++ b/file.txt
@@ -1 +1,2 @@
 line1
+line2 added
`;

const FILES: TaskFilesResult = {
  files: [
    { path: "file.txt", status: "modified", staged: false },
    { path: "new.txt", status: "added", staged: false },
  ],
};

/** Flushes pending microtasks, wrapped in `act` so React commits any resulting state updates before the caller asserts. */
async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

/** Resolves the initial task.files call and every pending task.fileDiff it triggers. */
async function resolveFilesAndDiffs(
  client: FakeWsClient,
  files: TaskFilesResult = FILES,
  diff: string = SAMPLE_DIFF,
): Promise<void> {
  client.nth("task.files", 0).resolve(files);
  await flush();
  for (let i = 0; ; i++) {
    const matches = client.calls.filter((c) => c.method === "task.fileDiff");
    if (i >= matches.length) break;
    client.nth("task.fileDiff", i).resolve({ diff } satisfies TaskFileDiffResult);
  }
  await flush();
}

describe("DiffViewerPane", () => {
  it("fetches task.files on mount and renders one collapsible entry per file with its own task.fileDiff", async () => {
    const client = new FakeWsClient();
    render(<DiffViewerPane client={client} task={TASK} />);

    expect(client.nth("task.files", 0).params).toEqual({ taskId: TASK.ID });
    await resolveFilesAndDiffs(client);

    expect(screen.getByTestId("diff-file-file.txt")).toBeInTheDocument();
    expect(screen.getByTestId("diff-file-new.txt")).toBeInTheDocument();

    const container = screen.getByTestId("diff-container-file.txt");
    expect(container.textContent).toContain("line2 added");
    expect(screen.queryByTestId("diff-empty")).not.toBeInTheDocument();
    expect(screen.queryByTestId("diff-error")).not.toBeInTheDocument();
  });

  it("collapses and re-expands a file, hiding then re-showing its diff", async () => {
    const client = new FakeWsClient();
    render(<DiffViewerPane client={client} task={TASK} />);
    await resolveFilesAndDiffs(client);

    fireEvent.click(screen.getByTestId("diff-file-header-file.txt"));
    expect(screen.queryByTestId("diff-container-file.txt")).not.toBeInTheDocument();

    fireEvent.click(screen.getByTestId("diff-file-header-file.txt"));
    expect(screen.getByTestId("diff-container-file.txt")).toBeInTheDocument();
  });

  it("shows a clear empty state for a task with no changes", async () => {
    const client = new FakeWsClient();
    render(<DiffViewerPane client={client} task={TASK} />);

    client.nth("task.files", 0).resolve({ files: [] } satisfies TaskFilesResult);
    await flush();

    expect(screen.getByTestId("diff-empty")).toHaveTextContent(/no changes/i);
  });

  it("surfaces a task.files failure as a visible error", async () => {
    const client = new FakeWsClient();
    render(<DiffViewerPane client={client} task={TASK} />);

    client.nth("task.files", 0).reject(new Error("boom"));
    await flush();

    expect(screen.getByTestId("diff-error")).toHaveTextContent("boom");
  });

  it("the Refresh control re-issues task.files", async () => {
    const client = new FakeWsClient();
    render(<DiffViewerPane client={client} task={TASK} />);
    await resolveFilesAndDiffs(client);

    fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
    await flush();
    expect(client.calls.filter((c) => c.method === "task.files")).toHaveLength(2);
  });

  it("disables the Refresh control and issues no request when there is no client", () => {
    render(<DiffViewerPane client={null} task={TASK} />);
    expect(screen.getByRole("button", { name: "Refresh" })).toBeDisabled();
  });
});

/** Minimal DaemonEvents stub: records listeners per topic, lets the test fire them. */
function makeEventsStub() {
  const listeners = new Map<string, Set<(payload: unknown) => void>>();
  return {
    events: {
      subscribe(topic: string, listener: (payload: unknown) => void): () => void {
        let set = listeners.get(topic);
        if (!set) {
          set = new Set();
          listeners.set(topic, set);
        }
        set.add(listener);
        return () => set!.delete(listener);
      },
    },
    fire(topic: string, payload: unknown): void {
      for (const l of listeners.get(topic) ?? []) l(payload);
    },
  };
}

describe("DiffViewerPane live refresh", () => {
  it("refetches task.diff on a terminal run.status for the viewed task", async () => {
    const client = new FakeWsClient();
    const stub = makeEventsStub();
    render(<DiffViewerPane client={client} task={TASK} events={stub.events} />);

    client.nth("task.diff", 0).resolve({ diff: "" } satisfies TaskDiffResult);
    await flush();
    expect(screen.getByTestId("diff-empty")).toBeInTheDocument();

    // Non-terminal status: no refetch.
    act(() => stub.fire("run.status", { runId: "r1", taskId: TASK.ID, status: "running" }));
    await flush();
    expect(client.calls.filter((c) => c.method === "task.diff")).toHaveLength(1);

    // Terminal status for the same task: refetch.
    act(() => stub.fire("run.status", { runId: "r1", taskId: TASK.ID, status: "done" }));
    await flush();
    expect(client.calls.filter((c) => c.method === "task.diff")).toHaveLength(2);
    client.nth("task.diff", 1).resolve({ diff: SAMPLE_DIFF } satisfies TaskDiffResult);
    await flush();
    expect(screen.getByTestId("diff-container").textContent).toContain("line2 added");
  });

  it("ignores terminal run.status events for a different task", async () => {
    const client = new FakeWsClient();
    const stub = makeEventsStub();
    render(<DiffViewerPane client={client} task={TASK} events={stub.events} />);

    client.nth("task.diff", 0).resolve({ diff: "" } satisfies TaskDiffResult);
    await flush();

    act(() => stub.fire("run.status", { runId: "r9", taskId: TASK.ID + 1, status: "done" }));
    await flush();

    expect(client.calls.filter((c) => c.method === "task.diff")).toHaveLength(1);
  });
});
