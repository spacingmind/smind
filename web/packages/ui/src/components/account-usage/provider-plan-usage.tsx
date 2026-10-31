import { ProviderUsageList } from "@/components/account-usage/provider-usage-list";
import { useProviderUsage } from "@/hooks/use-provider-usage";
import { useUsagePreferences } from "@/hooks/use-usage-preferences";

/**
 * Plan windows of provider instances (Z.AI). Renders nothing when the daemon
 * does not support `provider.usage`, the feature is off, or no row matches.
 * `instanceId` narrows to one instance; `touch` makes it a touching surface.
 */
export function ProviderPlanUsage({ instanceId, touch = false }: { instanceId?: string; touch?: boolean }) {
  const usage = useProviderUsage({ touch });
  const { displayAs } = useUsagePreferences();
  if (!usage.supports || !usage.enabled) return null;
  const rows = instanceId === undefined ? usage.reports : usage.reports.filter((r) => r.instanceId === instanceId);
  if (rows.length === 0) return null;
  return (
    <div data-testid="provider-plan-usage">
      <ProviderUsageList
        rows={rows}
        displayAs={displayAs}
        refreshing={usage.refreshing}
        failed={usage.failed}
        pressedAt={usage.pressedAt}
        onRefreshOne={(id) => void usage.refresh(id)}
      />
    </div>
  );
}
