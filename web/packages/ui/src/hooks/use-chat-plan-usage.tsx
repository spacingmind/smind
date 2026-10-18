import { useMemo } from "react";

import { useUsageReports, type UsageSnapshot } from "@/hooks/use-usage-reports";
import { useUsagePreferences } from "@/hooks/use-usage-preferences";
import { chatPlanAccounts, planProviderFor, type PlanProvider } from "@/lib/account-usage/chat-provider";
import type { UsageDisplayAs } from "@/lib/account-usage/format";
import { resolveAccountFilter } from "@/lib/account-usage/model";
import type { AccountUsage } from "@/lib/account-usage/types";
import { useDaemonConnection } from "@/lib/query/daemon-query-provider";

export interface ChatPlanUsage {
  /** Null: this chat's provider has no plan windows, or there is no connection, or the daemon lacks `account.usage` / polling is off. The section is absent. */
  provider: PlanProvider | null;
  snapshot: UsageSnapshot;
  /** Every OAuth account of the provider, the filter-selected one first. */
  accounts: AccountUsage[];
  displayAs: UsageDisplayAs;
}

/**
 * Plan usage for one chat's provider, from the shared report cache. It reads
 * `cached` once when the cache is empty (a mounted pill needs to know whether
 * it has plan data to show) and otherwise only reads: it never sends `touch`
 * or `refresh` -- the hover card is not an explicit surface (AC56); the Usage
 * modal, reached from its "Open Usage" link, is.
 */
export function useChatPlanUsage(chatProvider: string | null | undefined): ChatPlanUsage {
  const { client } = useDaemonConnection();
  const wanted = planProviderFor(chatProvider);
  const { snapshot } = useUsageReports({ enabled: wanted !== null });
  const prefs = useUsagePreferences();

  const accounts = useMemo(() => {
    if (!wanted) return [];
    const filterKey = resolveAccountFilter(prefs.accountKey, snapshot.reports);
    return chatPlanAccounts(snapshot.reports, wanted, filterKey);
  }, [wanted, prefs.accountKey, snapshot.reports]);

  const active = wanted !== null && client !== null && snapshot.supportsUsage && snapshot.enabled;
  return {
    provider: active ? wanted : null,
    snapshot,
    accounts: active ? accounts : [],
    displayAs: prefs.displayAs,
  };
}
