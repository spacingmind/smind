import { act, fireEvent, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { ArchiveTaskDialog, CreateWorkspaceDialog } from "@/components/crud-dialogs";
import { FakeWsClient } from "@/test/fake-ws-client";
import { renderWithClientProp as render } from "@/test/render-with-daemon";
import type { Task } from "@/lib/types";
import type { WsClient } from "@/lib/ws-client";

/** ArchiveTaskDialog takes a full WsClient; the fake is structurally enough at runtime. */
function wsFake(): FakeWsClient & WsClient {
  return new FakeWsClient() as unknown as FakeWsClient & WsClient;
}

// visual-identity-console Item 4: a dialog's Cancel button is the "quiet"
// call site -- non-committal next to the primary Create action, via the
// shared `FormActions` row every CRUD dialog renders through.
describe("crud dialog Cancel button", () => {
  it("renders with the quiet variant", () => {
    render(
      <CreateWorkspaceDialog client={null} open={true} onOpenChange={vi.fn()} onCreated={vi.fn()} />,
    );

    expect(screen.getByTestId("dialog-new-workspace-cancel")).toHaveAttribute("data-variant", "quiet");
  });
});

const ARCHIVE_TASK: Task = {
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

function child(id: number, title: string): Task {
  return { ...ARCHIVE_TASK, ID: id, Title: title, ParentTaskID: ARCHIVE_TASK.ID, Status: "active" };
}

function flush(): Promise<void> {
  return act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

describe("ArchiveTaskDialog children (PR-U2)", () => {
  function treeNode(p: { id: number; title: string; state: "running" | "waiting_permission" | "done" | "error" | "stopped" | "idle"; root?: boolean }) {
    return {
      id: p.id,
      parentId: p.root ? null : 7,
      workspaceId: 1,
      title: p.title,
      depth: 1,
      taskStatus: "created",
      tier: null,
      childCount: 0,
      run: { state: p.state, runningRunIds: [], pendingPermissions: 0 },
      counts: { running: 0, waitingPermission: 0, failed: 0, done: 0, idle: 0 },
      usage: {
        own: { runs: 0, inputTokens: null, outputTokens: null, cacheReadTokens: null, cacheWriteTokens: null, reasoningTokens: null, costUsd: null },
        subtree: { runs: 0, inputTokens: null, outputTokens: null, cacheReadTokens: null, cacheWriteTokens: null, reasoningTokens: null, costUsd: null },
        archivedDescendants: 0,
      },
    };
  }

  it("archive dialog lists live children", async () => {
    const client = wsFake();
    render(
      <ArchiveTaskDialog client={client} task={ARCHIVE_TASK} open={true} onOpenChange={vi.fn()} onArchived={vi.fn()} />,
    );
    expect(client.calls.map((c) => c.method)).toContain("task.tree");

    await act(async () => {
      client.nth("task.tree").resolve({
        nodes: [
          treeNode({ id: 7, title: "Parent task", state: "idle", root: true }),
          treeNode({ id: 8, title: "Coder one", state: "running" }),
          treeNode({ id: 9, title: "Coder two", state: "waiting_permission" }),
        ],
        truncated: false,
      });
    });
    await flush();

    const block = screen.getByTestId("archive-children");
    expect(block.textContent).toContain("These tasks stay and become top-level:");
    expect(block.textContent).toContain("Coder one · running");
    expect(block.textContent).toContain("Coder two · needs you");
    expect(screen.getByTestId("dialog-archive-task-confirm").textContent).toBe("Archive parent only");
  });

  it("archive dialog with no children is unchanged", async () => {
    const client = wsFake();
    render(
      <ArchiveTaskDialog client={client} task={ARCHIVE_TASK} open={true} onOpenChange={vi.fn()} onArchived={vi.fn()} />,
    );
    await act(async () => {
      client.nth("task.tree").resolve({ nodes: [treeNode({ id: 7, title: "Parent task", state: "idle", root: true })], truncated: false });
    });
    await flush();

    expect(screen.queryByTestId("archive-children")).toBeNull();
    expect(screen.getByTestId("dialog-archive-task-confirm").textContent).toBe("Archive");
  });

  it("archive dialog confirms with task.archive for the parent only", async () => {
    const client = wsFake();
    const onArchived = vi.fn();
    const onOpenChange = vi.fn();
    render(
      <ArchiveTaskDialog client={client} task={ARCHIVE_TASK} open={true} onOpenChange={onOpenChange} onArchived={onArchived} />,
    );
    await act(async () => {
      client.nth("task.tree").resolve({
        nodes: [treeNode({ id: 7, title: "Parent task", state: "idle", root: true }), treeNode({ id: 8, title: "Coder one", state: "idle" })],
        truncated: false,
      });
    });
    await flush();

    await act(async () => {
      fireEvent.click(screen.getByTestId("dialog-archive-task-confirm"));
    });
    // Exactly one task.archive, and only for the parent id -- archiving
    // is never a cascade.
    expect(client.calls.filter((c) => c.method === "task.archive")).toHaveLength(1);
    expect(client.nth("task.archive").params).toEqual({ id: 7, taskId: 7 });
  });

  it("archive dialog child-list failure still allows archive with a warning", async () => {
    const client = wsFake();
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => {});
    render(
      <ArchiveTaskDialog client={client} task={ARCHIVE_TASK} open={true} onOpenChange={vi.fn()} onArchived={vi.fn()} />,
    );
    await act(async () => {
      client.nth("task.tree").reject(new Error("boom"));
    });
    await act(async () => {
      client.nth("task.list").reject(new Error("boom"));
    });
    await flush();

    expect(screen.getByText("Could not check for child tasks.")).toBeTruthy();
    expect(screen.getByTestId("dialog-archive-task-confirm").textContent).toBe("Archive");

    await act(async () => {
      fireEvent.click(screen.getByTestId("dialog-archive-task-confirm"));
    });
    expect(client.calls.filter((c) => c.method === "task.archive")).toHaveLength(1);
    consoleError.mockRestore();
  });

  it("archive dialog falls back to titles when task.tree fails", async () => {
    const client = wsFake();
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => {});
    render(
      <ArchiveTaskDialog client={client} task={ARCHIVE_TASK} open={true} onOpenChange={vi.fn()} onArchived={vi.fn()} />,
    );
    await act(async () => {
      client.nth("task.tree").reject(new Error("method not found: task.tree"));
    });
    await act(async () => {
      client.nth("task.list").resolve([child(8, "Coder one"), child(9, "Coder two")]);
    });
    await flush();

    const block = screen.getByTestId("archive-children");
    expect(block.textContent).toContain("Coder one");
    expect(block.textContent).not.toContain("Coder one ·");
    expect(screen.getByTestId("dialog-archive-task-confirm").textContent).toBe("Archive parent only");
    consoleError.mockRestore();
  });
});
