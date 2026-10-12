import { UsageOptions } from "@/components/account-usage/usage-options";
import { UsageRefreshAllButton } from "@/components/account-usage/usage-refresh-button";
import type { UsageSurface } from "@/components/account-usage/use-usage-surface";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";

const ALL = "__all__";

/**
 * The controls on the right of every usage title row (Paseo's UsageControls):
 * the account filter when there is more than one account, Refresh all, and
 * Settings. A surface that cannot show usage (old daemon) keeps none of them.
 */
export function UsageControls({
  surface,
  labelledSettings = false,
}: {
  surface: UsageSurface;
  /** Settings -> Usage shows a visible "Settings" label instead of a bare cog. */
  labelledSettings?: boolean;
}) {
  const { view, accounts, filterKey } = surface;
  if (view.kind === "unavailable") return null;
  return (
    <div className="flex min-w-0 shrink items-center gap-1.5">
      {accounts.length > 1 && (
        <Select value={filterKey ?? ALL} onValueChange={(v) => surface.setFilterKey(v === ALL ? null : v)}>
          <SelectTrigger
            aria-label="Account"
            data-testid="usage-account-filter-trigger"
            className="h-7 max-w-40 min-w-0 text-ui-sm md:text-ui-sm"
          >
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL} data-testid="usage-account-filter-item-all">
              All accounts
            </SelectItem>
            {accounts.map((a) => (
              <SelectItem key={a.key} value={a.key} data-testid={`usage-account-filter-item-${a.key}`}>
                {a.displayName}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      )}
      {(surface.canRefresh || surface.snapshot.refreshingAll) && (
        <UsageRefreshAllButton busy={surface.busy} onRefresh={surface.refreshAll} />
      )}
      <UsageOptions
        displayAs={surface.displayAs}
        onDisplayAs={surface.setDisplayAs}
        inSidebar={surface.inSidebar}
        onInSidebar={surface.setInSidebar}
        labelled={labelledSettings}
      />
    </div>
  );
}
