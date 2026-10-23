import { describe, expect, it, vi } from "vitest";

import type { AccountUsage, UsageQueryData } from "@/lib/account-usage/types";
import { applyDaemonEvent } from "@/lib/query/event-bridge";
import { createQueryClient } from "@/lib/query/query-client";
import { queryKeys } from "@/lib/query-keys";
import type { Account, AgentProfile } from "@/lib/types";

const account = (id: number, label: string): Account => ({
  id,
  provider: "anthropic",
  label,
  credentialType: "oauth",
  createdAt: "",
  updatedAt: "",
});
const profile = (id: number, name: string): AgentProfile => ({
  ID: id,
  Name: name,
  Provider: "claude-native",
  PermissionMode: "",
  AutoAccept: false,
  ThinkingLevel: "",
  Notes: "",
  CreatedAt: "",
  UpdatedAt: "",
});

const usage = (accountId: number, displayName: string): AccountUsage => ({
  accountId,
  key: `anthropic-${accountId}`,
  provider: "anthropic",
  displayName,
  credentialType: "oauth",
  stale: false,
  nextRefreshAt: null,
  refreshBlocked: null,
  inFlight: false,
  lastError: null,
  report: { status: "available", fetchedAt: "2026-10-18T10:00:00Z" },
});
const usageData = (reports: AccountUsage[]): UsageQueryData => ({ supportsUsage: true, enabled: true, ttlSeconds: 300, reports });

describe("applyDaemonEvent", () => {
  it("a terminal run.status invalidates that task's path index and the shared run list only", () => {
    const qc = createQueryClient();
    const invalidate = vi.spyOn(qc, "invalidateQueries");
    applyDaemonEvent(qc, "run.status", { runId: "r", taskId: 4, chatId: 1, status: "running" });
    expect(invalidate).not.toHaveBeenCalled();
    applyDaemonEvent(qc, "run.status", { runId: "r", taskId: 4, chatId: 1, status: "done" });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: queryKeys.taskSearchIndex(4) });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: queryKeys.runList() });
    expect(invalidate).toHaveBeenCalledTimes(2);
  });

  it("account.updated upserts the account list and re-reads the cached usage list", () => {
    const qc = createQueryClient();
    const invalidate = vi.spyOn(qc, "invalidateQueries");
    qc.setQueryData(queryKeys.accountList(), [account(1, "Work")]);
    applyDaemonEvent(qc, "account.updated", { account: account(1, "Renamed") });
    applyDaemonEvent(qc, "account.updated", { account: account(2, "New") });
    expect(qc.getQueryData<Account[]>(queryKeys.accountList())?.map((a) => a.label)).toEqual(["Renamed", "New"]);
    expect(invalidate).toHaveBeenCalledWith({ queryKey: queryKeys.accountUsage() });
  });

  it("account.removed drops the account from the list and the usage reports", () => {
    const qc = createQueryClient();
    qc.setQueryData(queryKeys.accountList(), [account(1, "A"), account(2, "B")]);
    qc.setQueryData(queryKeys.accountUsage(), usageData([usage(1, "A"), usage(2, "B")]));
    applyDaemonEvent(qc, "account.removed", { id: 1 });
    expect(qc.getQueryData<Account[]>(queryKeys.accountList())?.map((a) => a.id)).toEqual([2]);
    expect(qc.getQueryData<UsageQueryData>(queryKeys.accountUsage())?.reports.map((r) => r.accountId)).toEqual([2]);
  });

  it("account.usage upserts a known account's report", () => {
    const qc = createQueryClient();
    qc.setQueryData(queryKeys.accountUsage(), usageData([usage(1, "A")]));
    applyDaemonEvent(qc, "account.usage", { usage: usage(1, "A renamed") });
    expect(qc.getQueryData<UsageQueryData>(queryKeys.accountUsage())?.reports.map((r) => r.displayName)).toEqual(["A renamed"]);
  });

  it("account.usage for a new account is kept once the account list has it", () => {
    const qc = createQueryClient();
    qc.setQueryData(queryKeys.accountUsage(), usageData([usage(1, "A")]));
    qc.setQueryData(queryKeys.accountList(), [account(1, "A"), account(2, "Fresh")]);
    applyDaemonEvent(qc, "account.usage", { usage: usage(2, "Fresh") });
    expect(qc.getQueryData<UsageQueryData>(queryKeys.accountUsage())?.reports.map((r) => r.displayName)).toEqual(["A", "Fresh"]);
  });

  it("account.usage after an account was removed does not resurrect it", () => {
    const qc = createQueryClient();
    const invalidate = vi.spyOn(qc, "invalidateQueries");
    qc.setQueryData(queryKeys.accountList(), [account(1, "A"), account(2, "B")]);
    qc.setQueryData(queryKeys.accountUsage(), usageData([usage(1, "A"), usage(2, "B")]));
    applyDaemonEvent(qc, "account.removed", { id: 2 });
    applyDaemonEvent(qc, "account.usage", { usage: usage(2, "B") });
    expect(qc.getQueryData<UsageQueryData>(queryKeys.accountUsage())?.reports.map((r) => r.accountId)).toEqual([1]);
    // Settled by one more *cached* read, never a provider call.
    expect(invalidate).toHaveBeenCalledWith({ queryKey: queryKeys.accountUsage() });
  });

  it("profile.created/updated upsert the cached list; deleted filters it", () => {
    const qc = createQueryClient();
    qc.setQueryData(queryKeys.profileList(), [profile(1, "Quick")]);
    applyDaemonEvent(qc, "profile.created", { profile: profile(2, "New") });
    applyDaemonEvent(qc, "profile.updated", { profile: profile(1, "Renamed") });
    expect(qc.getQueryData<AgentProfile[]>(queryKeys.profileList())?.map((p) => p.Name)).toEqual(["Renamed", "New"]);
    applyDaemonEvent(qc, "profile.deleted", { id: 2 });
    expect(qc.getQueryData<AgentProfile[]>(queryKeys.profileList())?.map((p) => p.ID)).toEqual([1]);
  });

  it("profile events with a malformed payload or no cached list invalidate instead", () => {
    const qc = createQueryClient();
    const invalidate = vi.spyOn(qc, "invalidateQueries");
    applyDaemonEvent(qc, "profile.created", { profile: profile(2, "New") });
    applyDaemonEvent(qc, "profile.updated", {});
    applyDaemonEvent(qc, "profile.deleted", {});
    expect(qc.getQueryData(queryKeys.profileList())).toBeUndefined();
    expect(invalidate).toHaveBeenCalledWith({ queryKey: queryKeys.profileList() });
  });

  it("provider.updated invalidates the provider list", () => {
    const qc = createQueryClient();
    const invalidate = vi.spyOn(qc, "invalidateQueries");
    applyDaemonEvent(qc, "provider.updated", { id: "claude-native", defaultModel: "opus" });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: queryKeys.providerList() });
  });

  it("does not create cache entries nobody asked for", () => {
    const qc = createQueryClient();
    applyDaemonEvent(qc, "account.usage", { usage: usage(2, "Fresh") });
    applyDaemonEvent(qc, "account.removed", { id: 2 });
    expect(qc.getQueryData(queryKeys.accountUsage())).toBeUndefined();
    expect(qc.getQueryData(queryKeys.accountList())).toBeUndefined();
  });

  it("profile.updated for a profile the cache does not hold re-reads instead of resurrecting it", () => {
    const qc = createQueryClient();
    const invalidate = vi.spyOn(qc, "invalidateQueries");
    qc.setQueryData(queryKeys.profileList(), [{ ID: 1, Name: "A" }]);
    applyDaemonEvent(qc, "profile.updated", { profile: { ID: 9, Name: "Ghost" } });
    expect(qc.getQueryData<{ ID: number }[]>(queryKeys.profileList())?.map((p) => p.ID)).toEqual([1]);
    expect(invalidate).toHaveBeenCalledWith({ queryKey: queryKeys.profileList() });
    // A *created* profile is upserted.
    applyDaemonEvent(qc, "profile.created", { profile: { ID: 2, Name: "B" } });
    expect(qc.getQueryData<{ ID: number }[]>(queryKeys.profileList())?.map((p) => p.ID)).toEqual([1, 2]);
  });

  it("account.updated / account.removed drop the provider's cached health answer", () => {
    const qc = createQueryClient();
    qc.setQueryData(queryKeys.providerTest("anthropic"), { ok: false, detail: "old" });
    qc.setQueryData(queryKeys.providerTest("openai"), { ok: true, detail: "ok" });
    qc.setQueryData(queryKeys.accountList(), [account(1, "A"), { ...account(2, "B"), provider: "openai" }]);
    applyDaemonEvent(qc, "account.updated", { account: account(1, "A") });
    expect(qc.getQueryData(queryKeys.providerTest("anthropic"))).toBeUndefined();
    expect(qc.getQueryData(queryKeys.providerTest("openai"))).toBeDefined();
    applyDaemonEvent(qc, "account.removed", { id: 2 });
    expect(qc.getQueryData(queryKeys.providerTest("openai"))).toBeUndefined();
  });

  describe("workspace tree (ADR 0009 lifecycle topics)", () => {
    const ws = { ID: 1, Path: "/a", Title: "A", RoutingPolicy: "", CreatedAt: "", UpdatedAt: "" };
    const task = (id: number, title: string) =>
      ({ ID: id, WorkspaceID: 1, SpaceID: null, Title: title, Status: "idle", WorktreePath: null, Branch: null, CreatedAt: "", UpdatedAt: "", ArchivedAt: null }) as never;
    const tree = () => [{ ...ws, spaces: [], ungroupedTasks: [task(1, "one")] }];

    it("lifecycle events splice into the cached tree with no refetch", () => {
      const qc = createQueryClient();
      const invalidate = vi.spyOn(qc, "invalidateQueries");
      qc.setQueryData(queryKeys.workspaceTree(), tree());
      applyDaemonEvent(qc, "task.created", { task: task(2, "two") });
      const held = qc.getQueryData<{ ungroupedTasks: { ID: number }[] }[]>(queryKeys.workspaceTree());
      expect(held?.[0]?.ungroupedTasks.map((t) => t.ID)).toEqual([1, 2]);
      applyDaemonEvent(qc, "task.archived", { task: task(1, "one") });
      expect(qc.getQueryData<{ ungroupedTasks: { ID: number }[] }[]>(queryKeys.workspaceTree())?.[0]?.ungroupedTasks.map((t) => t.ID)).toEqual([2]);
      applyDaemonEvent(qc, "workspace.deleted", { id: 1 });
      expect(qc.getQueryData(queryKeys.workspaceTree())).toEqual([]);
      expect(invalidate).not.toHaveBeenCalledWith({ queryKey: queryKeys.workspaceTree() });
    });

    it("an event before the first fetch is dropped (the fetch reads committed state), and creates no entry", () => {
      const qc = createQueryClient();
      applyDaemonEvent(qc, "task.created", { task: task(2, "two") });
      expect(qc.getQueryData(queryKeys.workspaceTree())).toBeUndefined();
    });

    it("event.dropped re-reads the tree (a dropped event may have been a create or a delete)", () => {
      const qc = createQueryClient();
      const invalidate = vi.spyOn(qc, "invalidateQueries");
      applyDaemonEvent(qc, "event.dropped", {});
      expect(invalidate).toHaveBeenCalledWith({ queryKey: queryKeys.workspaceTree() });
    });
  });

  describe("learnedSkill.probe.*", () => {
    const topics = ["learnedSkill.probe.started", "learnedSkill.probe.arm", "learnedSkill.probe.finished", "learnedSkill.probe.failed"];

    it.each(topics)("%s invalidates that proposal's probe, estimate and its workspace's list only", (topic) => {
      const qc = createQueryClient();
      const invalidate = vi.spyOn(qc, "invalidateQueries");
      applyDaemonEvent(qc, topic, { probeId: 5, proposalId: 9, workspaceId: 1, status: "running" });
      expect(invalidate.mock.calls.map((c) => c[0]?.queryKey)).toEqual([
        queryKeys.probe(9),
        queryKeys.probeEstimateOf(9),
        queryKeys.probesOfWorkspace(1),
      ]);
    });

    it("falls back to every probe entry when the payload names nothing", () => {
      const qc = createQueryClient();
      const invalidate = vi.spyOn(qc, "invalidateQueries");
      applyDaemonEvent(qc, "learnedSkill.probe.arm", null);
      expect(invalidate.mock.calls.map((c) => c[0]?.queryKey)).toEqual([
        queryKeys.probeAll(),
        queryKeys.probeEstimateAll(),
        queryKeys.probesAll(),
      ]);
    });
  });
});
