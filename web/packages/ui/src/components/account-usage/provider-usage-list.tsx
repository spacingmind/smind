import { UsageCard } from "@/components/account-usage/usage-card";
import type { UsageDisplayAs } from "@/lib/account-usage/format";
import type { UsageRefresh } from "@/lib/account-usage/model";
import type { ProviderUsageRow } from "@/lib/account-usage/provider-usage-types";

function refreshStateOf(id: string, refreshing: ReadonlySet<string>, failed: ReadonlySet<string>): UsageRefresh {
  if (refreshing.has(id)) return "pending";
  return failed.has(id) ? "failed" : "idle";
}

/** The row's cooldown end when it is still ahead, else null. */
function activeUntil(row: ProviderUsageRow): Date | null {
  if (!row.cooldownUntil) return null;
  const at = new Date(row.cooldownUntil);
  return Number.isNaN(at.getTime()) || at.getTime() <= Date.now() ? null : at;
}

/** One card per provider instance (the account list's twin, keyed by instance id). */
export function ProviderUsageList({
  rows,
  displayAs,
  refreshing,
  failed,
  pressedAt,
  onRefreshOne,
}: {
  rows: readonly ProviderUsageRow[];
  displayAs: UsageDisplayAs;
  refreshing: ReadonlySet<string>;
  failed: ReadonlySet<string>;
  pressedAt: Readonly<Record<string, number>>;
  onRefreshOne: (instanceId: string) => void;
}) {
  if (rows.length === 0) return null;
  return (
    <div className="flex flex-col gap-3" data-testid="provider-usage-list">
      {rows.map((row) => {
        const until = activeUntil(row);
        return (
          <div key={row.instanceId} className="rounded-xl border bg-card" data-testid={`provider-usage-${row.instanceId}`}>
            <UsageCard
              usage={row}
              displayAs={displayAs}
              refresh={{
                state: refreshStateOf(row.instanceId, refreshing, failed),
                pressedAt: pressedAt[row.instanceId],
                onRefresh: () => onRefreshOne(row.instanceId),
              }}
            />
            {until ? (
              <p data-testid={`provider-usage-cooldown-${row.instanceId}`} className="px-4 pb-3 text-ui-sm text-muted-foreground">
                {`Rate limited until ${until.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}`}
              </p>
            ) : null}
          </div>
        );
      })}
    </div>
  );
}
