import { formatPct } from "./format";
import { displayPercent } from "./model";
import { effectiveUsagePins, type UsagePreferences } from "./preferences";
import { windowTone } from "./tone";
import type { AccountUsage, UsageTone, UsageWindow } from "./types";

/** One pinned (or default summary) window of one account, as the sidebar Usage item shows it. */
export interface PinnedUsageWindow {
  key: string;
  /** Accessible description: the account, window, percent and what the percent means. */
  label: string;
  /** The window's short name ("5h"), empty for none, or its label when the daemon sends none. */
  shortLabel: string;
  /** The share shown, under the user's used/remaining preference. */
  percent: number;
  percentText: string;
  tone: UsageTone;
}

/** One account's summary windows, under its provider's logo. */
export interface PinnedUsageSource {
  key: string;
  provider: string;
  windows: PinnedUsageWindow[];
}

/** How much of each window the one-line summary has room for. */
export type PinnedUsageLayout = "meters" | "labels" | "percents";

/** A meter narrower than this is a sliver that reads as noise rather than a share. */
export const MIN_METER_WIDTH = 16;
/** The space between a meter and its text. */
export const METER_GAP = 4;

/**
 * The richest layout that fits on one line: every window's meter, else its short label, else its
 * percent alone. Meters are all or none; a line never mixes them. Until the widths are known it
 * shows labels, which never flashes meters that turn out not to fit.
 */
export function choosePinnedUsageLayout(input: {
  available: number | null;
  labelsWidth: number | null;
  percentsWidth: number | null;
  windowCount: number;
}): PinnedUsageLayout {
  const { available, labelsWidth, percentsWidth, windowCount } = input;
  if (available === null || labelsWidth === null || percentsWidth === null) return "labels";
  if (labelsWidth + windowCount * (MIN_METER_WIDTH + METER_GAP) <= available) return "meters";
  if (labelsWidth <= available) return "labels";
  return percentsWidth < labelsWidth ? "percents" : "labels";
}

/** Reports with every account of a provider together, providers in the order they first appear. */
function groupByProvider(reports: readonly AccountUsage[]): AccountUsage[] {
  const providers = [...new Set(reports.map((usage) => usage.provider))];
  return providers.flatMap((provider) => reports.filter((usage) => usage.provider === provider));
}

function shortLabelOf(window: UsageWindow): string {
  return window.shortLabel ?? window.label ?? window.id;
}

/**
 * The sidebar Usage item's summary: one group per account, grouped by provider, each with its
 * windows in its report's order. Accounts with nothing to show (unavailable, error, no window
 * with a percent that is pinned) are left out. Every field may be missing or reshaped (AC39).
 */
export function resolvePinnedUsage(
  reports: readonly AccountUsage[],
  preferences: UsagePreferences,
): PinnedUsageSource[] {
  const pins = effectiveUsagePins(preferences, reports);
  const meaning = preferences.displayAs === "remaining" ? "left" : "used";
  return groupByProvider(reports).flatMap((usage) => {
    const report = usage.report;
    if (report?.status !== "available" || !Array.isArray(report.windows)) return [];
    const windows: PinnedUsageWindow[] = [];
    for (const window of report.windows) {
      if (window == null || typeof window !== "object") continue;
      const percent = displayPercent(window, preferences.displayAs);
      if (percent === null) continue;
      if (!pins.some((pin) => pin.provider === usage.provider && pin.windowId === window.id)) continue;
      const percentText = formatPct(percent);
      windows.push({
        key: `${usage.key}/${window.id}`,
        label: `${usage.displayName || usage.provider} ${window.label || window.id} ${percentText} ${meaning}`,
        shortLabel: shortLabelOf(window),
        percent,
        percentText,
        tone: windowTone(window),
      });
    }
    if (windows.length === 0) return [];
    return [{ key: usage.key, provider: usage.provider, windows }];
  });
}
