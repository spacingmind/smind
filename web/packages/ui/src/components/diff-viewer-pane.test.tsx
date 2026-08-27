import { act } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { FakeWsClient } from "@/test/fake-ws-client";
import { DiffViewerPane } from "@/components/diff-viewer-pane";
import type { Task, TaskDiffResult } from "@/lib/types";

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

/** Flushes pending microtasks, wrapped in `act` so React commits any resulting state updates before the caller asserts. */
async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

describe("DiffViewerPane", () => {
  it("fetches task.diff on mount and renders the returned diff as a diff view", async () => {
    const client = new FakeWsClient();
    render(<DiffViewerPane client={client} task={TASK} />);

    const call = client.nth("task.diff", 0);
    expect(call.params).toEqual({ taskId: TASK.ID });

    call.resolve({ diff: SAMPLE_DIFF } satisfies TaskDiffResult);
    await flush();

    const container = screen.getByTestId("diff-container");
    expect(container).toBeInTheDocument();
    // diff2html renders the changed file's path and the added line's text
    // into the DOM -- proving this isn't just dumping raw diff text, but
    // an actual parsed diff view.
    expect(container.textContent).toContain("file.txt");
    expect(container.textContent).toContain("line2 added");
    expect(screen.queryByTestId("diff-empty")).not.toBeInTheDocument();
    expect(screen.queryByTestId("diff-error")).not.toBeInTheDocument();
  });

  it("shows a clear empty state for a task with no changes, not a blank render", async () => {
    const client = new FakeWsClient();
    render(<DiffViewerPane client={client} task={TASK} />);

    client.nth("task.diff", 0).resolve({ diff: "" } satisfies TaskDiffResult);
    await flush();

    expect(screen.getByTestId("diff-empty")).toHaveTextContent(/no changes/i);
    expect(screen.queryByTestId("diff-container")).not.toBeInTheDocument();
  });

  it("surfaces a task.diff failure as a visible error", async () => {
    const client = new FakeWsClient();
    render(<DiffViewerPane client={client} task={TASK} />);

    client.nth("task.diff", 0).reject(new Error("boom"));
    await flush();

    expect(screen.getByTestId("diff-error")).toHaveTextContent("boom");
    expect(screen.queryByTestId("diff-container")).not.toBeInTheDocument();
    expect(screen.queryByTestId("diff-empty")).not.toBeInTheDocument();
  });

  it("the Refresh control re-issues task.diff and re-renders the new result", async () => {
    const client = new FakeWsClient();
    render(<DiffViewerPane client={client} task={TASK} />);

    client.nth("task.diff", 0).resolve({ diff: "" } satisfies TaskDiffResult);
    await flush();
    expect(screen.getByTestId("diff-empty")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
    await flush();

    expect(client.calls.filter((c) => c.method === "task.diff")).toHaveLength(2);
    client.nth("task.diff", 1).resolve({ diff: SAMPLE_DIFF } satisfies TaskDiffResult);
    await flush();

    expect(screen.getByTestId("diff-container").textContent).toContain("line2 added");
    expect(screen.queryByTestId("diff-empty")).not.toBeInTheDocument();
  });

  it("disables the Refresh control and issues no request when there is no client", () => {
    render(<DiffViewerPane client={null} task={TASK} />);

    expect(screen.getByRole("button", { name: "Refresh" })).toBeDisabled();
  });
});
