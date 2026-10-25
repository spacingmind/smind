import { describe, expect, it } from "vitest";

import {
  availabilityFromTaskTree,
  sidebarTreeActions,
  treeActionAvailability,
  type TreeRunState,
} from "@/lib/task-tree-actions";
import type { TaskTreeNode } from "@/lib/task-lineage";
import type { Task } from "@/lib/types";

describe("treeActionAvailability (PR-H11)", () => {
  it("stop is enabled when any task in the subtree runs", () => {
    const a = treeActionAvailability("idle", ["done", "running"]);
    expect(a.stop.enabled).toBe(true);
    expect(a.stop.reason).toBeNull();
    // waiting_permission is running work too.
    const b = treeActionAvailability("running", []);
    expect(b.stop.enabled).toBe(true);
    const c = treeActionAvailability("idle", ["waiting_permission"]);
    expect(c.stop.enabled).toBe(true);
  });

  it("stop disabled with reason when nothing runs", () => {
    const a = treeActionAvailability("idle", ["done", "stopped", "idle"]);
    expect(a.stop.enabled).toBe(false);
    expect(a.stop.reason).toBe("No running tasks in this tree");
  });

  it("archive enabled when a descendant is finished", () => {
    const a = treeActionAvailability("idle", ["done"]);
    expect(a.archive.enabled).toBe(true);
    expect(a.archive.reason).toBeNull();
    const b = treeActionAvailability("idle", ["error", "stopped"]);
    expect(b.archive.enabled).toBe(true);
  });

  it("archive disabled with reason when no descendants", () => {
    const a = treeActionAvailability("idle", []);
    expect(a.archive.enabled).toBe(false);
    expect(a.archive.reason).toBe("No sub-tasks");
  });

  it("archive disabled with reason when descendants are all unfinished", () => {
    const a = treeActionAvailability("idle", ["running", "idle", "waiting_permission"]);
    expect(a.archive.enabled).toBe(false);
    expect(a.archive.reason).toBe("No finished sub-tasks");
  });

  it("self is never counted for archive but is for stop", () => {
    // Self finished with no descendants: nothing to archive...
    const a = treeActionAvailability("done", []);
    expect(a.archive.enabled).toBe(false);
    // ...and nothing to stop.
    expect(a.stop.enabled).toBe(false);
    // Self running with no descendants: stop covers it, archive still has
    // nothing (never self).
    const b = treeActionAvailability("running", []);
    expect(b.stop.enabled).toBe(true);
    expect(b.archive.enabled).toBe(false);
  });
});

function treeTask(id: number, parent: number | null, title = `Task ${id}`): Task {
  return {
    ID: id,
    WorkspaceID: 1,
    SpaceID: null,
    ParentTaskID: parent,
    Title: title,
    Status: "active",
    WorktreePath: null,
    Branch: null,
    CreatedAt: "2026-10-11T00:00:00Z",
    UpdatedAt: "2026-10-11T00:00:00Z",
    ArchivedAt: null,
  };
}

function treeNode(id: number, parent: number | null, state: TreeRunState): TaskTreeNode {
  return {
    id,
    parentId: parent,
    workspaceId: 1,
    title: `Task ${id}`,
    depth: 0,
    taskStatus: "created",
    tier: null,
    childCount: 0,
    run: { state, runningRunIds: [], pendingPermissions: 0 },
    counts: { running: 0, waitingPermission: 0, failed: 0, done: 0, idle: 0 },
    usage: {
      own: { runs: 0, inputTokens: null, outputTokens: null, cacheReadTokens: null, cacheWriteTokens: null, reasoningTokens: null, costUsd: null },
      subtree: { runs: 0, inputTokens: null, outputTokens: null, cacheReadTokens: null, cacheWriteTokens: null, reasoningTokens: null, costUsd: null },
      archivedDescendants: 0,
    },
  };
}

describe("availabilityFromTaskTree", () => {
  it("reads nodes[0] as self and the rest as descendants", () => {
    const a = availabilityFromTaskTree([
      treeNode(1, null, "idle"),
      treeNode(2, 1, "done"),
      treeNode(3, 1, "running"),
    ]);
    expect(a?.stop.enabled).toBe(true);
    expect(a?.archive.enabled).toBe(true);
  });

  it("a done run cut off at a cap (needs-continue) does not count as finished", () => {
    const capped = treeNode(2, 1, "done");
    capped.run.disposition = "needs-continue";
    const a = availabilityFromTaskTree([treeNode(1, null, "idle"), capped]);
    expect(a?.archive.enabled).toBe(false);
  });

  it("an errored run carrying needs-continue (J1) still counts as finished, as the daemon's isFinishedNode does", () => {
    const resumable = treeNode(2, 1, "error");
    resumable.run.disposition = "needs-continue";
    const a = availabilityFromTaskTree([treeNode(1, null, "idle"), resumable]);
    expect(a?.archive.enabled).toBe(true);
  });

  it("returns null for an empty node list", () => {
    expect(availabilityFromTaskTree([])).toBeNull();
  });

  it("returns null when the task has no descendants (neither entry is offered)", () => {
    expect(availabilityFromTaskTree([treeNode(1, null, "running")])).toBeNull();
  });
});

describe("sidebarTreeActions", () => {
  it("answers only for tasks with live descendants and walks the whole subtree", () => {
    const tasks = [treeTask(1, null), treeTask(2, 1), treeTask(3, 2), treeTask(4, null)];
    const runStatus = new Map([
      
      [2, "done" as const],
      [3, "running" as const],
    ]);
    const actions = sidebarTreeActions(tasks, runStatus);
    expect(actions.has(1)).toBe(true);
    expect(actions.has(2)).toBe(true);
    expect(actions.has(4)).toBe(false);
    // Grandchild running -> stop enabled for the root; finished child ->
    // archive enabled.
    expect(actions.get(1)?.stop.enabled).toBe(true);
    expect(actions.get(1)?.archive.enabled).toBe(true);
    // Task 2's subtree has a running child and no finished one.
    expect(actions.get(2)?.stop.enabled).toBe(true);
    expect(actions.get(2)?.archive.enabled).toBe(false);
    expect(actions.get(2)?.archive.reason).toBe("No finished sub-tasks");
  });

  it("treats an absent run status as idle", () => {
    const tasks = [treeTask(1, null), treeTask(2, 1)];
    const actions = sidebarTreeActions(tasks, new Map());
    expect(actions.get(1)?.stop.enabled).toBe(false);
    expect(actions.get(1)?.archive.enabled).toBe(false);
    expect(actions.get(1)?.archive.reason).toBe("No finished sub-tasks");
  });
});
