import { UsageBalanceBar } from "@/components/account-usage/usage-balance-bar";
import { UsageRefreshButton } from "@/components/account-usage/usage-refresh-button";
import { UsageSourceIcon } from "@/components/account-usage/usage-source-icon";
import { UsageWindowBar } from "@/components/account-usage/usage-window-bar";
import { StatusBadge } from "@/components/ui/status-badge";
import { StatusDot } from "@/components/ui/status-dot";
import { useIsMobile } from "@/hooks/use-mobile";
import { usageCopy } from "@/lib/account-usage/copy";
import type { UsageDisplayAs } from "@/lib/account-usage/format";
import { formatUsageFreshness, type UsageRefresh } from "@/lib/account-usage/model";
import type { AccountUsage, UsageBalance, UsageDetail, UsageWindow } from "@/lib/account-usage/types";
import { cn } from "@/lib/utils";

export interface UsageCardRefresh {
  state: UsageRefresh;
  pressedAt?: number;
  onRefresh: () => void;
}

/** The names a provider's account goes by already; a display name equal to one needs no provider suffix. */
const PROVIDER_OWN_NAMES: Record<string, string[]> = {
  anthropic: ["claude"],
  openai: ["codex"],
};

function repeatsProvider(displayName: string, provider: string): boolean {
  const name = displayName.trim().toLowerCase();
  const id = provider.trim().toLowerCase();
  if (!id) return true;
  return name === id || (PROVIDER_OWN_NAMES[id] ?? []).includes(name);
}

/** The entries of a list field that are objects; a reshaped payload (null, a number) is skipped, never thrown on (AC39). */
function list<T>(value: T[] | undefined | null): T[] {
  return Array.isArray(value) ? value.filter((entry) => entry !== null && typeof entry === "object") : [];
}

/**
 * One account's plan usage (Paseo's UsageCard, minus pins): header, why it has no windows if
 * it has none, one bar per window then per balance, detail rows, freshness. Every field may be
 * missing or reshaped (AC39).
 */
export function UsageCard({
  usage,
  displayAs,
  refresh,
  compact = false,
}: {
  usage: AccountUsage;
  displayAs: UsageDisplayAs;
  /** Null = no Refresh button; the freshness is then printed on the card. */
  refresh: UsageCardRefresh | null;
  compact?: boolean;
}) {
  const isMobile = useIsMobile();
  const report = usage.report;
  const available = report?.status === "available";
  const windows: UsageWindow[] = available ? list(report.windows) : [];
  const balances: UsageBalance[] = available ? list(report.balances) : [];
  const details: UsageDetail[] = available ? list(report.details) : [];
  const message = usageCopy.problem(usage);
  const statusLabel =
    report && !available ? (report.status === "error" ? usageCopy.errorLabel : usageCopy.unavailableLabel) : null;
  const hasBody = windows.length > 0 || balances.length > 0 || details.length > 0;
  // Where there is no hover the freshness is printed on the card; elsewhere the Refresh tooltip says it.
  const showsFreshnessInline = refresh === null || isMobile;
  const provider = typeof usage.provider === "string" ? usage.provider : "";
  const displayName = (typeof usage.displayName === "string" && usage.displayName) || provider;

  return (
    <div data-testid={`usage-report-${usage.accountId}`} className={cn("flex flex-col gap-3", compact ? "" : "p-4")}>
      <div className="flex items-center gap-2">
        <UsageSourceIcon provider={provider} />
        <span className="min-w-0 truncate text-ui-base text-foreground">{displayName}</span>
        {repeatsProvider(displayName, provider) ? null : (
          <span className="shrink-0 text-ui-sm text-muted-foreground" data-testid="usage-provider">
            {provider}
          </span>
        )}
        {available && report.planLabel ? (
          <StatusBadge status="success" className="border-border bg-muted text-muted-foreground">
            {report.planLabel}
          </StatusBadge>
        ) : null}
        <div className="flex-1" />
        {statusLabel ? (
          <span className="flex shrink-0 items-center gap-1.5 text-ui-sm text-muted-foreground">
            <StatusDot status={report?.status === "error" ? "danger" : "neutral"} />
            {statusLabel}
          </span>
        ) : null}
        {refresh ? (
          <div className="-my-1 -mr-1.5 shrink-0">
            <UsageRefreshButton
              usage={usage}
              state={refresh.state}
              pressedAt={refresh.pressedAt}
              onRefresh={refresh.onRefresh}
            />
          </div>
        ) : null}
      </div>

      {message ? (
        <p className="text-ui-sm leading-snug text-destructive" data-testid="usage-problem">
          {message}
        </p>
      ) : null}

      {windows.length > 0 || balances.length > 0 ? (
        <div className="flex flex-col gap-1">
          {windows.map((window, i) => (
            <UsageWindowBar key={window.id ?? i} window={window} displayAs={displayAs} />
          ))}
          {balances.map((balance, i) => (
            <UsageBalanceBar key={balance.id ?? i} balance={balance} />
          ))}
        </div>
      ) : null}

      {details.length > 0 ? (
        <div className="flex flex-col gap-1">
          {details.map((detail, i) => (
            <div key={detail.id ?? i} className="flex justify-between gap-2 text-ui-sm" data-testid={`usage-detail-${detail.id}`}>
              <span className="min-w-0 truncate text-muted-foreground">{detail.label}</span>
              <span className="shrink-0 text-foreground">{detail.value}</span>
            </div>
          ))}
        </div>
      ) : null}

      {!hasBody && !message ? (
        <p className="text-ui-sm text-muted-foreground" data-testid="usage-empty">
          —
        </p>
      ) : null}

      {showsFreshnessInline && report ? (
        <p className="truncate text-ui-sm text-muted-foreground" data-testid="usage-freshness">
          {formatUsageFreshness(report.fetchedAt)}
        </p>
      ) : null}

      {refresh?.state === "failed" ? (
        <p className="text-ui-sm leading-snug text-destructive" data-testid="usage-refresh-error">
          {usageCopy.refreshFailed}
        </p>
      ) : null}
    </div>
  );
}
