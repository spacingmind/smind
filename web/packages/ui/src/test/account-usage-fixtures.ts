import type {
  AccountUsage,
  AccountUsageResult,
  UsageProblemKind,
  UsageReport,
  UsageWindow,
} from "@/lib/account-usage/types";

/** A fixed clock for tests that format relative times. */
export const FIXTURE_NOW = Date.parse("2026-10-12T12:00:00Z");

const HOUR = 3_600_000;

function iso(ms: number): string {
  return new Date(ms).toISOString();
}

function summaryWindows(now: number, fiveHour: number, weekly: number): UsageWindow[] {
  return [
    {
      id: "five_hour",
      label: "5-hour",
      shortLabel: "5h",
      summary: true,
      usedPct: fiveHour,
      remainingPct: 100 - fiveHour,
      resetsAt: iso(now + 2 * HOUR + 10 * 60_000),
      durationSeconds: 5 * 3600,
    },
    {
      id: "weekly",
      label: "Weekly",
      shortLabel: "wk",
      summary: true,
      usedPct: weekly,
      remainingPct: 100 - weekly,
      resetsAt: iso(now + 3 * 24 * HOUR + HOUR),
      durationSeconds: 7 * 24 * 3600,
    },
  ];
}

function availableReport(now: number, planLabel: string, fiveHour: number, weekly: number): UsageReport {
  return {
    v: 2,
    status: "available",
    planLabel,
    windows: summaryWindows(now, fiveHour, weekly),
    fetchedAt: iso(now - 3 * 60_000),
  };
}

/** Claude account 1: 31% 5-hour, 54% weekly, plan "max". */
export function claudeAccount(overrides: Partial<AccountUsage> = {}, now: number = FIXTURE_NOW): AccountUsage {
  return {
    accountId: 1,
    key: "anthropic-1",
    provider: "anthropic",
    displayName: "Claude",
    credentialType: "oauth",
    stale: false,
    nextRefreshAt: null,
    refreshBlocked: null,
    inFlight: false,
    lastError: null,
    report: availableReport(now, "max", 31, 54),
    ...overrides,
  };
}

/** Codex account 2: 7% 5-hour, 12% weekly, plan "plus". */
export function codexAccount(overrides: Partial<AccountUsage> = {}, now: number = FIXTURE_NOW): AccountUsage {
  return {
    accountId: 2,
    key: "openai-2",
    provider: "openai",
    displayName: "Codex",
    credentialType: "oauth",
    stale: false,
    nextRefreshAt: null,
    refreshBlocked: null,
    inFlight: false,
    lastError: null,
    report: availableReport(now, "plus", 7, 12),
    ...overrides,
  };
}

/** An account whose report explains why there are no windows. */
export function problemAccount(
  kind: UsageProblemKind | "error",
  overrides: Partial<AccountUsage> = {},
  now: number = FIXTURE_NOW,
): AccountUsage {
  const base = claudeAccount({}, now);
  const fetchedAt = iso(now - 60_000);
  switch (kind) {
    case "expired":
      return {
        ...base,
        report: { status: "unavailable", problem: { kind: "expired", expiresAt: iso(now - 2 * HOUR) }, fetchedAt },
        ...overrides,
      };
    case "rejected":
      return {
        ...base,
        report: { status: "unavailable", problem: { kind: "rejected", status: 401 }, fetchedAt },
        ...overrides,
      };
    case "no_quota":
      return { ...base, report: { status: "unavailable", problem: { kind: "no_quota" }, fetchedAt }, ...overrides };
    case "error":
      return {
        ...base,
        report: { status: "error", error: "timeout", fetchedAt },
        nextRefreshAt: iso(now + 2 * 60_000),
        ...overrides,
      };
  }
}

/** An `account.usage` RPC result. */
export function usageResult(
  reports: AccountUsage[],
  { enabled = true, ttlSeconds = 300 }: { enabled?: boolean; ttlSeconds?: number } = {},
): AccountUsageResult {
  return { enabled, ttlSeconds, reports };
}
