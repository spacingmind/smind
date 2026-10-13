
import { UsageMeter } from "@/components/account-usage/usage-meter";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { usageCopy } from "@/lib/account-usage/copy";
import { formatDisplayPct, formatResetLabel, type UsageDisplayAs } from "@/lib/account-usage/format";
import { displayPercent, usageWindowRowLabel } from "@/lib/account-usage/model";
import { windowTone } from "@/lib/account-usage/tone";
import type { UsageTone, UsageWindow } from "@/lib/account-usage/types";
import { cn } from "@/lib/utils";
import { Icons } from "@/lib/icons";

const STATUS_TEXT: Record<UsageTone, string> = {
  ok: "text-success",
  warning: "text-warning",
  danger: "text-destructive",
  default: "text-foreground",
};

/** What a pinnable row needs: whether it is pinned, how to toggle it, and what it pins ("Pin Work Max Session"). */
export interface UsageWindowPin {
  pinned: boolean;
  onToggle: () => void;
  pinLabel: string;
}

/**
 * One window as a row: label left, value and reset label right, the meter
 * below (Paseo's UsageWindowBar). Every field may be missing (AC39): a
 * window without a percent reads "—" over an empty meter, unless it carries a
 * limiting `status` (an observed window may have nothing else), which is then
 * written out in its tone in place of the meter. An observed window is marked
 * "from recent traffic". With `pin` (only while "Show summary in sidebar" is
 * on, AC30) the whole row is a checkbox that pins the window to the sidebar
 * footer: a pin glyph with a tooltip, the pinned surface behind a pinned row,
 * and the same surface at half strength under a hovered unpinned row so a
 * hover never reads as the selection. Pinnable and plain rows share padding,
 * so toggling the switch does not move anything.
 */
export function UsageWindowBar({
  window,
  displayAs,
  pin,
}: {
  window: UsageWindow;
  displayAs: UsageDisplayAs;
  pin?: UsageWindowPin | null;
}) {
  const percent = displayPercent(window, displayAs);
  const reset = formatResetLabel(window.resetsAt);
  const label = window.label || window.id;
  const tone = windowTone(window);
  const status = percent == null ? usageCopy.windowStatus(window.status) : null;
  const value = status ?? formatDisplayPct(window, displayAs);
  const content = (
    <>
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
            value
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
    </>
  );

  if (!pin) {
    return (
      <div data-testid={`usage-window-${window.id}`} className="-mx-2 flex flex-col gap-1 px-2 py-1.5">
        {content}
      </div>
    );
  }
  return (
    <button
      type="button"
      role="checkbox"
      aria-checked={pin.pinned}
      aria-label={usageWindowRowLabel({ pinLabel: pin.pinLabel, value, trailing: reset })}
      data-testid={`usage-window-${window.id}`}
      data-pinned={pin.pinned}
      onClick={pin.onToggle}
      className={cn(
        "-mx-2 flex w-[calc(100%+1rem)] cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-left outline-none focus-visible:ring-2 focus-visible:ring-ring",
        pin.pinned ? "bg-muted" : "hover:bg-muted/50",
      )}
    >
      <div className="flex min-w-0 flex-1 flex-col gap-1">{content}</div>
      <PinGlyph pinned={pin.pinned} />
    </button>
  );
}

function PinGlyph({ pinned }: { pinned: boolean }) {
  return (
    <TooltipProvider>
      <Tooltip delayDuration={300}>
        <TooltipTrigger asChild>
          <span
            data-testid={pinned ? "usage-pin-glyph-pinned" : "usage-pin-glyph-unpinned"}
            className="flex w-3 shrink-0 justify-center text-muted-foreground"
          >
            <Icons.pin aria-hidden className={cn("size-3", pinned && "fill-current")} />
          </span>
        </TooltipTrigger>
        <TooltipContent side="top">{pinned ? usageCopy.unpin : usageCopy.pin}</TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}
