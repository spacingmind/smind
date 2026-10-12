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

/** A window with no percent that still says something: a limit reached or nearly reached (an observed one). */
function isLimitStatusOnly(window: UsageWindow | null | undefined): boolean {
  return (
    window != null &&
    typeof window === "object" &&
    !hasPercent(window) &&
    (window.status === "rejected" || window.status === "allowed_warning")
  );
}

/**
 * The windows the compact card draws: the daemon-flagged `summary` ones, else
 * the first window with a percent (the same rule the default pins use), plus
 * any window with no percent but a `rejected`/`allowed_warning` status, which
 * the card writes out as a word instead of a meter.
 */
export function summaryWindows(usage: AccountUsage): UsageWindow[] {
  const windows = usage.report?.status === "available" && Array.isArray(usage.report.windows) ? usage.report.windows : [];
  const withPercent = windows.filter(hasPercent);
  const marked = withPercent.filter((window) => window.summary);
  const shown = new Set<UsageWindow>(marked.length > 0 ? marked : withPercent.slice(0, 1));
  return windows.filter((window) => shown.has(window) || isLimitStatusOnly(window));
}

/** Whether an account has anything to say: a report, or the last error of a fetch that never produced one. */
export function hasPlanData(usage: AccountUsage): boolean {
  return usage.report != null || Boolean(usage.lastError);
}

/** Plan data worth a trigger on a chat that has no token usage: at least one account with windows to show. */
export function hasPlanWindows(accounts: readonly AccountUsage[]): boolean {
  return accounts.some((usage) => summaryWindows(usage).length > 0);
}
