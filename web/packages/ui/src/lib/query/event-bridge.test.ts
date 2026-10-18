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

  it("does not create cache entries nobody asked for", () => {
    const qc = createQueryClient();
    applyDaemonEvent(qc, "account.usage", { usage: usage(2, "Fresh") });
    applyDaemonEvent(qc, "account.removed", { id: 2 });
    expect(qc.getQueryData(queryKeys.accountUsage())).toBeUndefined();
    expect(qc.getQueryData(queryKeys.accountList())).toBeUndefined();
  });
});
