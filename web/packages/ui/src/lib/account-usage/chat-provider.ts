import type { AccountUsage, UsageWindow } from "./types";

/** The account providers that have plan windows (`quota` fetchers: Anthropic OAuth, ChatGPT OAuth). */
export type PlanProvider = "anthropic" | "openai";

/**
 * The account provider whose plan a chat's runs spend (ADR-0028 D5, PR-E):
 * `claude-native` -> `anthropic`, `codex-native` -> `openai`. Every other
 * chat provider (GLM, Kimi, ACP agents) has no plan windows, so the chat
 * shows no Plan usage section.
 */
export function planProviderFor(chatProvider: string | null | undefined): PlanProvider | null {
  switch (chatProvider) {
    case "claude-native":
      return "anthropic";
    case "codex-native":
      return "openai";
    default:
      return null;
  }
}

/** "Claude" / "Codex": how the untracked-login note names the provider. */
export function planProviderName(provider: PlanProvider): string {
  return provider === "anthropic" ? "Claude" : "Codex";
}

/**
 * Every OAuth account of the provider (smind does not track which login a run
 * used, ADR-0020), the account the user filtered to first, the rest in the
 * daemon's order. API-key accounts never appear: they have no plan windows.
 */
export function chatPlanAccounts(
  reports: readonly AccountUsage[],
  provider: PlanProvider,
  filterKey: string | null,
): AccountUsage[] {
  const accounts = reports.filter((usage) => usage.provider === provider && usage.credentialType === "oauth");
  if (filterKey === null) return accounts;
  const picked = accounts.filter((usage) => usage.key === filterKey);
  return [...picked, ...accounts.filter((usage) => usage.key !== filterKey)];
}

function hasPercent(window: UsageWindow | null | undefined): boolean {
  return window != null && typeof window === "object" && (window.usedPct != null || window.remainingPct != null);
}

/**
 * The windows the compact card draws: the daemon-flagged `summary` ones, else
 * the first window with a percent (the same rule the default pins use).
 */
export function summaryWindows(usage: AccountUsage): UsageWindow[] {
  const windows = usage.report?.status === "available" && Array.isArray(usage.report.windows) ? usage.report.windows : [];
  const withPercent = windows.filter(hasPercent);
  const marked = withPercent.filter((window) => window.summary);
  return marked.length > 0 ? marked : withPercent.slice(0, 1);
}

/** Whether an account has anything to say: a report, or the last error of a fetch that never produced one. */
export function hasPlanData(usage: AccountUsage): boolean {
  return usage.report != null || Boolean(usage.lastError);
}

/** Plan data worth a trigger on a chat that has no token usage: at least one account with windows to show. */
export function hasPlanWindows(accounts: readonly AccountUsage[]): boolean {
  return accounts.some((usage) => summaryWindows(usage).length > 0);
}
