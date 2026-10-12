import { describe, expect, it } from "vitest";

import { DEFAULT_USAGE_PREFERENCES, type UsagePreferences } from "./preferences";
import { choosePinnedUsageLayout, resolvePinnedUsage, type PinnedUsageWindow } from "./pinned";
import type { AccountUsage, UsageWindow } from "./types";

function account(input: {
  id: number;
  provider: string;
  displayName: string;
  windows: UsageWindow[];
}): AccountUsage {
  return {
    accountId: input.id,
    key: `${input.provider}-${input.id}`,
    provider: input.provider,
    displayName: input.displayName,
    credentialType: "oauth",
    stale: false,
    nextRefreshAt: null,
    refreshBlocked: null,
    inFlight: false,
    lastError: null,
    report: { status: "available", windows: input.windows, fetchedAt: "2026-10-12T00:00:00Z" },
  };
}

const claude = account({
  id: 1,
  provider: "anthropic",
  displayName: "Claude",
  windows: [
    { id: "five_hour", label: "Session", shortLabel: "5h", usedPct: 31, summary: true },
    { id: "weekly", label: "Weekly", shortLabel: "wk", usedPct: 80, summary: true },
    { id: "weekly_opus", label: "Weekly · Opus", usedPct: 8 },
  ],
});
const codex = account({
  id: 2,
  provider: "openai",
  displayName: "Codex",
  windows: [{ id: "weekly", label: "Weekly", remainingPct: 88 }],
});

function prefs(over: Partial<UsagePreferences> = {}): UsagePreferences {
  return { ...DEFAULT_USAGE_PREFERENCES, ...over };
}

function windows(reports: AccountUsage[], preferences: UsagePreferences): PinnedUsageWindow[] {
  return resolvePinnedUsage(reports, preferences).flatMap((source) => source.windows);
}

describe("choosePinnedUsageLayout", () => {
  const widths = { labelsWidth: 200, percentsWidth: 120, windowCount: 2 };

  it("shows meters only when every window's meter fits beside the labels", () => {
    expect(choosePinnedUsageLayout({ ...widths, available: 240 })).toBe("meters");
    expect(choosePinnedUsageLayout({ ...widths, available: 239 })).toBe("labels");
  });

  it("drops the labels before it lets the line overflow", () => {
    expect(choosePinnedUsageLayout({ ...widths, available: 199 })).toBe("percents");
  });

  it("keeps labels when the percents alone are no narrower", () => {
    expect(choosePinnedUsageLayout({ available: 50, labelsWidth: 80, percentsWidth: 80, windowCount: 1 })).toBe("labels");
  });

  it("shows labels until the widths are measured", () => {
    expect(choosePinnedUsageLayout({ ...widths, available: null })).toBe("labels");
    expect(choosePinnedUsageLayout({ ...widths, available: 999, labelsWidth: null })).toBe("labels");
    expect(choosePinnedUsageLayout({ ...widths, available: 999, percentsWidth: null })).toBe("labels");
  });
});

describe("resolvePinnedUsage", () => {
  it("defaults to each account's summary windows, else its first window with a percent", () => {
    expect(windows([claude, codex], prefs()).map((w) => [w.key, w.percentText, w.shortLabel])).toEqual([
      ["anthropic-1/five_hour", "31%", "5h"],
      ["anthropic-1/weekly", "80%", "wk"],
      ["openai-2/weekly", "12%", "Weekly"],
    ]);
  });

  it("names the account, window and what the percent means", () => {
    const [first] = windows([claude], prefs());
    expect(first.label).toBe("Claude Session 31% used");
    expect(windows([codex], prefs({ displayAs: "remaining" }))[0].label).toBe("Codex Weekly 88% left");
  });

  it("pins replace the defaults and an explicit empty selection stays empty", () => {
    const pinned = prefs({ pins: [{ provider: "anthropic", windowId: "weekly_opus" }] });
    expect(windows([claude, codex], pinned).map((w) => w.key)).toEqual(["anthropic-1/weekly_opus"]);
    expect(windows([claude, codex], prefs({ pins: [] }))).toEqual([]);
  });

  it("a pin matches the window on every account of the provider, grouped by provider in report order", () => {
    const work = account({
      id: 3,
      provider: "anthropic",
      displayName: "Work Max",
      windows: [{ id: "weekly", label: "Weekly", shortLabel: "wk", usedPct: 90 }],
    });
    const sources = resolvePinnedUsage(
      [claude, codex, work],
      prefs({
        pins: [
          { provider: "openai", windowId: "weekly" },
          { provider: "anthropic", windowId: "weekly" },
          { provider: "anthropic", windowId: "five_hour" },
        ],
      }),
    );
    expect(sources.map((s) => [s.key, s.windows.map((w) => w.key)])).toEqual([
      ["anthropic-1", ["anthropic-1/five_hour", "anthropic-1/weekly"]],
      ["anthropic-3", ["anthropic-3/weekly"]],
      ["openai-2", ["openai-2/weekly"]],
    ]);
    expect(sources[1].windows[0]).toMatchObject({ label: "Work Max Weekly 90% used", tone: "warning" });
  });

  it("keeps an empty short label empty, for a percent that needs no name", () => {
    const quiet = account({
      id: 4,
      provider: "anthropic",
      displayName: "Claude",
      windows: [{ id: "rolling", label: "Rolling", shortLabel: "", usedPct: 21 }],
    });
    expect(windows([quiet], prefs()).map((w) => w.shortLabel)).toEqual([""]);
  });

  it("leaves out unavailable, error and missing reports", () => {
    const expired: AccountUsage = {
      ...claude,
      report: { status: "unavailable", problem: { kind: "expired" }, fetchedAt: "2026-10-12T00:00:00Z" },
    };
    const failed: AccountUsage = { ...codex, report: { status: "error", error: "timeout", fetchedAt: "2026-10-12T00:00:00Z" } };
    const none: AccountUsage = { ...codex, accountId: 9, key: "openai-9", report: null };
    expect(resolvePinnedUsage([expired, failed, none], prefs())).toEqual([]);
  });

  it("leaves out a pinned window that no report has or that reports no percent", () => {
    const noPercent = account({
      id: 5,
      provider: "openai",
      displayName: "Codex",
      windows: [{ id: "monthly", label: "Monthly" }],
    });
    expect(
      windows(
        [claude, noPercent],
        prefs({
          pins: [
            { provider: "anthropic", windowId: "monthly" },
            { provider: "openai", windowId: "monthly" },
          ],
        }),
      ),
    ).toEqual([]);
  });

  it("tolerates reshaped windows (null entries, non-array) without throwing", () => {
    const reshaped = {
      ...claude,
      report: { status: "available", windows: [null, 3, { id: "weekly", label: "Weekly", usedPct: 40 }], fetchedAt: "" },
    } as unknown as AccountUsage;
    const noList = { ...codex, report: { status: "available", windows: "nope", fetchedAt: "" } } as unknown as AccountUsage;
    expect(windows([reshaped, noList], prefs({ pins: [{ provider: "anthropic", windowId: "weekly" }] })).map((w) => w.key)).toEqual([
      "anthropic-1/weekly",
    ]);
  });

  it("a tone from the daemon wins; none derives from the used share", () => {
    const toned = account({
      id: 6,
      provider: "anthropic",
      displayName: "Claude",
      windows: [{ id: "a", label: "A", usedPct: 10, tone: "danger", summary: true }],
    });
    expect(windows([toned], prefs())[0].tone).toBe("danger");
    expect(windows([claude], prefs())[1].tone).toBe("warning");
  });
});
