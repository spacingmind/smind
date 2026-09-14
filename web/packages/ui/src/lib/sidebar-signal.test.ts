import { describe, expect, it } from "vitest";

import { aggregateStatus, attentionDotStatus, primaryAttentionReason, runDotStatus, workspaceTasks } from "@/lib/sidebar-signal";
import { buildWorkspaceTree } from "@/lib/workspace-tree";
import type { AttentionReason, TaskRunStatus } from "@/hooks/use-task-attention";
import type { RunStatusValue, Space, Task, Workspace } from "@/lib/types";

const WORKSPACE: Workspace = {
  ID: 1,
  Path: "/tmp/ws",
  Title: "Workspace",
  RoutingPolicy: "",
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-01T00:00:00Z",
};

const SPACE: Space = {
  ID: 10,
  WorkspaceID: 1,
  Title: "Space",
  EnvData: "",
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-01T00:00:00Z",
};

function task(id: number, spaceId: number | null = null): Task {
  return {
    ID: id,
    WorkspaceID: 1,
    SpaceID: spaceId,
    Title: `Task ${id}`,
    Status: "created",
    WorktreePath: null,
    Branch: null,
    CreatedAt: "2024-01-01T00:00:00Z",
    UpdatedAt: "2024-01-01T00:00:00Z",
    ArchivedAt: null,
  };
}

function attentionOf(entries: [number, AttentionReason[]][]): Map<number, Set<AttentionReason>> {
  return new Map(entries.map(([id, reasons]) => [id, new Set(reasons)]));
}

function runStatusOf(entries: [number, RunStatusValue][]): TaskRunStatus {
  return new Map(entries);
}

describe("primaryAttentionReason", () => {
  it("returns null when a task has no reasons at all", () => {
    expect(primaryAttentionReason(undefined)).toBeNull();
    expect(primaryAttentionReason(new Set())).toBeNull();
  });

  it("prefers error over permission and permission over finished", () => {
    expect(primaryAttentionReason(new Set<AttentionReason>(["finished", "permission", "error"]))).toBe("error");
    expect(primaryAttentionReason(new Set<AttentionReason>(["finished", "permission"]))).toBe("permission");
    expect(primaryAttentionReason(new Set<AttentionReason>(["finished"]))).toBe("finished");
  });
});

describe("attentionDotStatus", () => {
  it("gives each of the three reasons its own dot variant", () => {
    const variants = (["error", "permission", "finished"] as const).map(attentionDotStatus);
    expect(new Set(variants).size).toBe(3);
  });
});

describe("runDotStatus", () => {
  it("maps each run status to a dot, treating a user-initiated stop as neutral rather than a fault", () => {
    expect(runDotStatus("running")).toBe("running");
    expect(runDotStatus("done")).toBe("success");
    expect(runDotStatus("error")).toBe("danger");
    expect(runDotStatus("stopped")).toBe("neutral");
  });

  it("yields no dot for a task that has never run", () => {
    expect(runDotStatus(undefined)).toBeNull();
  });
});

describe("aggregateStatus", () => {
  const tasks = [task(1), task(2), task(3)];

  it("is null for a bucket with nothing to say, so an idle tree stays visually quiet", () => {
    expect(aggregateStatus(tasks, new Map(), new Map())).toBeNull();
    expect(aggregateStatus([], undefined, new Map())).toBeNull();
  });

  it("surfaces an errored task above everything else in the bucket", () => {
    expect(
      aggregateStatus(tasks, attentionOf([[2, ["error"]]]), runStatusOf([[1, "running"]])),
    ).toBe("danger");
    // ...including when the error is only visible as a run status.
    expect(aggregateStatus(tasks, new Map(), runStatusOf([[3, "error"]]))).toBe("danger");
  });

  it("ranks a waiting permission above a merely busy task", () => {
    expect(
      aggregateStatus(tasks, attentionOf([[3, ["permission"]]]), runStatusOf([[1, "running"]])),
    ).toBe("warning");
  });

  it("falls back to running, then to an unseen finished result", () => {
    expect(aggregateStatus(tasks, attentionOf([[1, ["finished"]]]), runStatusOf([[2, "running"]]))).toBe("running");
    expect(aggregateStatus(tasks, attentionOf([[1, ["finished"]]]), new Map())).toBe("success");
  });

  it("ignores tasks outside the bucket it was given", () => {
    expect(aggregateStatus([task(1)], attentionOf([[2, ["error"]]]), runStatusOf([[2, "error"]]))).toBeNull();
  });
});

describe("workspaceTasks", () => {
  it("collects every task under a workspace, across its spaces and its ungrouped bucket", () => {
    const tree = buildWorkspaceTree(WORKSPACE, [SPACE], [task(1), task(2, 10)]);
    expect(workspaceTasks(tree).map((t) => t.ID).sort()).toEqual([1, 2]);
  });
});
