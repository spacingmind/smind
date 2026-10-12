import { UsageCard } from "@/components/account-usage/usage-card";
import type { UsageDisplayAs } from "@/lib/account-usage/format";
import type { UsageRefresh } from "@/lib/account-usage/model";
import type { AccountUsage } from "@/lib/account-usage/types";

export interface UsageRefreshProps {
  /** Account ids with a refresh in flight. */
  refreshing: ReadonlySet<number>;
  /** Account ids whose last refresh failed. */
  failed: ReadonlySet<number>;
  /** When Refresh was last pressed per account (ms). */
  pressedAt: Readonly<Record<number, number>>;
  onRefreshOne: (accountId: number) => void;
}

function refreshStateOf(accountId: number, refreshing: ReadonlySet<number>, failed: ReadonlySet<number>): UsageRefresh {
  if (refreshing.has(accountId)) return "pending";
  return failed.has(accountId) ? "failed" : "idle";
}

/** One card per account (Paseo's UsageList). */
export function UsageList({
  reports,
  displayAs,
  refreshing,
  failed,
  pressedAt,
  onRefreshOne,
}: { reports: readonly AccountUsage[]; displayAs: UsageDisplayAs } & UsageRefreshProps) {
  return (
    <div className="flex flex-col gap-3" data-testid="usage-list">
      {reports.map((usage) => (
        <div key={usage.accountId} className="rounded-xl border bg-card">
          <UsageCard
            usage={usage}
            displayAs={displayAs}
            refresh={{
              state: refreshStateOf(usage.accountId, refreshing, failed),
              pressedAt: pressedAt[usage.accountId],
              onRefresh: () => onRefreshOne(usage.accountId),
            }}
          />
        </div>
      ))}
    </div>
  );
}
