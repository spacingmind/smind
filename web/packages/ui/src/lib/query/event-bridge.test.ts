import { describe, expect, it, vi } from "vitest";

import type { AccountUsage, UsageQueryData } from "@/lib/account-usage/types";
import { applyDaemonEvent } from "@/lib/query/event-bridge";
import { createQueryClient } from "@/lib/query/query-client";
import { queryKeys } from "@/lib/query-keys";
import type { Account } from "@/lib/types";

const account = (id: number, label: string): Account => ({
  id,
  provider: "anthropic",
  label,
  credentialType: "oauth",
  createdAt: "",
  updatedAt: "",
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

  it("account.usage upserts a report, including for an account the cache has not seen", () => {
    const qc = createQueryClient();
    qc.setQueryData(queryKeys.accountUsage(), usageData([usage(1, "A")]));
    applyDaemonEvent(qc, "account.usage", { usage: usage(2, "Fresh") });
    expect(qc.getQueryData<UsageQueryData>(queryKeys.accountUsage())?.reports.map((r) => r.displayName)).toEqual(["A", "Fresh"]);
  });

  it("does not create cache entries nobody asked for", () => {
    const qc = createQueryClient();
    applyDaemonEvent(qc, "account.usage", { usage: usage(2, "Fresh") });
    applyDaemonEvent(qc, "account.removed", { id: 2 });
    expect(qc.getQueryData(queryKeys.accountUsage())).toBeUndefined();
    expect(qc.getQueryData(queryKeys.accountList())).toBeUndefined();
  });
});
