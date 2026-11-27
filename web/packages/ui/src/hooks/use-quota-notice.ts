import { useCallback, useMemo, useSyncExternalStore } from "react";

import { useNow } from "@/hooks/use-now";
import { useProviderCatalog } from "@/hooks/use-provider-catalog";
import { useUsageReports } from "@/hooks/use-usage-reports";
import { pickQuotaNotice, type QuotaNotice } from "@/lib/ambient-quota";
import { providerIdentity } from "@/lib/provider-catalog";
import {
  dismissQuotaNotice,
  readDismissedQuotaNotices,
  subscribeDismissedQuotaNotices,
} from "@/lib/quota-notice-store";

/**
 * The ambient quota notice for the composer's provider (ADR-0059 F, D5).
 * Reads only the shared usage cache that a usage surface or a live `account.usage`
 * event fills (`useUsageReports({ enabled: false })`): it sends no request of any
 * mode, not even the `cached` read, so the app never calls `account.usage` before a
 * usage surface opens (ADR-0028 call budget; `App.test.tsx` pins it).
 */
export function useQuotaNotice(provider: string): { notice: QuotaNotice | null; dismiss: () => void } {
  const { snapshot } = useUsageReports({ enabled: false });
  const now = useNow();
  const { providers } = useProviderCatalog();
  const keys = useSyncExternalStore(subscribeDismissedQuotaNotices, readDismissedQuotaNotices, readDismissedQuotaNotices);
  const dismissed = useMemo(() => new Set(keys), [keys]);

  const accountProvider = providers.find((p) => p.id === provider)?.accountProvider ?? providerIdentity(provider)?.accountProvider;
  const notice = useMemo(
    () => pickQuotaNotice({ usages: snapshot.reports, accountProvider, now, dismissed }),
    [snapshot.reports, accountProvider, now, dismissed],
  );

  const dismiss = useCallback(() => {
    if (notice) dismissQuotaNotice(notice.key);
  }, [notice]);

  return { notice, dismiss };
}
