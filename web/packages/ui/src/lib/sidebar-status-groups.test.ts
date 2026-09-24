import { describe, expect, it } from "vitest";

import { groupTasksByStatus, statusGroupForTask } from "@/lib/sidebar-status-groups";
import type { AttentionReason, TaskAttention, TaskRunStatus } from "@/hooks/use-task-attention";
import type { Task } from "@/lib/types";

function task(id: number, title = `Task ${id}`): Task {
  return {
    ID: id,
    WorkspaceID: 1,
    SpaceID: null,
    Title: title,
    Status: "active",
    WorktreePath: null,
    Branch: null,
    CreatedAt: "2024-01-01T00:00:00Z",
    UpdatedAt: "2024-01-01T00:00:00Z",
    ArchivedAt: null,
  };
}

function attentionOf(entries: [number, AttentionReason[]][]): TaskAttention {
  return new Map(entries.map(([id, reasons]) => [id, new Set(reasons)]));
}

describe("statusGroupForTask", () => {
  it("a pending permission wins over a running status", () => {
    const attention = attentionOf([[1, ["permission"]]]);
    const runStatus: TaskRunStatus = new Map([[1, "running"]]);
    expect(statusGroupForTask(1, attention, runStatus)).toBe("needs-attention");
  });

  it("an errored run is 'error' even once its attention reason has been seen (cleared)", () => {
    const runStatus: TaskRunStatus = new Map([[1, "error"]]);
    expect(statusGroupForTask(1, new Map(), runStatus)).toBe("error");
  });

  it("an unseen finished run is 'needs-attention', not 'done'", () => {
    const attention = attentionOf([[1, ["finished"]]]);
    const runStatus: TaskRunStatus = new Map([[1, "done"]]);
    expect(statusGroupForTask(1, attention, runStatus)).toBe("needs-attention");
  });

  it("a running task with no attention is 'running'", () => {
    const runStatus: TaskRunStatus = new Map([[1, "running"]]);
    expect(statusGroupForTask(1, new Map(), runStatus)).toBe("running");
  });

  it("a done task with no attention is 'done'", () => {
    const runStatus: TaskRunStatus = new Map([[1, "done"]]);
    expect(statusGroupForTask(1, new Map(), runStatus)).toBe("done");
  });

  it("a task that's never run and has no attention is 'idle'", () => {
    expect(statusGroupForTask(1, new Map(), new Map())).toBe("idle");
  });

  it("a stopped run with no attention is 'idle', not 'error'", () => {
    const runStatus: TaskRunStatus = new Map([[1, "stopped"]]);
    expect(statusGroupForTask(1, new Map(), runStatus)).toBe("idle");
  });
});

describe("groupTasksByStatus", () => {
  it("buckets tasks and orders groups needs-attention, error, running, done, idle -- dropping empty ones", () => {
    const tasks = [task(1), task(2), task(3), task(4), task(5)];
    const attention = attentionOf([[1, ["permission"]]]);
    const runStatus: TaskRunStatus = new Map([
      [2, "error"],
      [3, "running"],
      [4, "done"],
    ]);

    const groups = groupTasksByStatus(tasks, attention, runStatus);

    expect(groups.map((g) => g.key)).toEqual(["needs-attention", "error", "running", "done", "idle"]);
    expect(groups.find((g) => g.key === "needs-attention")?.tasks.map((t) => t.ID)).toEqual([1]);
    expect(groups.find((g) => g.key === "error")?.tasks.map((t) => t.ID)).toEqual([2]);
    expect(groups.find((g) => g.key === "idle")?.tasks.map((t) => t.ID)).toEqual([5]);
  });

  it("omits a bucket entirely when nothing falls into it", () => {
    const groups = groupTasksByStatus([task(1)], new Map(), new Map());
    expect(groups).toEqual([{ key: "idle", label: "Idle", tasks: [task(1)] }]);
  });

  it("returns nothing for an empty task list", () => {
    expect(groupTasksByStatus([], new Map(), new Map())).toEqual([]);
  });
});
