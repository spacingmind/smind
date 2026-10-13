import { Clock } from "lucide-react";

import { UsageMeter } from "@/components/account-usage/usage-meter";
import { UsageSourceIcon } from "@/components/account-usage/usage-source-icon";
import { useActiveCooldown } from "@/components/account-usage/use-active-cooldown";
import { StatusBadge } from "@/components/ui/status-badge";
import { summaryWindows } from "@/lib/account-usage/chat-provider";
import { usageCopy } from "@/lib/account-usage/copy";
import { formatDisplayPct, formatResetLabel, type UsageDisplayAs } from "@/lib/account-usage/format";
import { displayPercent, formatUsageFreshness } from "@/lib/account-usage/model";
import { windowTone } from "@/lib/account-usage/tone";
import type { AccountUsage, UsageTone, UsageWindow } from "@/lib/account-usage/types";

const STATUS_TEXT: Record<UsageTone, string> = {
  ok: "text-success",
  warning: "text-warning",
  danger: "text-destructive",
  default: "text-foreground",
};

function CompactWindow({ window, displayAs }: { window: UsageWindow; displayAs: UsageDisplayAs }) {
  const percent = displayPercent(window, displayAs);
  const reset = formatResetLabel(window.resetsAt);
  const label = window.shortLabel || window.label || window.id;
  const tone = windowTone(window);
  const status = percent == null ? usageCopy.windowStatus(window.status) : null;
  // Only a window the daemon built from proxied-request headers says so; a fetched one never does.
  const hint =
    window.source === "observed" ? (
      <span className="min-w-0 truncate text-ui-xs text-muted-foreground/80" data-testid="plan-usage-window-observed">
        {usageCopy.observedWindow}
      </span>
    ) : null;
  return (
    <div data-testid={`plan-usage-window-${window.id}`} className="flex flex-col gap-0.5">
      <div className="grid grid-cols-[3.25rem_minmax(0,1fr)_auto] items-center gap-2 text-ui-sm">
        <span className="truncate text-muted-foreground">{label}</span>
        {status ? (
          // No meter to draw: the muted hint (if any) takes its place, so the row stays one line.
          <span className="flex min-w-0">{hint}</span>
        ) : (
          <div
            role="progressbar"
            aria-label={window.label || window.id}
            aria-valuemin={0}
            aria-valuemax={100}
            aria-valuenow={percent == null ? undefined : Math.round(percent)}
          >
            <UsageMeter percent={percent ?? 0} tone={tone} />
          </div>
        )}
        <span className="text-right font-medium whitespace-nowrap text-foreground">
          {status ? (
            <span className={STATUS_TEXT[tone]} data-testid="plan-usage-window-status" data-tone={tone}>
              {status}
            </span>
          ) : (
            formatDisplayPct(window, displayAs)
          )}
          {reset ? <span className="font-normal text-muted-foreground">{` · ${reset}`}</span> : null}
        </span>
      </div>
      {status ? null : hint ? <div className="flex pl-[3.75rem]">{hint}</div> : null}
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
  const cooldown = useActiveCooldown(usage);
  const cooldownCopy = cooldown ? usageCopy.cooldown(cooldown) : null;
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
      {cooldownCopy ? (
        <p
          data-testid="plan-usage-cooldown"
          title={[cooldownCopy.rejected, cooldownCopy.note].filter(Boolean).join(" · ")}
          className="flex min-w-0 items-center gap-1 text-ui-sm font-medium text-destructive"
        >
          <Clock aria-hidden className="size-3 shrink-0" />
          <span className="min-w-0 truncate">{cooldownCopy.headline}</span>
        </p>
      ) : null}
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
