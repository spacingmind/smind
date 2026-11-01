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
  task: [
    row("1", 4, 182_400, 21_300, 940_000, 3.42, ["claude-code"]),
    row("2", 3, 96_100, 11_800, 512_000, 1.87, ["claude-code"]),
    row("3", 2, 41_000, 4_900, 120_000, 0.74, ["claude-code"]),
    row("4", 3, 120_500, 15_200, 633_000, 2.31, ["claude-code"]),
    row("5", 2, 58_200, 7_100, 301_000, 1.12, ["claude-code"]),
    row("6", 1, 22_000, 3_400, 88_000, 0.38, ["claude-code"]),
  ],
  workspace: [row("1", 9, 319_500, 38_000, 1_572_000, 6.03, ["claude-code"]), row("2", 5, 178_700, 22_300, 934_000, 3.43, ["claude-code"]), row("3", 1, 22_000, 3_400, 88_000, 0.38, ["claude-code"])],
  provider: [row("claude-code", 15, 520_200, 63_700, 2_594_000, 9.84, ["claude-code"])],
  model: [row("claude-sonnet-5-5", 12, 410_000, 50_100, 2_050_000, 6.1, ["claude-code"]), row("claude-haiku-5-5", 3, 110_200, 13_600, 544_000, 3.74, ["claude-code"])],
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
