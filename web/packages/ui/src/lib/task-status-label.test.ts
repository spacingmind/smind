import { describe, expect, it } from "vitest";

import { describeTaskStatus } from "@/lib/task-status-label";
import type { TaskGit, TaskGitPr } from "@/lib/types";

const pr = (over: Partial<TaskGitPr>): TaskGitPr => ({ number: 7, state: "open", draft: false, ...over }) as TaskGitPr;
const git = (over: Partial<TaskGit>): TaskGit => ({ taskId: 1, untrusted: false, computedAt: "", ...over });
const d = (over: Partial<Parameters<typeof describeTaskStatus>[0]>) =>
  describeTaskStatus({ runState: undefined, lifecycle: "created", ...over });

describe("describeTaskStatus", () => {
  it("running wins over everything", () => {
    expect(d({ runState: "running", git: git({ merged: { state: "merged", via: "" } }) })).toBe("Running");
  });
  it("error: Failed, or Rate limited when limited", () => {
    expect(d({ runState: "error" })).toBe("Failed");
    expect(d({ runState: "error", limited: true })).toBe("Rate limited");
    expect(d({ runState: "error", git: git({ pr: pr({ state: "merged" }) }) })).toBe("Failed");
  });
  it("merged via git.merged or pr.state, beating PR state and done", () => {
    expect(d({ runState: "done", git: git({ merged: { state: "merged", via: "" } }) })).toBe("Merged");
    expect(d({ runState: "done", git: git({ pr: pr({ state: "merged" }) }) })).toBe("Merged");
    expect(d({ git: git({ merged: { state: "unmerged", via: "" }, pr: pr({ state: "open" }) }) })).toBe("PR #7 open");
  });
  it("PR states", () => {
    expect(d({ runState: "done", git: git({ pr: pr({ state: "open" }) }) })).toBe("PR #7 open");
    expect(d({ git: git({ pr: pr({ state: "open", draft: true }) }) })).toBe("Draft PR #7");
    expect(d({ runState: "done", git: git({ pr: pr({ state: "closed" }) }) })).toBe("PR #7 closed");
  });
  it("done / stopped", () => {
    expect(d({ runState: "done" })).toBe("Done");
    expect(d({ runState: "stopped" })).toBe("Stopped");
    expect(d({ runState: "done", git: git({ diff: { files: 1, insertions: 1, deletions: 0 } }) })).toBe("Done");
  });
  it("falls back to the lifecycle value", () => {
    expect(d({})).toBe("created");
    expect(d({ lifecycle: "overridden", git: git({}) })).toBe("overridden");
  });
});
