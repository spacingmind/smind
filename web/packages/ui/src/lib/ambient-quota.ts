import type { AccountUsage, UsageWindow } from "@/lib/account-usage/types";

/**
 * The ambient quota notice (ADR-0059 F, D5): one non-blocking, dismissible
 * warning when a plan window used by the composer's provider reaches 90 %
 * used. Pure logic here; the dismissal store lives in
 * `lib/quota-notice-store.ts`, the hook in `hooks/use-quota-notice.ts`.
 */

export type QuotaLevel = "warn" | "full";

/** The percent used at which a window warns. */
export const QUOTA_WARN_PCT = 90;

/** `resetsAt` is rounded to this bucket when keyed: seconds of jitter is the same window. */
export const RESET_BUCKET_MS = 5 * 60 * 1000;

/** A report older than this is not a basis for a notice: the window may have reset since. */
export const AMBIENT_STALE_MS = 30 * 60 * 1000;

/** The one notice the composer shows, with the text ready to render. */
export interface QuotaNotice {
  key: string;
  accountId: number;
  windowId: string;
  level: QuotaLevel;
  usedPct: number;
  resetsAt: string;
  text: string;
}

/** The dismissal key: `accountId|windowId|resetsAtBucket|level`. Window ids never contain `|`. */
export function quotaNoticeKey(accountId: number, windowId: string, resetsAtMs: number, level: QuotaLevel): string {
  return `${accountId}|${windowId}|${Math.round(resetsAtMs / RESET_BUCKET_MS)}|${level}`;
}

/** The reset a dismissal key's bucket stands for (ms), or null when the key is malformed. */
export function quotaKeyResetMs(key: string): number | null {
  const parts = key.split("|");
  if (parts.length !== 4) return null;
  const bucket = Number(parts[2]);
  if (!Number.isInteger(bucket) || bucket <= 0) return null;
  return bucket * RESET_BUCKET_MS;
}

/** `full` at 100 %, `warn` from `QUOTA_WARN_PCT`, else null (no notice). */
export function quotaLevel(usedPct: number): QuotaLevel | null {
  if (usedPct >= 100) return "full";
  if (usedPct >= QUOTA_WARN_PCT) return "warn";
  return null;
}

/**
 * The notice's text: "Claude · 5-hour window 92% used, resets 20:52", "… used up, resets Fri 20:52"
 * when the reset is on another local day. It always names the provider, and the account
 * too ("Claude (work) · …") when the provider has several accounts (D1 names the most used one).
 */
export function quotaNoticeText(a: {
  providerLabel: string;
  windowName: string;
  level: QuotaLevel;
  usedPct: number;
  resetsAtMs: number;
  now: number;
  accountName?: string;
  locale?: string;
}): string {
  const reset = new Date(a.resetsAtMs);
  const sameDay = reset.toDateString() === new Date(a.now).toDateString();
  const time = new Intl.DateTimeFormat(
    a.locale,
    sameDay
      ? { hour: "2-digit", minute: "2-digit", hourCycle: "h23" }
      : { weekday: "short", hour: "2-digit", minute: "2-digit", hourCycle: "h23" },
  ).format(reset);
  const name = /window/i.test(a.windowName) ? a.windowName : `${a.windowName} window`;
  const body =
    a.level === "full" ? `${name} used up, resets ${time}` : `${name} ${Math.floor(a.usedPct)}% used, resets ${time}`;
  return `${a.accountName ? `${a.providerLabel} (${a.accountName})` : a.providerLabel} · ${body}`;
}

/** A window percent: `usedPct` when present, else the complement of `remainingPct`; null when there is none. */
function usedPercent(window: UsageWindow): number | null {
  if (window.usedPct != null && Number.isFinite(window.usedPct)) return window.usedPct;
  if (window.remainingPct != null && Number.isFinite(window.remainingPct)) return 100 - window.remainingPct;
  return null;
}

/** One account's fresh reading of one window: has a percent and a future reset, whatever the level. */
interface Reading {
  accountId: number;
  accountName: string;
  usedPct: number;
  resetsAtMs: number;
  resetsAt: string;
  level: QuotaLevel | null;
  window: UsageWindow;
}

interface Candidate extends QuotaNotice {
  resetsAtMs: number;
}

/**
 * The whole policy (ADR-0059 Behaviour): among the provider's accounts, a
 * window notices only when every account with a fresh reading of it is at or
 * over the level (a pool with headroom left stays quiet), one notice at a
 * time: `full` before `warn`, higher percent, earlier reset, then window id.
 */
export function pickQuotaNotice(input: {
  usages: readonly AccountUsage[];
  accountProvider: string | undefined;
  providerLabel: string;
  now: number;
  dismissed: ReadonlySet<string>;
  locale?: string;
}): QuotaNotice | null {
  const { usages, accountProvider, providerLabel, now, dismissed, locale } = input;
  if (accountProvider === undefined) return null;
  const accounts = usages.filter((u) => u.provider === accountProvider);

  // Fresh readings grouped by window id: the pool rule is decided per window, not per account.
  const groups = new Map<string, Reading[]>();
  for (const account of accounts) {
    if (account.stale) continue;
    const report = account.report;
    if (!report || report.status !== "available" || !Array.isArray(report.windows)) continue;
    const fetched = Date.parse(report.fetchedAt);
    if (!Number.isFinite(fetched) || now - fetched > AMBIENT_STALE_MS) continue;
    for (const window of report.windows) {
      const usedPct = usedPercent(window);
      const resetsAtMs = Date.parse(window.resetsAt ?? "");
      // No percent or no future reset: the window is not readable at all. Below-threshold readings
      // are kept: they are what tells the pool rule the window still has headroom.
      if (usedPct === null || !Number.isFinite(resetsAtMs) || resetsAtMs <= now) continue;
      const reading: Reading = {
        accountId: account.accountId,
        accountName: account.displayName || account.provider,
        usedPct,
        resetsAtMs,
        resetsAt: window.resetsAt ?? "",
        level: quotaLevel(usedPct),
        window,
      };
      const readings = groups.get(window.id);
      if (readings) readings.push(reading);
      else groups.set(window.id, [reading]);
    }
  }

  const candidates: Candidate[] = [];
  for (const [windowId, readings] of groups) {
    // An account still under the threshold leaves the pool headroom: no notice for this window (D1).
    if (readings.length === 0 || readings.some((reading) => reading.level === null)) continue;
    const level: QuotaLevel = readings.some((reading) => reading.level === "warn") ? "warn" : "full";
    // The shown reading is the most used account (the one the text names); ties go to the lower id.
    let shown = readings[0];
    for (const reading of readings.slice(1)) {
      if (reading.usedPct > shown.usedPct || (reading.usedPct === shown.usedPct && reading.accountId < shown.accountId)) {
        shown = reading;
      }
    }
    const key = quotaNoticeKey(shown.accountId, windowId, shown.resetsAtMs, level);
    if (dismissed.has(key)) continue;
    const windowName = shown.window.label || shown.window.shortLabel || windowId;
    const accountName = accounts.length > 1 ? shown.accountName : undefined;
    candidates.push({
      key,
      accountId: shown.accountId,
      windowId,
      level,
      usedPct: shown.usedPct,
      resetsAtMs: shown.resetsAtMs,
      resetsAt: shown.resetsAt,
      text: quotaNoticeText({ providerLabel, windowName, level, usedPct: shown.usedPct, resetsAtMs: shown.resetsAtMs, now, accountName, locale }),
    });
  }

  if (candidates.length === 0) return null;
  candidates.sort((a, b) => {
    if (a.level !== b.level) return a.level === "full" ? -1 : 1;
    if (a.usedPct !== b.usedPct) return b.usedPct - a.usedPct;
    if (a.resetsAtMs !== b.resetsAtMs) return a.resetsAtMs - b.resetsAtMs;
    return a.windowId < b.windowId ? -1 : a.windowId > b.windowId ? 1 : 0;
  });
  return candidates[0];
}
