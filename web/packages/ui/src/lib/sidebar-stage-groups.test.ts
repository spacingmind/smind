import { describe, expect, it } from "vitest";

import { groupTasksByStage, stageForTask, type StageKey, type StageSignals } from "@/lib/sidebar-stage-groups";
import type { AttentionReason, TaskAttention, TaskRunStatus } from "@/hooks/use-task-attention";
import type { Task, TaskGit, TaskGitPr } from "@/lib/types";

function task(id: number, updatedAt = "2024-01-01T00:00:00Z"): Task {
  return {
    ID: id,
    WorkspaceID: 1,
    SpaceID: null,
    Title: `Task ${id}`,
    Status: "active",
    WorktreePath: null,
    Branch: null,
    CreatedAt: "2024-01-01T00:00:00Z",
    UpdatedAt: updatedAt,
    ArchivedAt: null,
  };
}

function git(patch: Partial<TaskGit> = {}): TaskGit {
  return { taskId: 1, untrusted: false, computedAt: "2024-01-01T00:00:00Z", ...patch };
}

function pr(state: TaskGitPr["state"], draft = false): TaskGitPr {
  return { number: 1, repo: "o/r", url: "", title: "", state, draft, headRef: "", headSha: "", baseRef: "", fromFork: false, mergeable: "unknown", reviewDecision: "none", fetchedAt: "", stale: "", refreshAvailableAt: "" };
}

function signals(opts: { run?: "running" | "error" | "done" | "stopped"; attention?: AttentionReason[]; git?: TaskGit } = {}): StageSignals {
  const attention: TaskAttention = new Map(opts.attention ? [[1, new Set(opts.attention)]] : []);
  const runStatus: TaskRunStatus = new Map(opts.run ? [[1, opts.run]] : []);
  const gitByTask = new Map<number, TaskGit>(opts.git ? [[1, opts.git]] : []);
  return { attention, runStatus, gitByTask };
}

describe("stageForTask", () => {
  const cases: [string, StageSignals, StageKey][] = [
    ["running", signals({ run: "running" }), "working"],
    ["permission-waiting", signals({ attention: ["permission"], git: git() }), "working"],
    ["running beats merged", signals({ run: "running", git: git({ merged: { state: "merged", via: "x" } }) }), "working"],
    ["done run + dirty worktree", signals({ run: "done", git: git({ worktree: { dirty: true, staged: 0, unstaged: 1, untracked: 0 } }) }), "ready"],
    ["done run + commits ahead", signals({ run: "done", git: git({ commits: { aheadOfBase: 2 } }) }), "ready"],
    ["open PR", signals({ git: git({ pr: pr("open") }) }), "ready"],
    ["draft PR", signals({ git: git({ pr: pr("open", true) }) }), "ready"],
    ["merged PR", signals({ git: git({ pr: pr("merged"), commits: { aheadOfBase: 3 } }) }), "done"],
    ["merged.state merged", signals({ git: git({ merged: { state: "merged", via: "ancestor" } }) }), "done"],
    ["merged.state unmerged, nothing else", signals({ git: git({ merged: { state: "unmerged", via: "" } }) }), "idle"],
    ["closed PR, nothing else", signals({ git: git({ pr: pr("closed") }) }), "idle"],
    ["failed last run + ahead commits", signals({ run: "error", git: git({ commits: { aheadOfBase: 1 } }) }), "ready"],
    ["failed last run + nothing to review", signals({ run: "error", git: git({ worktree: { dirty: false, staged: 0, unstaged: 0, untracked: 0 }, commits: { aheadOfBase: 0 } }) }), "idle"],
    ["no task.git, running", signals({ run: "running" }), "working"],
    ["no task.git, done run", signals({ run: "done" }), "idle"],
    ["no task.git, nothing", signals(), "idle"],
  ];
  it.each(cases)("%s", (_name, s, want) => {
    expect(stageForTask(1, s)).toBe(want);
  });

  it("a run starting moves a ready task to working", () => {
    const g = git({ commits: { aheadOfBase: 1 } });
    expect(stageForTask(1, signals({ run: "done", git: g }))).toBe("ready");
    expect(stageForTask(1, signals({ run: "running", git: g }))).toBe("working");
  });

  it("the PR merging moves a task to done", () => {
    expect(stageForTask(1, signals({ git: git({ pr: pr("open") }) }))).toBe("ready");
    expect(stageForTask(1, signals({ git: git({ pr: pr("merged") }) }))).toBe("done");
  });
});

describe("groupTasksByStage", () => {
  it("orders groups working, ready, done, idle and drops empty ones", () => {
    const tasks = [task(1), task(2), task(3), task(4)];
    const gitByTask = new Map<number, TaskGit>([
      [2, git({ taskId: 2, commits: { aheadOfBase: 1 } })],
      [3, git({ taskId: 3, merged: { state: "merged", via: "x" } })],
    ]);
    const groups = groupTasksByStage(tasks, { attention: new Map(), runStatus: new Map([[1, "running"]]), gitByTask });
    expect(groups.map((g) => [g.key, g.label, g.tasks.map((t) => t.ID)])).toEqual([
      ["working", "Working", [1]],
      ["ready", "Ready to review", [2]],
      ["done", "Done", [3]],
      ["idle", "Idle", [4]],
    ]);
  });

  it("omits groups with no tasks and handles an empty list", () => {
    expect(groupTasksByStage([task(1)], signals()).map((g) => g.key)).toEqual(["idle"]);
    expect(groupTasksByStage([], signals())).toEqual([]);
  });

  it("sorts each group by UpdatedAt descending, ties by id descending, unparsable dates last", () => {
    const tasks = [
      task(1, "2024-01-01T00:00:00Z"),
      task(2, "2024-03-01T00:00:00Z"),
      task(3, "not-a-date"),
      task(4, "2024-03-01T00:00:00Z"),
      task(5, "2024-02-01T00:00:00Z"),
    ];
    const [group] = groupTasksByStage(tasks, signals());
    expect(group.tasks.map((t) => t.ID)).toEqual([4, 2, 5, 1, 3]);
  });
});
