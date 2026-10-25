import type { AccountUsage, UsageWindow } from "./types";

function hasPercent(window: UsageWindow | null | undefined): boolean {
  return window != null && typeof window === "object" && (window.usedPct != null || window.remainingPct != null);
}

/** A window with no percent that still says something: a limit reached or nearly reached (an observed one). */
function isLimitStatusOnly(window: UsageWindow | null | undefined): boolean {
  return (
    window != null &&
    typeof window === "object" &&
    !hasPercent(window) &&
    (window.status === "rejected" || window.status === "allowed_warning")
  );
}

/**
 * The windows the compact card draws: the daemon-flagged `summary` ones, else
 * the first window with a percent (the same rule the default pins use), plus
 * any window with no percent but a `rejected`/`allowed_warning` status, which
 * the card writes out as a word instead of a meter.
 */
export function summaryWindows(usage: AccountUsage): UsageWindow[] {
  const windows = usage.report?.status === "available" && Array.isArray(usage.report.windows) ? usage.report.windows : [];
  const withPercent = windows.filter(hasPercent);
  const marked = withPercent.filter((window) => window.summary);
  const shown = new Set<UsageWindow>(marked.length > 0 ? marked : withPercent.slice(0, 1));
  return windows.filter((window) => shown.has(window) || isLimitStatusOnly(window));
}
