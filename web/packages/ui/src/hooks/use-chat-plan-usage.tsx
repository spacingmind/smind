import { createContext, useContext, useMemo, type ReactNode } from "react";

import type { DaemonEvents } from "@/hooks/use-daemon-events";
import { useAccountUsage, type UsageSnapshot } from "@/hooks/use-account-usage";
import { useUsagePreferences } from "@/hooks/use-usage-preferences";
import { chatPlanAccounts, planProviderFor, type PlanProvider } from "@/lib/account-usage/chat-provider";
import type { UsageDisplayAs } from "@/lib/account-usage/format";
import { resolveAccountFilter } from "@/lib/account-usage/model";
import type { AccountUsage } from "@/lib/account-usage/types";
import type { WsClient } from "@/lib/ws-client";

/**
 * Where the chat pills read plan usage from (ADR-0028 PR-E). The shell
 * provides its client and event bus once; a pill rendered outside it (a unit
 * test of the token rows, an old embedding) finds none and shows no Plan
 * usage section, exactly as before PR-E.
 */
interface PlanUsageSource {
  client: WsClient | null;
  events: DaemonEvents | null;
}

const PlanUsageSourceContext = createContext<PlanUsageSource | null>(null);

export function PlanUsageSourceProvider({
  client,
  events,
  children,
}: PlanUsageSource & { children: ReactNode }) {
  const value = useMemo(() => ({ client, events }), [client, events]);
  return <PlanUsageSourceContext.Provider value={value}>{children}</PlanUsageSourceContext.Provider>;
}

export interface ChatPlanUsage {
  /** Null: this chat's provider has no plan windows, or there is no source, or the daemon lacks `account.usage` / polling is off. The section is absent. */
  provider: PlanProvider | null;
  snapshot: UsageSnapshot;
  /** Every OAuth account of the provider, the filter-selected one first. */
  accounts: AccountUsage[];
  displayAs: UsageDisplayAs;
}

/**
 * Plan usage for one chat's provider, from the shared report store. It lists
 * `cached` once when the store is empty (a mounted pill needs to know whether
 * it has plan data to show) and otherwise only reads: it never sends `touch`
 * or `refresh` -- the hover card is not an explicit surface (AC56); the Usage
 * modal, reached from its "Open Usage" link, is.
 */
export function useChatPlanUsage(chatProvider: string | null | undefined): ChatPlanUsage {
  const source = useContext(PlanUsageSourceContext);
  const wanted = planProviderFor(chatProvider);
  const { snapshot } = useAccountUsage(wanted && source ? source.client : null, source?.events, { touch: false });
  const prefs = useUsagePreferences();

  const accounts = useMemo(() => {
    if (!wanted) return [];
    const filterKey = resolveAccountFilter(prefs.accountKey, snapshot.reports);
    return chatPlanAccounts(snapshot.reports, wanted, filterKey);
  }, [wanted, prefs.accountKey, snapshot.reports]);

  const active = wanted !== null && source?.client != null && snapshot.supportsUsage && snapshot.enabled;
  return {
    provider: active ? wanted : null,
    snapshot,
    accounts: active ? accounts : [],
    displayAs: prefs.displayAs,
  };
}
