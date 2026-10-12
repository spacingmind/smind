import { UsageMeter } from "@/components/account-usage/usage-meter";
import { UsageSourceIcon } from "@/components/account-usage/usage-source-icon";
import { StatusBadge } from "@/components/ui/status-badge";
import { summaryWindows } from "@/lib/account-usage/chat-provider";
import { usageCopy } from "@/lib/account-usage/copy";
import { formatDisplayPct, formatResetLabel, type UsageDisplayAs } from "@/lib/account-usage/format";
import { displayPercent, formatUsageFreshness } from "@/lib/account-usage/model";
import { windowTone } from "@/lib/account-usage/tone";
import type { AccountUsage, UsageWindow } from "@/lib/account-usage/types";

function CompactWindow({ window, displayAs }: { window: UsageWindow; displayAs: UsageDisplayAs }) {
  const percent = displayPercent(window, displayAs);
  const reset = formatResetLabel(window.resetsAt);
  const label = window.shortLabel || window.label || window.id;
  return (
    <div data-testid={`plan-usage-window-${window.id}`} className="grid grid-cols-[3.25rem_minmax(0,1fr)_auto] items-center gap-2 text-ui-sm">
      <span className="truncate text-muted-foreground">{label}</span>
      <div
        role="progressbar"
        aria-label={window.label || window.id}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={percent == null ? undefined : Math.round(percent)}
      >
        <UsageMeter percent={percent ?? 0} tone={windowTone(window)} />
      </div>
      <span className="text-right font-medium whitespace-nowrap text-foreground">
        {formatDisplayPct(window, displayAs)}
        {reset ? <span className="font-normal text-muted-foreground">{` · ${reset}`}</span> : null}
      </span>
    </div>
  );
}

/**
 * One account for the chat hover card (PR-E): name and plan, one meter line
 * per summary window, and either its problem in one sentence or its age.
 * No pins, no Refresh -- the Usage modal is where an account is refreshed.
 * Every field may be missing (AC39).
 */
export function PlanUsageCompact({ usage, displayAs }: { usage: AccountUsage; displayAs: UsageDisplayAs }) {
  const report = usage.report;
  const provider = typeof usage.provider === "string" ? usage.provider : "";
  const displayName = (typeof usage.displayName === "string" && usage.displayName) || provider;
  const windows = summaryWindows(usage);
  const message = usageCopy.problem(usage);
  const informational = report?.status === "unavailable" && report.problem?.kind === "no_quota";
  return (
    <div data-testid={`plan-usage-${usage.accountId}`} className="flex flex-col gap-1.5">
      <div className="flex items-center gap-1.5">
        <UsageSourceIcon provider={provider} className="size-3.5" />
        <span className="min-w-0 truncate text-ui-sm font-medium text-foreground">{displayName}</span>
        {report?.status === "available" && report.planLabel ? (
          <StatusBadge status="success" className="border-border bg-muted text-muted-foreground">
            {report.planLabel}
          </StatusBadge>
        ) : null}
      </div>
      {message ? (
        <p
          data-testid="plan-usage-problem"
          data-tone={informational ? "info" : "problem"}
          className={informational ? "text-ui-sm leading-snug text-muted-foreground" : "text-ui-sm leading-snug text-destructive"}
        >
          {message}
        </p>
      ) : null}
      {windows.map((window, i) => (
        <CompactWindow key={window.id ?? i} window={window} displayAs={displayAs} />
      ))}
      {report && !informational ? (
        <p data-testid="plan-usage-freshness" className="truncate text-ui-xs text-muted-foreground">
          {formatUsageFreshness(report.fetchedAt)}
        </p>
      ) : null}
    </div>
  );
}
