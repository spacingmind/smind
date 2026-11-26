import { describe, expect, it } from "vitest";

import type { AccountUsage, UsageWindow } from "@/lib/account-usage/types";
import {
  pickQuotaNotice,
  quotaLevel,
  quotaNoticeKey,
  quotaNoticeText,
} from "@/lib/ambient-quota";

/** Local date parts, so reset-time formatting is timezone independent. */
const NOW = new Date(2026, 10, 26, 18, 30, 0).getTime();
const HOUR = 3_600_000;

function iso(ms: number): string {
  return new Date(ms).toISOString();
}

function win(overrides: Partial<UsageWindow> = {}): UsageWindow {
  return {
    id: "five_hour",
    label: "5-hour",
    usedPct: 92,
    resetsAt: iso(NOW + 2 * HOUR),
    ...overrides,
  };
}

function usage(overrides: Partial<AccountUsage> & { windows?: UsageWindow[] } = {}): AccountUsage {
  const { windows = [win()], ...rest } = overrides;
  return {
    accountId: 1,
    key: "anthropic-1",
    provider: "anthropic",
    displayName: "main",
    credentialType: "oauth",
    stale: false,
    nextRefreshAt: null,
    refreshBlocked: null,
    inFlight: false,
    lastError: null,
    report: {
      status: "available",
      fetchedAt: iso(NOW - 60_000),
      windows,
    },
    ...rest,
  };
}

function pick(
  usages: readonly AccountUsage[],
  dismissed: ReadonlySet<string> = new Set(),
  accountProvider: string | undefined = "anthropic",
) {
  return pickQuotaNotice({ usages, accountProvider, now: NOW, dismissed, locale: "en-US" });
}

/** The time the notice shows, built with the same formatter the code uses. */
function localTime(resetsAtMs: number, withWeekday = false): string {
  return new Intl.DateTimeFormat(
    "en-US",
    withWeekday
      ? { weekday: "short", hour: "2-digit", minute: "2-digit", hourCycle: "h23" }
      : { hour: "2-digit", minute: "2-digit", hourCycle: "h23" },
  ).format(new Date(resetsAtMs));
}

describe("the ambient quota notice (ADR-0059 F)", () => {
  it("ambient_quota_fires_at_90_not_89", () => {
    expect(pick([usage({ windows: [win({ usedPct: 89 })] })])).toBeNull();
    expect(pick([usage({ windows: [win({ usedPct: 89.9 })] })])).toBeNull();
    const at90 = pick([usage({ windows: [win({ usedPct: 90 })] })]);
    expect(at90?.level).toBe("warn");
    expect(pick([usage({ windows: [win({ usedPct: 92 })] })])?.level).toBe("warn");
    expect(quotaLevel(100)).toBe("full");
    expect(quotaLevel(89)).toBeNull();
    expect(quotaLevel(90)).toBe("warn");
  });

  it("ambient_quota_100_says_used_up", () => {
    const full = pick([usage({ windows: [win({ usedPct: 100 })] })]);
    expect(full?.level).toBe("full");
    expect(full?.text).toBe(`5-hour window used up, resets ${localTime(NOW + 2 * HOUR)}`);
    const floored = pick([usage({ windows: [win({ usedPct: 99.7 })] })]);
    expect(floored?.text).toContain("99% used");
  });

  it("ambient_quota_text_names_window_and_local_reset_time", () => {
    const today = new Date(2026, 10, 26, 20, 52, 0).getTime();
    const text = quotaNoticeText({
      windowName: "5-hour",
      level: "warn",
      usedPct: 92,
      resetsAtMs: today,
      now: NOW,
      locale: "en-US",
    });
    expect(text).toBe("5-hour window 92% used, resets 20:52");
    expect(
      quotaNoticeText({ windowName: "Weekly", level: "warn", usedPct: 92, resetsAtMs: today, now: NOW, locale: "en-US" }),
    ).toBe("Weekly window 92% used, resets 20:52");
    // a label that already says "window" is not doubled
    expect(
      quotaNoticeText({ windowName: "5-hour window", level: "warn", usedPct: 92, resetsAtMs: today, now: NOW, locale: "en-US" }),
    ).toBe("5-hour window 92% used, resets 20:52");
    // a reset on another local day adds the weekday
    const otherDay = new Date(2026, 10, 27, 20, 52, 0).getTime();
    expect(
      quotaNoticeText({ windowName: "5-hour", level: "warn", usedPct: 92, resetsAtMs: otherDay, now: NOW, locale: "en-US" }),
    ).toBe(`5-hour window 92% used, resets ${localTime(otherDay, true)}`);
  });

  it("ambient_quota_none_for_stale_or_missing_reading", () => {
    const reset = iso(NOW + 2 * HOUR);
    expect(pick([usage({ stale: true })])).toBeNull();
    expect(pick([usage({ report: null })])).toBeNull();
    expect(pick([usage({ report: { status: "unavailable", fetchedAt: iso(NOW - 60_000), windows: [win()] } })])).toBeNull();
    expect(pick([usage({ report: { status: "available", fetchedAt: iso(NOW - 31 * 60_000), windows: [win()] } })])).toBeNull();
    expect(pick([usage({ report: { status: "available", fetchedAt: "not-a-date", windows: [win()] } })])).toBeNull();
    expect(pick([usage({ report: { status: "available", fetchedAt: "", windows: [win()] } })])).toBeNull();
    expect(pick([usage({ windows: [win({ resetsAt: iso(NOW - HOUR) })] })])).toBeNull();
    expect(pick([usage({ windows: [win({ resetsAt: undefined })] })])).toBeNull();
    expect(pick([usage({ windows: [{ id: "five_hour", label: "5-hour", usedPct: 92, resetsAt: reset }] })])?.level).toBe("warn");
  });

  it("ambient_quota_none_without_windows_or_percent", () => {
    expect(pick([usage({ report: { status: "available", fetchedAt: iso(NOW - 60_000), windows: [] } })])).toBeNull();
    expect(pick([usage({ report: { status: "available", fetchedAt: iso(NOW - 60_000) } })])).toBeNull();
    expect(
      pick([usage({ windows: [{ id: "five_hour", label: "5-hour", status: "allowed_warning" }] })]),
    ).toBeNull();
    expect(
      pickQuotaNotice({ usages: [usage()], accountProvider: undefined, now: NOW, dismissed: new Set() }),
    ).toBeNull();
  });

  it("ambient_quota_only_the_providers_accounts", () => {
    const openai = usage({ provider: "openai", key: "openai-1", windows: [win({ usedPct: 95 })] });
    expect(pick([openai])).toBeNull();
    expect(pick([openai, usage({ windows: [win({ usedPct: 50 })] })])).toBeNull();
    // the other provider's account neither triggers nor suppresses the notice
    expect(pick([openai, usage({ windows: [win({ usedPct: 95 })] })])?.level).toBe("warn");
  });

  it("ambient_quota_pool_waits_for_every_account", () => {
    const second = (usedPct: number, overrides: Partial<AccountUsage> = {}) =>
      usage({ accountId: 2, key: "anthropic-2", displayName: "alt", windows: [win({ usedPct })], ...overrides });
    // a pool with headroom stays quiet
    expect(pick([usage({ windows: [win({ usedPct: 95 })] }), second(50)])).toBeNull();
    // every account at or over the threshold: the most used one is named
    const warn = pick([usage({ windows: [win({ usedPct: 95 })] }), second(91)]);
    expect(warn?.level).toBe("warn");
    expect(warn?.text).toBe(`main: 5-hour window 95% used, resets ${localTime(NOW + 2 * HOUR)}`);
    // the lower level wins: one full and one warn account is a warn pool
    expect(pick([usage({ windows: [win({ usedPct: 100 })] }), second(91)])?.level).toBe("warn");
    expect(pick([usage({ windows: [win({ usedPct: 100 })] }), second(100)])?.level).toBe("full");
    // a single account warns on its own and is not named
    const single = pick([usage({ windows: [win({ usedPct: 95 })] })]);
    expect(single?.text).toBe(`5-hour window 95% used, resets ${localTime(NOW + 2 * HOUR)}`);
    // an account with no fresh reading is unknown headroom, not headroom: the fresh one still warns
    expect(pick([usage({ windows: [win({ usedPct: 95 })] }), second(50, { stale: true })])?.level).toBe("warn");
  });

  it("ambient_quota_key_buckets_resets_at", () => {
    const t = NOW + 2 * HOUR;
    expect(quotaNoticeKey(1, "five_hour", t, "warn")).toBe(quotaNoticeKey(1, "five_hour", t + 20_000, "warn"));
    expect(quotaNoticeKey(1, "five_hour", t, "warn")).not.toBe(quotaNoticeKey(1, "five_hour", t + HOUR, "warn"));
    const notice = pick([usage({ windows: [win({ resetsAt: iso(t) })] })]);
    expect(notice?.key).toBe(quotaNoticeKey(1, "five_hour", t, "warn"));
    expect(notice?.key).toContain("1");
    expect(notice?.key).toContain("five_hour");
    expect(notice?.key).toContain("warn");
  });

  it("ambient_quota_orders_windows", () => {
    const twoWindows = (fiveHour: Partial<UsageWindow>, weekly: Partial<UsageWindow>) =>
      usage({ windows: [win({ ...fiveHour }), win({ id: "weekly", label: "Weekly", resetsAt: iso(NOW + 3 * HOUR), ...weekly })] });

    // higher percent first
    expect(pick([twoWindows({ usedPct: 92 }, { usedPct: 95 })])?.windowId).toBe("weekly");
    // full before warn
    expect(pick([twoWindows({ usedPct: 100 }, { usedPct: 99 })])?.windowId).toBe("five_hour");
    // equal percent: the earlier reset first
    const both = twoWindows({ usedPct: 92, resetsAt: iso(NOW + 2 * HOUR) }, { usedPct: 92, resetsAt: iso(NOW + HOUR) });
    const first = pick([both]);
    expect(first?.windowId).toBe("weekly");
    // after a dismissal the next one shows; both dismissed: nothing
    const second = pick([both], new Set([first!.key]));
    expect(second?.windowId).toBe("five_hour");
    expect(pick([both], new Set([first!.key, second!.key]))).toBeNull();
  });
});
