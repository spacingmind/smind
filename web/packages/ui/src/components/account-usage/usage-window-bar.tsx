import { UsageMeter } from "@/components/account-usage/usage-meter";
import { formatDisplayPct, formatResetLabel, type UsageDisplayAs } from "@/lib/account-usage/format";
import { windowTone } from "@/lib/account-usage/tone";
import type { UsageWindow } from "@/lib/account-usage/types";

/** The percent the meter fills to under the used/remaining preference; null when the window has none. */
function meterPercent(window: UsageWindow, displayAs: UsageDisplayAs): number | null {
  const used = window.usedPct ?? (window.remainingPct != null ? 100 - window.remainingPct : null);
  if (displayAs === "used") return used;
  return window.remainingPct ?? (used != null ? 100 - used : null);
}

/**
 * One window as a row: label left, value and reset label right, the meter
 * below (Paseo's UsageWindowBar, minus pinning). Every field may be missing
 * (AC39): a window without a percent reads "—" over an empty meter.
 */
export function UsageWindowBar({ window, displayAs }: { window: UsageWindow; displayAs: UsageDisplayAs }) {
  const percent = meterPercent(window, displayAs);
  const reset = formatResetLabel(window.resetsAt);
  const label = window.label || window.id;
  return (
    <div data-testid={`usage-window-${window.id}`} className="flex flex-col gap-1 py-1">
      <div className="flex items-center justify-between gap-2 text-ui-sm">
        <span className="min-w-0 flex-1 truncate text-muted-foreground">{label}</span>
        <span className="shrink-0 font-medium text-foreground">
          {formatDisplayPct(window, displayAs)}
          {reset ? <span className="font-normal text-muted-foreground">{` · ${reset}`}</span> : null}
        </span>
      </div>
      <div
        role="progressbar"
        aria-label={label}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={percent == null ? undefined : Math.round(percent)}
      >
        <UsageMeter percent={percent ?? 0} tone={windowTone(window)} />
      </div>
    </div>
  );
}
