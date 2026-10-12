import { readStored, STORAGE_KEYS, writeStored } from "@/lib/storage";
import type { UsageDisplayAs } from "./format";
import type { AccountUsage } from "./types";

/** A window the user pinned to the sidebar Usage item. It matches every account of the provider. */
export interface PinnedWindow {
  provider: string;
  windowId: string;
}

/** Device-local. `pins` is in selection order. */
export interface UsagePreferences {
  displayAs: UsageDisplayAs;
  /** The `AccountUsage.key` the user filtered to; null shows every account. */
  accountKey: string | null;
  /** Whether the sidebar footer shows the usage summary. Off until the user turns it on. */
  inSidebar: boolean;
  /** null follows the daemon's defaults; an array is the user's selection, including none. */
  pins: PinnedWindow[] | null;
}

export const DEFAULT_USAGE_PREFERENCES: UsagePreferences = {
  displayAs: "used",
  accountKey: null,
  inSidebar: false,
  pins: null,
};

function isPinnedWindow(value: unknown): value is PinnedWindow {
  if (typeof value !== "object" || value === null) return false;
  const pin = value as Record<string, unknown>;
  return typeof pin.provider === "string" && typeof pin.windowId === "string";
}

/** Stored preferences read field by field, so one stale field never drops the rest. */
export function parseUsagePreferences(value: unknown): UsagePreferences {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    return DEFAULT_USAGE_PREFERENCES;
  }
  const raw = value as Record<string, unknown>;
  return {
    displayAs: raw.displayAs === "remaining" ? "remaining" : "used",
    accountKey: typeof raw.accountKey === "string" ? raw.accountKey : null,
    inSidebar: raw.inSidebar === true,
    pins: Array.isArray(raw.pins) && raw.pins.every(isPinnedWindow)
      ? raw.pins.map((pin) => ({ provider: pin.provider, windowId: pin.windowId }))
      : null,
  };
}

export function readUsagePreferences(): UsagePreferences {
  const stored = readStored<unknown>(STORAGE_KEYS.usage, null, (_value): _value is unknown => true);
  return parseUsagePreferences(stored);
}

export function writeUsagePreferences(preferences: UsagePreferences): void {
  writeStored(STORAGE_KEYS.usage, preferences);
}

function samePin(a: PinnedWindow, b: PinnedWindow): boolean {
  return a.provider === b.provider && a.windowId === b.windowId;
}

/** A window with a percent to show; the same under Used and Remaining, since each derives from the other. */
function hasPercent(window: { usedPct?: number | null; remainingPct?: number | null }): boolean {
  return window.usedPct != null || window.remainingPct != null;
}

/** The only owner of default and customized pin selection, shared by every usage surface. */
export function effectiveUsagePins(
  preferences: UsagePreferences,
  reports: readonly AccountUsage[],
): PinnedWindow[] {
  if (preferences.pins !== null) return preferences.pins;
  const pins: PinnedWindow[] = [];
  for (const usage of reports) {
    const report = usage.report;
    if (report?.status !== "available" || !Array.isArray(report.windows)) continue;
    const withPercent = report.windows.filter((window) => hasPercent(window));
    const marked = withPercent.filter((window) => window.summary);
    for (const window of marked.length > 0 ? marked : withPercent.slice(0, 1)) {
      const pin = { provider: usage.provider, windowId: window.id };
      if (!pins.some((existing) => samePin(existing, pin))) pins.push(pin);
    }
  }
  return pins;
}

export function isUsagePinned(
  preferences: UsagePreferences,
  pin: PinnedWindow,
  reports: readonly AccountUsage[],
): boolean {
  return effectiveUsagePins(preferences, reports).some((pinned) => samePin(pinned, pin));
}

/** The first edit snapshots the displayed defaults before toggling one window. */
export function toggleUsagePin(
  preferences: UsagePreferences,
  defaultsForFirstToggle: readonly PinnedWindow[],
  pin: PinnedWindow,
): UsagePreferences {
  const selected = preferences.pins ?? defaultsForFirstToggle;
  const pins = selected.some((existing) => samePin(existing, pin))
    ? selected.filter((existing) => !samePin(existing, pin))
    : [...selected, { provider: pin.provider, windowId: pin.windowId }];
  return { ...preferences, pins };
}

export function setUsageDisplayAs(
  preferences: UsagePreferences,
  displayAs: UsageDisplayAs,
): UsagePreferences {
  return { ...preferences, displayAs };
}

export function setUsageAccount(
  preferences: UsagePreferences,
  accountKey: string | null,
): UsagePreferences {
  return { ...preferences, accountKey };
}

export function setUsageInSidebar(
  preferences: UsagePreferences,
  inSidebar: boolean,
): UsagePreferences {
  return { ...preferences, inSidebar };
}
