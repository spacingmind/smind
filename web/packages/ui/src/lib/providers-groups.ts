import type {
  Account,
  ProviderInfo,
  RunSummary,
} from "@/lib/types";

/** One runtime-provider group: the ProviderInfo row plus the accounts mapped to it (empty => the group renders its not-connected row). */
export interface ProviderGroup {
  info: ProviderInfo;
  accounts: Account[];
}

/**
 * Partitions accounts into per-runtime-provider groups (in provider.list's
 * own display order) plus the "other" leftovers no runtime provider maps.
 * Exported for the grouping tests; the component itself is thin over it.
 */
export function groupAccountsByProvider(
  providers: ProviderInfo[],
  accounts: Account[],
): { groups: ProviderGroup[]; other: Account[] } {
  const groups = providers.map((info) => ({
    info,
    accounts: info.accountProvider
      ? accounts.filter((a) => a.provider === info.accountProvider)
      : [],
  }));
  const mapped = new Set(
    providers.filter((p) => p.accountProvider).map((p) => p.accountProvider),
  );
  return { groups, other: accounts.filter((a) => !mapped.has(a.provider)) };
}

/**
 * The Remove-confirmation warning (ADR-0015's accepted condition 1, the
 * plan's Decision 4): how many running runs sit on the account's runtime
 * provider, and whether a sibling account exists to fail over to. Runs
 * carry a runner provider -- never an account id -- so the count is
 * provider-level by construction, which is exactly what the accepted
 * warning copy states. Exported for the warning tests.
 */
export function removeWarning(
  account: Account,
  providers: ProviderInfo[],
  accounts: Account[],
  runs: RunSummary[],
): { runningCount: number; lastAccount: boolean; providerLabel: string } {
  const label =
    providers.find((p) => p.accountProvider === account.provider)?.label ?? account.provider;
  const siblings = accounts.filter((a) => a.provider === account.provider).length;
  const runnerIds = providers
    .filter((p) => p.accountProvider === account.provider)
    .map((p) => p.id);
  const runningCount = runs.filter(
    (r) => r.Status === "running" && runnerIds.includes(r.Provider),
  ).length;
  return { runningCount, lastAccount: siblings <= 1, providerLabel: label };
}

export type ProviderConsumer = "proxy" | "none";

/**
 * Which real consumer, if any, reads an internal/accounts provider kind's
 * credential today. internal/server/proxy.go hardcodes exactly two routed
 * account.provider strings (its providerAnthropic/providerOpenAI
 * constants -- the only ones POST /v1/messages and /v1/chat/completions
 * serve, per proxy.serve's exact-match filter). No internal/taskrunner
 * backend reads an account credential at all -- acp.GLMCommand/KimiCommand
 * spawn bare subprocesses with no credential injection, and Claude
 * Code/Codex's native backends use their own out-of-band CLI logins -- so
 * nothing is runner-consumed. Anything outside the proxy-routed set (kimi,
 * xai, antigravity, or any other account.add-able string, including a
 * "glm"-labeled account, which the backend's own ProviderInfo never maps
 * to a credential row at all) has no consumer.
 */
const PROXY_ROUTED_PROVIDERS = new Set(["anthropic", "openai"]);

export function providerConsumer(accountProvider: string): ProviderConsumer {
  return PROXY_ROUTED_PROVIDERS.has(accountProvider) ? "proxy" : "none";
}
