import { act, fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { FakeWsClient } from "@/test/fake-ws-client";
import { DiffViewerPane } from "@/components/diff-viewer-pane";
import type { Task, TaskCreatePrResult, TaskFileDiffResult, TaskFilesResult } from "@/lib/types";

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

describe("DiffViewerPane staging", () => {
  it("stage checkbox calls task.stage and updates the file's staged state", async () => {
    const client = new FakeWsClient();
    render(<DiffViewerPane client={client} task={TASK} />);
    await resolveFilesAndDiffs(client);

    const box = screen.getByTestId("stage-file.txt");
    expect(box).not.toBeChecked();
    // Commit stays disabled: nothing staged yet.
    expect(screen.getByTestId("commit-button")).toBeDisabled();

    fireEvent.click(box);
    const call = client.nth("task.stage", 0);
    expect(call.params).toEqual({ taskId: TASK.ID, path: "file.txt", staged: true });
    call.resolve({});
    await flush();

    expect(screen.getByTestId("stage-file.txt")).toBeChecked();
    expect(screen.getByTestId("commit-button")).toHaveTextContent(/1 staged/);
  });

  it("unchecking a staged file sends staged: false", async () => {
    const client = new FakeWsClient();
    render(<DiffViewerPane client={client} task={TASK} />);
    client.nth("task.files", 0).resolve({
      files: [{ path: "file.txt", status: "modified", staged: true }],
    } satisfies TaskFilesResult);
    await flush();

    fireEvent.click(screen.getByTestId("stage-file.txt"));
    expect(client.nth("task.stage", 0).params).toEqual({
      taskId: TASK.ID,
      path: "file.txt",
      staged: false,
    });
  });

  it("a stage failure renders inline", async () => {
    const client = new FakeWsClient();
    render(<DiffViewerPane client={client} task={TASK} />);
    await resolveFilesAndDiffs(client);

    fireEvent.click(screen.getByTestId("stage-file.txt"));
    client.nth("task.stage", 0).reject(new Error("stage blew up"));
    await flush();

    expect(screen.getByTestId("diff-error")).toHaveTextContent("stage blew up");
    expect(screen.getByTestId("stage-file.txt")).not.toBeChecked();
  });
});

describe("DiffViewerPane commit bar", () => {
  it("Commit stays disabled until at least one file is staged and the message is non-empty", async () => {
    const client = new FakeWsClient();
    render(<DiffViewerPane client={client} task={TASK} />);
    await resolveFilesAndDiffs(client);

    const button = screen.getByTestId("commit-button");
    fireEvent.change(screen.getByTestId("commit-message"), { target: { value: "a message" } });
    expect(button).toBeDisabled();

    fireEvent.click(screen.getByTestId("stage-file.txt"));
    client.nth("task.stage", 0).resolve({});
    await flush();
    expect(button).toBeEnabled();
  });

  it("a successful commit shows sha+subject and refreshes the files list", async () => {
    const client = new FakeWsClient();
    render(<DiffViewerPane client={client} task={TASK} />);
    await resolveFilesAndDiffs(client);

    fireEvent.click(screen.getByTestId("stage-file.txt"));
    client.nth("task.stage", 0).resolve({});
    await flush();
    fireEvent.change(screen.getByTestId("commit-message"), { target: { value: "my commit" } });
    fireEvent.click(screen.getByTestId("commit-button"));

    const call = client.nth("task.commit", 0);
    expect(call.params).toEqual({ taskId: TASK.ID, message: "my commit", author: "human" });
    call.resolve({ commit: "abcdef1234567890", subject: "my commit", files: 1 });
    await flush();

    expect(screen.getByTestId("commit-success")).toHaveTextContent("my commit");
    expect(screen.getByTestId("commit-success")).toHaveTextContent("abcdef12");
    // Post-commit refresh of the files list.
    expect(client.calls.filter((c) => c.method === "task.files")).toHaveLength(2);
  });

  it("a commit failure renders inline and does not refresh", async () => {
    const client = new FakeWsClient();
    render(<DiffViewerPane client={client} task={TASK} />);
    await resolveFilesAndDiffs(client);

    fireEvent.click(screen.getByTestId("stage-file.txt"));
    client.nth("task.stage", 0).resolve({});
    await flush();
    fireEvent.change(screen.getByTestId("commit-message"), { target: { value: "my commit" } });
    fireEvent.click(screen.getByTestId("commit-button"));
    client.nth("task.commit", 0).reject(new Error("nothing staged to commit"));
    await flush();

    expect(screen.getByTestId("commit-error")).toHaveTextContent("nothing staged to commit");
    expect(client.calls.filter((c) => c.method === "task.files")).toHaveLength(1);
  });
});

describe("DiffViewerPane Create PR", () => {
  it("calls task.createPr and renders the returned URL as a link", async () => {
    const client = new FakeWsClient();
    render(<DiffViewerPane client={client} task={TASK} />);
    await resolveFilesAndDiffs(client);

    fireEvent.click(screen.getByTestId("create-pr-button"));
    const call = client.nth("task.createPr", 0);
    expect(call.params).toEqual({ taskId: TASK.ID });
    call.resolve({ url: "https://github.com/example/repo/pull/42" } satisfies TaskCreatePrResult);
    await flush();

    const link = screen.getByTestId("pr-url-link");
    expect(link).toHaveTextContent("https://github.com/example/repo/pull/42");
    expect(link).toHaveAttribute("href", "https://github.com/example/repo/pull/42");
    expect(screen.queryByTestId("pr-error")).not.toBeInTheDocument();
  });

  it("surfaces a task.createPr failure inline instead of failing silently", async () => {
    const client = new FakeWsClient();
    render(<DiffViewerPane client={client} task={TASK} />);
    await resolveFilesAndDiffs(client);

    fireEvent.click(screen.getByTestId("create-pr-button"));
    client.nth("task.createPr", 0).reject(new Error("gh pr create: HTTP 429: rate limited"));
    await flush();

    expect(screen.getByTestId("pr-error")).toHaveTextContent("429");
    expect(screen.queryByTestId("pr-url")).not.toBeInTheDocument();
  });

  it("disables the Create PR control when there is no client", () => {
    render(<DiffViewerPane client={null} task={TASK} />);
    expect(screen.getByTestId("create-pr-button")).toBeDisabled();
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
  it("refetches task.files on a terminal run.status for the viewed task", async () => {
    const client = new FakeWsClient();
    const stub = makeEventsStub();
    render(<DiffViewerPane client={client} task={TASK} events={stub.events} />);
    client.nth("task.files", 0).resolve({ files: [] } satisfies TaskFilesResult);
    await flush();
    expect(screen.getByTestId("diff-empty")).toBeInTheDocument();

    // Non-terminal status: no refetch.
    act(() => stub.fire("run.status", { runId: "r1", taskId: TASK.ID, status: "running" }));
    await flush();
    expect(client.calls.filter((c) => c.method === "task.files")).toHaveLength(1);

    // Terminal status for the same task: refetch.
    act(() => stub.fire("run.status", { runId: "r1", taskId: TASK.ID, status: "done" }));
    await flush();
    expect(client.calls.filter((c) => c.method === "task.files")).toHaveLength(2);
  });

  it("ignores terminal run.status events for a different task", async () => {
    const client = new FakeWsClient();
    const stub = makeEventsStub();
    render(<DiffViewerPane client={client} task={TASK} events={stub.events} />);
    client.nth("task.files", 0).resolve({ files: [] } satisfies TaskFilesResult);
    await flush();

    act(() => stub.fire("run.status", { runId: "r9", taskId: TASK.ID + 1, status: "done" }));
    await flush();

    expect(client.calls.filter((c) => c.method === "task.files")).toHaveLength(1);
  });
});
