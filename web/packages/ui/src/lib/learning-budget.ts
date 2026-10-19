import type { LearningWorkspaceBudget } from "@/lib/learning-types";

export type BudgetLevel = "ok" | "warn" | "exhausted";

export interface BudgetState {
  /** The larger of the dollar and token fractions, clamped to 0..1. */
  usedFraction: number;
  level: BudgetLevel;
  costFraction: number;
  tokenFraction: number;
  /** "$0.04 of $0.25", or just "$0.04" when the cap is 0 (no limit). */
  costLabel: string;
  /** "18k of 150k tokens", or "18k tokens" when there is no cap. */
  tokensLabel: string;
}

/** Warn from this share of either cap; at 1 the daemon stops starting reflection jobs. */
export const BUDGET_WARN_FRACTION = 0.8;

function fraction(used: number, cap: number): number {
  if (!Number.isFinite(used) || !Number.isFinite(cap) || cap <= 0) return 0;
  return Math.min(1, Math.max(0, used / cap));
}

function formatUsd(usd: number): string {
  return `$${(Number.isFinite(usd) ? Math.max(0, usd) : 0).toFixed(2)}`;
}

/** "950", "1.5k", "18k", "1.3M": at most one decimal and none once there are two digits. */
export function formatTokens(tokens: number): string {
  const n = Number.isFinite(tokens) ? Math.max(0, tokens) : 0;
  const compact = (value: number, unit: string) => `${value >= 9.95 ? Math.round(value) : Math.round(value * 10) / 10}${unit}`;
  if (n >= 999_500) return compact(n / 1_000_000, "M");
  if (n >= 1_000) return compact(n / 1_000, "k");
  return String(Math.round(n));
}

/** What the budget row shows: the two fractions, the level they imply and the formatted values. */
export function budgetState(b: LearningWorkspaceBudget): BudgetState {
  const costFraction = fraction(b.usedUsd, b.capUsd);
  const tokenFraction = fraction(b.usedTokens, b.capTokens);
  const usedFraction = Math.max(costFraction, tokenFraction);
  const level: BudgetLevel = usedFraction >= 1 ? "exhausted" : usedFraction >= BUDGET_WARN_FRACTION ? "warn" : "ok";
  return {
    usedFraction,
    level,
    costFraction,
    tokenFraction,
    costLabel: b.capUsd > 0 ? `${formatUsd(b.usedUsd)} of ${formatUsd(b.capUsd)}` : formatUsd(b.usedUsd),
    tokensLabel: b.capTokens > 0 ? `${formatTokens(b.usedTokens)} of ${formatTokens(b.capTokens)} tokens` : `${formatTokens(b.usedTokens)} tokens`,
  };
}
