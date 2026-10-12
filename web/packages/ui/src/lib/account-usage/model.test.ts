import { describe, expect, it } from "vitest";
import {
  displayPercent,
  formatUsageFreshness,
  replaceReport,
  resolveAccountFilter,
  resolveUsageRefresh,
  resolveUsageView,
  settleReports,
  upsertReport,
  usageWindowRowLabel,
} from "./model";
import type { AccountUsage, UsageWindow } from "./types";

const NOW = Date.parse("2026-10-01T12:00:00Z");

function entry(input: { id: number; plan?: string; fetchedAt?: string; noReport?: boolean }): AccountUsage {
  return {
    accountId: input.id,
    key: `anthropic-${input.id}`,
    provider: "anthropic",
    displayName: `Account ${input.id}`,
    credentialType: "oauth",
    stale: false,
    nextRefreshAt: null,
    refreshBlocked: null,
    inFlight: false,
    lastError: null,
    report: input.noReport
      ? null
      : {
          status: "available",
          planLabel: input.plan,
          fetchedAt: input.fetchedAt ?? "2026-01-01T00:00:00.000Z",
        },
  };
}

describe("displayPercent", () => {
  const window = (input: Partial<UsageWindow>): UsageWindow => ({ id: "w", label: "W", ...input });

  it("reads the share used or the share left", () => {
    expect(displayPercent(window({ usedPct: 31 }), "used")).toBe(31);
    expect(displayPercent(window({ usedPct: 31 }), "remaining")).toBe(69);
    expect(displayPercent(window({ remainingPct: 40 }), "used")).toBe(60);
    expect(displayPercent(window({ usedPct: 50, remainingPct: 45 }), "remaining")).toBe(45);
  });

  it("has no percent when the window reports none", () => {
    expect(displayPercent(window({}), "used")).toBeNull();
    expect(displayPercent(window({}), "remaining")).toBeNull();
  });
});

describe("usageWindowRowLabel", () => {
  it("names the window, its percent and its reset after what pinning it pins", () => {
    expect(
      usageWindowRowLabel({ pinLabel: "Pin Claude Session", value: "31%", trailing: "resets 2h" }),
    ).toBe("Pin Claude Session, 31% · resets 2h");
    expect(usageWindowRowLabel({ pinLabel: "Pin Claude Session", value: "31%", trailing: null })).toBe(
      "Pin Claude Session, 31%",
    );
  });
});

describe("formatUsageFreshness", () => {
  it("says when the report was fetched", () => {
    expect(formatUsageFreshness("2026-10-01T11:59:50Z", NOW)).toBe("Updated just now");
    expect(formatUsageFreshness("2026-10-01T11:57:00Z", NOW)).toBe("Updated 3m ago");
  });

  it("does not guess when the time is missing or unreadable", () => {
    expect(formatUsageFreshness(undefined, NOW)).toBe("Updated —");
    expect(formatUsageFreshness("garbage", NOW)).toBe("Updated —");
  });
});

describe("resolveUsageRefresh", () => {
  it("is pending while a refresh runs, even after an earlier failure", () => {
    expect(resolveUsageRefresh({ isPending: true, error: new Error("boom") })).toBe("pending");
  });

  it("is failed after a refresh errors", () => {
    expect(resolveUsageRefresh({ isPending: false, error: new Error("boom") })).toBe("failed");
  });

  it("is idle before and after a successful refresh", () => {
    expect(resolveUsageRefresh({ isPending: false, error: null })).toBe("idle");
  });
});

describe("replaceReport", () => {
  const alpha = entry({ id: 1, plan: "Old" });
  const beta = entry({ id: 2 });

  it("swaps only the refreshed account, in place", () => {
    const refreshed = entry({ id: 1, plan: "New" });
    expect(replaceReport([alpha, beta], 1, refreshed)).toEqual([refreshed, beta]);
  });

  it("drops an account the daemon no longer knows", () => {
    expect(replaceReport([alpha, beta], 1, null)).toEqual([beta]);
  });
});

describe("upsertReport", () => {
  const alpha = entry({ id: 1, plan: "Old" });
  const beta = entry({ id: 2 });

  it("starts a list from the first streamed report", () => {
    expect(upsertReport(undefined, alpha)).toEqual([alpha]);
  });

  it("appends an account the list does not have yet, in a new array", () => {
    const list = [alpha];
    const next = upsertReport(list, beta);
    expect(next).toEqual([alpha, beta]);
    expect(next).not.toBe(list);
    expect(list).toEqual([alpha]);
  });

  it("replaces an account the list already has, in place", () => {
    const streamed = entry({ id: 1, plan: "New" });
    expect(upsertReport([alpha, beta], streamed)).toEqual([streamed, beta]);
  });

  it("keeps a copy fetched after the streamed one, such as a Refresh that landed first", () => {
    const refreshed = entry({ id: 1, plan: "Refreshed", fetchedAt: "2026-01-01T00:05:00.000Z" });
    expect(upsertReport([refreshed, beta], alpha)).toEqual([refreshed, beta]);
  });

  it("never lets a copy with no report replace one that has a report", () => {
    const empty = entry({ id: 1, noReport: true });
    expect(upsertReport([alpha, beta], empty)).toEqual([alpha, beta]);
  });

  it("lets a report replace a copy that has none", () => {
    const empty = entry({ id: 1, noReport: true });
    expect(upsertReport([empty], alpha)).toEqual([alpha]);
  });
});

describe("settleReports", () => {
  const alpha = entry({ id: 1, plan: "Old" });
  const beta = entry({ id: 2 });

  it("takes the finished list, dropping accounts the daemon no longer has", () => {
    const streamed = entry({ id: 1, plan: "New" });
    expect(settleReports([alpha, beta], [streamed])).toEqual([streamed]);
  });

  it("keeps an account refreshed while the list streamed over its older copy", () => {
    const refreshed = entry({ id: 1, plan: "Refreshed", fetchedAt: "2026-01-01T00:05:00.000Z" });
    expect(settleReports([refreshed, beta], [alpha, beta])).toEqual([refreshed, beta]);
  });

  it("takes the finished list as is when nothing was shown yet", () => {
    expect(settleReports(undefined, [alpha, beta])).toEqual([alpha, beta]);
  });
});

describe("resolveUsageView", () => {
  const base = { supportsUsage: true, enabled: true, phase: "loading" as const, reports: [], error: null };

  it("asks for a daemon update before anything else", () => {
    expect(resolveUsageView({ ...base, supportsUsage: false, enabled: false, phase: "ready" })).toEqual({
      kind: "unavailable",
      message: "Update the daemon to see plan usage",
    });
  });

  it("says usage is turned off", () => {
    const view = resolveUsageView({ ...base, enabled: false, phase: "ready" });
    expect(view.kind).toBe("disabled");
    expect(view).toMatchObject({ message: expect.stringContaining("accountUsage.enabled") });
  });

  it("moves from loading to ready to error", () => {
    const reports = [entry({ id: 1 })];
    expect(resolveUsageView(base)).toEqual({ kind: "loading" });
    expect(resolveUsageView({ ...base, phase: "idle" })).toEqual({ kind: "loading" });
    expect(resolveUsageView({ ...base, phase: "ready", reports })).toEqual({ kind: "ready", reports });
    expect(resolveUsageView({ ...base, phase: "error", error: "boom" })).toEqual({
      kind: "error",
      message: "boom",
    });
    expect(resolveUsageView({ ...base, phase: "error" })).toEqual({
      kind: "error",
      message: "Unable to refresh usage",
    });
  });
});

describe("resolveAccountFilter", () => {
  const reports = [entry({ id: 1 }), entry({ id: 2 })];

  it("keeps the picked account while it exists", () => {
    expect(resolveAccountFilter("anthropic-2", reports)).toBe("anthropic-2");
  });

  it("shows All when nothing is picked", () => {
    expect(resolveAccountFilter(null, reports)).toBeNull();
  });

  it("falls back to All when the picked account is gone", () => {
    expect(resolveAccountFilter("anthropic-9", reports)).toBeNull();
    expect(resolveAccountFilter("anthropic-1", [])).toBeNull();
  });
});
