import type { QueryClient } from "@tanstack/react-query";

import { upsertReport } from "@/lib/account-usage/model";
import type { AccountUsage, AccountUsageEventPayload, UsageQueryData } from "@/lib/account-usage/types";
import { queryKeys } from "@/lib/query-keys";
import type { Account } from "@/lib/types";

/**
 * The daemon-event -> query-cache bridge (ADR-0032 decision 1). It is the one
 * place events touch the cache: `useDaemonEvents` calls it for every
 * notification before dispatching to listeners, so components never subscribe
 * to an event just to refetch.
 *
 * Rules: a payload that carries the whole entity is written with
 * `setQueryData` (no round trip); anything else invalidates. Invalidating the
 * usage query is safe -- it only ever re-reads the daemon's *cached* reports
 * (`usageQueryOptions`), never a provider -- and the usage query is never
 * invalidated from a focus, mount or retry path.
 */
export function applyDaemonEvent(queryClient: QueryClient, topic: string, payload: unknown): void {
  switch (topic) {
    case "account.updated": {
      const account = (payload as { account?: Account } | null)?.account;
      if (account && typeof account.id === "number") {
        queryClient.setQueryData<Account[]>(queryKeys.accountList(), (held) =>
          held ? upsertById(held, account) : held,
        );
      } else {
        void queryClient.invalidateQueries({ queryKey: queryKeys.accountList() });
      }
      // A new account has no usage row yet and a renamed one has a new displayName: one more
      // cached read picks both up.
      void queryClient.invalidateQueries({ queryKey: queryKeys.accountUsage() });
      void queryClient.invalidateQueries({ queryKey: queryKeys.providerList() });
      return;
    }
    case "account.removed": {
      const id = (payload as { id?: unknown } | null)?.id;
      if (typeof id !== "number") {
        void queryClient.invalidateQueries({ queryKey: queryKeys.accountList() });
        void queryClient.invalidateQueries({ queryKey: queryKeys.accountUsage() });
        return;
      }
      queryClient.setQueryData<Account[]>(queryKeys.accountList(), (held) => held?.filter((a) => a.id !== id));
      queryClient.setQueryData<UsageQueryData>(queryKeys.accountUsage(), (held) =>
        held ? { ...held, reports: held.reports.filter((r) => r.accountId !== id) } : held,
      );
      void queryClient.invalidateQueries({ queryKey: queryKeys.providerList() });
      return;
    }
    case "account.usage": {
      const usage = (payload as AccountUsageEventPayload | null)?.usage;
      if (typeof usage !== "object" || usage === null || typeof (usage as AccountUsage).accountId !== "number") return;
      const held = queryClient.getQueryData<UsageQueryData>(queryKeys.accountUsage());
      if (!held) return;
      // An account the usage cache has not seen is kept only when the account list says it exists (a
      // new account's `account.updated` arrives first); otherwise a report published just after the
      // account's removal would resurrect it. Anything else is settled by one more cached read.
      const known = held.reports.some((r) => r.accountId === usage.accountId);
      const listed = queryClient
        .getQueryData<Account[]>(queryKeys.accountList())
        ?.some((a) => a.id === usage.accountId);
      if (!known && listed !== true) {
        void queryClient.invalidateQueries({ queryKey: queryKeys.accountUsage() });
        return;
      }
      queryClient.setQueryData<UsageQueryData>(queryKeys.accountUsage(), (current) =>
        current ? { ...current, reports: upsertReport(current.reports, usage) } : current,
      );
      return;
    }
    case "profile.created":
    case "profile.updated":
    case "profile.deleted":
      void queryClient.invalidateQueries({ queryKey: queryKeys.profileList() });
      return;
    default:
      return;
  }
}

function upsertById(list: Account[], account: Account): Account[] {
  const index = list.findIndex((existing) => existing.id === account.id);
  if (index === -1) return [...list, account];
  const next = list.slice();
  next[index] = account;
  return next;
}
