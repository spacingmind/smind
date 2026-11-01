import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";
import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it } from "vitest";

import { queryKeys } from "@/lib/query-keys";

const SRC = join(__dirname, "..");

function sourceFiles(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) return sourceFiles(path);
    return /\.(ts|tsx)$/.test(name) && !/\.test\.(ts|tsx)$/.test(name) ? [path] : [];
  });
}

describe("query keys", () => {
  it("query keys only come from query-keys.ts", () => {
    const offenders = sourceFiles(SRC)
      .filter((path) => relative(SRC, path) !== join("lib", "query-keys.ts"))
      .filter((path) => /queryKey\s*:\s*\[/.test(readFileSync(path, "utf8")))
      .map((path) => relative(SRC, path));
    expect(offenders).toEqual([]);
  });

  it("is the RPC name plus its params", () => {
    expect(queryKeys.accountList()).toEqual(["account.list"]);
    expect(queryKeys.taskSearchIndex(7)).toEqual(["task.searchIndex", { taskId: 7 }]);
    expect(queryKeys.forgeStatus()).toEqual(["forge.status"]);
    expect(queryKeys.forgeSearch({ taskId: 7 }, "pr", "fix")).toEqual(["forge.search", { taskId: 7, kind: "pr", query: "fix" }]);
    expect(queryKeys.forgeSearch({ workspaceId: 3 }, "issue", "")).toEqual(["forge.search", { workspaceId: 3, kind: "issue", query: "" }]);
    expect(queryKeys.workspaceBranches(3, "fe", 30)).toEqual(["workspace.branches", { workspaceId: 3, query: "fe", limit: 30 }]);
    expect(queryKeys.accountUsage()).toEqual(["account.usage", { cached: true }]);
    expect(queryKeys.providerTest("anthropic")).toEqual(["provider.test", "anthropic"]);
  });

  it("archive keys are the RPC name plus its params, and the partial key prefixes the full one", () => {
    expect(queryKeys.archivePreview(7)).toEqual(["task.archivePreview", { taskId: 7 }]);
    expect(queryKeys.archivedTasks(3)).toEqual(["task.list", { workspaceId: 3, archived: true }]);
    const qc = new QueryClient();
    qc.setQueryData(queryKeys.archivedTasks(3), []);
    qc.setQueryData(queryKeys.archivedTasks(4), []);
    void qc.invalidateQueries({ queryKey: queryKeys.archivedTasksAll() });
    expect(qc.getQueryState(queryKeys.archivedTasks(3))?.isInvalidated).toBe(true);
    expect(qc.getQueryState(queryKeys.archivedTasks(4))?.isInvalidated).toBe(true);
  });

  it("attachment keys are the RPC name plus its params", () => {
    expect(queryKeys.daemonInfo()).toEqual(["daemon.info"]);
    expect(queryKeys.runAttachment("run_1", "2")).toEqual(["run.attachment", { runId: "run_1", id: "2" }]);
  });

  it("learning keys are the RPC name plus its params, and the partial keys prefix the full ones", () => {
    expect(queryKeys.learningStatus()).toEqual(["learning.status"]);
    expect(queryKeys.learnedSkills(3, ["active"])).toEqual([
      "learning.skills.list",
      { workspaceId: 3, statuses: ["active"], includeBody: false },
    ]);
    expect(queryKeys.learnedSkill(9)).toEqual(["learning.skills.get", { id: 9 }]);
    expect(queryKeys.proposals(3, "pending")).toEqual(["learning.proposals.list", { workspaceId: 3, status: "pending" }]);
    expect(queryKeys.proposal(4)).toEqual(["learning.proposals.get", { id: 4 }]);
    expect(queryKeys.learningJobs(3, 10)).toEqual(["learning.jobs.list", { workspaceId: 3, limit: 10 }]);
    // A partial key must match the full keys of its workspace (invalidateQueries' partial matching).
    const qc = new QueryClient();
    qc.setQueryData(queryKeys.learnedSkills(3, ["active"]), []);
    qc.setQueryData(queryKeys.learnedSkills(4, ["active"]), []);
    expect(qc.getQueryCache().findAll({ queryKey: queryKeys.learnedSkillsOf(3) })).toHaveLength(1);
    expect(qc.getQueryCache().findAll({ queryKey: queryKeys.learnedSkillsAll() })).toHaveLength(2);
  });

  it("probe keys are the RPC name plus its params, and the partial keys prefix the full ones", () => {
    expect(queryKeys.probeEstimate(9, true)).toEqual(["learning.probe.estimate", { id: 9, quick: true }]);
    expect(queryKeys.probe(9)).toEqual(["learning.probe.get", { id: 9 }]);
    expect(queryKeys.probesOf(1, 5)).toEqual(["learning.probe.list", { workspaceId: 1, limit: 5 }]);
    const qc = new QueryClient();
    qc.setQueryData(queryKeys.probeEstimate(9, true), 1);
    qc.setQueryData(queryKeys.probeEstimate(9, false), 1);
    qc.setQueryData(queryKeys.probeEstimate(8, false), 1);
    qc.setQueryData(queryKeys.probesOf(1, 5), 1);
    qc.setQueryData(queryKeys.probesOf(2), 1);
    expect(qc.getQueryCache().findAll({ queryKey: queryKeys.probeEstimateOf(9) })).toHaveLength(2);
    expect(qc.getQueryCache().findAll({ queryKey: queryKeys.probesOfWorkspace(1) })).toHaveLength(1);
    expect(qc.getQueryCache().findAll({ queryKey: queryKeys.probesAll() })).toHaveLength(2);
  });

  it("steward and policy keys are the RPC name plus its params, and the steward prefix matches every review", () => {
    expect(queryKeys.stewardReview(7)).toEqual(["steward", "review", 7]);
    expect(queryKeys.stewardAll()).toEqual(["steward"]);
    expect(queryKeys.policyStatus()).toEqual(["policy", "get"]);
    expect(queryKeys.policyAudit()).toEqual(["policy", "audit"]);
    // `steward.updated` invalidates every cached review without touching the policy reads.
    const qc = new QueryClient();
    qc.setQueryData(queryKeys.stewardReview(7), null);
    qc.setQueryData(queryKeys.stewardReview(8), null);
    qc.setQueryData(queryKeys.policyStatus(), null);
    void qc.invalidateQueries({ queryKey: queryKeys.stewardAll() });
    expect(qc.getQueryState(queryKeys.stewardReview(7))?.isInvalidated).toBe(true);
    expect(qc.getQueryState(queryKeys.stewardReview(8))?.isInvalidated).toBe(true);
    expect(qc.getQueryState(queryKeys.policyStatus())?.isInvalidated).toBe(false);
  });

  it("steward frame key is outside the steward prefix", () => {
    // A frame is immutable per review: a steward.updated invalidation of stewardAll() must not re-read its bytes.
    expect(queryKeys.stewardFrame(1, "a.png")).toEqual(["steward.frame", 1, "a.png"]);
    expect(queryKeys.stewardFrame(1, "a.png")[0]).not.toBe(queryKeys.stewardAll()[0]);
  });
});
