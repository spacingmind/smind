import { UsageMeter } from "@/components/account-usage/usage-meter";
import { usageCopy } from "@/lib/account-usage/copy";
import { formatDisplayPct, formatResetLabel, type UsageDisplayAs } from "@/lib/account-usage/format";
import { displayPercent } from "@/lib/account-usage/model";
import { windowTone } from "@/lib/account-usage/tone";
import type { UsageTone, UsageWindow } from "@/lib/account-usage/types";

const STATUS_TEXT: Record<UsageTone, string> = {
  ok: "text-success",
  warning: "text-warning",
  danger: "text-destructive",
  default: "text-foreground",
};

/**
 * One window as a row: label left, value and reset label right, the meter
 * below (Paseo's UsageWindowBar, minus pinning). Every field may be missing
 * (AC39): a window without a percent reads "—" over an empty meter, unless it
 * carries a limiting `status` (an observed window may have nothing else), which
 * is then written out in its tone in place of the meter. An observed window is
 * marked "from recent traffic".
 */
export function UsageWindowBar({ window, displayAs }: { window: UsageWindow; displayAs: UsageDisplayAs }) {
  const percent = displayPercent(window, displayAs);
  const reset = formatResetLabel(window.resetsAt);
  const label = window.label || window.id;
  const tone = windowTone(window);
  const status = percent == null ? usageCopy.windowStatus(window.status) : null;
  return (
    <div data-testid={`usage-window-${window.id}`} className="flex flex-col gap-1 py-1">
      <div className="flex items-center justify-between gap-2 text-ui-sm">
        <span className="flex min-w-0 flex-1 items-baseline gap-1.5">
          <span className="min-w-0 truncate text-muted-foreground">{label}</span>
          {window.source === "observed" ? (
            <span className="shrink-0 text-ui-xs text-muted-foreground/80" data-testid="usage-window-observed">
              {usageCopy.observedWindow}
            </span>
          ) : null}
        </span>
        <span className="shrink-0 font-medium text-foreground">
          {status ? (
            <span className={STATUS_TEXT[tone]} data-testid="usage-window-status" data-tone={tone}>
              {status}
            </span>
          ) : (
            formatDisplayPct(window, displayAs)
          )}
          {reset ? <span className="font-normal text-muted-foreground">{` · ${reset}`}</span> : null}
        </span>
      </div>
      {status ? null : (
        <div
          role="progressbar"
          aria-label={label}
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuenow={percent == null ? undefined : Math.round(percent)}
        >
          <UsageMeter percent={percent ?? 0} tone={tone} />
        </div>
      )}
    </div>
  );
}
