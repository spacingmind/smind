import { skipToken, useQueries } from "@tanstack/react-query";

import { useAccounts } from "@/hooks/use-accounts";
import { useNow } from "@/hooks/use-now";
import { useUsageReports } from "@/hooks/use-usage-reports";
import { accountStatus, type AccountStatus } from "@/lib/account-status";
import { queryKeys } from "@/lib/query-keys";
import { healthBadge, type HealthBadge } from "@/lib/providers-view";
import type { AccountUsage } from "@/lib/account-usage/types";
import type { Account, ProviderTestResult } from "@/lib/types";

export interface AccountRowData {
  account: Account;
  usage: AccountUsage | undefined;
  test: ProviderTestResult | undefined;
  status: AccountStatus;
  badge: HealthBadge;
}

/**
 * One row per account, combining the shared account list, the usage
 * cache (read-only: `useUsageReports()` with its default `touch: false`,
 * so mounting rows never adds a touch cadence) and the cached local
 * `provider.test` results, folded into the row's status and badge.
 */
export function useAccountRows(): { rows: AccountRowData[]; loaded: boolean; error: Error | null } {
  const accountsQuery = useAccounts();
  const accounts = accountsQuery.data;
  const { snapshot } = useUsageReports();
  const now = useNow();

  const providerKeys = [...new Set((accounts ?? []).map((a) => a.provider))];
  // Same shape as useProviderTestResult: a cache read per key, never a fetch.
  const testQueries = useQueries({
    queries: providerKeys.map((key) => ({
      queryKey: queryKeys.providerTest(key),
      queryFn: skipToken,
      staleTime: Infinity,
      gcTime: Infinity,
    })),
  });
  const testByKey = new Map<string, ProviderTestResult | undefined>(
    providerKeys.map((key, i) => [key, testQueries[i]?.data as ProviderTestResult | undefined]),
  );

  const rows = (accounts ?? []).map((account) => {
    const usage = snapshot.reports.find((r) => r.accountId === account.id);
    const test = testByKey.get(account.provider);
    return {
      account,
      usage,
      test,
      status: accountStatus({ account, usage, test, now }),
      badge: healthBadge({ usage, test, credentialType: account.credentialType, now }),
    };
  });
  return { rows, loaded: accounts !== undefined, error: accounts === undefined ? (accountsQuery.error ?? null) : null };
}
