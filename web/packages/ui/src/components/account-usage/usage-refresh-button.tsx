import { useEffect, useState } from "react";
import { Icons } from "@/lib/icons";

import { Button } from "@/components/ui/button";
import { InlineSpinner } from "@/components/ui/inline-spinner";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { REFRESH_COOLDOWN_MS } from "@/hooks/use-usage-reports";
import { usageCopy } from "@/lib/account-usage/copy";
import { formatUsageFreshness, type UsageRefresh } from "@/lib/account-usage/model";
import type { AccountUsage } from "@/lib/account-usage/types";

/** Refreshes every report; shows a spinner and is disabled while a refresh is in flight. */
export function UsageRefreshAllButton({ busy, onRefresh }: { busy: boolean; onRefresh: () => void }) {
  return (
    <Button
      type="button"
      variant="ghost"
      size="icon-sm"
      aria-label="Refresh all"
      disabled={busy}
      onClick={onRefresh}
      data-testid="usage-refresh-all"
      className="text-foreground-muted"
    >
      {busy ? <InlineSpinner className="text-inherit" /> : <Icons.refresh aria-hidden className="size-3.5" />}
    </Button>
  );
}

/**
 * Refreshes one account (AC36). Disabled while its refresh is pending, while the daemon says a
 * refresh would be refused (backoff, floor, budget, rejected login), and for
 * {@link REFRESH_COOLDOWN_MS} after a press. The tooltip says when the report was fetched, or why
 * the button is blocked.
 */
export function UsageRefreshButton({
  usage,
  state,
  pressedAt,
  onRefresh,
  now,
}: {
  usage: AccountUsage;
  state: UsageRefresh;
  pressedAt?: number;
  onRefresh: () => void;
  now?: number;
}) {
  const [tick, setTick] = useState(0);
  const clock = now ?? Date.now();
  const cooldownEndsAt = pressedAt != null ? pressedAt + REFRESH_COOLDOWN_MS : null;
  const coolingDown = cooldownEndsAt != null && cooldownEndsAt > clock;

  // The daemon's own refusal (`refreshBlocked`) lifts at `nextRefreshAt`, which it rounds up to the
  // second so re-enabling exactly then never lands inside its floor. A rejected login stays blocked
  // until the account is reconnected, whatever the clock says.
  const blocked = usageCopy.refreshState(usage, clock);
  const blockedUntil = usage.nextRefreshAt ? Date.parse(usage.nextRefreshAt) : Number.NaN;
  const blockLifted =
    usage.refreshBlocked !== "rejected" && Number.isFinite(blockedUntil) && blockedUntil <= clock;
  const blockedNow = blocked.disabled && !blockLifted;

  // Re-enable by itself at the next boundary (cooldown end or block lift).
  useEffect(() => {
    const boundaries = [cooldownEndsAt, blocked.disabled && usage.refreshBlocked !== "rejected" ? blockedUntil : null];
    const upcoming = boundaries.filter((t): t is number => t != null && Number.isFinite(t) && t > Date.now());
    if (upcoming.length === 0) return;
    const timer = setTimeout(() => setTick((n) => n + 1), Math.min(...upcoming) - Date.now());
    return () => clearTimeout(timer);
    // `tick` re-arms the timer for the next boundary after each wake-up.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [cooldownEndsAt, blockedUntil, blocked.disabled, usage.refreshBlocked, tick]);

  const pending = state === "pending";
  const disabled = pending || blockedNow || coolingDown;
  const tooltip = blockedNow ? blocked.label : formatUsageFreshness(usage.report?.fetchedAt, clock);

  return (
    <TooltipProvider>
      <Tooltip>
        {/* A disabled button swallows pointer events, so the span carries the tooltip. */}
        <TooltipTrigger asChild>
          <span className="inline-flex" tabIndex={disabled ? 0 : -1} data-testid="usage-refresh-hint">
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label={`Refresh ${usage.displayName}`}
              disabled={disabled}
              onClick={onRefresh}
              data-testid="usage-refresh"
              className="text-foreground-muted"
            >
              {pending ? <InlineSpinner className="text-inherit" /> : <Icons.refresh aria-hidden className="size-3.5" />}
            </Button>
          </span>
        </TooltipTrigger>
        <TooltipContent side="top">{tooltip}</TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}
