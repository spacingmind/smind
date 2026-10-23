import { act, fireEvent, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { ArchiveTaskDialog } from "@/components/crud-dialogs";
import type { ArchivePreview, Task } from "@/lib/types";
import { RpcError, type WsClient } from "@/lib/ws-client";
import { FakeWsClient } from "@/test/fake-ws-client";
import { renderWithClientProp as render } from "@/test/render-with-daemon";

const TASK: Task = {
  ID: 7,
  WorkspaceID: 1,
  SpaceID: null,
  Title: "Fix the parser",
  Status: "active",
  WorktreePath: "/tmp/wt",
  Branch: "smind/task-7",
  CreatedAt: "2026-10-11T00:00:00Z",
  UpdatedAt: "2026-10-11T00:00:00Z",
  ArchivedAt: null,
};

function preview(over: Partial<ArchivePreview> = {}): ArchivePreview {
  return {
    branch: { name: "smind/task-7", owned: true },
    merged: { state: "merged", via: "ancestor" },
    base: { ref: "develop" },
    risks: { uncommitted: { files: 0, added: 0, deleted: 0 }, commitsAheadOfOrigin: 0, commitsAheadOfBase: 0, published: true },
    ignoredFilesLost: true,
    hasTeardown: false,
    canDeleteBranch: true,
    deleteBranchDefault: true,
    ...over,
  };
}

async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 4; i++) await Promise.resolve();
  });
}

async function open(p: ArchivePreview, onOpenChange = vi.fn()) {
  const client = new FakeWsClient() as unknown as FakeWsClient & WsClient;
  render(<ArchiveTaskDialog client={client} task={TASK} open onOpenChange={onOpenChange} onArchived={vi.fn()} />);
  expect(client.nth("task.archivePreview").params).toEqual({ taskId: 7 });
  await act(async () => client.nth("task.archivePreview").resolve(p));
  await flush();
  return { client, onOpenChange };
}

describe("ArchiveTaskDialog branch cleanup (ADR-0039 §9)", () => {
  it("shows the risk rows from the preview", async () => {
    await open(
      preview({
        merged: { state: "unmerged", via: "" },
        risks: { uncommitted: { files: 2, added: 12, deleted: 3 }, commitsAheadOfOrigin: 2, commitsAheadOfBase: 2, published: true },
      }),
    );
    const rows = screen.getByTestId("archive-risks");
    expect(rows).toHaveTextContent("Uncommitted changes (12 added, 3 deleted lines) — saved to the branch first");
    expect(rows).toHaveTextContent("2 commits not pushed");
    expect(rows).toHaveTextContent("Branch smind/task-7 is not merged anywhere");
    expect(rows).toHaveTextContent("Files git ignores (node_modules, .env) are not kept");
  });

  it("says when the branch is merged, and into what", async () => {
    await open(preview());
    expect(screen.getByTestId("archive-risk-merged")).toHaveTextContent("Branch smind/task-7 is merged into develop");
    expect(screen.queryByTestId("archive-risk-uncommitted")).not.toBeInTheDocument();
  });

  it("defaults Delete branch on only when merged and owned", async () => {
    const { client } = await open(preview());
    const box = screen.getByTestId("archive-delete-branch");
    expect(box).toBeChecked();
    fireEvent.click(screen.getByTestId("dialog-archive-task-confirm"));
    await flush();
    expect(client.nth("task.archive").params).toEqual({ id: 7, taskId: 7, deleteBranch: true });
  });

  it("keeps the branch when the daemon does not default delete on", async () => {
    const { client } = await open(preview({ deleteBranchDefault: false }));
    expect(screen.getByTestId("archive-delete-branch")).not.toBeChecked();
    fireEvent.click(screen.getByTestId("dialog-archive-task-confirm"));
    await flush();
    expect(client.nth("task.archive").params).toEqual({ id: 7, taskId: 7 });
  });

  it("explains why deletion is disabled", async () => {
    const reason = "This is your own branch; smind never deletes it.";
    const { client } = await open(
      preview({ branch: { name: "feature/mine", owned: false }, canDeleteBranch: false, deleteBranchDefault: false, deleteBlockedReason: reason }),
    );
    expect(screen.getByTestId("archive-delete-branch")).toBeDisabled();
    expect(screen.getByTestId("archive-delete-branch-reason")).toHaveTextContent(reason);
    // A branch smind didn't create has no "anyway" option.
    expect(screen.queryByTestId("archive-delete-branch-force")).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId("dialog-archive-task-confirm"));
    await flush();
    expect(client.nth("task.archive").params).toEqual({ id: 7, taskId: 7 });
  });

  it("offers delete-anyway for an owned unmerged branch and sends force", async () => {
    const { client } = await open(
      preview({
        merged: { state: "unmerged", via: "" },
        risks: { uncommitted: { files: 0, added: 0, deleted: 0 }, commitsAheadOfOrigin: 0, commitsAheadOfBase: 3, published: true },
        canDeleteBranch: false,
        deleteBranchDefault: false,
        deleteBlockedReason: "The branch is not merged anywhere yet.",
      }),
    );
    const anyway = screen.getByTestId("archive-delete-branch-force");
    expect(anyway).not.toBeChecked();
    expect(anyway.closest("label")).toHaveTextContent("Delete the branch anyway — 3 commits are not merged anywhere");
    expect(anyway.closest("label")).toHaveClass("text-destructive");
    fireEvent.click(anyway);
    fireEvent.click(screen.getByTestId("dialog-archive-task-confirm"));
    await flush();
    expect(client.nth("task.archive").params).toEqual({ id: 7, taskId: 7, deleteBranch: true, force: true });
  });

  it("shows a typed error and stays open", async () => {
    const { client, onOpenChange } = await open(preview());
    fireEvent.click(screen.getByTestId("dialog-archive-task-confirm"));
    await flush();
    await act(async () => {
      client.nth("task.archive").reject(new RpcError("The worktree has uncommitted changes, so smind won't delete the branch.", "branch_dirty"));
    });
    await flush();
    expect(screen.getByTestId("dialog-archive-task-error")).toHaveTextContent("The worktree has uncommitted changes, so smind won't delete the branch.");
    expect(onOpenChange).not.toHaveBeenCalledWith(false);
    expect(screen.getByTestId("dialog-archive-task")).toBeInTheDocument();
  });

  it("never shows a typed error's raw text", async () => {
    const { client } = await open(preview());
    fireEvent.click(screen.getByTestId("dialog-archive-task-confirm"));
    await flush();
    await act(async () => {
      client.nth("task.archive").reject(new RpcError("git [branch -D smind/task-7]: exit status 1", "branch_unmerged"));
    });
    await flush();
    const error = screen.getByTestId("dialog-archive-task-error");
    expect(error.textContent).not.toMatch(/exit status|branch -D/);
    expect(error).toHaveTextContent("isn't merged anywhere");
  });

  it("still archives when the preview is unavailable", async () => {
    const client = new FakeWsClient() as unknown as FakeWsClient & WsClient;
    render(<ArchiveTaskDialog client={client} task={TASK} open onOpenChange={vi.fn()} onArchived={vi.fn()} />);
    await act(async () => client.nth("task.archivePreview").reject(new Error("method not found: task.archivePreview")));
    await flush();
    expect(screen.queryByTestId("archive-delete-branch")).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId("dialog-archive-task-confirm"));
    await flush();
    expect(client.nth("task.archive").params).toEqual({ id: 7, taskId: 7 });
  });
});
