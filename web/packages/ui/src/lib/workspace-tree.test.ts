import { describe, expect, it } from "vitest";

import { applyLifecycleEvent, buildWorkspaceTree, LIFECYCLE_TOPICS } from "@/lib/workspace-tree";
import type { Space, Task, Workspace } from "@/lib/types";

const WS_1: Workspace = {
  ID: 1,
  Path: "/tmp/one",
  Title: "One",
  RoutingPolicy: "",
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-01T00:00:00Z",
};

const WS_2: Workspace = { ...WS_1, ID: 2, Path: "/tmp/two", Title: "Two" };

const SPACE: Space = {
  ID: 10,
  WorkspaceID: 1,
  Title: "Space",
  EnvData: "",
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-01T00:00:00Z",
};

function task(id: number, over: Partial<Task> = {}): Task {
  return {
    ID: id,
    WorkspaceID: 1,
    SpaceID: null,
    Title: `Task ${id}`,
    Status: "active",
    WorktreePath: null,
    Branch: null,
    CreatedAt: "2024-01-01T00:00:00Z",
    UpdatedAt: "2024-01-01T00:00:00Z",
    ArchivedAt: null,
    ...over,
  };
}

/** All task IDs in a workspace, ungrouped first then per space, in render order. */
function ids(tree: ReturnType<typeof buildWorkspaceTree>[], workspaceId = 1): { ungrouped: number[]; spaces: Record<number, number[]> } {
  const ws = tree.find((w) => w.ID === workspaceId)!;
  return {
    ungrouped: ws.ungroupedTasks.map((t) => t.ID),
    spaces: Object.fromEntries(ws.spaces.map((sp) => [sp.ID, sp.tasks.map((t) => t.ID)])),
  };
}

describe("buildWorkspaceTree", () => {
  it("groups tasks by SpaceID and keeps space-less ones alongside the spaces", () => {
    const tree = buildWorkspaceTree(WS_1, [SPACE], [task(1), task(2, { SpaceID: 10 })]);
    expect(tree.ungroupedTasks.map((t) => t.ID)).toEqual([1]);
    expect(tree.spaces[0]!.tasks.map((t) => t.ID)).toEqual([2]);
  });

  it("gives a space with no tasks an empty list rather than dropping it", () => {
    const tree = buildWorkspaceTree(WS_1, [SPACE], []);
    expect(tree.spaces).toHaveLength(1);
    expect(tree.spaces[0]!.tasks).toEqual([]);
  });
});

describe("applyLifecycleEvent", () => {
  const base = () => [buildWorkspaceTree(WS_1, [SPACE], [task(5), task(7, { SpaceID: 10 })])];

  it("task.created inserts the row without a refetch, in the ID order a refetch would return", () => {
    const next = applyLifecycleEvent(base(), "task.created", { task: task(6) });
    expect(ids(next).ungrouped).toEqual([5, 6]);
  });

  it("task.created for a space places it in that space, not in the ungrouped bucket", () => {
    const next = applyLifecycleEvent(base(), "task.created", { task: task(6, { SpaceID: 10 }) });
    expect(ids(next)).toEqual({ ungrouped: [5], spaces: { 10: [6, 7] } });
  });

  it("re-delivering the same task.created is an upsert, not a double-insert", () => {
    // ADR 0009: the acting client's own refresh() and the event race, so an
    // insert must be idempotent whichever arrives first.
    const once = applyLifecycleEvent(base(), "task.created", { task: task(6) });
    const twice = applyLifecycleEvent(once, "task.created", { task: task(6, { Title: "renamed" }) });
    expect(ids(twice).ungrouped).toEqual([5, 6]);
    expect(twice[0]!.ungroupedTasks.find((t) => t.ID === 6)!.Title).toBe("renamed");
  });

  it("task.updated moves a task between buckets when its SpaceID changed", () => {
    const next = applyLifecycleEvent(base(), "task.updated", { task: task(5, { SpaceID: 10 }) });
    expect(ids(next)).toEqual({ ungrouped: [], spaces: { 10: [5, 7] } });
  });

  it("task.archived removes the row, matching the tree a refetch would return", () => {
    // ListTasks filters archived rows out, so archive reads as removal here
    // even though ADR 0009 keeps the row and emits archived (not deleted).
    const next = applyLifecycleEvent(base(), "task.archived", { task: task(7, { SpaceID: 10 }) });
    expect(ids(next)).toEqual({ ungrouped: [5], spaces: { 10: [] } });
  });

  it("a task.created carrying an already-archived row removes it instead of inserting it", () => {
    const next = applyLifecycleEvent(base(), "task.updated", { task: task(5, { ArchivedAt: "2024-02-01T00:00:00Z" }) });
    expect(ids(next).ungrouped).toEqual([]);
  });

  it("task.deleted prunes by id from whichever bucket holds it", () => {
    const next = applyLifecycleEvent(base(), "task.deleted", { id: 7, workspaceId: 1, spaceId: 10 });
    expect(ids(next)).toEqual({ ungrouped: [5], spaces: { 10: [] } });
  });

  it("deleting a task that is already gone is a no-op that keeps the same tree reference", () => {
    const tree = base();
    expect(applyLifecycleEvent(tree, "task.deleted", { id: 999, workspaceId: 1, spaceId: null })).toBe(tree);
  });

  it("workspace.created appends a workspace with empty buckets", () => {
    const next = applyLifecycleEvent(base(), "workspace.created", { workspace: WS_2 });
    expect(next.map((w) => w.ID)).toEqual([1, 2]);
    expect(next[1]!.spaces).toEqual([]);
    expect(next[1]!.ungroupedTasks).toEqual([]);
  });

  it("workspace.created for a workspace already held keeps its fetched subtree", () => {
    const next = applyLifecycleEvent(base(), "workspace.created", { workspace: { ...WS_1, Title: "Renamed" } });
    expect(next).toHaveLength(1);
    expect(next[0]!.Title).toBe("Renamed");
    expect(ids(next)).toEqual({ ungrouped: [5], spaces: { 10: [7] } });
  });

  it("workspace.deleted prunes the whole subtree from one root event", () => {
    // ADR 0009 publishes only the root event for a cascade -- never one per
    // descendant -- so the client prunes the descendants itself.
    const next = applyLifecycleEvent(base(), "workspace.deleted", { id: 1 });
    expect(next).toEqual([]);
  });

  it("space.created inserts an empty space into its workspace", () => {
    const next = applyLifecycleEvent(base(), "space.created", { space: { ...SPACE, ID: 11, Title: "Second" } });
    expect(next[0]!.spaces.map((sp) => sp.ID)).toEqual([10, 11]);
    expect(next[0]!.spaces[1]!.tasks).toEqual([]);
  });

  it("space.deleted removes the space and the tasks that cascaded with it", () => {
    const next = applyLifecycleEvent(base(), "space.deleted", { id: 10, workspaceId: 1 });
    expect(next[0]!.spaces).toEqual([]);
    expect(next[0]!.ungroupedTasks.map((t) => t.ID)).toEqual([5]);
  });

  it("an event for a workspace this client does not hold leaves the tree untouched by reference", () => {
    const tree = base();
    expect(applyLifecycleEvent(tree, "task.created", { task: task(9, { WorkspaceID: 2 }) })).toBe(tree);
    expect(applyLifecycleEvent(tree, "space.created", { space: { ...SPACE, ID: 12, WorkspaceID: 2 } })).toBe(tree);
    expect(applyLifecycleEvent(tree, "workspace.deleted", { id: 2 })).toBe(tree);
    expect(applyLifecycleEvent(tree, "space.deleted", { id: 10, workspaceId: 2 })).toBe(tree);
  });

  it("a task.created naming a space this client has not fetched is ignored rather than dropped elsewhere", () => {
    const tree = base();
    expect(applyLifecycleEvent(tree, "task.created", { task: task(9, { SpaceID: 99 }) })).toBe(tree);
  });

  it("a malformed or unknown payload never throws and never mutates", () => {
    const tree = base();
    for (const payload of [null, undefined, 42, "nope", {}, { task: null }, { task: { Title: "no id" } }, { id: "3" }]) {
      for (const topic of LIFECYCLE_TOPICS) {
        expect(applyLifecycleEvent(tree, topic, payload)).toBe(tree);
      }
    }
    expect(applyLifecycleEvent(tree, "run.status", { runId: "r1" })).toBe(tree);
  });
});
