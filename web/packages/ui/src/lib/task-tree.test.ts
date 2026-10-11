import { describe, expect, it } from "vitest";

import type { AttentionReason, TaskRunStatus } from "@/hooks/use-task-attention";
import { attentionRollup, descendantsOf, nestTasks, tierOf } from "@/lib/task-tree";
import type { Task } from "@/lib/types";

function task(id: number, overrides: Partial<Task> = {}): Task {
  return {
    ID: id,
    WorkspaceID: 1,
    SpaceID: null,
    Title: `task ${id}`,
    Status: "",
    WorktreePath: null,
    Branch: null,
    CreatedAt: "",
    UpdatedAt: "",
    ArchivedAt: null,
    ...overrides,
  };
}


describe("nestTasks", () => {
  it("nests children under parent in id order", () => {
    const forest = nestTasks([task(5, { ParentTaskID: 1 }), task(1), task(3, { ParentTaskID: 1 }), task(2)]);
    expect(forest.map((n) => n.task.ID)).toEqual([1, 2]);
    const parent = forest[0];
    expect(parent.children.map((c) => c.task.ID)).toEqual([3, 5]);
    expect(parent.children[0].depth).toBe(1);
  });

  it("orphan whose parent is missing is a root", () => {
    const forest = nestTasks([task(9, { ParentTaskID: 404 })]);
    expect(forest).toHaveLength(1);
    expect(forest[0].task.ID).toBe(9);
    expect(forest[0].depth).toBe(0);
  });

  it("child in another space nests under its parent", () => {
    // Hierarchy trumps the space grouping (Decision 2): nestTasks gets all
    // tasks of the workspace and ignores SpaceID entirely.
    const forest = nestTasks([task(1, { SpaceID: 7 }), task(2, { SpaceID: null, ParentTaskID: 1 })]);
    expect(forest.map((n) => n.task.ID)).toEqual([1]);
    expect(forest[0].children.map((c) => c.task.ID)).toEqual([2]);
  });

  it("cycle does not loop", () => {
    const forest = nestTasks([task(1, { ParentTaskID: 2 }), task(2, { ParentTaskID: 1 }), task(3)]);
    // Both cycle members survive; 3 is a normal root.
    const all = [forest.map((n) => n.task.ID), ...forest.map((n) => descendantsOf(n).map((t) => t.ID))];
    expect(all.flat().sort()).toEqual([1, 2, 3]);
  });

  it("depth is derived from the chain", () => {
    const forest = nestTasks([
      task(1),
      task(2, { ParentTaskID: 1 }),
      task(3, { ParentTaskID: 2 }),
      task(4, { ParentTaskID: 3 }),
    ]);
    const great = forest[0].children[0].children[0].children[0];
    expect(great.task.ID).toBe(4);
    expect(great.depth).toBe(3);
    expect(forest[0].children[0].children[0].depth).toBe(2);
    expect(descendantsOf(forest[0]).map((t) => t.ID)).toEqual([2, 3, 4]);
  });
});

describe("attentionRollup", () => {
  function rollup(tasks: Task[], rootId: number, attention: Map<number, Set<AttentionReason>>, runStatus: TaskRunStatus) {
    const forest = nestTasks(tasks);
    const root = forest.find((n) => n.task.ID === rootId)!;
    return attentionRollup(root, attention, runStatus);
  }

  it("rollup uses the same precedence as a row dot", () => {
    const tasks = [task(1), task(2, { ParentTaskID: 1 }), task(3, { ParentTaskID: 1 }), task(4, { ParentTaskID: 1 })];
    const attention = new Map([
      [3, new Set(["finished"] as AttentionReason[])],
      [4, new Set(["permission"] as AttentionReason[])],
    ]);
    const runStatus = new Map([[2, "error"]]);
    expect(rollup(tasks, 1, attention, runStatus)).toEqual({ reason: "error", count: 3 });

    // Error gone: permission outranks finished, same as a row's own dot.
    const runStatus2 = new Map();
    expect(rollup(tasks, 1, attention, runStatus2)).toEqual({ reason: "permission", count: 2 });

    const attention3 = new Map([[3, new Set(["finished"] as AttentionReason[])]]);
    expect(rollup(tasks, 1, attention3, runStatus2)).toEqual({ reason: "finished", count: 1 });
  });

  it("rollup counts every descendant once", () => {
    const tasks = [task(1), task(2, { ParentTaskID: 1 }), task(3, { ParentTaskID: 2 }), task(4, { ParentTaskID: 2 })];
    const attention = new Map([
      [2, new Set(["permission"] as AttentionReason[])],
      [3, new Set(["error"] as AttentionReason[])],
      [4, new Set(["error"] as AttentionReason[])],
    ]);
    expect(rollup(tasks, 1, attention, new Map())).toEqual({ reason: "error", count: 3 });
  });

  it("rollup ignores a seen finished child", () => {
    const tasks = [task(1), task(2, { ParentTaskID: 1 })];
    const attention = new Map<number, Set<AttentionReason>>();
    expect(rollup(tasks, 1, attention, new Map())).toEqual({ reason: null, count: 0 });
  });
});

describe("tierOf", () => {
  it("tierOf table", () => {
    expect(tierOf(false, true)).toBe("supervisor");
    expect(tierOf(true, true)).toBe("leader");
    expect(tierOf(true, false)).toBe("coder");
    expect(tierOf(false, false)).toBeNull();
  });
});
