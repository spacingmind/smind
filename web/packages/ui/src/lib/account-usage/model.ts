import { formatRelativeTime } from "@/lib/relative-time";
import { usageCopy } from "./copy";
import type { UsageDisplayAs } from "./format";
import type {
  AccountUsage,
  UsageCooldown,
  UsageCooldownReason,
  UsageCooldownScope,
  UsageCooldownWindow,
  UsageView,
  UsageWindow,
} from "./types";

export function usedPercent(window: UsageWindow): number | null {
  if (window.usedPct != null) return window.usedPct;
  if (window.remainingPct != null) return 100 - window.remainingPct;
  return null;
}

/** The percent a window shows under the user's used/remaining preference. */
export function displayPercent(window: UsageWindow, displayAs: UsageDisplayAs): number | null {
  if (displayAs === "used") return usedPercent(window);
  if (window.remainingPct != null) return window.remainingPct;
  const used = usedPercent(window);
  return used == null ? null : 100 - used;
}

/**
 * A window row's accessible label: what pinning it pins, then what the row shows. The row is a
 * checkbox, so its checked state says whether it is pinned: "Pin Claude Session, 31% · resets 2h",
 * checked.
 */
export function usageWindowRowLabel(input: {
  pinLabel: string;
  value: string;
  trailing: string | null | undefined;
}): string {
  const summary = input.trailing ? `${input.value} · ${input.trailing}` : input.value;
  return `${input.pinLabel}, ${summary}`;
}

/** When a report was fetched: "Updated 3m ago", "Updated just now". */
export function formatUsageFreshness(fetchedAt: string | undefined, now: number = Date.now()): string {
  if (!fetchedAt || Number.isNaN(new Date(fetchedAt).getTime())) return `${usageCopy.updated} —`;
  return `${usageCopy.updated} ${formatRelativeTime(fetchedAt, now)}`;
}

/** A user-requested refresh of one report. The previous report stays on screen throughout. */
export type UsageRefresh = "idle" | "pending" | "failed";

export function resolveUsageRefresh(mutation: { isPending: boolean; error: unknown }): UsageRefresh {
  if (mutation.isPending) return "pending";
  if (mutation.error) return "failed";
  return "idle";
}

/**
 * The account list with one account swapped for its refreshed copy, in place.
 * `null` means the daemon no longer knows the account, so it leaves the list.
 */
export function replaceReport(
  reports: readonly AccountUsage[],
  accountId: number,
  refreshed: AccountUsage | null,
): AccountUsage[] {
  if (!refreshed) return reports.filter((usage) => usage.accountId !== accountId);
  return reports.map((usage) => (usage.accountId === accountId ? refreshed : usage));
}

/** When a copy's report was fetched; a missing or unreadable one is older than any fetch. */
function fetchedTime(usage: AccountUsage): number {
  const time = Date.parse(usage.report?.fetchedAt ?? "");
  return Number.isNaN(time) ? Number.NEGATIVE_INFINITY : time;
}

/**
 * The later-fetched of two copies of one account. A list request's copies were fetched when it
 * started, so a Refresh that lands while it streams is newer than what the request still delivers;
 * a copy with no report never replaces one that has a report. A tie takes the arriving copy, which
 * carries the newer in-flight state.
 */
function laterFetched(shown: AccountUsage, arriving: AccountUsage): AccountUsage {
  return fetchedTime(shown) > fetchedTime(arriving) ? shown : arriving;
}

/**
 * An account list with one streamed account in place of its previous copy, or appended if new. A
 * copy fetched after the streamed one stays.
 */
export function upsertReport(
  reports: readonly AccountUsage[] | undefined,
  report: AccountUsage,
): AccountUsage[] {
  if (!reports?.some((usage) => usage.accountId === report.accountId)) {
    return [...(reports ?? []), report];
  }
  return reports.map((usage) =>
    usage.accountId === report.accountId ? laterFetched(usage, report) : usage,
  );
}

/**
 * The list a finished request leaves: its accounts, dropping any the daemon no longer has, except
 * that a copy on screen fetched after the request's copy stays.
 */
export function settleReports(
  shown: readonly AccountUsage[] | undefined,
  finished: readonly AccountUsage[],
): AccountUsage[] {
  return finished.map((report) => {
    const copy = shown?.find((usage) => usage.accountId === report.accountId);
    return copy ? laterFetched(copy, report) : report;
  });
}

export function resolveUsageView(input: {
  /** False when the daemon lacks `account.usage`. */
  supportsUsage: boolean;
  enabled: boolean;
  phase: "idle" | "loading" | "ready" | "error";
  reports: readonly AccountUsage[];
  error: string | null;
}): UsageView {
  if (!input.supportsUsage) return { kind: "unavailable", message: usageCopy.daemonTooOld };
  if (!input.enabled) return { kind: "disabled", message: usageCopy.disabled };
  if (input.phase === "ready") return { kind: "ready", reports: [...input.reports] };
  if (input.phase === "error") return { kind: "error", message: input.error || usageCopy.refreshFailed };
  return { kind: "loading" };
}

/** The account the modal filters to: the picked one while it still exists, else null (All). */
export function resolveAccountFilter(
  picked: string | null,
  reports: readonly AccountUsage[],
): string | null {
  if (picked === null) return null;
  return reports.find((usage) => usage.key === picked)?.key ?? null;
}

/** A passive cooldown that is still in force: the wire's, with `until` parsed and every field validated. */
export interface ActiveCooldown {
  untilMs: number;
  scope: UsageCooldownScope;
  model?: string;
  reason: UsageCooldownReason;
  window?: UsageCooldownWindow;
}

const COOLDOWN_WINDOWS: readonly string[] = ["five_hour", "weekly", "weekly_model", "unified"];

/**
 * The account's passive cooldown while `until` is still ahead of `now`; null when it is absent,
 * malformed (no readable `until`) or over. A reshaped optional field degrades (an unknown scope
 * reads as the whole account, an unknown reason as a plain rejection, an unknown window as none)
 * rather than hiding a real rate limit (AC39). Never throws.
 */
export function activeCooldown(usage: AccountUsage | null | undefined, now: number = Date.now()): ActiveCooldown | null {
  const raw: unknown = usage?.cooldown;
  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) return null;
  const cooldown = raw as Partial<Record<keyof UsageCooldown, unknown>>;
  if (typeof cooldown.until !== "string") return null;
  const untilMs = Date.parse(cooldown.until);
  if (!Number.isFinite(untilMs) || untilMs <= now) return null;
  const model = typeof cooldown.model === "string" && cooldown.model.trim() ? cooldown.model.trim() : undefined;
  const window = COOLDOWN_WINDOWS.includes(cooldown.window as string)
    ? (cooldown.window as UsageCooldownWindow)
    : undefined;
  return {
    untilMs,
    scope: cooldown.scope === "model" ? "model" : "account",
    model,
    reason: cooldown.reason === "fallback" ? "fallback" : "rejected",
    window,
  };
}
