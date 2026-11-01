import type { AccountUsage, AccountUsageResult } from "@/lib/account-usage/types";
import type { ProviderUsageResult } from "@/lib/account-usage/provider-usage-types";
import type { ProviderInfo, UsageGroupBy, UsageSummaryRow } from "@/lib/types";

import type { UsageFixtures } from "../types";

const HOUR = 3_600_000;

function row(key: string, count: number, input: number, output: number, cacheRead: number, cost: number, providers: string[]): UsageSummaryRow {
  return { scope: "runs", key, count, inputTokens: input, outputTokens: output, cacheReadTokens: cacheRead, cacheWriteTokens: Math.round(input / 6), reasoningTokens: 0, costUsd: cost, providers };
}

/** `usage.summary` grouped as the wire groups it: the key is the task/workspace id, the provider id or the model. */
const SUMMARY: Record<UsageGroupBy, UsageSummaryRow[]> = {
  // keys are task ids; the costs are the landing hero's run footers ($0.03, $0.02, $0.05, $0.04, $0.09, $0.01, $0.31 and the two Peers)
  task: [
    row("1", 1, 48_200, 5_100, 190_000, 0.05, ["claude-code"]),
    row("2", 1, 30_000, 2_600, 120_000, 0.03, ["claude-code"]),
    row("3", 1, 22_000, 1_900, 88_000, 0.02, ["claude-code"]),
    row("4", 1, 38_000, 3_200, 152_000, 0.04, ["claude-code"]),
    row("5", 1, 70_000, 6_100, 280_000, 0.09, ["claude-code"]),
    row("6", 1, 9_000, 900, 36_000, 0.01, ["claude-code"]),
    row("12", 1, 120_000, 9_800, 480_000, 0.31, ["claude-code"]),
    row("13", 1, 18_000, 1_500, 72_000, 0.05, ["claude-code"]),
    row("14", 1, 20_000, 1_700, 80_000, 0.06, ["claude-code"]),
  ],
  workspace: [
    row("1", 5, 208_200, 18_900, 830_000, 0.23, ["claude-code"]),
    row("2", 1, 9_000, 900, 36_000, 0.01, ["claude-code"]),
    row("3", 3, 158_000, 13_000, 632_000, 0.42, ["claude-code"]),
  ],
  provider: [row("claude-code", 9, 375_200, 32_800, 1_500_000, 0.66, ["claude-code"])],
  model: [row("claude-sonnet-5-5", 9, 375_200, 32_800, 1_500_000, 0.66, ["claude-code"])],
};

function account(now: number): AccountUsage {
  const iso = (ms: number) => new Date(ms).toISOString();
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
    report: {
      v: 2,
      status: "available",
      planLabel: "pro",
      fetchedAt: iso(now - 2 * 60_000),
      windows: [
        { id: "five_hour", label: "5-hour", shortLabel: "5h", summary: true, usedPct: 62, remainingPct: 38, resetsAt: iso(now + 2 * HOUR + 14 * 60_000), durationSeconds: 5 * 3600 },
        { id: "weekly", label: "Weekly", shortLabel: "wk", summary: true, usedPct: 41, remainingPct: 59, resetsAt: iso(now + 3 * 24 * HOUR), durationSeconds: 7 * 24 * 3600 },
      ],
    },
  };
}

export function usageFixtures(now: number): UsageFixtures {
  const acct = account(now);
  const accountUsage: AccountUsageResult = { enabled: true, ttlSeconds: 300, reports: [acct] };
  // The plan windows come from the account (account.usage); a per-instance report on top would show the same account twice.
  const providerUsage: ProviderUsageResult = { enabled: true, ttlSeconds: 300, reports: [] };
  return { summary: SUMMARY, providerUsage, accountUsage };
}

export const PROVIDERS: { providers: ProviderInfo[] } = {
  providers: [
    {
      id: "claude-code",
      label: "Claude Code",
      runner: "claude-native",
      harness: "claude-code",
      vendor: "anthropic",
      kind: "cli",
      credentialKind: "oauth",
      accountProvider: "anthropic",
      thinking: "supported",
      modes: [
        { id: "default", label: "Default", description: "Ask before each edit or command" },
        { id: "acceptEdits", label: "Accept edits", description: "Edits are applied; commands still ask" },
        { id: "plan", label: "Plan", description: "Read-only planning" },
      ],
      defaultMode: "default",
      models: [
        { id: "claude-sonnet-5-5", label: "Sonnet 5.5", default: true },
        { id: "claude-haiku-5-5", label: "Haiku 5.5" },
      ],
      defaultModel: "claude-sonnet-5-5",
    },
  ],
};
